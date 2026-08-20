-- +goose Up
-- Stage 4 source-only DCP v2 core. These tables are deliberately empty after
-- migration: no target adapter, installed runtime or Task submit is activated.
CREATE TABLE dcp_v2_core_authority (
    authority_id          TEXT PRIMARY KEY CHECK (authority_id = 'dcp-v2-core-stage4'),
    control_plane_commit  TEXT NOT NULL CHECK (control_plane_commit = '8be08577673722edc9ae036dedea46c88ceac129'),
    architecture_version  TEXT NOT NULL CHECK (architecture_version = 'dcp.wbc-integration-twin/v2'),
    stage                 INTEGER NOT NULL CHECK (stage = 4),
    adapter_activated     INTEGER NOT NULL CHECK (adapter_activated = 0),
    installed             INTEGER NOT NULL CHECK (installed = 0),
    created_at            TIMESTAMP NOT NULL
);
INSERT INTO dcp_v2_core_authority VALUES (
    'dcp-v2-core-stage4', '8be08577673722edc9ae036dedea46c88ceac129',
    'dcp.wbc-integration-twin/v2', 4, 0, 0, CURRENT_TIMESTAMP
);

CREATE TABLE dcp_v2_task (
    task_id                 TEXT PRIMARY KEY CHECK (length(task_id) BETWEEN 1 AND 64),
    target_spec_version     TEXT NOT NULL CHECK (length(target_spec_version) > 0),
    repository              TEXT NOT NULL CHECK (length(repository) > 0),
    repository_id           INTEGER NOT NULL CHECK (repository_id > 0),
    owner_id                INTEGER NOT NULL CHECK (owner_id > 0),
    base_ref                TEXT NOT NULL CHECK (length(base_ref) > 0),
    profile                 TEXT NOT NULL CHECK (profile IN ('repo-only','live-runtime')),
    request_digest          TEXT NOT NULL CHECK (length(request_digest) = 64),
    scope_digest            TEXT NOT NULL CHECK (length(scope_digest) = 64),
    policy_digest           TEXT NOT NULL CHECK (length(policy_digest) = 64),
    initial_worker_budget   INTEGER NOT NULL CHECK (initial_worker_budget = 1),
    repair_budget           INTEGER NOT NULL CHECK (repair_budget = 1),
    repair_used             INTEGER NOT NULL DEFAULT 0 CHECK (repair_used IN (0, 1)),
    max_readmissions        INTEGER NOT NULL CHECK (max_readmissions BETWEEN 0 AND 32),
    readmission_count       INTEGER NOT NULL DEFAULT 0 CHECK (readmission_count BETWEEN 0 AND max_readmissions),
    current_revision_id     TEXT NOT NULL CHECK (length(current_revision_id) > 0),
    state                   TEXT NOT NULL CHECK (state IN (
        'worker_queued','worker_running','checks_waiting','review_queued','review_running',
        'repair_queued','repair_running','arbiter_queued','arbiter_running','admission_waiting',
        'readmission','release_waiting','merge_observing','release_verified','deployment_waiting',
        'deployment_observing','human_gate','failed','merged','deployed'
    )),
    state_revision          INTEGER NOT NULL CHECK (state_revision >= 1),
    terminal_result_id      TEXT NOT NULL DEFAULT '',
    human_gate_question     TEXT NOT NULL DEFAULT '',
    error_code              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMP NOT NULL,
    updated_at              TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    CHECK ((state = 'human_gate') = (human_gate_question <> '')),
    CHECK ((state = 'failed') = (error_code <> '')),
    CHECK ((state IN ('merged','deployed')) = (terminal_result_id <> ''))
);

CREATE TABLE dcp_v2_revision (
    revision_id             TEXT PRIMARY KEY CHECK (length(revision_id) > 0),
    task_id                 TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    sequence                INTEGER NOT NULL CHECK (sequence >= 1),
    kind                    TEXT NOT NULL CHECK (kind IN ('work_input','worker_output','repair_output','readmission_output')),
    repository              TEXT NOT NULL CHECK (length(repository) > 0),
    base_ref                TEXT NOT NULL CHECK (length(base_ref) > 0),
    base_sha                TEXT NOT NULL CHECK (length(base_sha) = 40),
    head_ref                TEXT NOT NULL CHECK (length(head_ref) > 0),
    head_sha                TEXT NOT NULL CHECK (length(head_sha) = 40),
    predecessor_revision_id TEXT NOT NULL DEFAULT '',
    cause_command_id        TEXT NOT NULL DEFAULT '',
    pr_number               INTEGER NOT NULL DEFAULT 0 CHECK (pr_number >= 0),
    evidence_digest         TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    created_at              TIMESTAMP NOT NULL,
    UNIQUE (task_id, sequence),
    UNIQUE (task_id, head_sha, kind),
    UNIQUE (revision_id, task_id),
    CHECK ((sequence = 1) = (kind = 'work_input')),
    CHECK ((sequence = 1) = (predecessor_revision_id = '')),
    CHECK ((sequence = 1) = (cause_command_id = '')),
    CHECK (sequence <> 1 OR base_sha = head_sha)
);

