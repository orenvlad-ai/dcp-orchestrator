-- name: GetDCPCard12RebaseHeadFinalization :one
SELECT * FROM dcp_review_lab_card12_rebase_head_finalization
WHERE finalization_id = ?;

-- name: ListDCPCard12RebaseHeadFinalizations :many
SELECT * FROM dcp_review_lab_card12_rebase_head_finalization
ORDER BY authorized_at;

-- name: CountExactDCPFinalizationQuarantine :one
SELECT count(*) FROM dcp_governed_startup_quarantine q
WHERE q.recovery_id = 'dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f'
  AND q.contract_commit = '623c3896a50d410e5b305ed08cf29abdc40b5b23'
  AND q.verification_count >= 5
  AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
    OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))
  AND (SELECT min(verification_count) FROM dcp_governed_startup_quarantine) =
      (SELECT max(verification_count) FROM dcp_governed_startup_quarantine);

-- name: CountExactDCPRebaseHeadFinalizationAuditRecovery :one
SELECT count(*)
FROM dcp_card12_rebase_head_finalization_audit_recovery audit
JOIN dcp_review_lab_card12_rebase_head_finalization finalization
  ON finalization.finalization_id = audit.finalization_id
JOIN dcp_review_lab_card12_cold_start_recovery predecessor
  ON predecessor.recovery_id = audit.predecessor_recovery_id
JOIN dcp_card12_cold_start_tool_path_recovery tool
  ON tool.recovery_id = predecessor.recovery_id
JOIN dcp_card12_cold_start_auto_merge_recovery auto
  ON auto.recovery_id = predecessor.recovery_id
WHERE audit.correction_id = 'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b'
  AND audit.finalization_generation = 1
  AND audit.finalization_identity = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND audit.prior_status = 'failed' AND audit.prior_error_code = 'identity_drift'
  AND audit.prior_revision = 1 AND audit.prior_worker_calls = 0
  AND audit.prior_arbiter_calls = 0 AND audit.prior_action_count = 0
  AND audit.prior_reviewer_calls = 0 AND audit.prior_provider_new_head = ''
  AND audit.prior_review_run_id = '' AND audit.prior_merge_commit_sha = ''
  AND audit.failed_source_sha = '6f53f74f456b869c98bb82d928f671b54672808a'
  AND audit.failed_source_tree = '0fab2ee443d8bf20a0efcc524851e8c9589e6dd9'
  AND audit.tool_audit_rows = 1 AND audit.auto_audit_rows = 1
  AND audit.stale_tool_query_matches = 0 AND audit.stale_auto_query_matches = 0
  AND audit.quarantine_rows = 2 AND audit.quarantine_verifications = 10
  AND audit.recovery_reason = 'terminal_predecessor_was_checked_with_historical_authorized_state_audit_queries'
  AND finalization.status = 'authorized' AND finalization.revision = 2
  AND finalization.worker_model_call_count = 0
  AND finalization.arbiter_model_call_count = 0
  AND finalization.model_free_action_count = 0
  AND finalization.reviewer_model_call_count = 0
  AND finalization.provider_new_head = '' AND finalization.review_run_id = ''
  AND finalization.merge_commit_sha = '' AND finalization.error_code = ''
  AND predecessor.status = 'failed'
  AND predecessor.error_code = 'model_free_action_failed'
  AND predecessor.revision = 7 AND predecessor.worker_model_call_count = 0
  AND predecessor.arbiter_model_call_count = 0
  AND predecessor.model_free_action_count = 1
  AND predecessor.reviewer_model_call_count = 0
  AND predecessor.backup_digest = finalization.backup_digest
  AND predecessor.local_ref_before = finalization.old_head
  AND predecessor.local_ref_after = '' AND predecessor.new_head = ''
  AND predecessor.provider_new_head = ''
  AND predecessor.recovery_review_run_id = ''
  AND predecessor.merge_commit_sha = '' AND predecessor.finished_at IS NOT NULL
  AND tool.correction_id = audit.tool_audit_correction_id
  AND auto.correction_id = audit.auto_audit_correction_id
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = predecessor.recovery_id
         AND q.contract_commit = predecessor.contract_commit
         AND q.verification_count >= 6
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2
  AND (SELECT min(verification_count) FROM dcp_governed_startup_quarantine) =
      (SELECT max(verification_count) FROM dcp_governed_startup_quarantine);

-- name: CountExactDCPRebaseHeadFinalizationProviderBaseRecovery :one
SELECT count(*)
FROM dcp_card12_rebase_head_finalization_provider_base_recovery recovery
JOIN dcp_review_lab_card12_rebase_head_finalization finalization
  ON finalization.finalization_id = recovery.finalization_id
JOIN dcp_card12_rebase_head_finalization_audit_recovery first_recovery
  ON first_recovery.finalization_id = finalization.finalization_id
