-- name: GetDCPCard12FreshWorkerRecovery :one
SELECT * FROM dcp_review_lab_card12_fresh_worker_recovery
WHERE recovery_id = ?;

-- name: ListDCPCard12FreshWorkerRecoveries :many
SELECT * FROM dcp_review_lab_card12_fresh_worker_recovery ORDER BY authorized_at;

-- name: PrepareDCPCard12FreshWorkerRecovery :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET input_json = sqlc.arg(input_json), input_digest = sqlc.arg(input_digest),
    input_path = sqlc.arg(input_path), result_path = sqlc.arg(result_path),
    log_path = sqlc.arg(log_path), status = 'requested', revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'authorized'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 0
  AND reviewer_model_call_count = 0 AND launch_id = '' AND input_json = ''
  AND worker_codex_session_id = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: StartDCPCard12FreshWorkerRecovery :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'running', worker_model_call_count = 1,
    launch_id = runtime_action_id, revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'requested'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 0
  AND reviewer_model_call_count = 0 AND input_json <> '' AND input_digest <> ''
  AND launch_id = '' AND worker_codex_session_id = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12FreshWorkerPreflight :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'preflight_failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status IN ('authorized', 'requested')
  AND worker_model_call_count = 0 AND reviewer_model_call_count = 0
  AND launch_id = '' AND worker_codex_session_id = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12FreshWorkerCall :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'running'
  AND worker_model_call_count = 1 AND reviewer_model_call_count = 0
  AND launch_id = runtime_action_id AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: CompleteDCPCard12FreshWorkerCall :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'worker_succeeded', worker_codex_session_id = sqlc.arg(worker_codex_session_id),
    worker_token_count = sqlc.arg(worker_token_count),
    worker_result_digest = sqlc.arg(worker_result_digest),
    worker_log_digest = sqlc.arg(worker_log_digest),
    new_head = sqlc.arg(new_head), new_commit = sqlc.arg(new_commit),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'running'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 1
  AND reviewer_model_call_count = 0 AND launch_id = runtime_action_id
  AND worker_codex_session_id = '' AND new_head = ''
  AND recovery_review_run_id = '' AND merge_commit_sha = '' AND error_code = ''
  AND sqlc.arg(new_head) <> old_head AND sqlc.arg(new_commit) = sqlc.arg(new_head);

-- name: FenceDCPCard12FreshRecoveryReview :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'review_running', reviewer_model_call_count = 1,
    recovery_review_run_id = sqlc.arg(review_run_id),
    recovery_review_id = sqlc.arg(review_id),
    recovery_review_batch_id = sqlc.arg(batch_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND status = 'worker_succeeded' AND worker_model_call_count = 1
  AND reviewer_model_call_count = 0 AND session_id = sqlc.arg(session_id)
  AND pr_url = sqlc.arg(pr_url) AND new_head = sqlc.arg(target_sha)
  AND recovery_review_run_id = '' AND error_code = '';

-- name: FailDCPCard12FreshRecoveryReview :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'review_running'
  AND worker_model_call_count = 1 AND reviewer_model_call_count = 1
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND merge_commit_sha = '' AND error_code = '';

-- name: RebindDCPAdmissionAfterCard12FreshWorkerRecovery :execrows
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
    SELECT 1 FROM dcp_review_lab_card12_fresh_worker_recovery recovery
    WHERE recovery.recovery_id = sqlc.arg(recovery_id)
      AND recovery.status = 'review_running'
      AND recovery.recovery_review_run_id = sqlc.arg(new_review_run_id)
      AND recovery.new_head = sqlc.arg(new_target_sha)
      AND recovery.worker_model_call_count = 1
      AND recovery.reviewer_model_call_count = 1
  );

-- name: MarkDCPCard12FreshWorkerRecoveryReviewed :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'recovery_reviewed', recovery_check_id = sqlc.arg(check_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'review_running'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha) AND reviewer_model_call_count = 1
  AND error_code = '';

-- name: CompleteDCPCard12FreshWorkerRecovery :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'succeeded', merge_commit_sha = sqlc.arg(merge_commit_sha),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = sqlc.arg(recovery_id) AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha)
  AND worker_model_call_count = 1 AND reviewer_model_call_count = 1
  AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12FreshWorkerTerminal :execrows
UPDATE dcp_review_lab_card12_fresh_worker_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE recovery_id = 'dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b'
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(review_run_id)
  AND new_head = sqlc.arg(target_sha)
  AND worker_model_call_count = 1 AND reviewer_model_call_count = 1
  AND merge_commit_sha = '' AND error_code = '';
