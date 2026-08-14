-- +goose Up
-- Preserve the exact Scenario-A generation-2 false launch failure before one
-- model-free validation of its unchanged successful result. This grants zero
-- model calls and cannot create another incident generation.
CREATE TABLE dcp_future_card_arbiter_result_validation_recovery_v1 (
    recovery_id              TEXT PRIMARY KEY CHECK (recovery_id = 'dcp-future-arbiter-result-recovery-9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'),
    incident_id              TEXT NOT NULL UNIQUE REFERENCES dcp_future_card_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    identity_digest          TEXT NOT NULL CHECK (identity_digest = '9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'),
    input_digest             TEXT NOT NULL CHECK (input_digest = '73ca0795f7905293141988fda3b899ab630d6b9a7c7683fa5b11eab2abddbab9'),
    model_action_id          TEXT NOT NULL UNIQUE REFERENCES dcp_model_action (id) ON DELETE RESTRICT,
    prior_status             TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code         TEXT NOT NULL CHECK (prior_error_code = 'launch_failed'),
    prior_finished_at        TIMESTAMP NOT NULL,
    prior_model_call_count   INTEGER NOT NULL CHECK (prior_model_call_count = 1),
    prior_decision_digest    TEXT NOT NULL CHECK (prior_decision_digest = ''),
    runtime_handle_id        TEXT NOT NULL CHECK (runtime_handle_id = incident_id),
    physical_runtime_handle  TEXT NOT NULL CHECK (physical_runtime_handle = 'dcp-future-arbiter-9e94bbd542baf-631f35f9'),
    input_artifact_digest    TEXT NOT NULL CHECK (input_artifact_digest = '2aa161bb06c7173ad93d72df1ef7fe8ec25947946c53e8907e1d7034251e9ce5'),
    input_artifact_size      INTEGER NOT NULL CHECK (input_artifact_size = 3799),
    schema_artifact_digest   TEXT NOT NULL CHECK (schema_artifact_digest = '0518e24e8b73dbef8cabf068d358e3744bfa581e2b12d1040883c093f13e7c0f'),
    schema_artifact_size     INTEGER NOT NULL CHECK (schema_artifact_size = 1575),
    result_artifact_digest   TEXT NOT NULL CHECK (result_artifact_digest = 'b8d34711413d429d2ae75eccd078c58a6ece778a4b0ad7d606361ce30a51d36d'),
    result_artifact_size     INTEGER NOT NULL CHECK (result_artifact_size = 1158),
    codex_session_id         TEXT NOT NULL CHECK (codex_session_id = '01a002a6-56e1-7781-917b-ff5640953091'),
    inference_tokens         INTEGER NOT NULL CHECK (inference_tokens = 10569),
    contract_commit          TEXT NOT NULL CHECK (contract_commit = 'd8660bb43f15e95d1850cd26a553e30438062c1f'),
    status                   TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'failed')),
    error_code               TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMP NOT NULL,
    finished_at              TIMESTAMP,
    CHECK ((status = 'pending') = (finished_at IS NULL)),
    CHECK ((status IN ('pending', 'applied')) = (error_code = ''))
);

INSERT INTO dcp_future_card_arbiter_result_validation_recovery_v1 (
    recovery_id, incident_id, identity_digest, input_digest, model_action_id,
    prior_status, prior_error_code, prior_finished_at, prior_model_call_count,
    prior_decision_digest, runtime_handle_id, physical_runtime_handle,
    input_artifact_digest, input_artifact_size,
    schema_artifact_digest, schema_artifact_size,
    result_artifact_digest, result_artifact_size,
    codex_session_id, inference_tokens, contract_commit, status, created_at
)
SELECT
    'dcp-future-arbiter-result-recovery-9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10',
    arb.incident_id, arb.identity_digest, arb.input_digest, arb.model_action_id,
    arb.status, arb.error_code, arb.finished_at, arb.model_call_count,
    arb.decision_digest, arb.runtime_handle_id,
    'dcp-future-arbiter-9e94bbd542baf-631f35f9',
    '2aa161bb06c7173ad93d72df1ef7fe8ec25947946c53e8907e1d7034251e9ce5', 3799,
    '0518e24e8b73dbef8cabf068d358e3744bfa581e2b12d1040883c093f13e7c0f', 1575,
    'b8d34711413d429d2ae75eccd078c58a6ece778a4b0ad7d606361ce30a51d36d', 1158,
    '01a002a6-56e1-7781-917b-ff5640953091', 10569,
    'd8660bb43f15e95d1850cd26a553e30438062c1f', 'pending', CURRENT_TIMESTAMP
