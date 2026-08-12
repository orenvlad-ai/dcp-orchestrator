-- +goose Up
-- The exact fbcf4929 package failed closed before the worker call fence because
-- its conflict preflight required an added path relative to current main. The
-- exact frozen topology instead has that path on both current main and the old
-- candidate with different bytes, so the required current-main-to-candidate
-- status is modified. Preserve the zero-call failure and re-arm only the same
-- generation-1 row once. This is not a worker retry or additional budget.
CREATE TABLE dcp_card12_fresh_worker_preflight_recovery (
    recovery_id                 TEXT PRIMARY KEY REFERENCES dcp_review_lab_card12_fresh_worker_recovery (recovery_id) ON DELETE RESTRICT,
    recovery_generation         INTEGER NOT NULL CHECK (recovery_generation = 1),
    recovery_identity_digest    TEXT NOT NULL CHECK (recovery_identity_digest = 'd2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'),
    prior_status                TEXT NOT NULL CHECK (prior_status = 'preflight_failed'),
    prior_error_code            TEXT NOT NULL CHECK (prior_error_code = 'identity_drift'),
    prior_revision              INTEGER NOT NULL CHECK (prior_revision = 1),
    prior_worker_calls          INTEGER NOT NULL CHECK (prior_worker_calls = 0),
    prior_reviewer_calls        INTEGER NOT NULL CHECK (prior_reviewer_calls = 0),
    prior_launch_id             TEXT NOT NULL CHECK (prior_launch_id = ''),
    prior_worker_session_id     TEXT NOT NULL CHECK (prior_worker_session_id = ''),
    prior_worker_tokens         INTEGER NOT NULL CHECK (prior_worker_tokens = 0),
    prior_input_digest          TEXT NOT NULL CHECK (prior_input_digest = ''),
    prior_result_digest         TEXT NOT NULL CHECK (prior_result_digest = ''),
    prior_log_digest            TEXT NOT NULL CHECK (prior_log_digest = ''),
    prior_new_head              TEXT NOT NULL CHECK (prior_new_head = ''),
    prior_review_run_id         TEXT NOT NULL CHECK (prior_review_run_id = ''),
    prior_merge_commit_sha      TEXT NOT NULL CHECK (prior_merge_commit_sha = ''),
    prior_authorized_at         TIMESTAMP NOT NULL,
    prior_updated_at            TIMESTAMP NOT NULL,
    prior_finished_at           TIMESTAMP NOT NULL,
    failed_source_sha           TEXT NOT NULL CHECK (failed_source_sha = 'fbcf4929f9192f7cce9c5097b0bc6a449d28e663'),
    authority_contract_sha      TEXT NOT NULL CHECK (authority_contract_sha = '2a174899ae72bf1db548c3b2f172d963488191f1'),
    observed_diff_status        TEXT NOT NULL CHECK (observed_diff_status = 'M'),
    recovery_reason             TEXT NOT NULL CHECK (recovery_reason = 'exact_conflict_path_is_modified_from_current_main'),
    rearmed_at                  TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_dcp_card12_one_fresh_worker_preflight_recovery
    ON dcp_card12_fresh_worker_preflight_recovery ((1));

INSERT INTO dcp_card12_fresh_worker_preflight_recovery (
    recovery_id, recovery_generation, recovery_identity_digest, prior_status,
    prior_error_code, prior_revision, prior_worker_calls,
    prior_reviewer_calls, prior_launch_id, prior_worker_session_id,
    prior_worker_tokens, prior_input_digest, prior_result_digest,
    prior_log_digest, prior_new_head, prior_review_run_id,
    prior_merge_commit_sha, prior_authorized_at, prior_updated_at,
    prior_finished_at, failed_source_sha, authority_contract_sha,
    observed_diff_status, recovery_reason, rearmed_at
)
SELECT
    recovery_id, recovery_generation, recovery_identity_digest, status,
    error_code, revision, worker_model_call_count, reviewer_model_call_count,
    launch_id, worker_codex_session_id, worker_token_count, input_digest,
    worker_result_digest, worker_log_digest, new_head,
    recovery_review_run_id, merge_commit_sha, authorized_at, updated_at,
    finished_at, 'fbcf4929f9192f7cce9c5097b0bc6a449d28e663',
    '2a174899ae72bf1db548c3b2f172d963488191f1', 'M',
    'exact_conflict_path_is_modified_from_current_main', CURRENT_TIMESTAMP
FROM dcp_review_lab_card12_fresh_worker_recovery
WHERE recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND recovery_generation = 1
  AND recovery_identity_digest = 'd2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND incident_id = 'dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60'
  AND successor_attempt_id = 'dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d'
  AND accepted_decision_digest = '237472879b22a8db65c5a3a0715510dc17aee1de93c45eaab45dde538cefb939'
  AND admission_id = 'dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND session_id = 'dcp-review-lab-12' AND task_id = 'i13-arbiter-b'
  AND source_branch = 'ao/dcp-review-lab-12/root' AND pr_number = 9
  AND old_head = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND current_main = 'b34b31b5443890e69128db2862726950a6bbac0d'
  AND predecessor_status = 'failed' AND predecessor_error = 'repair_launch_failed'
  AND old_agent_session_id = '' AND old_runtime_launch_id = ''
  AND contract_commit = '2a174899ae72bf1db548c3b2f172d963488191f1'
  AND status = 'preflight_failed' AND error_code = 'identity_drift'
  AND revision = 1 AND worker_model_call_count = 0
  AND reviewer_model_call_count = 0 AND launch_id = ''
  AND worker_codex_session_id = '' AND worker_token_count = 0
  AND input_json = '' AND input_digest = '' AND result_path = ''
  AND log_path = '' AND worker_result_digest = '' AND worker_log_digest = ''
  AND new_head = '' AND new_commit = '' AND recovery_review_run_id = ''
  AND merge_commit_sha = '' AND finished_at IS NOT NULL;

UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'authorized', error_code = '', finished_at = NULL,
    revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND status = 'preflight_failed' AND error_code = 'identity_drift'
  AND worker_model_call_count = 0 AND reviewer_model_call_count = 0
  AND EXISTS (
    SELECT 1
    FROM dcp_card12_fresh_worker_preflight_recovery audit
    WHERE audit.recovery_id = dcp_review_lab_card12_fresh_worker_recovery.recovery_id
      AND audit.recovery_identity_digest = dcp_review_lab_card12_fresh_worker_recovery.recovery_identity_digest
  );

-- +goose Down
CREATE TABLE dcp_card12_fresh_worker_preflight_rollback_guard (
    eligible_rows INTEGER NOT NULL CHECK (eligible_rows = 1),
    status TEXT NOT NULL CHECK (status = 'authorized'),
    worker_calls INTEGER NOT NULL CHECK (worker_calls = 0),
    reviewer_calls INTEGER NOT NULL CHECK (reviewer_calls = 0),
    launch_id TEXT NOT NULL CHECK (launch_id = ''),
    new_head TEXT NOT NULL CHECK (new_head = '')
);
INSERT INTO dcp_card12_fresh_worker_preflight_rollback_guard
SELECT count(*), coalesce(max(worker.status), ''),
       coalesce(max(worker.worker_model_call_count), -1),
       coalesce(max(worker.reviewer_model_call_count), -1),
       coalesce(max(worker.launch_id), 'foreign'),
       coalesce(max(worker.new_head), 'foreign')
FROM dcp_review_lab_card12_fresh_worker_recovery worker
JOIN dcp_card12_fresh_worker_preflight_recovery audit
  ON audit.recovery_id = worker.recovery_id
WHERE worker.status = 'authorized' AND worker.worker_model_call_count = 0
  AND worker.reviewer_model_call_count = 0 AND worker.launch_id = ''
  AND worker.new_head = '';

UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'preflight_failed', error_code = 'identity_drift',
    revision = (
        SELECT prior_revision
        FROM dcp_card12_fresh_worker_preflight_recovery audit
        WHERE audit.recovery_id = dcp_review_lab_card12_fresh_worker_recovery.recovery_id
    ),
    updated_at = (
        SELECT prior_updated_at
        FROM dcp_card12_fresh_worker_preflight_recovery audit
        WHERE audit.recovery_id = dcp_review_lab_card12_fresh_worker_recovery.recovery_id
    ),
    finished_at = (
        SELECT prior_finished_at
        FROM dcp_card12_fresh_worker_preflight_recovery audit
        WHERE audit.recovery_id = dcp_review_lab_card12_fresh_worker_recovery.recovery_id
    )
WHERE status = 'authorized' AND worker_model_call_count = 0
  AND reviewer_model_call_count = 0 AND launch_id = '' AND new_head = ''
  AND EXISTS (
    SELECT 1
    FROM dcp_card12_fresh_worker_preflight_recovery audit
    WHERE audit.recovery_id = dcp_review_lab_card12_fresh_worker_recovery.recovery_id
  );

DROP TABLE dcp_card12_fresh_worker_preflight_recovery;
DROP TABLE dcp_card12_fresh_worker_preflight_rollback_guard;
