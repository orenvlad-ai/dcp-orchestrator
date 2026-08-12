-- +goose Up
-- One exact model-free correction for the generation-2 result validator. The
-- successor call/result are not re-armed or replaced; prior failed state is
-- copied into this immutable audit row before one exact startup validation.
CREATE TABLE dcp_arbiter_successor_result_validation_recovery (
    attempt_id                 TEXT PRIMARY KEY CHECK (attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    incident_id                TEXT NOT NULL CHECK (incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    attempt_generation         INTEGER NOT NULL CHECK (attempt_generation = 2),
    attempt_identity_digest    TEXT NOT NULL CHECK (attempt_identity_digest = '3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'),
    input_digest               TEXT NOT NULL CHECK (input_digest = 'aa44c625c940048d5e0266dac23dd4835a1afcf7648116a056758093b67160e6'),
    prior_status               TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_error_code           TEXT NOT NULL CHECK (prior_error_code = 'submit_failed'),
    prior_finished_at          TIMESTAMP NOT NULL,
    prior_model_call_count     INTEGER NOT NULL CHECK (prior_model_call_count = 1),
    prior_decision_digest      TEXT NOT NULL CHECK (prior_decision_digest = ''),
    prior_recovery_wake_count  INTEGER NOT NULL CHECK (prior_recovery_wake_count = 0),
    result_artifact_digest     TEXT NOT NULL CHECK (result_artifact_digest = '9b5ff7847db2533e56bdbbc424114e5bea8e5e3c352ad1d029a99deaba05c172'),
    result_artifact_size       INTEGER NOT NULL CHECK (result_artifact_size = 1705),
    merge_tree_evidence_digest TEXT NOT NULL CHECK (merge_tree_evidence_digest = 'a19c64060d0f41320b6bf652c47ff5c58810ebec0416d003963bc1b4fcdf524f'),
    codex_session_id           TEXT NOT NULL CHECK (codex_session_id = '019ff3a1-7f0e-79e2-baa5-cbaa1cc6fc37'),
    token_count                INTEGER NOT NULL CHECK (token_count = 12271),
    contract_commit            TEXT NOT NULL CHECK (contract_commit = '28546ce0cc2be84349221464c4938c98ed11d32a'),
    status                     TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'failed')),
    error_code                 TEXT NOT NULL DEFAULT '',
    created_at                 TIMESTAMP NOT NULL,
    finished_at                TIMESTAMP,
    CHECK ((status = 'pending') = (finished_at IS NULL)),
    FOREIGN KEY (attempt_id) REFERENCES dcp_review_lab_arbiter_v1_successor_attempt (attempt_id) ON DELETE RESTRICT
);

INSERT INTO dcp_arbiter_successor_result_validation_recovery (
    attempt_id, incident_id, attempt_generation, attempt_identity_digest,
    input_digest, prior_status, prior_error_code, prior_finished_at,
    prior_model_call_count, prior_decision_digest, prior_recovery_wake_count,
    result_artifact_digest, result_artifact_size,
    merge_tree_evidence_digest, codex_session_id, token_count,
    contract_commit, status, created_at
)
SELECT
    successor.attempt_id, successor.incident_id, successor.attempt_generation,
    successor.attempt_identity_digest, successor.input_digest,
    successor.status, successor.error_code, successor.finished_at,
    successor.model_call_count, successor.decision_digest,
    successor.recovery_wake_count,
    '9b5ff7847db2533e56bdbbc424114e5bea8e5e3c352ad1d029a99deaba05c172', 1705,
    'a19c64060d0f41320b6bf652c47ff5c58810ebec0416d003963bc1b4fcdf524f',
    '019ff3a1-7f0e-79e2-baa5-cbaa1cc6fc37', 12271,
    '28546ce0cc2be84349221464c4938c98ed11d32a', 'pending', CURRENT_TIMESTAMP
FROM dcp_review_lab_arbiter_v1_successor_attempt successor
JOIN dcp_review_lab_arbiter_v1 original ON original.incident_id = successor.incident_id
WHERE successor.attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'
  AND successor.attempt_generation = 2
  AND successor.attempt_identity_digest = '3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'
  AND successor.input_digest = 'aa44c625c940048d5e0266dac23dd4835a1afcf7648116a056758093b67160e6'
  AND successor.status = 'failed'
  AND successor.model_call_count = 1
  AND successor.decision_json = '' AND successor.decision_digest = ''
  AND successor.recovery_owner_session_id = '' AND successor.recovery_path = ''
  AND successor.recovery_wake_count = 0
  AND successor.error_code = 'submit_failed' AND successor.finished_at IS NOT NULL
  AND original.incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND original.status = 'failed' AND original.model_call_count = 1
  AND original.decision_json = '' AND original.decision_digest = ''
  AND original.recovery_wake_count = 0 AND original.error_code = 'submit_failed';

-- +goose Down
CREATE TABLE dcp_arbiter_successor_validation_rollback_guard (
    status TEXT NOT NULL CHECK (status = 'pending')
);
INSERT INTO dcp_arbiter_successor_validation_rollback_guard (status)
SELECT status FROM dcp_arbiter_successor_result_validation_recovery;
DROP TABLE dcp_arbiter_successor_result_validation_recovery;
DROP TABLE dcp_arbiter_successor_validation_rollback_guard;
