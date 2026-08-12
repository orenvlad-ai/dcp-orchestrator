-- name: InsertDCPReleaseArbiterIncident :execrows
INSERT INTO dcp_review_lab_arbiter_v1 (
    incident_id, generation, identity_digest, admission_id, incident_lease_id,
    source_packet_json, source_packet_digest, input_json, input_digest, task_id,
    session_id, worktree_path, source_branch, pr_url, pr_number, target_sha,
    reviewed_base_sha, current_base_sha, review_id, review_run_id, batch_id,
    scope_digest, history_digest, diff_digest, check_set_digest, review_set_digest,
    frozen_queue_digest, mechanical_digest, model, reasoning, token_budget,
    runtime_handle_id, launch_id, status, created_at, updated_at
)
SELECT
    sqlc.arg(incident_id), 1, sqlc.arg(identity_digest), a.id, a.lease_id,
    a.incident_packet, sqlc.arg(source_packet_digest), sqlc.arg(input_json),
    sqlc.arg(input_digest), sqlc.arg(task_id), a.session_id,
    sqlc.arg(worktree_path), sqlc.arg(source_branch), a.pr_url, a.pr_number,
    a.target_sha, a.review_base_sha, sqlc.arg(current_base_sha), a.review_id,
    a.review_run_id, sqlc.arg(batch_id), sqlc.arg(scope_digest),
    sqlc.arg(history_digest), sqlc.arg(diff_digest), sqlc.arg(check_set_digest),
    sqlc.arg(review_set_digest), sqlc.arg(frozen_queue_digest),
    sqlc.arg(mechanical_digest), 'gpt-5.6-sol', 'xhigh', 16384,
    'dcp-global-release-arbiter-v1', sqlc.arg(incident_id), 'requested',
    sqlc.arg(created_at), sqlc.arg(updated_at)
FROM dcp_review_lab_admission a
WHERE a.id = sqlc.arg(admission_id)
  AND a.sequence = sqlc.arg(admission_sequence)
  AND a.session_id = sqlc.arg(session_id)
  AND a.review_run_id = sqlc.arg(review_run_id)
  AND a.target_sha = sqlc.arg(target_sha)
  AND a.status = 'incident'
  AND a.error_code = 'merge_conflict_or_ambiguity'
  AND a.lease_id = sqlc.arg(incident_lease_id)
  AND a.incident_packet = sqlc.arg(source_packet_json)
  AND json_extract(a.incident_packet, '$.reason') = 'merge_conflict_or_ambiguity'
  AND json_extract(a.incident_packet, '$.sessionId') = a.session_id
  AND json_extract(a.incident_packet, '$.reviewRunId') = a.review_run_id
  AND json_extract(a.incident_packet, '$.targetSha') = a.target_sha
ON CONFLICT (admission_id, generation) DO NOTHING;

-- name: GetDCPReleaseArbiterIncidentByID :one
SELECT * FROM dcp_review_lab_arbiter_v1 WHERE incident_id = ?;

-- name: GetDCPReleaseArbiterIncidentByAdmission :one
SELECT * FROM dcp_review_lab_arbiter_v1 WHERE admission_id = ? AND generation = 1;

-- name: GetDCPReleaseArbiterIncidentBySession :one
SELECT * FROM dcp_review_lab_arbiter_v1 WHERE session_id = ?;

-- name: ListDCPReleaseArbiterIncidents :many
SELECT * FROM dcp_review_lab_arbiter_v1 ORDER BY created_at, incident_id;

-- name: StartDCPReleaseArbiterCall :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'running', model_call_count = 1, updated_at = sqlc.arg(updated_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND identity_digest = sqlc.arg(identity_digest)
  AND input_digest = sqlc.arg(input_digest)
  AND status = 'requested'
  AND model_call_count = 0
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_admission a
    WHERE a.id = dcp_review_lab_arbiter_v1.admission_id
      AND a.status = 'incident'
      AND a.lease_id = dcp_review_lab_arbiter_v1.incident_lease_id
      AND a.incident_packet = dcp_review_lab_arbiter_v1.source_packet_json
  );

