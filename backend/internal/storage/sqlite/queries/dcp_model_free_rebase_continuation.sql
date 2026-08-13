-- name: GetDCPCard12ModelFreeRebaseContinuation :one
SELECT * FROM dcp_review_lab_card12_model_free_rebase_continuation
WHERE continuation_id = ?;

-- name: ListDCPCard12ModelFreeRebaseContinuations :many
SELECT * FROM dcp_review_lab_card12_model_free_rebase_continuation
ORDER BY authorized_at;

-- name: GetDCPCard12ModelFreeRebaseDurableCounts :one
SELECT
  (SELECT count(*) FROM sessions) AS session_count,
  (SELECT count(*) FROM review_run) AS review_run_count,
  (SELECT count(*) FROM dcp_review_lab_admission) AS admission_count,
  (SELECT count(*) FROM dcp_review_lab_arbiter_v1) AS incident_count,
  (SELECT count(*) FROM dcp_review_lab_arbiter_v1_successor_attempt) AS successor_count,
  (SELECT count(*) FROM dcp_review_lab_card12_fresh_worker_recovery) AS fresh_worker_count,
  (SELECT count(*) FROM dcp_card12_fresh_worker_preflight_recovery) AS preflight_audit_count,
  (SELECT count(*) FROM dcp_review_lab_card12_model_free_rebase_continuation) AS continuation_count;

-- name: GetDCPCard12ModelFreeProviderBaseCorrectionCount :one
SELECT count(*)
FROM dcp_review_lab_card12_model_free_provider_base_correction
WHERE correction_id = 'dcp-card12-model-free-provider-base-correction-25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59'
  AND generation = 1
  AND identity_digest = '25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59'
  AND contract_commit = '9610bf1a8fa41f631ca5ed336d0d9b0313d7d73f'
  AND continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'
  AND original_contract_commit = 'e17fa9080434b5642667392fb06db61cf35f19bd'
  AND reviewed_source_commit = 'a7b5476fb886bcbb6bbd91aa89da17966547b3b8'
  AND provider_base_sha = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'
  AND current_main_sha = 'b34b31b5443890e69128db2862726950a6bbac0d';

-- name: StartDCPCard12ModelFreeRebaseContinuation :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'running', model_free_action_count = 1,
    local_ref_before = old_head, revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status = 'authorized' AND revision = sqlc.arg(revision)
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND model_free_action_count = 0 AND reviewer_model_call_count = 0
  AND local_ref_before = '' AND local_ref_after = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: CompleteDCPCard12ModelFreeRebaseContinuation :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'candidate_ready', local_ref_after = sqlc.arg(new_head),
    new_head = sqlc.arg(new_head), new_commit = sqlc.arg(new_head),
    provider_new_head = sqlc.arg(new_head), revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status = 'running' AND revision = sqlc.arg(revision)
  AND model_free_action_count = 1 AND reviewer_model_call_count = 0
  AND local_ref_before = old_head AND local_ref_after = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = ''
  AND sqlc.arg(new_head) <> old_head AND length(sqlc.arg(new_head)) = 40;

-- name: FailDCPCard12ModelFreeRebaseContinuation :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status IN ('authorized', 'running')
  AND reviewer_model_call_count = 0 AND recovery_review_run_id = ''
  AND merge_commit_sha = '' AND error_code = '';

-- name: FenceDCPCard12ModelFreeRebaseReview :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'review_running', reviewer_model_call_count = 1,
    recovery_review_run_id = sqlc.arg(review_run_id),
    recovery_review_id = sqlc.arg(review_id),
    recovery_review_batch_id = sqlc.arg(batch_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'
  AND status = 'candidate_ready' AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND session_id = sqlc.arg(session_id)
  AND pr_url = sqlc.arg(pr_url) AND new_head = sqlc.arg(target_sha)
  AND provider_new_head = sqlc.arg(target_sha)
  AND recovery_review_run_id = '' AND error_code = '';

-- name: FailDCPCard12ModelFreeRebaseReview :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status = 'review_running' AND model_free_action_count = 1
  AND reviewer_model_call_count = 1
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND merge_commit_sha = ''
  AND error_code = '';

-- name: RebindDCPAdmissionAfterCard12ModelFreeRebase :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id),
    review_id = sqlc.arg(new_review_id), target_sha = sqlc.arg(new_target_sha),
    review_base_sha = sqlc.arg(new_review_base_sha), status = 'waiting',
    lease_id = '', admitted_base_sha = '', error_code = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(admission_id)
  AND dcp_review_lab_admission.sequence = 4
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = 'dcp-review-lab-12'
  AND dcp_review_lab_admission.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
  AND dcp_review_lab_admission.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.lease_id = 'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND dcp_review_lab_admission.error_code = 'merge_conflict_or_ambiguity'
  AND EXISTS (
    SELECT 1 FROM review_run rr
    WHERE rr.id = sqlc.arg(new_review_run_id)
      AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = 'dcp-review-lab-12'
      AND rr.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
      AND rr.target_sha = sqlc.arg(new_target_sha)
      AND rr.status = 'complete' AND rr.verdict = 'approved'
      AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = ''
  )
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_card12_model_free_rebase_continuation continuation
    WHERE continuation.continuation_id = sqlc.arg(continuation_id)
      AND continuation.status = 'review_running'
      AND continuation.recovery_review_run_id = sqlc.arg(new_review_run_id)
      AND continuation.new_head = sqlc.arg(new_target_sha)
      AND continuation.model_free_action_count = 1
      AND continuation.reviewer_model_call_count = 1
      AND continuation.worker_model_call_count = 0
      AND continuation.arbiter_model_call_count = 0
  );

-- name: MarkDCPCard12ModelFreeRebaseReviewed :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'recovery_reviewed', recovery_check_id = sqlc.arg(check_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status = 'review_running'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha)
  AND model_free_action_count = 1 AND reviewer_model_call_count = 1
  AND error_code = '';

-- name: CompleteDCPCard12ModelFreeRebase :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'succeeded', merge_commit_sha = sqlc.arg(merge_commit_sha),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE continuation_id = sqlc.arg(continuation_id)
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha)
  AND model_free_action_count = 1 AND reviewer_model_call_count = 1
  AND worker_model_call_count = 0 AND arbiter_model_call_count = 0
  AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12ModelFreeRebaseTerminal :execrows
UPDATE dcp_review_lab_card12_model_free_rebase_continuation
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE continuation_id = 'dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060'
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha)
  AND model_free_action_count = 1 AND reviewer_model_call_count = 1
  AND merge_commit_sha = '' AND error_code = '';
