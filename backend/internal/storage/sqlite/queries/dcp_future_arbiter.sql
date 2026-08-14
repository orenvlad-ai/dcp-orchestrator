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