JOIN dcp_review_lab_card12_cold_start_recovery predecessor
  ON predecessor.recovery_id = finalization.predecessor_recovery_id
WHERE recovery.correction_id = 'dcp-card12-rebase-head-finalization-provider-base-recovery-d140ac8daec5f311a278050c6e1e0b33011e28b0ee2ee9b52bb357f3b34ac923'
  AND recovery.finalization_generation = 1
  AND recovery.finalization_identity = 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND recovery.prior_status = 'failed'
  AND recovery.prior_error_code = 'provider_identity_drift'
  AND recovery.prior_revision = 4 AND recovery.prior_worker_calls = 0
  AND recovery.prior_arbiter_calls = 0 AND recovery.prior_action_count = 1
  AND recovery.prior_reviewer_calls = 0
  AND recovery.prior_provider_new_head = ''
  AND recovery.prior_review_run_id = ''
  AND recovery.prior_merge_commit_sha = ''
  AND recovery.old_head = finalization.old_head
  AND recovery.candidate_head = finalization.candidate_head
  AND recovery.historical_provider_base = finalization.provider_base
  AND recovery.post_push_provider_base = finalization.current_main
  AND recovery.failed_source_sha = '1f1e8cedf44d30773568f8801710f1371b14a47b'
  AND recovery.failed_source_tree = '4523bfacf690c15f75c155ccfc2f14831db7b2f2'
  AND recovery.first_check_name = 'dcp-review-lab'
  AND recovery.first_check_id = '94509683728'
  AND recovery.first_check_status = 'failed'
  AND recovery.first_check_conclusion = 'failure'
  AND recovery.observed_provider_mergeable = 'MERGEABLE'
  AND recovery.observed_provider_merge_state = 'UNSTABLE'
  AND recovery.quarantine_rows = 2
  AND recovery.quarantine_verifications = 12
  AND recovery.recovery_reason = 'post_push_provider_base_advanced_from_historical_base_to_current_main'
  AND finalization.status = 'running' AND finalization.revision = 5
  AND finalization.worker_model_call_count = 0
  AND finalization.arbiter_model_call_count = 0
  AND finalization.model_free_action_count = 1
  AND finalization.reviewer_model_call_count = 0
  AND finalization.provider_new_head = '' AND finalization.review_run_id = ''
  AND finalization.merge_commit_sha = '' AND finalization.error_code = ''
  AND first_recovery.correction_id = 'dcp-card12-rebase-head-finalization-audit-recovery-52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b'
  AND predecessor.status = 'failed'
  AND predecessor.error_code = 'model_free_action_failed'
  AND predecessor.revision = 7
  AND predecessor.worker_model_call_count = 0
  AND predecessor.arbiter_model_call_count = 0
  AND predecessor.model_free_action_count = 1
  AND predecessor.reviewer_model_call_count = 0
  AND predecessor.backup_digest = finalization.backup_digest
  AND predecessor.local_ref_before = finalization.old_head
  AND predecessor.local_ref_after = '' AND predecessor.new_head = ''
  AND predecessor.provider_new_head = ''
  AND predecessor.recovery_review_run_id = ''
  AND predecessor.merge_commit_sha = '' AND predecessor.finished_at IS NOT NULL
  AND (SELECT count(*) FROM dcp_governed_startup_quarantine q
       WHERE q.recovery_id = predecessor.recovery_id
         AND q.contract_commit = predecessor.contract_commit
         AND q.verification_count >= 7
         AND ((q.session_id = 'dcp-review-lab-11' AND q.classification = 'governed_terminal')
           OR (q.session_id = 'dcp-review-lab-12' AND q.classification = 'governed_recovery'))) = 2
  AND (SELECT min(verification_count) FROM dcp_governed_startup_quarantine) =
      (SELECT max(verification_count) FROM dcp_governed_startup_quarantine);

-- name: StartDCPCard12RebaseHeadFinalization :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'running', model_free_action_count = 1,
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE finalization_id = sqlc.arg(finalization_id) AND status = 'authorized'
  AND revision = sqlc.arg(revision) AND worker_model_call_count = 0
  AND arbiter_model_call_count = 0 AND model_free_action_count = 0
  AND reviewer_model_call_count = 0 AND provider_new_head = ''
  AND review_run_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: CompleteDCPCard12RebaseHeadFinalizationAction :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'candidate_ready', provider_new_head = candidate_head,
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE finalization_id = sqlc.arg(finalization_id) AND status = 'running'
  AND revision = sqlc.arg(revision) AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND provider_new_head = ''
  AND review_run_id = '' AND merge_commit_sha = '' AND error_code = ''
  AND candidate_head = sqlc.arg(candidate_head);

-- name: FailDCPCard12RebaseHeadFinalization :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE finalization_id = sqlc.arg(finalization_id)
  AND status IN ('authorized', 'running')
  AND reviewer_model_call_count = 0 AND review_run_id = ''
  AND merge_commit_sha = '' AND error_code = '';

