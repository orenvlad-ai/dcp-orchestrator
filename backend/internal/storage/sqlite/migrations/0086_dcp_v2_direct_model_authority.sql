-- +goose NO TRANSACTION
-- +goose Up
-- DCP v2 direct model authority. Legacy lifecycle tables remain untouched and
-- are not referenced by any ongoing DCP-v2 runtime decision.
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

DROP TRIGGER dcp_v2_command_guard;
DROP INDEX idx_dcp_v2_command_one_active_per_task;
DROP INDEX idx_dcp_v2_command_drain;

CREATE TABLE dcp_v2_command_v86 (
    sequence                 INTEGER PRIMARY KEY AUTOINCREMENT,
    command_id               TEXT NOT NULL UNIQUE CHECK (length(command_id) > 0),
    task_id                  TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id              TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    kind                     TEXT NOT NULL CHECK (kind IN (
        'worker.execute/v1','publication.execute/v1','checks.observe/v1','review.execute/v1','repair.execute/v1',
        'arbiter.execute/v1','human_gate.open/v1','admission.enqueue/v1',
        'readmission.materialize/v1','release.dispatch/v1','merge.observe/v1',
        'deployment.observe/v1','terminal.verify/v1'
    )),
    payload_json             TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    payload_digest           TEXT NOT NULL CHECK (length(payload_digest) = 64),
    prerequisite_digest      TEXT NOT NULL CHECK (length(prerequisite_digest) = 64),
    idempotency_key          TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) > 0),
    status                   TEXT NOT NULL CHECK (status IN ('pending','leased','succeeded','failed','superseded','cancelled')),
    lease_owner              TEXT NOT NULL DEFAULT '',
    lease_epoch              TEXT NOT NULL DEFAULT '',
    lease_token              TEXT NOT NULL DEFAULT '',
    effect_fence             TEXT NOT NULL DEFAULT '',
    recovery_generation      INTEGER NOT NULL DEFAULT 0 CHECK (recovery_generation BETWEEN 0 AND 32),
    result_digest            TEXT NOT NULL DEFAULT '' CHECK (result_digest = '' OR length(result_digest) = 64),
    error_code               TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMP NOT NULL,
    updated_at               TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (command_id, task_id, revision_id),
    FOREIGN KEY (revision_id, task_id) REFERENCES dcp_v2_revision (revision_id, task_id) ON DELETE RESTRICT,
    CHECK ((status = 'pending') = (lease_owner = '' AND lease_epoch = '' AND lease_token = '')),
    CHECK (status <> 'leased' OR (lease_owner <> '' AND lease_epoch <> '' AND lease_token <> '')),
    CHECK (status <> 'succeeded' OR result_digest <> '')
);

INSERT INTO dcp_v2_command_v86 SELECT * FROM dcp_v2_command;
DROP TABLE dcp_v2_command;
ALTER TABLE dcp_v2_command_v86 RENAME TO dcp_v2_command;

CREATE UNIQUE INDEX idx_dcp_v2_command_one_active_per_task
    ON dcp_v2_command (task_id) WHERE status IN ('pending','leased');
CREATE INDEX idx_dcp_v2_command_drain ON dcp_v2_command (status, sequence);

-- +goose StatementBegin
CREATE TRIGGER dcp_v2_command_guard BEFORE UPDATE ON dcp_v2_command
WHEN OLD.sequence <> NEW.sequence OR OLD.command_id <> NEW.command_id OR OLD.task_id <> NEW.task_id
 OR OLD.revision_id <> NEW.revision_id OR OLD.kind <> NEW.kind OR OLD.payload_json <> NEW.payload_json
 OR OLD.payload_digest <> NEW.payload_digest OR OLD.prerequisite_digest <> NEW.prerequisite_digest
 OR OLD.idempotency_key <> NEW.idempotency_key OR OLD.created_at <> NEW.created_at OR NEW.updated_at < OLD.updated_at
 OR NOT (
   (OLD.status = 'pending' AND NEW.status = 'leased' AND NEW.recovery_generation = OLD.recovery_generation)
   OR (OLD.status = 'leased' AND NEW.status = 'leased' AND (
        (OLD.effect_fence = '' AND NEW.effect_fence <> '' AND NEW.recovery_generation = OLD.recovery_generation)
        OR (OLD.effect_fence = '' AND NEW.effect_fence = '' AND NEW.recovery_generation = OLD.recovery_generation + 1)
      ))
   OR (OLD.status = 'leased' AND NEW.status IN ('succeeded','failed','superseded','cancelled'))
 )
BEGIN SELECT RAISE(ABORT, 'DCP v2 Command identity or transition violated'); END;
-- +goose StatementEnd

