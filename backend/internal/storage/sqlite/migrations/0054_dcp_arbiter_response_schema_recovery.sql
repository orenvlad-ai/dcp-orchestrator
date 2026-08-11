-- +goose Up
-- The exact 2fbd9bf4 package passed strict local config and reached Codex, but
-- the provider rejected the response schema's unsupported root oneOf before
-- inference, result output, or token use. Preserve that exact stopped attempt
-- and re-arm only the same incident/generation. This migration runs once and
-- is the final bounded pre-inference correction, not a retry policy.
CREATE TABLE dcp_review_lab_arbiter_v1_schema_recovery (
    incident_id                 TEXT PRIMARY KEY REFERENCES dcp_review_lab_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    generation                  INTEGER NOT NULL CHECK (generation = 1),
    identity_digest             TEXT NOT NULL CHECK (identity_digest = '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'),
    input_digest                TEXT NOT NULL CHECK (input_digest = 'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'),
    prior_status                TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_model_call_count      INTEGER NOT NULL CHECK (prior_model_call_count = 1),
    prior_error_code            TEXT NOT NULL CHECK (prior_error_code = 'child_failed'),
    prior_finished_at           TIMESTAMP NOT NULL,
    failed_launcher_source_sha  TEXT NOT NULL CHECK (failed_launcher_source_sha = '2fbd9bf4789a5b388fb12c58d9347968ed06e6de'),
    correction_contract_sha     TEXT NOT NULL CHECK (correction_contract_sha = '3f3a3bb2c2e951cbf7a34da75d3cc3f09d906001'),
    codex_session_id            TEXT NOT NULL CHECK (codex_session_id = '019ff21d-4cde-72d1-b70d-49efd3cd1c17'),
    provider_error_code         TEXT NOT NULL CHECK (provider_error_code = 'invalid_json_schema'),
    result_artifact_present     INTEGER NOT NULL CHECK (result_artifact_present = 0),
    token_record_present        INTEGER NOT NULL CHECK (token_record_present = 0),
    recovery_reason             TEXT NOT NULL CHECK (recovery_reason = 'unsupported_root_oneof_rejected_before_inference'),
    rearmed_at                  TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_review_lab_arbiter_v1_one_schema_recovery
    ON dcp_review_lab_arbiter_v1_schema_recovery ((1));

INSERT INTO dcp_review_lab_arbiter_v1_schema_recovery (
    incident_id, generation, identity_digest, input_digest, prior_status,
    prior_model_call_count, prior_error_code, prior_finished_at,
    failed_launcher_source_sha, correction_contract_sha, codex_session_id,
    provider_error_code, result_artifact_present, token_record_present,
    recovery_reason, rearmed_at
)
SELECT
    incident_id, generation, identity_digest, input_digest, status,
    model_call_count, error_code, finished_at,
    '2fbd9bf4789a5b388fb12c58d9347968ed06e6de',
    '3f3a3bb2c2e951cbf7a34da75d3cc3f09d906001',
    '019ff21d-4cde-72d1-b70d-49efd3cd1c17', 'invalid_json_schema', 0, 0,
    'unsupported_root_oneof_rejected_before_inference', CURRENT_TIMESTAMP
FROM dcp_review_lab_arbiter_v1
WHERE incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND generation = 1
  AND identity_digest = '2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND input_digest = 'f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49'
  AND source_packet_digest = 'fab52d627d14a21ea7ab2a7fdadb4d6f53478d5cdc496858ca74c37e1dfda057'
  AND task_id = 'i13-arbiter-b'
  AND session_id = 'dcp-review-lab-12'
  AND pr_number = 9
  AND target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND status = 'failed'
  AND model_call_count = 1
  AND error_code = 'child_failed'
  AND decision_json = ''
  AND decision_digest = ''
  AND recovery_wake_count = 0
  AND finished_at IS NOT NULL
  AND model = 'gpt-5.6-sol'
  AND reasoning = 'xhigh'
  AND token_budget = 16384
  AND runtime_handle_id = 'dcp-global-release-arbiter-v1'
  AND EXISTS (
    SELECT 1
    FROM dcp_review_lab_arbiter_v1_prelaunch_recovery p
    WHERE p.incident_id = dcp_review_lab_arbiter_v1.incident_id
      AND p.identity_digest = dcp_review_lab_arbiter_v1.identity_digest
      AND p.input_digest = dcp_review_lab_arbiter_v1.input_digest
  );

UPDATE dcp_review_lab_arbiter_v1
SET status = 'requested', model_call_count = 0, error_code = '',
    finished_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE status = 'failed'
  AND model_call_count = 1
  AND error_code = 'child_failed'
  AND EXISTS (
    SELECT 1
    FROM dcp_review_lab_arbiter_v1_schema_recovery r
    WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
      AND r.identity_digest = dcp_review_lab_arbiter_v1.identity_digest
      AND r.input_digest = dcp_review_lab_arbiter_v1.input_digest
  );

-- +goose Down
UPDATE dcp_review_lab_arbiter_v1
SET status = 'failed', model_call_count = 1,
    error_code = (
        SELECT prior_error_code
        FROM dcp_review_lab_arbiter_v1_schema_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    ),
    finished_at = (
        SELECT prior_finished_at
        FROM dcp_review_lab_arbiter_v1_schema_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    ),
    updated_at = (
        SELECT prior_finished_at
        FROM dcp_review_lab_arbiter_v1_schema_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    )
WHERE status = 'requested'
  AND model_call_count = 0
  AND decision_json = ''
  AND EXISTS (
    SELECT 1
    FROM dcp_review_lab_arbiter_v1_schema_recovery r
    WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
  );

DROP TABLE dcp_review_lab_arbiter_v1_schema_recovery;
