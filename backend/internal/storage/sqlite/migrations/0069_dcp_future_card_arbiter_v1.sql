-- +goose Up
-- Generalize the already-qualified one-shot arbiter pattern only for ordinary
-- future-policy incidents. The existing dcp_model_action queue remains the
-- sole three-slot authority; this migration adds the arbiter role to that same
-- queue and one subordinate incident-generation record.
DROP TRIGGER dcp_model_action_immutable;
ALTER TABLE dcp_model_action RENAME TO dcp_model_action_0069_old;

CREATE TABLE dcp_model_action (
    sequence       INTEGER PRIMARY KEY AUTOINCREMENT,
    id             TEXT NOT NULL UNIQUE CHECK (length(id) > 0),
    task_id        TEXT NOT NULL REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id     TEXT NOT NULL REFERENCES sessions (id) ON DELETE RESTRICT,
    kind           TEXT NOT NULL CHECK (kind IN ('initial_worker', 'repair_worker', 'reviewer', 'arbiter')),
    exact_head_sha TEXT NOT NULL DEFAULT '' CHECK (exact_head_sha = '' OR length(exact_head_sha) = 40),
    status         TEXT NOT NULL CHECK (status IN ('queued', 'claimed', 'running', 'succeeded', 'failed')),
    slot           INTEGER NOT NULL DEFAULT 0 CHECK (slot BETWEEN 0 AND 3),
    launch_id      TEXT NOT NULL DEFAULT '',
    review_run_id  TEXT NOT NULL DEFAULT '',
    incident_id    TEXT NOT NULL DEFAULT '' CHECK (incident_id = '' OR (incident_id GLOB 'dcp-future-arbiter-[0-9a-f]*' AND length(incident_id) = 83)),
    error_code     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (task_id, kind, exact_head_sha, incident_id),
    CHECK ((status IN ('claimed', 'running')) = (slot BETWEEN 1 AND 3)),
    CHECK ((kind = 'initial_worker') = (exact_head_sha = '')),
    CHECK ((kind = 'arbiter') = (incident_id <> '') OR (kind = 'repair_worker' AND incident_id <> '')),
    CHECK (kind <> 'reviewer' OR exact_head_sha <> '')
);

INSERT INTO dcp_model_action (
    sequence, id, task_id, session_id, kind, exact_head_sha, status, slot,
    launch_id, review_run_id, incident_id, error_code, created_at, updated_at
)
SELECT sequence, id, task_id, session_id, kind, exact_head_sha, status, slot,
       launch_id, review_run_id, '', error_code, created_at, updated_at
FROM dcp_model_action_0069_old;
DROP TABLE dcp_model_action_0069_old;

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
  OR OLD.incident_id <> NEW.incident_id
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

