-- +goose Up
-- Future policy tasks extend the existing DCP task/session authority. Cards
-- 1-12 and every qualification/recovery table remain untouched.
CREATE TABLE dcp_review_lab_policy_task (
    task_id           TEXT PRIMARY KEY CHECK (length(task_id) BETWEEN 1 AND 16),
    payload_json      TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    payload_digest    TEXT NOT NULL CHECK (length(payload_digest) = 64),
    target            TEXT NOT NULL CHECK (target = 'dcp-review-lab'),
    profile           TEXT NOT NULL CHECK (profile = 'synthetic-pr'),
    repository        TEXT NOT NULL CHECK (repository = 'orenvlad-ai/dcp-review-lab'),
    policy_version    TEXT NOT NULL CHECK (policy_version = 'dcp.review-lab.happy-path/v1'),
    session_id        TEXT NOT NULL UNIQUE REFERENCES sessions (id) ON DELETE RESTRICT,
    card_number       INTEGER NOT NULL UNIQUE CHECK (card_number > 12),
    worktree_path     TEXT NOT NULL CHECK (length(worktree_path) > 0),
    source_branch     TEXT NOT NULL CHECK (length(source_branch) > 0),
    prompt            TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 512),
    state             TEXT NOT NULL CHECK (state IN (
        'reserved', 'worker_queued', 'worker_running', 'ci_waiting',
        'review_queued', 'review_running', 'repair_queued', 'repair_running',
        'admission_waiting', 'merged', 'failed', 'incident'
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
    CHECK (session_id = 'dcp-review-lab-' || card_number),
    CHECK (source_branch = 'ao/' || session_id || '/root'),
    CHECK ((state = 'merged') = (merge_commit_sha <> '')),
    CHECK ((state = 'incident') = (incident_packet <> ''))
);
CREATE INDEX idx_dcp_review_lab_policy_task_state
    ON dcp_review_lab_policy_task (state, created_at, task_id);

-- Immutable task/payload/native identity. Runtime projection fields advance
-- only one revision at a time through guarded store transactions.
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
  OR OLD.state IN ('merged', 'failed', 'incident')
  OR NEW.repair_count < OLD.repair_count
  OR NEW.repair_count > OLD.repair_count + 1
  OR NEW.revision <> OLD.revision + 1
  OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp review-lab policy immutable identity or revision violated');
END;
-- +goose StatementEnd

CREATE TABLE dcp_model_action (
    sequence       INTEGER PRIMARY KEY AUTOINCREMENT,
    id             TEXT NOT NULL UNIQUE CHECK (length(id) > 0),
    task_id        TEXT NOT NULL REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id     TEXT NOT NULL REFERENCES sessions (id) ON DELETE RESTRICT,
    kind           TEXT NOT NULL CHECK (kind IN ('initial_worker', 'repair_worker', 'reviewer')),
    exact_head_sha TEXT NOT NULL DEFAULT '' CHECK (exact_head_sha = '' OR length(exact_head_sha) = 40),
    status         TEXT NOT NULL CHECK (status IN ('queued', 'claimed', 'running', 'succeeded', 'failed')),
    slot           INTEGER NOT NULL DEFAULT 0 CHECK (slot BETWEEN 0 AND 3),
    launch_id      TEXT NOT NULL DEFAULT '',
    review_run_id  TEXT NOT NULL DEFAULT '',
    error_code     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (task_id, kind, exact_head_sha),
    CHECK ((status IN ('claimed', 'running')) = (slot BETWEEN 1 AND 3)),
    CHECK ((kind = 'initial_worker') = (exact_head_sha = '')),
    CHECK (kind <> 'reviewer' OR exact_head_sha <> '')
);
CREATE UNIQUE INDEX idx_dcp_model_action_one_slot
    ON dcp_model_action (slot) WHERE status IN ('claimed', 'running');
CREATE UNIQUE INDEX idx_dcp_model_action_one_active_task
    ON dcp_model_action (task_id) WHERE status IN ('claimed', 'running');
CREATE UNIQUE INDEX idx_dcp_model_action_one_active_review_head
    ON dcp_model_action (session_id, exact_head_sha)
    WHERE kind = 'reviewer' AND status IN ('claimed', 'running');
CREATE INDEX idx_dcp_model_action_fifo
    ON dcp_model_action (status, sequence);

-- +goose StatementBegin
CREATE TRIGGER dcp_model_action_immutable
BEFORE UPDATE ON dcp_model_action
WHEN OLD.sequence <> NEW.sequence
  OR OLD.id <> NEW.id
  OR OLD.task_id <> NEW.task_id
  OR OLD.session_id <> NEW.session_id
  OR OLD.kind <> NEW.kind
  OR OLD.exact_head_sha <> NEW.exact_head_sha
  OR OLD.created_at <> NEW.created_at
  OR NEW.updated_at < OLD.updated_at
  OR NOT (
      (OLD.status = 'queued' AND NEW.status = 'claimed')
      OR (OLD.status = 'claimed' AND NEW.status IN ('running', 'failed'))
      OR (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed'))
  )
BEGIN
    SELECT RAISE(ABORT, 'dcp model action immutable identity or transition violated');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER dcp_model_action_immutable;
DROP TABLE dcp_model_action;
DROP TRIGGER dcp_review_lab_policy_task_immutable;
DROP TABLE dcp_review_lab_policy_task;
