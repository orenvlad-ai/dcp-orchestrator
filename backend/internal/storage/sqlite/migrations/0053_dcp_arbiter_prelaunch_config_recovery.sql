-- +goose Up
-- The first exact Stage 2 package used a top-level rollout_budget.* config.
-- Codex 0.145 rejected that shape during strict local configuration parsing,
-- before it created a session or provider request. Preserve that one failed
-- launch as immutable audit evidence and re-arm only the same incident and
-- generation. This migration runs once; it is not a retry policy.
CREATE TABLE dcp_review_lab_arbiter_v1_prelaunch_recovery (
    incident_id                 TEXT PRIMARY KEY REFERENCES dcp_review_lab_arbiter_v1 (incident_id) ON DELETE RESTRICT,
    generation                  INTEGER NOT NULL CHECK (generation = 1),
    identity_digest             TEXT NOT NULL CHECK (length(identity_digest) = 64),
    input_digest                TEXT NOT NULL CHECK (length(input_digest) = 64),
    prior_status                TEXT NOT NULL CHECK (prior_status = 'failed'),
    prior_model_call_count      INTEGER NOT NULL CHECK (prior_model_call_count = 1),
    prior_error_code            TEXT NOT NULL CHECK (prior_error_code = 'child_failed'),
    prior_finished_at           TIMESTAMP NOT NULL,
    failed_launcher_source_sha  TEXT NOT NULL CHECK (failed_launcher_source_sha = 'd5f9fd4b3459596fcb2d79efc0023bad4f7f0aa0'),
    correction_contract_sha     TEXT NOT NULL CHECK (correction_contract_sha = '4d3e0736635579db053516813e2d5944f903f777'),
    recovery_reason             TEXT NOT NULL CHECK (recovery_reason = 'strict_config_top_level_rollout_budget_rejected'),
    rearmed_at                  TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_review_lab_arbiter_v1_one_prelaunch_recovery
    ON dcp_review_lab_arbiter_v1_prelaunch_recovery ((1));

INSERT INTO dcp_review_lab_arbiter_v1_prelaunch_recovery (
    incident_id, generation, identity_digest, input_digest, prior_status,
    prior_model_call_count, prior_error_code, prior_finished_at,
    failed_launcher_source_sha, correction_contract_sha, recovery_reason,
    rearmed_at
)
SELECT
    incident_id, generation, identity_digest, input_digest, status,
    model_call_count, error_code, finished_at,
    'd5f9fd4b3459596fcb2d79efc0023bad4f7f0aa0',
    '4d3e0736635579db053516813e2d5944f903f777',
    'strict_config_top_level_rollout_budget_rejected', CURRENT_TIMESTAMP
FROM dcp_review_lab_arbiter_v1
WHERE generation = 1
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
  AND runtime_handle_id = 'dcp-global-release-arbiter-v1';

UPDATE dcp_review_lab_arbiter_v1
SET status = 'requested', model_call_count = 0, error_code = '',
    finished_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE status = 'failed'
  AND model_call_count = 1
  AND error_code = 'child_failed'
  AND EXISTS (
    SELECT 1
    FROM dcp_review_lab_arbiter_v1_prelaunch_recovery r
    WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
      AND r.identity_digest = dcp_review_lab_arbiter_v1.identity_digest
      AND r.input_digest = dcp_review_lab_arbiter_v1.input_digest
  );

-- +goose Down
UPDATE dcp_review_lab_arbiter_v1
SET status = 'failed', model_call_count = 1,
    error_code = (
        SELECT prior_error_code
        FROM dcp_review_lab_arbiter_v1_prelaunch_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    ),
    finished_at = (
        SELECT prior_finished_at
        FROM dcp_review_lab_arbiter_v1_prelaunch_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    ),
    updated_at = (
        SELECT prior_finished_at
        FROM dcp_review_lab_arbiter_v1_prelaunch_recovery r
        WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
    )
WHERE status = 'requested'
  AND model_call_count = 0
  AND decision_json = ''
  AND EXISTS (
    SELECT 1
    FROM dcp_review_lab_arbiter_v1_prelaunch_recovery r
    WHERE r.incident_id = dcp_review_lab_arbiter_v1.incident_id
  );

DROP TABLE dcp_review_lab_arbiter_v1_prelaunch_recovery;
