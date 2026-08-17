-- +goose NO TRANSACTION
-- +goose Up
-- Add one exact wb-core repo-only target whose physical terminal actor is the
-- repository-owned GitHub Actions Release Train. Existing rows are copied
-- byte-for-byte; no task, action, review or admission is created.
PRAGMA foreign_keys = OFF;

CREATE TABLE dcp_review_lab_policy_task_v78 (
    task_id           TEXT PRIMARY KEY CHECK (length(task_id) BETWEEN 1 AND 16),
    payload_json      TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    payload_digest    TEXT NOT NULL CHECK (length(payload_digest) = 64),
    target            TEXT NOT NULL,
    profile           TEXT NOT NULL,
    repository        TEXT NOT NULL,
    policy_version    TEXT NOT NULL,
    session_id        TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    card_number       INTEGER NOT NULL CHECK (card_number > 0),
    worktree_path     TEXT NOT NULL CHECK (length(worktree_path) > 0),
    source_branch     TEXT NOT NULL CHECK (length(source_branch) > 0),
    prompt            TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 512),
    state             TEXT NOT NULL CHECK (state IN (
        'reserved', 'worker_queued', 'worker_running', 'ci_waiting',
        'review_queued', 'review_running', 'repair_queued', 'repair_running',
        'admission_waiting', 'release_waiting', 'merged', 'failed', 'incident'
    )),
    revision          INTEGER NOT NULL CHECK (revision >= 1),
    repair_count      INTEGER NOT NULL DEFAULT 0 CHECK (repair_count IN (0, 1)),
    pr_url            TEXT NOT NULL DEFAULT '',
    pr_number         INTEGER NOT NULL DEFAULT 0 CHECK (pr_number >= 0),
    current_head_sha  TEXT NOT NULL DEFAULT '' CHECK (current_head_sha = '' OR length(current_head_sha) = 40),
    previous_head_sha TEXT NOT NULL DEFAULT '' CHECK (previous_head_sha = '' OR length(previous_head_sha) = 40),
    review_run_id     TEXT NOT NULL DEFAULT '',
    admission_id      TEXT NOT NULL DEFAULT '',
    merge_commit_sha  TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    error_code        TEXT NOT NULL DEFAULT '',
    incident_packet   TEXT NOT NULL DEFAULT '' CHECK (incident_packet = '' OR json_valid(incident_packet)),
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (target, card_number),
    CHECK (
      (target = 'dcp-review-lab' AND profile = 'synthetic-pr' AND
       repository = 'orenvlad-ai/dcp-review-lab' AND
       policy_version = 'dcp.review-lab.happy-path/v1' AND card_number > 12) OR
      (target = 'wb-browser-extension' AND profile = 'repo-only' AND
       repository = 'orenvlad-ai/wb-browser-extension' AND
       policy_version = 'dcp.repo-only.happy-path/v1') OR
      (target = 'wb-core' AND profile = 'repo-only' AND
       repository = 'orenvlad-ai/wb-core' AND
       policy_version = 'dcp.wb-core.repo-only.release-train/v1') OR
      (task_id = 'price-arch-v1' AND
       payload_digest = 'efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe' AND
       target = 'wb-price-extension' AND profile = 'repo-only' AND
       repository = 'orenvlad-ai/wb-price-extension' AND
       policy_version = 'dcp.repo-only.happy-path/v1' AND
       session_id = 'wb-price-extension-1' AND card_number = 1 AND
       worktree_path = '/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1' AND
       source_branch = 'ao/wb-price-extension-1/root' AND
       state = 'merged' AND revision = 7 AND repair_count = 0 AND
       pr_url = 'https://github.com/orenvlad-ai/wb-price-extension/pull/1' AND pr_number = 1 AND
       current_head_sha = 'afc748eba5ff05c0dc24d3002c690ec9f44984fb' AND previous_head_sha = '' AND
       review_run_id = 'b0acfb9e-600c-4816-bb2f-02a67817ea05' AND
       admission_id = 'dcp-admission-b0acfb9e-600c-4816-bb2f-02a67817ea05' AND
       merge_commit_sha = '62853496837f64522bb08ba56169f60f3b0f9a2c' AND
       error_code = '' AND incident_packet = '')
    ),
    CHECK (session_id = target || '-' || card_number),
    CHECK (source_branch = 'ao/' || session_id || '/root'),
    CHECK ((state = 'merged') = (merge_commit_sha <> '')),
    CHECK ((state = 'incident') = (incident_packet <> ''))
);

