-- name: InsertDCPFutureArbiterIncident :execrows
INSERT INTO dcp_future_card_arbiter_v1 (
  incident_id, generation, identity_digest, task_id, session_id, admission_id,
  admission_sequence, incident_lease_id, incident_kind, source_packet_json,
  source_packet_digest, pr_url, pr_number, candidate_head_sha,
  reviewed_base_sha, current_main_sha, review_run_id, affected_paths_json,
  cohort_json, cohort_digest, evidence_json, evidence_digest, input_json,
  input_digest, model_action_id, runtime_handle_id, model, reasoning,
  token_budget, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
          'gpt-5.6-sol', 'xhigh', 16384, 'requested', ?, ?)
ON CONFLICT (admission_id, generation) DO NOTHING;

-- name: GetDCPFutureArbiterIncidentByID :one
SELECT * FROM dcp_future_card_arbiter_v1 WHERE incident_id = ?;

-- name: GetDCPFutureArbiterIncidentByAdmission :one
SELECT * FROM dcp_future_card_arbiter_v1 WHERE admission_id = ? ORDER BY generation DESC LIMIT 1;

-- name: GetDCPFutureArbiterIncidentByTask :one
SELECT * FROM dcp_future_card_arbiter_v1 WHERE task_id = ? ORDER BY generation DESC LIMIT 1;

-- name: ListDCPFutureArbiterIncidents :many
SELECT * FROM dcp_future_card_arbiter_v1 ORDER BY created_at, incident_id;

-- name: CountDCPFutureArbiterGenerationsForTask :one
SELECT count(*) FROM dcp_future_card_arbiter_v1 WHERE task_id = ?;

-- name: GetDCPFutureArbiterSchemaRecoveryByPredecessor :one
SELECT * FROM dcp_future_card_arbiter_schema_recovery_v1
WHERE predecessor_incident_id = ?;

-- name: ConsumeDCPFutureArbiterSchemaRecovery :execrows
UPDATE dcp_future_card_arbiter_schema_recovery_v1
SET status = 'consumed', successor_incident_id = sqlc.arg(successor_incident_id),
    updated_at = sqlc.arg(consumed_at), consumed_at = sqlc.arg(consumed_at)
WHERE recovery_id = sqlc.arg(recovery_id)
  AND predecessor_incident_id = sqlc.arg(predecessor_incident_id)
  AND predecessor_identity_digest = sqlc.arg(predecessor_identity_digest)
  AND predecessor_input_digest = sqlc.arg(predecessor_input_digest)
  AND predecessor_model_action_id = sqlc.arg(predecessor_model_action_id)
  AND predecessor_schema_digest = sqlc.arg(predecessor_schema_digest)
  AND provider_error_digest = sqlc.arg(provider_error_digest)
  AND provider_inference_tokens = 0
  AND successor_generation = sqlc.arg(successor_generation)
  AND status = 'authorized'
  AND successor_incident_id = '';

-- name: GetDCPFutureArbiterResultValidationRecovery :one
SELECT * FROM dcp_future_card_arbiter_result_validation_recovery_v1
WHERE incident_id = ?;

-- name: RecoverDCPFutureArbiterExactDecision :execrows
UPDATE dcp_future_card_arbiter_v1
SET status='repair_queued', decision_json=sqlc.arg(decision_json),
    decision_digest=sqlc.arg(decision_digest), verdict='successor_repair',
    order_json=sqlc.arg(order_json), repair_task_id=sqlc.arg(repair_task_id),
    repair_objective=sqlc.arg(repair_objective),
    repair_paths_json=sqlc.arg(repair_paths_json), human_question='',
    repair_action_id=sqlc.arg(repair_action_id), error_code='',
    updated_at=sqlc.arg(decision_at), decision_at=sqlc.arg(decision_at),
    finished_at=NULL