FROM dcp_future_card_arbiter_v1 arb
JOIN dcp_model_action action ON action.id = arb.model_action_id
JOIN dcp_future_card_arbiter_schema_recovery_v1 schema_recovery
  ON schema_recovery.successor_incident_id = arb.incident_id
WHERE arb.incident_id = 'dcp-future-arbiter-9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'
  AND arb.generation = 2
  AND arb.identity_digest = '9e94bbd542bafa1c1d3fd37ca4c1429dcf0aed444b71f07a6645655155cbcd10'
  AND arb.task_id = 'arb-a-second'
  AND arb.session_id = 'dcp-review-lab-22'
  AND arb.admission_id = 'dcp-admission-f3e6b021-dc04-494e-acae-57d0a8b76404'
  AND arb.admission_sequence = 14
  AND arb.candidate_head_sha = '8b3f601ae7b82b68bfd3f3810069c7a91774ca72'
  AND arb.current_main_sha = '55e0c64b67560dc075d12a3dbc45a3d0674f405c'
  AND arb.input_digest = '73ca0795f7905293141988fda3b899ab630d6b9a7c7683fa5b11eab2abddbab9'
  AND arb.model_action_id = 'dcp-model-arb-a-second-arbiter-2'
  AND arb.runtime_handle_id = arb.incident_id
  AND arb.status = 'failed' AND arb.error_code = 'launch_failed'
  AND arb.model_call_count = 1 AND arb.decision_json = '' AND arb.decision_digest = ''
  AND arb.finished_at = '2026-08-14 23:40:49.515941 +0000 UTC'
  AND action.task_id = arb.task_id AND action.session_id = arb.session_id
  AND action.kind = 'arbiter' AND action.incident_id = arb.incident_id
  AND action.status = 'failed' AND action.error_code = 'launch_failed'
  AND action.slot = 0 AND action.launch_id = arb.incident_id
  AND schema_recovery.status = 'consumed'
  AND schema_recovery.predecessor_incident_id = 'dcp-future-arbiter-141e3d64af9568aea9ea1fb6835045060dfd566bc3b21d50ff6f3f90f3f67a52'
  AND schema_recovery.successor_generation = 2
  AND schema_recovery.provider_inference_tokens = 0;

-- +goose StatementBegin
CREATE TRIGGER dcp_future_card_arbiter_result_validation_recovery_immutable
BEFORE UPDATE ON dcp_future_card_arbiter_result_validation_recovery_v1
WHEN OLD.recovery_id <> NEW.recovery_id
  OR OLD.incident_id <> NEW.incident_id
  OR OLD.identity_digest <> NEW.identity_digest
  OR OLD.input_digest <> NEW.input_digest
  OR OLD.model_action_id <> NEW.model_action_id
  OR OLD.prior_status <> NEW.prior_status
  OR OLD.prior_error_code <> NEW.prior_error_code
  OR OLD.prior_finished_at <> NEW.prior_finished_at
  OR OLD.prior_model_call_count <> NEW.prior_model_call_count
  OR OLD.prior_decision_digest <> NEW.prior_decision_digest
  OR OLD.runtime_handle_id <> NEW.runtime_handle_id
  OR OLD.physical_runtime_handle <> NEW.physical_runtime_handle
  OR OLD.input_artifact_digest <> NEW.input_artifact_digest
  OR OLD.input_artifact_size <> NEW.input_artifact_size
  OR OLD.schema_artifact_digest <> NEW.schema_artifact_digest
  OR OLD.schema_artifact_size <> NEW.schema_artifact_size
  OR OLD.result_artifact_digest <> NEW.result_artifact_digest
  OR OLD.result_artifact_size <> NEW.result_artifact_size
  OR OLD.codex_session_id <> NEW.codex_session_id
  OR OLD.inference_tokens <> NEW.inference_tokens
  OR OLD.contract_commit <> NEW.contract_commit
  OR OLD.created_at <> NEW.created_at
  OR NOT (OLD.status = 'pending' AND NEW.status IN ('applied', 'failed'))
BEGIN
    SELECT RAISE(ABORT, 'DCP future arbiter result recovery is immutable');
END;
-- +goose StatementEnd

-- +goose Down
SELECT RAISE(ABORT, '0071 future-card arbiter result recovery is immutable evidence');