-- name: FailDCPReleaseArbiterPreflight :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'preflight_failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND status = 'requested'
  AND model_call_count = 0;

-- name: RecordDCPReleaseArbiterDecision :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = sqlc.arg(status), decision_json = sqlc.arg(decision_json),
    decision_digest = sqlc.arg(decision_digest), error_code = sqlc.arg(error_code),
    decision_at = sqlc.arg(decision_at), finished_at = sqlc.narg(finished_at),
    updated_at = sqlc.arg(decision_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND identity_digest = sqlc.arg(identity_digest)
  AND input_digest = sqlc.arg(input_digest)
  AND status = 'running'
  AND model_call_count = 1
  AND decision_json = ''
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_admission a
    WHERE a.id = dcp_review_lab_arbiter_v1.admission_id
      AND a.status = 'incident'
      AND a.session_id = dcp_review_lab_arbiter_v1.session_id
      AND a.review_run_id = dcp_review_lab_arbiter_v1.review_run_id
      AND a.target_sha = dcp_review_lab_arbiter_v1.target_sha
      AND a.lease_id = dcp_review_lab_arbiter_v1.incident_lease_id
      AND a.incident_packet = dcp_review_lab_arbiter_v1.source_packet_json
  );

-- name: ConsumeDCPReleaseArbiterRepair :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'repairing', recovery_owner_session_id = session_id,
    recovery_path = 'same_worker_conflict_repair', recovery_wake_count = 1,
    updated_at = sqlc.arg(updated_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND decision_digest = sqlc.arg(decision_digest)
  AND status = 'decided'
  AND recovery_wake_count = 0
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_admission a
    WHERE a.id = dcp_review_lab_arbiter_v1.admission_id
      AND a.status = 'incident'
      AND a.lease_id = dcp_review_lab_arbiter_v1.incident_lease_id
  );

-- name: FailDCPReleaseArbiterCall :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND status = 'running'
  AND model_call_count = 1
  AND decision_json = '';

-- name: FailDCPReleaseArbiterAfterDecision :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND status IN ('decided', 'repairing')
  AND model_call_count = 1
  AND decision_json <> '';

-- name: RebindDCPAdmissionAfterArbiterRepair :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id),
    review_id = sqlc.arg(new_review_id),
    target_sha = sqlc.arg(new_target_sha),
    review_base_sha = sqlc.arg(new_review_base_sha),
    status = 'waiting', lease_id = '', admitted_base_sha = '', error_code = '',
    recovered_incident_packet = incident_packet, incident_packet = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(admission_id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.pr_url = sqlc.arg(pr_url)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(old_target_sha)
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.lease_id = sqlc.arg(incident_lease_id)
  AND dcp_review_lab_admission.error_code = 'merge_conflict_or_ambiguity'
  AND dcp_review_lab_admission.recovered_incident_packet = ''
  AND sqlc.arg(new_target_sha) <> dcp_review_lab_admission.target_sha
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1 arb
    WHERE arb.admission_id = dcp_review_lab_admission.id
      AND arb.incident_id = sqlc.arg(incident_id)
      AND arb.status = 'repairing'
      AND arb.recovery_owner_session_id = dcp_review_lab_admission.session_id
      AND arb.recovery_path = 'same_worker_conflict_repair'
      AND arb.recovery_wake_count = 1
  )
  AND EXISTS (
    SELECT 1 FROM review_run rr
    JOIN pr ON pr.url = rr.pr_url AND pr.session_id = rr.session_id
    WHERE rr.id = sqlc.arg(new_review_run_id)
      AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = sqlc.arg(session_id)
      AND rr.pr_url = sqlc.arg(pr_url)
      AND rr.target_sha = sqlc.arg(new_target_sha)
      AND rr.status = 'complete' AND rr.verdict = 'approved'
      AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = ''
      AND pr.head_sha = rr.target_sha AND pr.pr_state = 'open'
      AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  );