-- name: FenceDCPCard12RebaseHeadFinalizationReview :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'review_running', reviewer_model_call_count = 1,
    review_run_id = sqlc.arg(review_run_id), review_id = sqlc.arg(review_id),
    review_batch_id = sqlc.arg(batch_id), revision = revision + 1,
    updated_at = sqlc.arg(updated_at)
WHERE finalization_id = 'dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b'
  AND status = 'candidate_ready' AND model_free_action_count = 1
  AND reviewer_model_call_count = 0 AND session_id = sqlc.arg(session_id)
  AND pr_url = sqlc.arg(pr_url) AND candidate_head = sqlc.arg(target_sha)
  AND provider_new_head = sqlc.arg(target_sha) AND review_run_id = ''
  AND error_code = '';

-- name: FailDCPCard12RebaseHeadFinalizationReview :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE finalization_id = sqlc.arg(finalization_id)
  AND status = 'review_running' AND reviewer_model_call_count = 1
  AND review_run_id = sqlc.arg(review_run_id)
  AND candidate_head = sqlc.arg(target_sha)
  AND merge_commit_sha = '' AND error_code = '';

-- name: RebindDCPAdmissionAfterCard12RebaseHeadFinalization :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id),
    review_id = sqlc.arg(new_review_id), target_sha = sqlc.arg(new_target_sha),
    review_base_sha = sqlc.arg(new_review_base_sha), status = 'waiting',
    lease_id = '', admitted_base_sha = '', error_code = '',
    recovered_incident_packet = incident_packet, incident_packet = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(admission_id)
  AND dcp_review_lab_admission.sequence = 4
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = 'dcp-review-lab-12'
  AND dcp_review_lab_admission.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
  AND dcp_review_lab_admission.target_sha = 'd4fcb68051ae113ed497d02151a759800ee85633'
  AND dcp_review_lab_admission.review_base_sha = 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da'
  AND dcp_review_lab_admission.lease_id = 'dcp-incident-dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34'
  AND dcp_review_lab_admission.error_code = 'merge_conflict_or_ambiguity'
  AND dcp_review_lab_admission.recovered_incident_packet = ''
  AND dcp_review_lab_admission.refresh_wake_count = 0
  AND dcp_review_lab_admission.merge_commit_sha = ''
  AND EXISTS (SELECT 1 FROM review_run rr
    WHERE rr.id = sqlc.arg(new_review_run_id) AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = 'dcp-review-lab-12'
      AND rr.pr_url = 'https://github.com/orenvlad-ai/dcp-review-lab/pull/9'
      AND rr.target_sha = sqlc.arg(new_target_sha) AND rr.status = 'complete'
      AND rr.verdict = 'approved' AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = '')
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_card12_rebase_head_finalization finalization
    WHERE finalization.finalization_id = sqlc.arg(finalization_id)
      AND finalization.admission_id = dcp_review_lab_admission.id
      AND finalization.status = 'review_running'
      AND finalization.model_free_action_count = 1
      AND finalization.reviewer_model_call_count = 1
      AND finalization.review_run_id = sqlc.arg(new_review_run_id)
      AND finalization.candidate_head = sqlc.arg(new_target_sha)
      AND finalization.provider_new_head = sqlc.arg(new_target_sha)
  );

-- name: MarkDCPCard12RebaseHeadFinalizationReviewed :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'recovery_reviewed', check_id = sqlc.arg(check_id),
    revision = revision + 1, updated_at = sqlc.arg(updated_at)
WHERE finalization_id = sqlc.arg(finalization_id)
  AND status = 'review_running' AND reviewer_model_call_count = 1
  AND review_run_id = sqlc.arg(review_run_id)
  AND candidate_head = sqlc.arg(target_sha)
  AND check_id = '' AND merge_commit_sha = '' AND error_code = '';

-- name: CompleteDCPCard12RebaseHeadFinalization :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'succeeded', merge_commit_sha = sqlc.arg(merge_commit_sha),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE finalization_id = sqlc.arg(finalization_id)
  AND status = 'recovery_reviewed' AND model_free_action_count = 1
  AND reviewer_model_call_count = 1 AND review_run_id = sqlc.arg(review_run_id)
  AND candidate_head = sqlc.arg(target_sha) AND check_id <> ''
  AND merge_commit_sha = '' AND error_code = '';

-- name: FailDCPCard12RebaseHeadFinalizationTerminal :execrows
UPDATE dcp_review_lab_card12_rebase_head_finalization
SET status = 'failed', error_code = sqlc.arg(error_code),
    revision = revision + 1, updated_at = sqlc.arg(updated_at),
    finished_at = sqlc.arg(finished_at)
WHERE status = 'recovery_reviewed' AND reviewer_model_call_count = 1
  AND review_run_id = sqlc.arg(review_run_id)
  AND candidate_head = sqlc.arg(target_sha)
  AND merge_commit_sha = '' AND error_code = '';
