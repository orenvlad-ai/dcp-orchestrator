-- +goose Up
-- I11 adds one durable, model-free DCP task state and its own semantic event
-- stream to the existing authoritative ao.db. It does not change AO sessions,
-- change_log, workers, or lifecycle tables.
CREATE TABLE dcp_tasks (
    task_id                 TEXT PRIMARY KEY CHECK (length(task_id) > 0),
    idempotency_key         TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    approved_task_json      TEXT NOT NULL CHECK (json_valid(approved_task_json) AND json_type(approved_task_json) = 'object' AND json_extract(approved_task_json, '$.schemaVersion') = 'dcp.task/v1'),
    approved_scope_json     TEXT NOT NULL CHECK (json_valid(approved_scope_json) AND json_type(approved_scope_json) = 'object' AND json_extract(approved_scope_json, '$.schemaVersion') = 'dcp.scope/v1'),
    approved_digest         TEXT NOT NULL CHECK (length(approved_digest) = 64),
    target_project_id       TEXT NOT NULL CHECK (target_project_id = 'dcp-lab') REFERENCES projects (id),
    target_repository       TEXT NOT NULL CHECK (target_repository = 'dcp-lab'),
    target_path             TEXT NOT NULL CHECK (length(target_path) > 0),
    target_head_sha         TEXT NOT NULL CHECK (length(target_head_sha) = 40),
    target_marker_digest    TEXT NOT NULL CHECK (length(target_marker_digest) = 64),
    target_identity_digest  TEXT NOT NULL CHECK (length(target_identity_digest) = 64),
    state                   TEXT NOT NULL CHECK (state = 'SUBMITTED'),
    revision                INTEGER NOT NULL CHECK (revision >= 1),
    created_at              TIMESTAMP NOT NULL,
    updated_at              TIMESTAMP NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX idx_dcp_tasks_target_updated
    ON dcp_tasks (target_project_id, updated_at DESC, task_id DESC);

CREATE TABLE dcp_task_events (
    task_id          TEXT NOT NULL REFERENCES dcp_tasks (task_id) ON DELETE CASCADE,
    sequence         INTEGER NOT NULL CHECK (sequence >= 1),
    event_id         TEXT NOT NULL UNIQUE CHECK (length(event_id) > 0),
    schema_version   TEXT NOT NULL CHECK (schema_version = 'dcp.event/v1'),
    event_type       TEXT NOT NULL CHECK (event_type IN ('task.submitted', 'system.reconciled')),
    source_kind      TEXT NOT NULL CHECK (source_kind = 'daemon'),
    source_id        TEXT NOT NULL CHECK (length(source_id) > 0),
    correlation_id   TEXT NOT NULL CHECK (length(correlation_id) > 0),
    causation_id     TEXT CHECK (causation_id IS NULL OR length(causation_id) > 0),
    idempotency_key  TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    from_state       TEXT CHECK (from_state IS NULL OR from_state = 'SUBMITTED'),
    to_state         TEXT NOT NULL CHECK (to_state = 'SUBMITTED'),
    task_revision    INTEGER NOT NULL CHECK (task_revision >= 1),
    occurred_at      TIMESTAMP NOT NULL,
    recorded_at      TIMESTAMP NOT NULL CHECK (recorded_at >= occurred_at),
    payload_json     TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object' AND json_type(payload_json, '$.schemaVersion') = 'text'),
    evidence_digest  TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    integrity_digest TEXT NOT NULL CHECK (length(integrity_digest) = 64),
    PRIMARY KEY (task_id, sequence),
    UNIQUE (task_id, idempotency_key)
);
CREATE INDEX idx_dcp_task_events_recorded
    ON dcp_task_events (recorded_at, event_id);

-- The task/event stream is append-only and strictly monotonic. Store methods
-- still calculate the next sequence inside the write transaction; these
-- triggers are the final physical guard against a bypass or implementation bug.
-- +goose StatementBegin
CREATE TRIGGER dcp_task_events_monotonic
BEFORE INSERT ON dcp_task_events
WHEN NEW.sequence <> COALESCE(
    (SELECT MAX(sequence) + 1 FROM dcp_task_events WHERE task_id = NEW.task_id),
    1
)
BEGIN
    SELECT RAISE(ABORT, 'dcp task event sequence is not monotonic');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_task_events_match_task
BEFORE INSERT ON dcp_task_events
WHEN NEW.task_revision <> NEW.sequence
    OR (NEW.sequence = 1 AND NEW.from_state IS NOT NULL)
    OR (NEW.sequence > 1 AND NEW.from_state IS NULL)
    OR (NEW.sequence > 1 AND NEW.from_state <> (
        SELECT to_state FROM dcp_task_events
        WHERE task_id = NEW.task_id AND sequence = NEW.sequence - 1
    ))
    OR NOT EXISTS (
    SELECT 1 FROM dcp_tasks
    WHERE task_id = NEW.task_id
      AND state = NEW.to_state
      AND revision = NEW.task_revision
)
BEGIN
    SELECT RAISE(ABORT, 'dcp task event does not match task state revision');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_task_events_no_update
BEFORE UPDATE ON dcp_task_events
BEGIN
    SELECT RAISE(ABORT, 'dcp task events are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER dcp_task_events_no_delete
BEFORE DELETE ON dcp_task_events
BEGIN
    SELECT RAISE(ABORT, 'dcp task events are append-only');
END;
-- +goose StatementEnd

-- Approved content, idempotency, and target identity never change. The only
-- permitted update is a one-revision compare-and-set with an atomic event.
-- +goose StatementBegin
CREATE TRIGGER dcp_tasks_immutable_contract
BEFORE UPDATE ON dcp_tasks
WHEN OLD.idempotency_key <> NEW.idempotency_key
    OR OLD.task_id <> NEW.task_id
    OR OLD.approved_task_json <> NEW.approved_task_json
    OR OLD.approved_scope_json <> NEW.approved_scope_json
    OR OLD.approved_digest <> NEW.approved_digest
    OR OLD.target_project_id <> NEW.target_project_id
    OR OLD.target_repository <> NEW.target_repository
    OR OLD.target_path <> NEW.target_path
    OR OLD.target_head_sha <> NEW.target_head_sha
    OR OLD.target_marker_digest <> NEW.target_marker_digest
    OR OLD.target_identity_digest <> NEW.target_identity_digest
    OR OLD.created_at <> NEW.created_at
    OR NEW.revision <> OLD.revision + 1
    OR NEW.updated_at < OLD.updated_at
BEGIN
    SELECT RAISE(ABORT, 'dcp task immutable contract or revision violated');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER dcp_tasks_immutable_contract;
DROP TRIGGER dcp_task_events_no_delete;
DROP TRIGGER dcp_task_events_no_update;
DROP TRIGGER dcp_task_events_match_task;
DROP TRIGGER dcp_task_events_monotonic;
DROP TABLE dcp_task_events;
DROP TABLE dcp_tasks;