INSERT INTO dcp_review_lab_policy_task_v78
SELECT * FROM dcp_review_lab_policy_task;

DROP TRIGGER dcp_review_lab_policy_task_immutable;
DROP INDEX idx_dcp_review_lab_policy_task_state;
DROP TABLE dcp_review_lab_policy_task;
ALTER TABLE dcp_review_lab_policy_task_v78 RENAME TO dcp_review_lab_policy_task;
CREATE INDEX idx_dcp_review_lab_policy_task_state
    ON dcp_review_lab_policy_task (state, created_at, task_id);

-- +goose StatementBegin
CREATE TRIGGER dcp_review_lab_policy_task_immutable
BEFORE UPDATE ON dcp_review_lab_policy_task
WHEN OLD.task_id <> NEW.task_id
  OR OLD.payload_json <> NEW.payload_json
  OR OLD.payload_digest <> NEW.payload_digest
  OR OLD.target <> NEW.target
  OR OLD.profile <> NEW.profile
  OR OLD.repository <> NEW.repository
  OR OLD.policy_version <> NEW.policy_version
  OR OLD.session_id <> NEW.session_id
  OR OLD.card_number <> NEW.card_number
  OR OLD.worktree_path <> NEW.worktree_path
  OR OLD.source_branch <> NEW.source_branch
  OR OLD.prompt <> NEW.prompt
  OR OLD.created_at <> NEW.created_at
  OR OLD.state IN ('merged', 'failed')
  OR (OLD.state = 'incident' AND NOT (
      NEW.state = 'repair_queued'
      AND NEW.repair_count = OLD.repair_count + 1
      AND EXISTS (
        SELECT 1 FROM dcp_future_card_arbiter_v1 arb
        WHERE arb.task_id = OLD.task_id
          AND arb.source_packet_json = OLD.incident_packet
          AND arb.status = 'repair_queued'
          AND arb.repair_task_id = OLD.task_id
      )
  ))
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

CREATE TABLE dcp_wb_core_release_handoff_v1 (
    authority_id            TEXT PRIMARY KEY CHECK (authority_id = 'wb-core-release-train-handoff-v1'),
    contract_commit         TEXT NOT NULL CHECK (contract_commit = '036b1101284f626c931f7edb1750ddd228634832'),
    predecessor_source      TEXT NOT NULL CHECK (predecessor_source = 'd152afae2bcbcc3d2b1874adf2e6855bebcf00fb'),
    target                  TEXT NOT NULL CHECK (target = 'wb-core'),
    repository              TEXT NOT NULL CHECK (repository = 'orenvlad-ai/wb-core'),
    profile                 TEXT NOT NULL CHECK (profile = 'repo-only'),
    provider_repository_id  INTEGER NOT NULL CHECK (provider_repository_id = 1201929580),
    provider_owner_id       INTEGER NOT NULL CHECK (provider_owner_id = 237411244),
    required_check          TEXT NOT NULL CHECK (required_check = 'baseline'),
    release_actor           TEXT NOT NULL CHECK (release_actor = 'wbc-github-actions-release-train'),
    compatibility_marker    TEXT NOT NULL CHECK (compatibility_marker = 'wb-core.dcp-release-handoff/v1'),
    installed_at            TIMESTAMP NOT NULL
);

INSERT INTO dcp_wb_core_release_handoff_v1 VALUES (
    'wb-core-release-train-handoff-v1',
    '036b1101284f626c931f7edb1750ddd228634832',
    'd152afae2bcbcc3d2b1874adf2e6855bebcf00fb',
    'wb-core', 'orenvlad-ai/wb-core', 'repo-only', 1201929580, 237411244,
    'baseline', 'wbc-github-actions-release-train',
    'wb-core.dcp-release-handoff/v1', CURRENT_TIMESTAMP
);

-- +goose StatementBegin
CREATE TRIGGER dcp_wb_core_release_handoff_v1_no_update
BEFORE UPDATE ON dcp_wb_core_release_handoff_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP wb-core release handoff authority is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_wb_core_release_handoff_v1_no_delete
BEFORE DELETE ON dcp_wb_core_release_handoff_v1
BEGIN
    SELECT RAISE(ABORT, 'DCP wb-core release handoff authority is immutable');
END;
-- +goose StatementEnd

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
