-- +goose Up
-- I13 Stage 2 adds exactly one bounded global-release incident/action record
-- beneath an existing Stage 1 admission incident. This is not a general
-- incident registry, queue, scheduler, retry surface, or second state store.
CREATE TABLE dcp_review_lab_arbiter_v1 (
    incident_id                TEXT PRIMARY KEY CHECK (incident_id GLOB 'dcp-global-release-[0-9a-f]*' AND length(incident_id) = 83),
    generation                 INTEGER NOT NULL CHECK (generation = 1),
    identity_digest            TEXT NOT NULL UNIQUE CHECK (length(identity_digest) = 64),
    admission_id               TEXT NOT NULL REFERENCES dcp_review_lab_admission (id) ON DELETE RESTRICT,
    incident_lease_id          TEXT NOT NULL CHECK (length(incident_lease_id) > 0),
    source_packet_json         TEXT NOT NULL CHECK (json_valid(source_packet_json) AND json_extract(source_packet_json, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'),
    source_packet_digest       TEXT NOT NULL CHECK (length(source_packet_digest) = 64),
    input_json                 TEXT NOT NULL CHECK (length(CAST(input_json AS BLOB)) <= 16384 AND json_valid(input_json) AND json_extract(input_json, '$.schemaVersion') = 'dcp.review-lab.global-release-arbiter-input/v1'),
    input_digest               TEXT NOT NULL CHECK (length(input_digest) = 64),
    task_id                    TEXT NOT NULL CHECK (task_id IN ('i13-arbiter-a', 'i13-arbiter-b')),
    session_id                 TEXT NOT NULL CHECK (session_id IN ('dcp-review-lab-11', 'dcp-review-lab-12')) REFERENCES sessions (id) ON DELETE RESTRICT,
    worktree_path              TEXT NOT NULL CHECK (length(worktree_path) > 0),
    source_branch              TEXT NOT NULL CHECK (source_branch IN ('ao/dcp-review-lab-11/root', 'ao/dcp-review-lab-12/root')),
    pr_url                     TEXT NOT NULL CHECK (length(pr_url) > 0),
    pr_number                  INTEGER NOT NULL CHECK (pr_number > 0),
    target_sha                 TEXT NOT NULL CHECK (length(target_sha) = 40),
    reviewed_base_sha          TEXT NOT NULL CHECK (length(reviewed_base_sha) = 40),
    current_base_sha           TEXT NOT NULL CHECK (length(current_base_sha) = 40),
    review_id                  TEXT NOT NULL CHECK (length(review_id) > 0),
    review_run_id              TEXT NOT NULL CHECK (length(review_run_id) > 0),
    batch_id                   TEXT NOT NULL CHECK (length(batch_id) > 0),
    scope_digest               TEXT NOT NULL CHECK (length(scope_digest) = 64),
    history_digest             TEXT NOT NULL CHECK (length(history_digest) = 64),
    diff_digest                TEXT NOT NULL CHECK (length(diff_digest) = 64),
    check_set_digest           TEXT NOT NULL CHECK (length(check_set_digest) = 64),
    review_set_digest          TEXT NOT NULL CHECK (length(review_set_digest) = 64),
    frozen_queue_digest        TEXT NOT NULL CHECK (length(frozen_queue_digest) = 64),
    mechanical_digest          TEXT NOT NULL CHECK (length(mechanical_digest) = 64),
    model                      TEXT NOT NULL CHECK (model = 'gpt-5.6-sol'),
    reasoning                  TEXT NOT NULL CHECK (reasoning = 'xhigh'),
    token_budget               INTEGER NOT NULL CHECK (token_budget = 16384),
    runtime_handle_id          TEXT NOT NULL CHECK (runtime_handle_id = 'dcp-global-release-arbiter-v1'),
    launch_id                  TEXT NOT NULL CHECK (launch_id = incident_id),
    status                     TEXT NOT NULL CHECK (status IN ('requested', 'preflight_failed', 'running', 'decided', 'safe_stopped', 'repairing', 'recovery_reviewed', 'succeeded', 'failed')),
    model_call_count           INTEGER NOT NULL DEFAULT 0 CHECK (model_call_count IN (0, 1)),
    decision_json              TEXT NOT NULL DEFAULT '' CHECK (decision_json = '' OR (json_valid(decision_json) AND json_extract(decision_json, '$.schemaVersion') = 'dcp.review-lab.global-release-arbiter-decision/v1')),
    decision_digest            TEXT NOT NULL DEFAULT '' CHECK (decision_digest = '' OR length(decision_digest) = 64),
    recovery_owner_session_id  TEXT NOT NULL DEFAULT '',
    recovery_path              TEXT NOT NULL DEFAULT '' CHECK (recovery_path IN ('', 'same_worker_conflict_repair')),
    recovery_wake_count        INTEGER NOT NULL DEFAULT 0 CHECK (recovery_wake_count IN (0, 1)),
    recovery_review_run_id     TEXT NOT NULL DEFAULT '',
    recovery_target_sha        TEXT NOT NULL DEFAULT '' CHECK (recovery_target_sha = '' OR length(recovery_target_sha) = 40),
    error_code                 TEXT NOT NULL DEFAULT '',
    created_at                 TIMESTAMP NOT NULL,
    updated_at                 TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    decision_at                TIMESTAMP,
    finished_at                TIMESTAMP,
    UNIQUE (admission_id, generation),
    CHECK ((status IN ('requested', 'preflight_failed') AND model_call_count = 0) OR (status NOT IN ('requested', 'preflight_failed') AND model_call_count = 1)),
    CHECK ((decision_json = '') = (decision_digest = '')),
    CHECK (status NOT IN ('requested', 'preflight_failed', 'running') OR decision_json = ''),
    CHECK (status NOT IN ('decided', 'safe_stopped', 'repairing', 'recovery_reviewed', 'succeeded') OR decision_json <> ''),
    CHECK (status NOT IN ('repairing', 'recovery_reviewed', 'succeeded') OR recovery_wake_count = 1),
    CHECK (status NOT IN ('requested', 'preflight_failed', 'running', 'decided', 'safe_stopped') OR recovery_wake_count = 0),
    CHECK ((recovery_wake_count = 0 AND recovery_owner_session_id = '' AND recovery_path = '') OR
           (recovery_wake_count = 1 AND recovery_owner_session_id = session_id AND recovery_path = 'same_worker_conflict_repair')),
    CHECK ((status IN ('preflight_failed', 'safe_stopped', 'succeeded', 'failed')) = (finished_at IS NOT NULL))
);

-- The contract permits one Stage 2 incident globally, not one per card.
CREATE UNIQUE INDEX idx_dcp_review_lab_arbiter_v1_single_incident
    ON dcp_review_lab_arbiter_v1 ((1));

-- +goose Down
DROP TABLE dcp_review_lab_arbiter_v1;