-- name: MarkDCPReleaseArbiterRecoveryReviewed :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'recovery_reviewed', recovery_review_run_id = sqlc.arg(recovery_review_run_id),
    recovery_target_sha = sqlc.arg(recovery_target_sha), updated_at = sqlc.arg(updated_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND status = 'repairing'
  AND recovery_wake_count = 1
  AND recovery_review_run_id = ''
  AND recovery_target_sha = '';

-- name: CompleteDCPReleaseArbiterIncident :execrows
UPDATE dcp_review_lab_arbiter_v1
SET status = 'succeeded', error_code = '', finished_at = sqlc.arg(finished_at),
    updated_at = sqlc.arg(finished_at)
WHERE admission_id = sqlc.arg(admission_id)
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(recovery_review_run_id)
  AND recovery_target_sha = sqlc.arg(recovery_target_sha);

-- name: GetDCPReleaseArbiterSuccessorAttemptByID :one
SELECT * FROM dcp_review_lab_arbiter_v1_successor_attempt WHERE attempt_id = ?;

-- name: GetDCPReleaseArbiterSuccessorAttemptByIncident :one
SELECT * FROM dcp_review_lab_arbiter_v1_successor_attempt WHERE incident_id = ?;

-- name: ListDCPReleaseArbiterSuccessorAttempts :many
SELECT * FROM dcp_review_lab_arbiter_v1_successor_attempt ORDER BY authorized_at, attempt_id;

-- name: PrepareDCPReleaseArbiterSuccessorAttempt :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'requested', input_json = sqlc.arg(input_json),
    input_digest = sqlc.arg(input_digest), updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND dcp_review_lab_arbiter_v1_successor_attempt.incident_id = sqlc.arg(incident_id)
  AND attempt_generation = 2
  AND attempt_identity_digest = sqlc.arg(attempt_identity_digest)
  AND incident_identity_digest = sqlc.arg(incident_identity_digest)
  AND incident_input_digest = sqlc.arg(incident_input_digest)
  AND status = 'authorized'
  AND model_call_count = 0
  AND input_json = ''
  AND input_digest = ''
  AND policy_max_worker_calls = 1
  AND policy_max_fresh_reviews = 1
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1 original
    WHERE original.incident_id = dcp_review_lab_arbiter_v1_successor_attempt.incident_id
      AND original.generation = dcp_review_lab_arbiter_v1_successor_attempt.incident_generation
      AND original.identity_digest = dcp_review_lab_arbiter_v1_successor_attempt.incident_identity_digest
      AND original.input_digest = dcp_review_lab_arbiter_v1_successor_attempt.incident_input_digest
      AND original.status = 'failed'
      AND original.model_call_count = 1
      AND original.decision_json = ''
      AND original.decision_digest = ''
      AND original.recovery_wake_count = 0
      AND original.error_code = 'submit_failed'
  );

-- name: StartDCPReleaseArbiterSuccessorCall :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'running', model_call_count = 1, updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND attempt_identity_digest = sqlc.arg(attempt_identity_digest)
  AND input_digest = sqlc.arg(input_digest)
  AND status = 'requested'
  AND model_call_count = 0
  AND input_json <> ''
  AND policy_max_worker_calls = 1
  AND policy_max_fresh_reviews = 1;

-- name: FailDCPReleaseArbiterSuccessorPreflight :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'preflight_failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND status = 'requested'
  AND model_call_count = 0;

-- name: RecordDCPReleaseArbiterSuccessorDecision :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = sqlc.arg(status), decision_json = sqlc.arg(decision_json),
    decision_digest = sqlc.arg(decision_digest), error_code = sqlc.arg(error_code),
    decision_at = sqlc.arg(decision_at), finished_at = sqlc.narg(finished_at),
    updated_at = sqlc.arg(decision_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND dcp_review_lab_arbiter_v1_successor_attempt.incident_id = sqlc.arg(incident_id)
  AND attempt_generation = 2
  AND attempt_identity_digest = sqlc.arg(attempt_identity_digest)
  AND dcp_review_lab_arbiter_v1_successor_attempt.input_digest = sqlc.arg(input_digest)
  AND status = 'running'
  AND model_call_count = 1
  AND decision_json = ''
  AND policy_max_worker_calls = 1
  AND policy_max_fresh_reviews = 1
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1 original
    WHERE original.incident_id = dcp_review_lab_arbiter_v1_successor_attempt.incident_id
      AND original.status = 'failed'
      AND original.model_call_count = 1
      AND original.decision_json = ''
      AND original.error_code = 'submit_failed'
  );