CREATE TABLE dcp_future_card_arbiter_v1 (
    incident_id            TEXT PRIMARY KEY CHECK (incident_id GLOB 'dcp-future-arbiter-[0-9a-f]*' AND length(incident_id) = 83),
    generation             INTEGER NOT NULL CHECK (generation >= 1),
    identity_digest        TEXT NOT NULL UNIQUE CHECK (length(identity_digest) = 64),
    task_id                TEXT NOT NULL REFERENCES dcp_review_lab_policy_task (task_id) ON DELETE RESTRICT,
    session_id             TEXT NOT NULL REFERENCES sessions (id) ON DELETE RESTRICT,
    admission_id           TEXT NOT NULL REFERENCES dcp_review_lab_admission (id) ON DELETE RESTRICT,
    admission_sequence     INTEGER NOT NULL CHECK (admission_sequence > 0),
    incident_lease_id      TEXT NOT NULL CHECK (length(incident_lease_id) > 0),
    incident_kind          TEXT NOT NULL CHECK (incident_kind IN ('merge_conflict_or_ambiguity', 'canonical_main_diverged', 'provider_not_clean')),
    source_packet_json     TEXT NOT NULL CHECK (json_valid(source_packet_json) AND json_extract(source_packet_json, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'),
    source_packet_digest   TEXT NOT NULL CHECK (length(source_packet_digest) = 64),
    pr_url                 TEXT NOT NULL CHECK (length(pr_url) > 0),
    pr_number              INTEGER NOT NULL CHECK (pr_number > 0),
    candidate_head_sha     TEXT NOT NULL CHECK (length(candidate_head_sha) = 40),
    reviewed_base_sha      TEXT NOT NULL CHECK (length(reviewed_base_sha) = 40),
    current_main_sha       TEXT NOT NULL CHECK (length(current_main_sha) = 40),
    review_run_id          TEXT NOT NULL CHECK (length(review_run_id) > 0),
    affected_paths_json    TEXT NOT NULL CHECK (json_valid(affected_paths_json) AND json_type(affected_paths_json) = 'array'),
    cohort_json            TEXT NOT NULL CHECK (json_valid(cohort_json) AND json_type(cohort_json) = 'array'),
    cohort_digest          TEXT NOT NULL CHECK (length(cohort_digest) = 64),
    evidence_json          TEXT NOT NULL CHECK (json_valid(evidence_json) AND json_type(evidence_json) = 'object'),
    evidence_digest        TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    input_json             TEXT NOT NULL CHECK (length(CAST(input_json AS BLOB)) <= 16384 AND json_valid(input_json) AND json_extract(input_json, '$.schemaVersion') = 'dcp.review-lab.future-arbiter-input/v1'),
    input_digest           TEXT NOT NULL CHECK (length(input_digest) = 64),
    model_action_id        TEXT NOT NULL UNIQUE,
    runtime_handle_id      TEXT NOT NULL CHECK (runtime_handle_id = incident_id),
    model                  TEXT NOT NULL CHECK (model = 'gpt-5.6-sol'),
    reasoning              TEXT NOT NULL CHECK (reasoning = 'xhigh'),
    token_budget           INTEGER NOT NULL CHECK (token_budget = 16384),
    status                 TEXT NOT NULL CHECK (status IN ('requested', 'claimed', 'running', 'hold', 'repair_queued', 'recovery_reviewed', 'human_gate', 'succeeded', 'failed')),
    model_call_count       INTEGER NOT NULL DEFAULT 0 CHECK (model_call_count IN (0, 1)),
    decision_json          TEXT NOT NULL DEFAULT '' CHECK (decision_json = '' OR (json_valid(decision_json) AND json_extract(decision_json, '$.schemaVersion') = 'dcp.review-lab.future-arbiter-decision/v1')),
    decision_digest        TEXT NOT NULL DEFAULT '' CHECK (decision_digest = '' OR length(decision_digest) = 64),
    verdict                TEXT NOT NULL DEFAULT '' CHECK (verdict IN ('', 'deterministic_order_hold', 'successor_repair', 'human_gate')),
    order_json             TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(order_json) AND json_type(order_json) = 'array'),
    repair_task_id         TEXT NOT NULL DEFAULT '',
    repair_objective       TEXT NOT NULL DEFAULT '',
    repair_paths_json      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(repair_paths_json) AND json_type(repair_paths_json) = 'array'),
    human_question         TEXT NOT NULL DEFAULT '' CHECK (length(CAST(human_question AS BLOB)) <= 512),
    repair_action_id       TEXT NOT NULL DEFAULT '',
    recovery_review_run_id TEXT NOT NULL DEFAULT '',
    recovery_head_sha      TEXT NOT NULL DEFAULT '' CHECK (recovery_head_sha = '' OR length(recovery_head_sha) = 40),
    merge_commit_sha       TEXT NOT NULL DEFAULT '' CHECK (merge_commit_sha = '' OR length(merge_commit_sha) = 40),
    error_code             TEXT NOT NULL DEFAULT '',
    created_at             TIMESTAMP NOT NULL,
    updated_at             TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    decision_at            TIMESTAMP,
    finished_at            TIMESTAMP,
    UNIQUE (admission_id, generation),
    CHECK ((status IN ('requested', 'claimed') AND model_call_count = 0)
        OR (status = 'failed' AND model_call_count IN (0, 1))
        OR (status NOT IN ('requested', 'claimed', 'failed') AND model_call_count = 1)),
    CHECK ((decision_json = '') = (decision_digest = '')),
    CHECK (status NOT IN ('hold', 'repair_queued', 'recovery_reviewed', 'human_gate', 'succeeded') OR decision_json <> ''),
    CHECK ((verdict = 'successor_repair') = (repair_task_id <> '' AND repair_objective <> '' AND repair_paths_json <> '[]')),
    CHECK ((verdict = 'human_gate') = (human_question <> '')),
    CHECK ((status = 'succeeded') = (merge_commit_sha <> '')),
    CHECK ((status IN ('human_gate', 'succeeded', 'failed')) = (finished_at IS NOT NULL))
);

CREATE INDEX idx_dcp_future_card_arbiter_task
    ON dcp_future_card_arbiter_v1 (task_id, generation);
CREATE INDEX idx_dcp_future_card_arbiter_status
    ON dcp_future_card_arbiter_v1 (status, created_at, incident_id);

DROP TRIGGER dcp_review_lab_policy_task_immutable;
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

-- +goose Down
SELECT RAISE(ABORT, '0069 future-card arbiter is an immutable runtime foundation');