CREATE TABLE dcp_v2_command (
    sequence                 INTEGER PRIMARY KEY AUTOINCREMENT,
    command_id               TEXT NOT NULL UNIQUE CHECK (length(command_id) > 0),
    task_id                  TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id              TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    kind                     TEXT NOT NULL CHECK (kind IN (
        'worker.execute/v1','checks.observe/v1','review.execute/v1','repair.execute/v1',
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
CREATE UNIQUE INDEX idx_dcp_v2_command_one_active_per_task
    ON dcp_v2_command (task_id) WHERE status IN ('pending','leased');
CREATE INDEX idx_dcp_v2_command_drain ON dcp_v2_command (status, sequence);

CREATE TABLE dcp_v2_action (
    sequence          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id         TEXT NOT NULL UNIQUE CHECK (length(action_id) > 0),
    command_id        TEXT NOT NULL UNIQUE REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    task_id           TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id       TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    role              TEXT NOT NULL CHECK (role IN ('worker','reviewer','repair','arbiter')),
    model             TEXT NOT NULL CHECK (length(model) > 0),
    reasoning         TEXT NOT NULL CHECK (length(reasoning) > 0),
    token_budget      INTEGER NOT NULL CHECK (token_budget > 0),
    time_budget_sec   INTEGER NOT NULL CHECK (time_budget_sec > 0),
    input_digest      TEXT NOT NULL CHECK (length(input_digest) = 64),
    attempt           INTEGER NOT NULL CHECK (attempt = 1),
    status            TEXT NOT NULL CHECK (status IN ('queued','launching','running','succeeded','failed')),
    slot              INTEGER NOT NULL DEFAULT 0 CHECK (slot BETWEEN 0 AND 3),
    launch_fence      TEXT NOT NULL DEFAULT '',
    runtime_id        TEXT NOT NULL DEFAULT '',
    result_digest     TEXT NOT NULL DEFAULT '' CHECK (result_digest = '' OR length(result_digest) = 64),
    error_code        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    FOREIGN KEY (command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK ((status IN ('launching','running')) = (slot BETWEEN 1 AND 3)),
    CHECK (status <> 'launching' OR launch_fence <> ''),
    CHECK (status <> 'running' OR (launch_fence <> '' AND runtime_id <> '')),
    CHECK (status <> 'succeeded' OR result_digest <> '')
);
CREATE UNIQUE INDEX idx_dcp_v2_action_one_slot
    ON dcp_v2_action (slot) WHERE status IN ('launching','running');
CREATE UNIQUE INDEX idx_dcp_v2_action_one_active_task
    ON dcp_v2_action (task_id) WHERE status IN ('launching','running');
CREATE UNIQUE INDEX idx_dcp_v2_action_one_initial_worker
    ON dcp_v2_action (task_id) WHERE role = 'worker';
CREATE UNIQUE INDEX idx_dcp_v2_action_one_repair
    ON dcp_v2_action (task_id) WHERE role = 'repair';
CREATE UNIQUE INDEX idx_dcp_v2_action_one_review_per_head
    ON dcp_v2_action (task_id, revision_id) WHERE role = 'reviewer';
CREATE INDEX idx_dcp_v2_action_fifo ON dcp_v2_action (status, sequence);

CREATE TABLE dcp_v2_admission (
    sequence           INTEGER PRIMARY KEY AUTOINCREMENT,
    admission_id       TEXT NOT NULL UNIQUE CHECK (length(admission_id) > 0),
    line_key           TEXT NOT NULL CHECK (length(line_key) > 0),
    task_id            TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id        TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    pr_number          INTEGER NOT NULL CHECK (pr_number > 0),
    head_sha           TEXT NOT NULL CHECK (length(head_sha) = 40),
    base_sha           TEXT NOT NULL CHECK (length(base_sha) = 40),
    main_sha           TEXT NOT NULL CHECK (length(main_sha) = 40),
    required_check_id  TEXT NOT NULL CHECK (length(required_check_id) > 0),
    review_id          TEXT NOT NULL CHECK (length(review_id) > 0),
    manifest_digest    TEXT NOT NULL CHECK (length(manifest_digest) = 64),
    status             TEXT NOT NULL CHECK (status IN ('waiting','leased','dispatched','readmission_required','succeeded','failed')),
    lease_owner        TEXT NOT NULL DEFAULT '',
    lease_epoch        TEXT NOT NULL DEFAULT '',
    lease_token        TEXT NOT NULL DEFAULT '',
    dispatch_fence     TEXT NOT NULL DEFAULT '',
    recovery_generation INTEGER NOT NULL DEFAULT 0 CHECK (recovery_generation BETWEEN 0 AND 32),
    result_id          TEXT NOT NULL DEFAULT '',
    error_code         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (task_id, revision_id),
    UNIQUE (line_key, manifest_digest),
    UNIQUE (admission_id, task_id, revision_id),
    FOREIGN KEY (revision_id, task_id) REFERENCES dcp_v2_revision (revision_id, task_id) ON DELETE RESTRICT,
    CHECK ((status = 'waiting') = (lease_owner = '' AND lease_epoch = '' AND lease_token = '' AND dispatch_fence = '')),
    CHECK (status <> 'leased' OR (lease_owner <> '' AND lease_epoch <> '' AND lease_token <> '')),
    CHECK (status <> 'dispatched' OR dispatch_fence <> ''),
    CHECK (status <> 'succeeded' OR result_id <> '')
);
CREATE UNIQUE INDEX idx_dcp_v2_admission_one_lease_per_line
    ON dcp_v2_admission (line_key) WHERE status IN ('leased','dispatched');
CREATE INDEX idx_dcp_v2_admission_fifo ON dcp_v2_admission (line_key, status, sequence);

CREATE TABLE dcp_v2_incident (
    incident_id       TEXT PRIMARY KEY CHECK (length(incident_id) > 0),
    task_id           TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id       TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    cause_command_id  TEXT NOT NULL REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    kind              TEXT NOT NULL CHECK (kind IN (
        'identity_drift','event_conflict','effect_reconciliation','model_runtime',
        'readmission_conflict','policy_ambiguity','provider_failure'
    )),
    evidence_digest   TEXT NOT NULL CHECK (length(evidence_digest) = 64),
    disposition       TEXT NOT NULL CHECK (disposition IN ('arbiter','human_gate','terminal')),
    owner_question    TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL,
    UNIQUE (task_id, revision_id, kind, evidence_digest),
    FOREIGN KEY (revision_id, task_id) REFERENCES dcp_v2_revision (revision_id, task_id) ON DELETE RESTRICT,
    FOREIGN KEY (cause_command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK ((disposition = 'human_gate') = (owner_question <> ''))
);

CREATE TABLE dcp_v2_external_event (
    delivery_id          TEXT PRIMARY KEY CHECK (length(delivery_id) > 0),
    provider             TEXT NOT NULL CHECK (length(provider) > 0),
    task_id              TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id          TEXT NOT NULL CHECK (length(revision_id) > 0),
    kind                 TEXT NOT NULL CHECK (length(kind) > 0),
    provider_sequence    INTEGER NOT NULL CHECK (provider_sequence >= 0),
    payload_digest       TEXT NOT NULL CHECK (length(payload_digest) = 64),
    prerequisite_digest  TEXT NOT NULL CHECK (length(prerequisite_digest) = 64),
    status               TEXT NOT NULL CHECK (status IN ('retained','applied','conflict')),
    command_id           TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (provider, task_id, kind, provider_sequence),
    CHECK (status <> 'applied' OR command_id <> '')
);

CREATE TABLE dcp_v2_result (
    result_id         TEXT PRIMARY KEY CHECK (length(result_id) > 0),
    task_id           TEXT NOT NULL REFERENCES dcp_v2_task (task_id) ON DELETE RESTRICT,
    revision_id       TEXT NOT NULL REFERENCES dcp_v2_revision (revision_id) ON DELETE RESTRICT,
    admission_id      TEXT REFERENCES dcp_v2_admission (admission_id) ON DELETE RESTRICT,
    command_id        TEXT NOT NULL REFERENCES dcp_v2_command (command_id) ON DELETE RESTRICT,
    kind              TEXT NOT NULL CHECK (kind IN ('release','deployment','failure')),
    provider          TEXT NOT NULL DEFAULT '',
    proof_id          TEXT NOT NULL DEFAULT '',
    run_id            TEXT NOT NULL DEFAULT '',
    actor             TEXT NOT NULL DEFAULT '',
    manifest_digest   TEXT NOT NULL DEFAULT '' CHECK (manifest_digest = '' OR length(manifest_digest) = 64),
    proof_digest      TEXT NOT NULL UNIQUE CHECK (length(proof_digest) = 64),
    merge_sha         TEXT NOT NULL DEFAULT '' CHECK (merge_sha = '' OR length(merge_sha) = 40),
    artifact_digest   TEXT NOT NULL DEFAULT '' CHECK (artifact_digest = '' OR length(artifact_digest) = 64),
    deployed_sha      TEXT NOT NULL DEFAULT '' CHECK (deployed_sha = '' OR length(deployed_sha) = 40),
    environment       TEXT NOT NULL DEFAULT '',
    service           TEXT NOT NULL DEFAULT '',
    probe_digest      TEXT NOT NULL DEFAULT '' CHECK (probe_digest = '' OR length(probe_digest) = 64),
    verified          INTEGER NOT NULL CHECK (verified IN (0, 1)),
    error_code        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL,
    CHECK (kind <> 'deployment' OR verified = 0 OR (
        merge_sha <> '' AND artifact_digest <> '' AND deployed_sha = merge_sha
        AND environment <> '' AND service <> '' AND probe_digest <> ''
    )),
    FOREIGN KEY (command_id, task_id, revision_id) REFERENCES dcp_v2_command (command_id, task_id, revision_id) ON DELETE RESTRICT,
    FOREIGN KEY (admission_id, task_id, revision_id) REFERENCES dcp_v2_admission (admission_id, task_id, revision_id) ON DELETE RESTRICT,
    CHECK (kind <> 'release' OR verified = 0 OR (merge_sha <> '' AND artifact_digest <> '')),
    CHECK (verified = 0 OR (provider <> '' AND proof_id <> '' AND run_id <> '' AND actor <> '' AND manifest_digest <> '')),
    CHECK (verified = 0 OR error_code = '')
);

-- Immutable identities and guarded one-way transitions.
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_core_authority_no_update BEFORE UPDATE ON dcp_v2_core_authority
BEGIN SELECT RAISE(ABORT, 'DCP v2 Stage 4 authority is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_core_authority_no_delete BEFORE DELETE ON dcp_v2_core_authority
BEGIN SELECT RAISE(ABORT, 'DCP v2 Stage 4 authority cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_task_guard BEFORE UPDATE ON dcp_v2_task
WHEN OLD.task_id <> NEW.task_id OR OLD.target_spec_version <> NEW.target_spec_version
 OR OLD.repository <> NEW.repository OR OLD.repository_id <> NEW.repository_id OR OLD.owner_id <> NEW.owner_id
 OR OLD.base_ref <> NEW.base_ref OR OLD.profile <> NEW.profile OR OLD.request_digest <> NEW.request_digest
 OR OLD.scope_digest <> NEW.scope_digest OR OLD.policy_digest <> NEW.policy_digest
 OR OLD.initial_worker_budget <> NEW.initial_worker_budget OR OLD.repair_budget <> NEW.repair_budget
 OR OLD.created_at <> NEW.created_at OR OLD.state IN ('failed','merged','deployed')
 OR NEW.state_revision <> OLD.state_revision + 1 OR NEW.repair_used < OLD.repair_used
 OR NEW.repair_used > OLD.repair_used + 1 OR NEW.readmission_count < OLD.readmission_count
 OR NEW.readmission_count > OLD.readmission_count + 1 OR NEW.updated_at < OLD.updated_at
BEGIN SELECT RAISE(ABORT, 'DCP v2 Task identity or transition violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_revision_no_update BEFORE UPDATE ON dcp_v2_revision
BEGIN SELECT RAISE(ABORT, 'DCP v2 Revision is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_revision_no_delete BEFORE DELETE ON dcp_v2_revision
BEGIN SELECT RAISE(ABORT, 'DCP v2 Revision cannot be deleted'); END;
-- +goose StatementEnd
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
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_action_guard BEFORE UPDATE ON dcp_v2_action
WHEN OLD.sequence <> NEW.sequence OR OLD.action_id <> NEW.action_id OR OLD.command_id <> NEW.command_id
 OR OLD.task_id <> NEW.task_id OR OLD.revision_id <> NEW.revision_id OR OLD.role <> NEW.role
 OR OLD.model <> NEW.model OR OLD.reasoning <> NEW.reasoning OR OLD.token_budget <> NEW.token_budget
 OR OLD.time_budget_sec <> NEW.time_budget_sec OR OLD.input_digest <> NEW.input_digest
 OR OLD.attempt <> NEW.attempt OR OLD.created_at <> NEW.created_at OR NEW.updated_at < OLD.updated_at
 OR NOT ((OLD.status = 'queued' AND NEW.status = 'launching')
      OR (OLD.status = 'launching' AND NEW.status IN ('running','failed'))
      OR (OLD.status = 'running' AND NEW.status IN ('succeeded','failed')))
BEGIN SELECT RAISE(ABORT, 'DCP v2 Action identity or transition violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_admission_guard BEFORE UPDATE ON dcp_v2_admission
WHEN OLD.sequence <> NEW.sequence OR OLD.admission_id <> NEW.admission_id OR OLD.line_key <> NEW.line_key
 OR OLD.task_id <> NEW.task_id OR OLD.revision_id <> NEW.revision_id OR OLD.pr_number <> NEW.pr_number
 OR OLD.head_sha <> NEW.head_sha OR OLD.base_sha <> NEW.base_sha OR OLD.main_sha <> NEW.main_sha
 OR OLD.required_check_id <> NEW.required_check_id OR OLD.review_id <> NEW.review_id
 OR OLD.manifest_digest <> NEW.manifest_digest OR OLD.created_at <> NEW.created_at OR NEW.updated_at < OLD.updated_at
 OR NOT ((OLD.status = 'waiting' AND NEW.status = 'leased'
           AND NEW.recovery_generation = 0 AND NEW.dispatch_fence = '')
      OR (OLD.status = 'leased' AND NEW.status = 'leased' AND (
           (OLD.dispatch_fence = '' AND NEW.dispatch_fence <> '' AND NEW.recovery_generation = OLD.recovery_generation
             AND NEW.lease_owner = OLD.lease_owner AND NEW.lease_epoch = OLD.lease_epoch AND NEW.lease_token = OLD.lease_token)
           OR (OLD.dispatch_fence = '' AND NEW.dispatch_fence = '' AND NEW.recovery_generation = OLD.recovery_generation + 1)
         ))
      OR (OLD.status = 'leased' AND NEW.status IN ('dispatched','readmission_required','failed')
           AND NEW.lease_owner = OLD.lease_owner AND NEW.lease_epoch = OLD.lease_epoch AND NEW.lease_token = OLD.lease_token
           AND NEW.dispatch_fence = OLD.dispatch_fence AND NEW.recovery_generation = OLD.recovery_generation)
      OR (OLD.status = 'dispatched' AND NEW.status IN ('readmission_required','succeeded','failed')
           AND NEW.lease_owner = OLD.lease_owner AND NEW.lease_epoch = OLD.lease_epoch AND NEW.lease_token = OLD.lease_token
           AND NEW.dispatch_fence = OLD.dispatch_fence AND NEW.recovery_generation = OLD.recovery_generation))
BEGIN SELECT RAISE(ABORT, 'DCP v2 Admission identity or transition violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_external_event_guard BEFORE UPDATE ON dcp_v2_external_event
WHEN OLD.delivery_id <> NEW.delivery_id OR OLD.provider <> NEW.provider OR OLD.task_id <> NEW.task_id
 OR OLD.revision_id <> NEW.revision_id OR OLD.kind <> NEW.kind OR OLD.provider_sequence <> NEW.provider_sequence
 OR OLD.payload_digest <> NEW.payload_digest OR OLD.prerequisite_digest <> NEW.prerequisite_digest
 OR OLD.created_at <> NEW.created_at OR NEW.updated_at < OLD.updated_at
 OR NOT (OLD.status = 'retained' AND NEW.status IN ('applied','conflict'))
BEGIN SELECT RAISE(ABORT, 'DCP v2 provider event identity or transition violated'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_result_no_update BEFORE UPDATE ON dcp_v2_result
BEGIN SELECT RAISE(ABORT, 'DCP v2 Result is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_result_no_delete BEFORE DELETE ON dcp_v2_result
BEGIN SELECT RAISE(ABORT, 'DCP v2 Result cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_incident_no_update BEFORE UPDATE ON dcp_v2_incident
BEGIN SELECT RAISE(ABORT, 'DCP v2 Incident is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER dcp_v2_incident_no_delete BEFORE DELETE ON dcp_v2_incident
BEGIN SELECT RAISE(ABORT, 'DCP v2 Incident cannot be deleted'); END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0084 DCP v2 Stage 4 core is forward-only');
