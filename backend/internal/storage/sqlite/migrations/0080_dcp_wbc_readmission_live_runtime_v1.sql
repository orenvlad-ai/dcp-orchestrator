-- +goose NO TRANSACTION
-- +goose Up
-- Add the versioned wb-core event-driven readmission authority and the exact
-- live-runtime profile. Existing incident/admission/action evidence is copied
-- unchanged; this migration creates no task, session, action, or release fact.
PRAGMA foreign_keys = OFF;

CREATE TABLE dcp_review_lab_policy_task_v80 (
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
    CHECK (session_id = target || '-' || card_number),
    CHECK (source_branch = 'ao/' || session_id || '/root'),
    CHECK ((state = 'merged') = (merge_commit_sha <> '')),
    CHECK ((state = 'incident') = (incident_packet <> '')),
    CHECK ((state = 'release_waiting') OR release_phase = '')
);

INSERT INTO dcp_review_lab_policy_task_v80 (
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
       CASE WHEN target = 'wb-core' AND state = 'release_waiting'
            THEN 'waiting_release_train' ELSE '' END,
       merge_commit_sha, error_code, incident_packet, created_at, updated_at
FROM dcp_review_lab_policy_task;

DROP TRIGGER dcp_review_lab_policy_task_immutable;
DROP INDEX idx_dcp_review_lab_policy_task_state;
DROP TABLE dcp_review_lab_policy_task;
ALTER TABLE dcp_review_lab_policy_task_v80 RENAME TO dcp_review_lab_policy_task;
CREATE INDEX idx_dcp_review_lab_policy_task_state
    ON dcp_review_lab_policy_task (state, created_at, task_id);

CREATE TABLE dcp_wbc_readmission_generation (
    sequence             INTEGER PRIMARY KEY AUTOINCREMENT,
    generation_id        TEXT NOT NULL UNIQUE,
    marker_digest        TEXT NOT NULL UNIQUE CHECK (length(marker_digest) = 64),
    marker_version       TEXT NOT NULL CHECK (marker_version IN (
        'wb-core.dcp-release-handoff/v1', 'wb-core.dcp-release-handoff/v2'
    )),
    marker_comment_id    INTEGER NOT NULL UNIQUE CHECK (marker_comment_id > 0),
    marker_author        TEXT NOT NULL CHECK (marker_author IN ('github-actions', 'github-actions[bot]')),
    marker_created_at    TIMESTAMP NOT NULL,
    marker_updated_at    TIMESTAMP NOT NULL CHECK (marker_updated_at = marker_created_at),
    marker_main_sha      TEXT NOT NULL CHECK (length(marker_main_sha) = 40),
    task_id              TEXT NOT NULL REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id           TEXT NOT NULL REFERENCES sessions (id) ON DELETE RESTRICT,
    old_admission_id     TEXT NOT NULL UNIQUE REFERENCES dcp_review_lab_admission (id) ON DELETE RESTRICT,
    pr_url               TEXT NOT NULL,
    pr_number            INTEGER NOT NULL CHECK (pr_number > 0),
    repository           TEXT NOT NULL CHECK (repository = 'orenvlad-ai/wb-core'),
    base_branch          TEXT NOT NULL CHECK (base_branch = 'main'),
    scope                TEXT NOT NULL CHECK (scope IN ('repo-only', 'live-runtime')),
    head_ref             TEXT NOT NULL,
    session_number       INTEGER NOT NULL CHECK (session_number > 0),
    admitted_head_sha    TEXT NOT NULL CHECK (length(admitted_head_sha) = 40),
    admitted_base_sha    TEXT NOT NULL CHECK (length(admitted_base_sha) = 40),
    observed_head_sha    TEXT NOT NULL CHECK (length(observed_head_sha) = 40),
    current_main_sha     TEXT NOT NULL CHECK (length(current_main_sha) = 40),
    ready_event_id       INTEGER NOT NULL CHECK (ready_event_id >= 0),
    admission_check_id   INTEGER NOT NULL CHECK (admission_check_id >= 0),
    handoff_proof_id     INTEGER NOT NULL CHECK (handoff_proof_id >= 0),
    reason               TEXT NOT NULL CHECK (reason = 'base-behind-after-admission'),
    status               TEXT NOT NULL CHECK (status IN (
        'observed', 'claimed', 'prepared', 'head_pushed', 'review_queued',
        'reviewed', 'admitted', 'release_waiting', 'terminal', 'conflict', 'failed'
    )),
    lease_id             TEXT NOT NULL DEFAULT '',
    merge_tree_sha       TEXT NOT NULL DEFAULT '' CHECK (merge_tree_sha = '' OR length(merge_tree_sha) = 40),
    new_head_sha         TEXT NOT NULL DEFAULT '' CHECK (new_head_sha = '' OR length(new_head_sha) = 40),
    review_action_id     TEXT NOT NULL DEFAULT '',
    review_run_id        TEXT NOT NULL DEFAULT '',
    admission_id         TEXT NOT NULL DEFAULT '',
    error_code           TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (task_id, sequence),
    CHECK (session_id = 'wb-core-' || session_number),
    CHECK (head_ref = 'ao/' || session_id || '/root'),
    CHECK (admitted_head_sha = observed_head_sha),
    CHECK (admitted_head_sha <> current_main_sha),
    CHECK (
      (status = 'observed' AND lease_id = '') OR
      status = 'failed' OR
      (status <> 'observed' AND lease_id <> '')
    ),
    CHECK (status = 'failed' OR ((status IN ('prepared', 'head_pushed', 'review_queued', 'reviewed', 'admitted', 'release_waiting', 'terminal')) = (new_head_sha <> ''))),
    CHECK (merge_tree_sha = '' OR new_head_sha <> ''),
    CHECK (status = 'failed' OR ((review_action_id <> '') = (status IN ('review_queued', 'reviewed', 'admitted', 'release_waiting', 'terminal')))),
    CHECK (status = 'failed' OR ((review_run_id <> '') = (status IN ('reviewed', 'admitted', 'release_waiting', 'terminal')))),
    CHECK (status = 'failed' OR ((admission_id <> '') = (status IN ('admitted', 'release_waiting', 'terminal'))))
);
CREATE UNIQUE INDEX idx_dcp_wbc_readmission_one_open_task
    ON dcp_wbc_readmission_generation (task_id)
    WHERE status NOT IN ('terminal', 'conflict', 'failed');

-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_readmission_generation_guard
BEFORE UPDATE ON dcp_wbc_readmission_generation
WHEN OLD.sequence <> NEW.sequence
  OR OLD.generation_id <> NEW.generation_id
  OR OLD.marker_digest <> NEW.marker_digest
  OR OLD.marker_version <> NEW.marker_version
  OR OLD.marker_comment_id <> NEW.marker_comment_id
  OR OLD.marker_author <> NEW.marker_author
  OR OLD.marker_created_at <> NEW.marker_created_at
  OR OLD.marker_updated_at <> NEW.marker_updated_at
  OR OLD.marker_main_sha <> NEW.marker_main_sha
  OR OLD.task_id <> NEW.task_id
  OR OLD.session_id <> NEW.session_id
  OR OLD.old_admission_id <> NEW.old_admission_id
  OR OLD.pr_url <> NEW.pr_url
  OR OLD.pr_number <> NEW.pr_number
  OR OLD.repository <> NEW.repository
  OR OLD.base_branch <> NEW.base_branch
  OR OLD.scope <> NEW.scope
  OR OLD.head_ref <> NEW.head_ref
  OR OLD.session_number <> NEW.session_number
  OR OLD.admitted_head_sha <> NEW.admitted_head_sha
  OR OLD.admitted_base_sha <> NEW.admitted_base_sha
  OR OLD.observed_head_sha <> NEW.observed_head_sha
  OR (OLD.current_main_sha <> NEW.current_main_sha AND NOT (
      OLD.status = 'claimed' AND NEW.status = 'prepared'
      AND length(NEW.current_main_sha) = 40
  ))
  OR OLD.ready_event_id <> NEW.ready_event_id
  OR OLD.admission_check_id <> NEW.admission_check_id
  OR OLD.handoff_proof_id <> NEW.handoff_proof_id
  OR OLD.reason <> NEW.reason
  OR OLD.created_at <> NEW.created_at
  OR NEW.updated_at < OLD.updated_at
  OR NOT (
      (OLD.status = 'observed' AND NEW.status IN ('claimed', 'failed')) OR
      (OLD.status = 'claimed' AND NEW.status IN ('prepared', 'conflict', 'failed')) OR
      (OLD.status = 'prepared' AND NEW.status IN ('head_pushed', 'failed')) OR
      (OLD.status = 'head_pushed' AND NEW.status IN ('review_queued', 'failed')) OR
      (OLD.status = 'review_queued' AND NEW.status IN ('review_queued', 'reviewed', 'failed')) OR
      (OLD.status = 'reviewed' AND NEW.status IN ('admitted', 'failed')) OR
      (OLD.status = 'admitted' AND NEW.status IN ('release_waiting', 'failed')) OR
      (OLD.status = 'release_waiting' AND NEW.status IN ('terminal', 'failed'))
  )
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission identity or transition violated');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_readmission_generation_no_delete
BEFORE DELETE ON dcp_wbc_readmission_generation
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC readmission evidence cannot be deleted');
END;
-- +goose StatementEnd

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

CREATE TABLE dcp_wbc_release_deploy_v1_authority (
    authority_id TEXT PRIMARY KEY CHECK (authority_id = 'dcp-wbc-release-deploy-v1'),
    target TEXT NOT NULL CHECK (target = 'wb-core'),
    repository TEXT NOT NULL CHECK (repository = 'orenvlad-ai/wb-core'),
    profiles TEXT NOT NULL CHECK (profiles = 'repo-only,live-runtime'),
    marker TEXT NOT NULL CHECK (marker = 'wb-core.dcp-release-handoff/v2'),
    release_actor TEXT NOT NULL CHECK (release_actor = 'wbc-github-actions-release-train'),
    direct_merge_eligible INTEGER NOT NULL CHECK (direct_merge_eligible = 0),
    installed_at TIMESTAMP NOT NULL
);
INSERT INTO dcp_wbc_release_deploy_v1_authority VALUES (
    'dcp-wbc-release-deploy-v1', 'wb-core', 'orenvlad-ai/wb-core',
    'repo-only,live-runtime', 'wb-core.dcp-release-handoff/v2',
    'wbc-github-actions-release-train', 0, CURRENT_TIMESTAMP
);
-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_release_deploy_v1_authority_no_update
BEFORE UPDATE ON dcp_wbc_release_deploy_v1_authority
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC release/deploy authority is immutable');
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_wbc_release_deploy_v1_authority_no_delete
BEFORE DELETE ON dcp_wbc_release_deploy_v1_authority
BEGIN
    SELECT RAISE(ABORT, 'DCP WBC release/deploy authority cannot be deleted');
END;
-- +goose StatementEnd

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT RAISE(ABORT, '0080 DCP WBC release/deploy authority is forward-only');