WHERE dcp_future_card_arbiter_v1.incident_id=sqlc.arg(incident_id)
  AND dcp_future_card_arbiter_v1.identity_digest=sqlc.arg(identity_digest)
  AND dcp_future_card_arbiter_v1.input_digest=sqlc.arg(input_digest)
  AND dcp_future_card_arbiter_v1.model_action_id=sqlc.arg(model_action_id)
  AND dcp_future_card_arbiter_v1.status='failed' AND dcp_future_card_arbiter_v1.error_code='launch_failed'
  AND dcp_future_card_arbiter_v1.model_call_count=1
  AND dcp_future_card_arbiter_v1.decision_json='' AND dcp_future_card_arbiter_v1.decision_digest=''
  AND EXISTS (
    SELECT 1
    FROM dcp_future_card_arbiter_result_validation_recovery_v1 recovery
    WHERE recovery.incident_id=dcp_future_card_arbiter_v1.incident_id
      AND recovery.identity_digest=dcp_future_card_arbiter_v1.identity_digest
      AND recovery.input_digest=dcp_future_card_arbiter_v1.input_digest
      AND recovery.model_action_id=dcp_future_card_arbiter_v1.model_action_id
      AND recovery.prior_status=dcp_future_card_arbiter_v1.status
      AND recovery.prior_error_code=dcp_future_card_arbiter_v1.error_code
      AND recovery.prior_finished_at=dcp_future_card_arbiter_v1.finished_at
      AND recovery.prior_model_call_count=dcp_future_card_arbiter_v1.model_call_count
      AND recovery.prior_decision_digest=dcp_future_card_arbiter_v1.decision_digest
      AND recovery.status='pending'
  );

-- name: MarkDCPFutureArbiterResultValidationRecoveryApplied :execrows
UPDATE dcp_future_card_arbiter_result_validation_recovery_v1
SET status='applied', error_code='', finished_at=?
WHERE incident_id=? AND status='pending';

-- name: FailDCPFutureArbiterResultValidationRecovery :execrows
UPDATE dcp_future_card_arbiter_result_validation_recovery_v1
SET status='failed', error_code=?, finished_at=?
WHERE incident_id=? AND status='pending';

-- name: ClaimDCPFutureArbiterIncident :execrows
UPDATE dcp_future_card_arbiter_v1 SET status='claimed', updated_at=?
WHERE incident_id=? AND model_action_id=? AND status='requested' AND model_call_count=0;

-- name: StartDCPFutureArbiterIncident :execrows
UPDATE dcp_future_card_arbiter_v1 SET status='running', model_call_count=1, updated_at=?
WHERE incident_id=? AND model_action_id=? AND status='claimed' AND model_call_count=0;

-- name: FailDCPFutureArbiterIncident :execrows
UPDATE dcp_future_card_arbiter_v1
SET status='failed', error_code=?, updated_at=?, finished_at=?
WHERE incident_id=? AND status IN ('claimed','running') AND decision_json='';

-- name: DecideDCPFutureArbiterIncident :execrows
UPDATE dcp_future_card_arbiter_v1
SET status=?, decision_json=?, decision_digest=?, verdict=?, order_json=?,
    repair_task_id=?, repair_objective=?, repair_paths_json=?, human_question=?,
    repair_action_id=?, updated_at=?, decision_at=?, finished_at=?
WHERE incident_id=? AND identity_digest=? AND input_digest=?
  AND status='running' AND model_call_count=1 AND decision_json='';

-- name: MarkDCPFutureArbiterRecoveryReviewed :execrows
UPDATE dcp_future_card_arbiter_v1
SET status='recovery_reviewed', recovery_review_run_id=?, recovery_head_sha=?, updated_at=?
WHERE incident_id=? AND status='repair_queued' AND verdict='successor_repair'
  AND repair_action_id<>'' AND recovery_review_run_id='' AND recovery_head_sha='';

-- name: RebindDCPFutureArbiterAdmission :execrows
UPDATE dcp_review_lab_admission
SET review_run_id=?, review_id=?, target_sha=?, review_base_sha=?, status='waiting',
    lease_id='', admitted_base_sha='', error_code='',
    recovered_incident_packet=incident_packet, incident_packet='', updated_at=?
WHERE id=? AND review_run_id=? AND session_id=? AND pr_url=? AND target_sha=?
  AND status='incident' AND lease_id=? AND error_code=?
  AND recovered_incident_packet='';

-- name: MarkDCPFutureArbiterSucceeded :execrows
UPDATE dcp_future_card_arbiter_v1
SET status='succeeded', merge_commit_sha=?, updated_at=?, finished_at=?
WHERE incident_id=? AND status='recovery_reviewed' AND recovery_head_sha=?
  AND recovery_review_run_id=? AND merge_commit_sha='';