-- name: ConsumeDCPReleaseArbiterSuccessorRepair :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'repairing', recovery_owner_session_id = 'dcp-review-lab-12',
    recovery_path = 'same_worker_conflict_repair', recovery_wake_count = 1,
    updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND decision_digest = sqlc.arg(decision_digest)
  AND status = 'decided'
  AND recovery_wake_count = 0
  AND policy_max_worker_calls = 1
  AND policy_max_fresh_reviews = 1;

-- name: FailDCPReleaseArbiterSuccessorCall :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND status = 'running'
  AND model_call_count = 1
  AND decision_json = '';

-- name: FailDCPReleaseArbiterSuccessorAfterDecision :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at), updated_at = sqlc.arg(finished_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND status IN ('decided', 'repairing')
  AND model_call_count = 1
  AND decision_json <> '';

-- name: RebindDCPAdmissionAfterArbiterSuccessorRepair :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id),
    review_id = sqlc.arg(new_review_id),
    target_sha = sqlc.arg(new_target_sha),
    review_base_sha = sqlc.arg(new_review_base_sha),
    status = 'waiting', lease_id = '', admitted_base_sha = '', error_code = '',
    recovered_incident_packet = incident_packet, incident_packet = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(admission_id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.pr_url = sqlc.arg(pr_url)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(old_target_sha)
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.lease_id = sqlc.arg(incident_lease_id)
  AND dcp_review_lab_admission.error_code = 'merge_conflict_or_ambiguity'
  AND dcp_review_lab_admission.recovered_incident_packet = ''
  AND sqlc.arg(new_target_sha) <> dcp_review_lab_admission.target_sha
  AND EXISTS (
    SELECT 1 FROM dcp_review_lab_arbiter_v1_successor_attempt successor
    WHERE successor.attempt_id = sqlc.arg(attempt_id)
      AND successor.incident_id = sqlc.arg(incident_id)
      AND successor.status = 'repairing'
      AND successor.attempt_generation = 2
      AND successor.recovery_owner_session_id = dcp_review_lab_admission.session_id
      AND successor.recovery_path = 'same_worker_conflict_repair'
      AND successor.recovery_wake_count = 1
      AND successor.policy_max_worker_calls = 1
      AND successor.policy_max_fresh_reviews = 1
  )
  AND EXISTS (
    SELECT 1 FROM review_run rr
    JOIN pr ON pr.url = rr.pr_url AND pr.session_id = rr.session_id
    WHERE rr.id = sqlc.arg(new_review_run_id)
      AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = sqlc.arg(session_id)
      AND rr.pr_url = sqlc.arg(pr_url)
      AND rr.target_sha = sqlc.arg(new_target_sha)
      AND rr.status = 'complete' AND rr.verdict = 'approved'
      AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = ''
      AND pr.head_sha = rr.target_sha AND pr.pr_state = 'open'
      AND pr.is_draft = 0 AND pr.is_merged = 0 AND pr.is_closed = 0
  );

-- name: MarkDCPReleaseArbiterSuccessorRecoveryReviewed :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'recovery_reviewed', recovery_review_run_id = sqlc.arg(recovery_review_run_id),
    recovery_target_sha = sqlc.arg(recovery_target_sha), updated_at = sqlc.arg(updated_at)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND status = 'repairing'
  AND recovery_wake_count = 1
  AND policy_max_fresh_reviews = 1
  AND recovery_review_run_id = ''
  AND recovery_target_sha = '';

-- name: CompleteDCPReleaseArbiterSuccessorAttempt :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'succeeded', error_code = '', finished_at = sqlc.arg(finished_at),
    updated_at = sqlc.arg(finished_at)
WHERE incident_id = sqlc.arg(incident_id)
  AND status = 'recovery_reviewed'
  AND recovery_review_run_id = sqlc.arg(recovery_review_run_id)
  AND recovery_target_sha = sqlc.arg(recovery_target_sha);

-- name: GetDCPArbiterSuccessorValidationRecovery :one
SELECT * FROM dcp_arbiter_successor_result_validation_recovery WHERE attempt_id = ?;

-- name: RecoverDCPArbiterSuccessorExactDecision :execrows
UPDATE dcp_review_lab_arbiter_v1_successor_attempt
SET status = 'decided', decision_json = sqlc.arg(decision_json),
    decision_digest = sqlc.arg(decision_digest), error_code = '',
    decision_at = sqlc.arg(decision_at), finished_at = NULL,
    updated_at = sqlc.arg(decision_at)
WHERE dcp_review_lab_arbiter_v1_successor_attempt.attempt_id = sqlc.arg(attempt_id)
  AND dcp_review_lab_arbiter_v1_successor_attempt.incident_id = sqlc.arg(incident_id)
  AND dcp_review_lab_arbiter_v1_successor_attempt.attempt_generation = 2
  AND dcp_review_lab_arbiter_v1_successor_attempt.attempt_identity_digest = sqlc.arg(attempt_identity_digest)
  AND dcp_review_lab_arbiter_v1_successor_attempt.input_digest = sqlc.arg(input_digest)
  AND dcp_review_lab_arbiter_v1_successor_attempt.status = 'failed'
  AND dcp_review_lab_arbiter_v1_successor_attempt.model_call_count = 1
  AND dcp_review_lab_arbiter_v1_successor_attempt.decision_json = ''
  AND dcp_review_lab_arbiter_v1_successor_attempt.decision_digest = ''
  AND dcp_review_lab_arbiter_v1_successor_attempt.recovery_owner_session_id = ''
  AND dcp_review_lab_arbiter_v1_successor_attempt.recovery_path = ''
  AND dcp_review_lab_arbiter_v1_successor_attempt.recovery_wake_count = 0
  AND dcp_review_lab_arbiter_v1_successor_attempt.error_code = 'submit_failed'
  AND dcp_review_lab_arbiter_v1_successor_attempt.finished_at IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM dcp_arbiter_successor_result_validation_recovery recovery
    WHERE recovery.attempt_id = dcp_review_lab_arbiter_v1_successor_attempt.attempt_id
      AND recovery.status = 'pending'
      AND recovery.input_digest = dcp_review_lab_arbiter_v1_successor_attempt.input_digest
      AND recovery.prior_status = dcp_review_lab_arbiter_v1_successor_attempt.status
      AND recovery.prior_error_code = dcp_review_lab_arbiter_v1_successor_attempt.error_code
      AND recovery.prior_finished_at = dcp_review_lab_arbiter_v1_successor_attempt.finished_at
      AND recovery.prior_model_call_count = dcp_review_lab_arbiter_v1_successor_attempt.model_call_count
      AND recovery.prior_decision_digest = dcp_review_lab_arbiter_v1_successor_attempt.decision_digest
      AND recovery.prior_recovery_wake_count = dcp_review_lab_arbiter_v1_successor_attempt.recovery_wake_count
  );

-- name: MarkDCPArbiterSuccessorValidationRecoveryApplied :execrows
UPDATE dcp_arbiter_successor_result_validation_recovery
SET status = 'applied', error_code = '', finished_at = sqlc.arg(finished_at)
WHERE attempt_id = sqlc.arg(attempt_id) AND status = 'pending';

-- name: FailDCPArbiterSuccessorValidationRecovery :execrows
UPDATE dcp_arbiter_successor_result_validation_recovery
SET status = 'failed', error_code = sqlc.arg(error_code),
    finished_at = sqlc.arg(finished_at)
WHERE attempt_id = sqlc.arg(attempt_id) AND status = 'pending';
