-- +goose NO TRANSACTION
-- +goose Up
-- The Stage 6 submit already owns the exact v2 Task/Revision/Command/Action.
-- Extend only the predecessor table used as its bounded native model shell;
-- every historical row, child identity and mutable-state guard is preserved.
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE dcp_review_lab_policy_task_v85 (
    task_id           TEXT PRIMARY KEY CHECK (
        length(task_id) BETWEEN 1 AND 16 OR task_id = 'dcp-v2-twin-canary-v1'
    ),
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
    release_phase     TEXT NOT NULL DEFAULT '' CHECK (release_phase IN (
        '', 'waiting_release_train', 'release_train_running', 'waiting_deploy', 'deploy_running'
    )),
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
      (target = 'wb-core' AND profile = 'live-runtime' AND
       repository = 'orenvlad-ai/wb-core' AND
       policy_version = 'dcp.wb-core.live-runtime.release-train/v1') OR
      (task_id = 'dcp-v2-twin-canary-v1' AND
       target = 'dcp-wbc-integration-lab' AND profile = 'live-runtime' AND
       repository = 'orenvlad-ai/dcp-wbc-integration-lab' AND
       policy_version = 'dcp.wbc-integration-twin/v2') OR
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
       release_phase = '' AND error_code = '' AND incident_packet = '')
    ),
    CHECK (
      (task_id = 'dcp-v2-twin-canary-v1') =
      (target = 'dcp-wbc-integration-lab' AND profile = 'live-runtime' AND
       repository = 'orenvlad-ai/dcp-wbc-integration-lab' AND
       policy_version = 'dcp.wbc-integration-twin/v2')
    ),
    CHECK (session_id = target || '-' || card_number),
    CHECK (source_branch = 'ao/' || session_id || '/root'),
    CHECK ((state = 'merged') = (merge_commit_sha <> '')),
    CHECK ((state = 'incident') = (incident_packet <> '')),
    CHECK ((state = 'release_waiting') OR release_phase = '')
);

INSERT INTO dcp_review_lab_policy_task_v85 (
    task_id, payload_json, payload_digest, target, profile, repository,
    policy_version, session_id, card_number, worktree_path, source_branch,
    prompt, state, revision, repair_count, pr_url, pr_number,
    current_head_sha, previous_head_sha, review_run_id, admission_id,
    release_phase, merge_commit_sha, error_code, incident_packet, created_at,
    updated_at
)
SELECT task_id, payload_json, payload_digest, target, profile, repository,
       policy_version, session_id, card_number, worktree_path, source_branch,
       prompt, state, revision, repair_count, pr_url, pr_number,
       current_head_sha, previous_head_sha, review_run_id, admission_id,
       release_phase, merge_commit_sha, error_code, incident_packet, created_at,
       updated_at
FROM dcp_review_lab_policy_task;

DROP TRIGGER dcp_review_lab_policy_task_immutable;
DROP INDEX idx_dcp_review_lab_policy_task_state;
DROP TABLE dcp_review_lab_policy_task;
ALTER TABLE dcp_review_lab_policy_task_v85 RENAME TO dcp_review_lab_policy_task;
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
      (NEW.state = 'repair_queued'
       AND NEW.repair_count = OLD.repair_count + 1
       AND EXISTS (
         SELECT 1 FROM dcp_future_card_arbiter_v1 arb
         WHERE arb.task_id = OLD.task_id
           AND arb.source_packet_json = OLD.incident_packet
           AND arb.status = 'repair_queued'
           AND arb.repair_task_id = OLD.task_id
       )) OR
      (NEW.state = 'ci_waiting'
       AND NEW.repair_count = OLD.repair_count
       AND NEW.admission_id = '' AND NEW.review_run_id = ''
       AND NEW.previous_head_sha = OLD.current_head_sha
       AND NEW.current_head_sha <> OLD.current_head_sha
       AND NEW.error_code = '' AND NEW.incident_packet = ''
       AND EXISTS (
         SELECT 1 FROM dcp_wbc_readmission_generation generation
         WHERE generation.task_id = OLD.task_id
           AND generation.session_id = OLD.session_id
           AND generation.old_admission_id = OLD.admission_id
           AND generation.admitted_head_sha = OLD.current_head_sha
           AND generation.new_head_sha = NEW.current_head_sha
           AND generation.status = 'head_pushed'
       ))
  ))
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
SELECT RAISE(ABORT, '0085 DCP v2 twin native-shell compatibility is forward-only');