CREATE TABLE dcp_v2_model_runtime (
    runtime_id                TEXT PRIMARY KEY CHECK (length(runtime_id) > 0),
    action_id                 TEXT NOT NULL UNIQUE REFERENCES dcp_v2_action (action_id) ON DELETE RESTRICT,
    command_id                TEXT NOT NULL UNIQUE REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    task_id                   TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id               TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    slot                      INTEGER NOT NULL CHECK (slot BETWEEN 1 AND 3),
    launch_fence              TEXT NOT NULL UNIQUE CHECK (length(launch_fence) > 0),
    provider_request_id       TEXT NOT NULL DEFAULT '',
    provider_request_digest   TEXT NOT NULL DEFAULT '' CHECK (provider_request_digest = '' OR length(provider_request_digest) = 64),
    worktree_path             TEXT NOT NULL CHECK (length(worktree_path) > 0),
    worktree_digest           TEXT NOT NULL CHECK (length(worktree_digest) = 64),
    state                     TEXT NOT NULL CHECK (state IN ('reserved','running','succeeded','failed')),
    created_at                TIMESTAMP NOT NULL,
    updated_at                TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    FOREIGN KEY (command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK ((state = 'reserved') = (provider_request_id = '' AND provider_request_digest = '')),
    CHECK (state <> 'running' OR (provider_request_id <> '' AND provider_request_digest <> ''))
);
CREATE UNIQUE INDEX idx_dcp_v2_model_runtime_one_slot
    ON dcp_v2_model_runtime (slot) WHERE state IN ('reserved','running');

CREATE TABLE dcp_v2_model_terminal_receipt (
    receipt_id       TEXT PRIMARY KEY CHECK (length(receipt_id) > 0),
    action_id        TEXT NOT NULL UNIQUE REFERENCES dcp_v2_action (action_id) ON DELETE RESTRICT,
    command_id       TEXT NOT NULL UNIQUE REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    task_id          TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id      TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    runtime_id       TEXT NOT NULL UNIQUE REFERENCES dcp_v2_model_runtime (runtime_id) ON DELETE RESTRICT,
    launch_fence     TEXT NOT NULL UNIQUE,
    status           TEXT NOT NULL CHECK (status IN ('succeeded','failed')),
    result_digest    TEXT NOT NULL DEFAULT '' CHECK (result_digest = '' OR length(result_digest) = 64),
    error_code       TEXT NOT NULL DEFAULT '',
    output_json      TEXT NOT NULL CHECK (json_valid(output_json) AND json_type(output_json) = 'object'),
    output_digest    TEXT NOT NULL CHECK (length(output_digest) = 64),
    head_ref         TEXT NOT NULL DEFAULT '',
    head_sha         TEXT NOT NULL DEFAULT '' CHECK (head_sha = '' OR length(head_sha) = 40),
    tree_sha         TEXT NOT NULL DEFAULT '' CHECK (tree_sha = '' OR length(tree_sha) = 40),
    base_sha         TEXT NOT NULL DEFAULT '' CHECK (base_sha = '' OR length(base_sha) = 40),
    worktree_path    TEXT NOT NULL CHECK (length(worktree_path) > 0),
    worktree_digest  TEXT NOT NULL DEFAULT '' CHECK (worktree_digest = '' OR length(worktree_digest) = 64),
    created_at       TIMESTAMP NOT NULL,
    FOREIGN KEY (command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK ((status = 'succeeded') = (result_digest <> '' AND error_code = '')),
    CHECK ((status = 'failed') = (result_digest = '' AND error_code <> ''))
);

CREATE TABLE dcp_v2_stage6_worker_adoption_v1 (
    adoption_id             TEXT PRIMARY KEY CHECK (adoption_id = 'dcp-v2-stage6-worker-adoption-v1'),
    task_id                 TEXT NOT NULL UNIQUE REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id             TEXT NOT NULL UNIQUE REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    command_id              TEXT NOT NULL UNIQUE REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    action_id               TEXT NOT NULL UNIQUE REFERENCES dcp_v2_action (action_id) ON DELETE RESTRICT,
    runtime_id              TEXT NOT NULL UNIQUE REFERENCES dcp_v2_model_runtime (runtime_id) ON DELETE RESTRICT,
    native_action_id        TEXT NOT NULL UNIQUE,
    native_sequence         INTEGER NOT NULL CHECK (native_sequence > 0),
    legacy_evidence_digest  TEXT NOT NULL UNIQUE CHECK (length(legacy_evidence_digest) = 64),
    commit_sha              TEXT NOT NULL CHECK (length(commit_sha) = 40),
    tree_sha                TEXT NOT NULL CHECK (length(tree_sha) = 40),
    branch                  TEXT NOT NULL CHECK (length(branch) > 0),
    worktree_digest         TEXT NOT NULL CHECK (length(worktree_digest) = 64),
    output_digest           TEXT NOT NULL CHECK (length(output_digest) = 64),
    receipt_id              TEXT NOT NULL UNIQUE REFERENCES dcp_v2_model_terminal_receipt (receipt_id) ON DELETE RESTRICT,
    consumed_at             TIMESTAMP NOT NULL
);

-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_runtime_insert_guard BEFORE INSERT ON dcp_v2_model_runtime
WHEN NOT EXISTS (
  SELECT 1
    FROM dcp_v2_action a
    JOIN dcp_v2_command c ON c.command_id = a.command_id
   WHERE a.action_id = NEW.action_id AND a.command_id = NEW.command_id
     AND a.task_id = NEW.task_id AND a.revision_id = NEW.revision_id
     AND a.slot = NEW.slot AND a.launch_fence = NEW.launch_fence
     AND a.status IN ('launching','running')
     AND c.task_id = NEW.task_id AND c.revision_id = NEW.revision_id
     AND c.status = 'leased' AND c.effect_fence = NEW.launch_fence
)
BEGIN SELECT RAISE(ABORT, 'DCP v2 runtime insert identity violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_terminal_receipt_insert_guard BEFORE INSERT ON dcp_v2_model_terminal_receipt
WHEN NOT EXISTS (
  SELECT 1
    FROM dcp_v2_model_runtime r
    JOIN dcp_v2_action a ON a.action_id = r.action_id
    JOIN dcp_v2_command c ON c.command_id = r.command_id
   WHERE r.runtime_id = NEW.runtime_id AND r.action_id = NEW.action_id
     AND r.command_id = NEW.command_id AND r.task_id = NEW.task_id
     AND r.revision_id = NEW.revision_id AND r.launch_fence = NEW.launch_fence
     AND r.state = 'running' AND a.status = 'running'
     AND a.runtime_id = NEW.runtime_id AND a.launch_fence = NEW.launch_fence
     AND c.status = 'leased' AND c.effect_fence = NEW.launch_fence
)
BEGIN SELECT RAISE(ABORT, 'DCP v2 terminal receipt insert identity violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_stage6_worker_adoption_insert_guard BEFORE INSERT ON dcp_v2_stage6_worker_adoption_v1
WHEN NOT EXISTS (
  SELECT 1
    FROM dcp_v2_model_terminal_receipt r
   WHERE r.receipt_id = NEW.receipt_id AND r.runtime_id = NEW.runtime_id
     AND r.action_id = NEW.action_id AND r.command_id = NEW.command_id
     AND r.task_id = NEW.task_id AND r.revision_id = NEW.revision_id
     AND r.status = 'succeeded' AND r.head_sha = NEW.commit_sha
     AND r.tree_sha = NEW.tree_sha AND r.head_ref = NEW.branch
     AND r.worktree_digest = NEW.worktree_digest AND r.output_digest = NEW.output_digest
)
BEGIN SELECT RAISE(ABORT, 'DCP v2 Stage 6 adoption insert identity violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_runtime_guard BEFORE UPDATE ON dcp_v2_model_runtime
WHEN OLD.runtime_id <> NEW.runtime_id OR OLD.action_id <> NEW.action_id OR OLD.command_id <> NEW.command_id
 OR OLD.task_id <> NEW.task_id OR OLD.revision_id <> NEW.revision_id OR OLD.slot <> NEW.slot
 OR OLD.launch_fence <> NEW.launch_fence OR OLD.worktree_path <> NEW.worktree_path
 OR OLD.worktree_digest <> NEW.worktree_digest OR OLD.created_at <> NEW.created_at OR NEW.updated_at < OLD.updated_at
 OR NOT ((OLD.state = 'reserved' AND NEW.state = 'running'
          AND OLD.provider_request_id = '' AND OLD.provider_request_digest = ''
          AND NEW.provider_request_id <> '' AND NEW.provider_request_digest <> '')
      OR (OLD.state = 'running' AND NEW.state IN ('succeeded','failed')
          AND OLD.provider_request_id = NEW.provider_request_id
          AND OLD.provider_request_digest = NEW.provider_request_digest))
BEGIN SELECT RAISE(ABORT, 'DCP v2 runtime identity or transition violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_runtime_no_delete BEFORE DELETE ON dcp_v2_model_runtime
BEGIN SELECT RAISE(ABORT, 'DCP v2 model runtime cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_terminal_receipt_no_update BEFORE UPDATE ON dcp_v2_model_terminal_receipt
BEGIN SELECT RAISE(ABORT, 'DCP v2 model terminal receipt is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_model_terminal_receipt_no_delete BEFORE DELETE ON dcp_v2_model_terminal_receipt
BEGIN SELECT RAISE(ABORT, 'DCP v2 model terminal receipt cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_stage6_worker_adoption_no_update BEFORE UPDATE ON dcp_v2_stage6_worker_adoption_v1
BEGIN SELECT RAISE(ABORT, 'DCP v2 Stage 6 adoption is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_stage6_worker_adoption_no_delete BEFORE DELETE ON dcp_v2_stage6_worker_adoption_v1
BEGIN SELECT RAISE(ABORT, 'DCP v2 Stage 6 adoption cannot be deleted'); END;
-- +goose StatementEnd

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
SELECT RAISE(ABORT, '0086 DCP v2 direct model authority is forward-only');
