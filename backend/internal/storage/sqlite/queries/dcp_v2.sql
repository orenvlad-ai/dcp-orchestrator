-- name: GetDCPV2Stage5Activation :one
SELECT * FROM dcp_v2_stage5_activation WHERE activation_id = ?;

-- name: CountDCPV2LifecycleRows :one
SELECT (SELECT count(*) FROM dcp_v2_task) +
       (SELECT count(*) FROM dcp_v2_revision) +
       (SELECT count(*) FROM dcp_v2_command) +
       (SELECT count(*) FROM dcp_v2_action) +
       (SELECT count(*) FROM dcp_v2_admission) +
       (SELECT count(*) FROM dcp_v2_incident) +
       (SELECT count(*) FROM dcp_v2_external_event) +
       (SELECT count(*) FROM dcp_v2_result);

-- name: InsertDCPV2Stage5Activation :exec
INSERT INTO dcp_v2_stage5_activation (
    activation_id, authority_commit, source_commit, source_tree,
    install_receipt_sha, target_spec_version, target_policy_digest,
    repository, repository_id, owner_id, base_ref, required_check,
    issuer_kind, issuer_actor, issuer_event, issuer_event_type, workflow_id,
    environment, service, adapter, activated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertDCPV2Task :exec
INSERT INTO dcp_v2_task (
    task_id, target_spec_version, repository, repository_id, owner_id, base_ref,
    profile, request_digest, scope_digest, policy_digest, initial_worker_budget,
    repair_budget, repair_used, max_readmissions, readmission_count,
    current_revision_id, state, state_revision, terminal_result_id,
    human_gate_question, error_code, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Task :one
SELECT * FROM dcp_v2_task WHERE task_id = ?;

-- name: ListDCPV2Tasks :many
SELECT * FROM dcp_v2_task ORDER BY created_at, task_id;

-- name: UpdateDCPV2TaskCAS :execrows
UPDATE dcp_v2_task SET
    repair_used = ?, readmission_count = ?, current_revision_id = ?, state = ?,
    state_revision = state_revision + 1, terminal_result_id = ?,
    human_gate_question = ?, error_code = ?, updated_at = ?
WHERE task_id = ? AND state_revision = ? AND current_revision_id = ? AND state = ?;

-- name: InsertDCPV2Revision :exec
INSERT INTO dcp_v2_revision (
    revision_id, task_id, sequence, kind, repository, base_ref, base_sha, head_ref,
    head_sha, predecessor_revision_id, cause_command_id, pr_number,
    evidence_digest, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Revision :one
SELECT * FROM dcp_v2_revision WHERE revision_id = ?;

-- name: ListDCPV2RevisionsByTask :many
SELECT * FROM dcp_v2_revision WHERE task_id = ? ORDER BY sequence;

-- name: InsertDCPV2Command :exec
INSERT INTO dcp_v2_command (
    command_id, task_id, revision_id, kind, payload_json, payload_digest,
    prerequisite_digest, idempotency_key, status, lease_owner, lease_epoch,
    lease_token, effect_fence, recovery_generation, result_digest, error_code,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Command :one
SELECT * FROM dcp_v2_command WHERE command_id = ?;

-- name: GetDCPV2CommandByIdempotencyKey :one
SELECT * FROM dcp_v2_command WHERE idempotency_key = ?;

-- name: ListDCPV2CommandsByTask :many
SELECT * FROM dcp_v2_command WHERE task_id = ? ORDER BY sequence;

-- name: ListPendingDCPV2Commands :many
SELECT * FROM dcp_v2_command WHERE status = 'pending' ORDER BY sequence;

-- name: ListLeasedDCPV2Commands :many
SELECT * FROM dcp_v2_command WHERE status = 'leased' ORDER BY sequence;

-- name: LeaseDCPV2Command :execrows
UPDATE dcp_v2_command SET
    status = 'leased', lease_owner = ?, lease_epoch = ?, lease_token = ?, updated_at = ?
WHERE command_id = ? AND status = 'pending';

-- name: FenceDCPV2CommandEffect :execrows
UPDATE dcp_v2_command SET effect_fence = ?, updated_at = ?
WHERE command_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ?
  AND lease_token = ? AND effect_fence = '';

-- name: RecoverDCPV2CommandLease :execrows
UPDATE dcp_v2_command SET
    lease_owner = ?, lease_epoch = ?, lease_token = ?,
    recovery_generation = recovery_generation + 1, updated_at = ?
WHERE command_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ?
  AND lease_token = ? AND effect_fence = '' AND recovery_generation = ?;

-- name: FinishDCPV2Command :execrows
UPDATE dcp_v2_command SET
    status = ?, result_digest = ?, error_code = ?, updated_at = ?
WHERE command_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ?
  AND lease_token = ?;

-- name: InsertDCPV2Action :exec
INSERT INTO dcp_v2_action (
    action_id, command_id, task_id, revision_id, role, model, reasoning,
    token_budget, time_budget_sec, input_digest, attempt, status, slot,
    launch_fence, runtime_id, result_digest, error_code, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Action :one
SELECT * FROM dcp_v2_action WHERE action_id = ?;

-- name: GetDCPV2ActionByCommand :one
SELECT * FROM dcp_v2_action WHERE command_id = ?;

-- name: ListDCPV2ActionsByTask :many
SELECT * FROM dcp_v2_action WHERE task_id = ? ORDER BY sequence;

-- name: ListQueuedDCPV2Actions :many
SELECT * FROM dcp_v2_action WHERE status = 'queued' ORDER BY sequence;

-- name: ListActiveDCPV2Actions :many
SELECT * FROM dcp_v2_action WHERE status IN ('launching','running') ORDER BY slot;

-- name: LaunchDCPV2Action :execrows
UPDATE dcp_v2_action SET status = 'launching', slot = ?, launch_fence = ?, updated_at = ?
WHERE action_id = ? AND status = 'queued' AND slot = 0 AND launch_fence = '';

-- name: StartDCPV2Action :execrows
UPDATE dcp_v2_action SET status = 'running', runtime_id = ?, updated_at = ?
WHERE action_id = ? AND status = 'launching' AND slot = ? AND launch_fence = ?;

-- name: FinishDCPV2Action :execrows
UPDATE dcp_v2_action SET
    status = sqlc.arg(status), slot = 0, runtime_id = sqlc.arg(runtime_id),
    result_digest = sqlc.arg(result_digest), error_code = sqlc.arg(error_code), updated_at = sqlc.arg(updated_at)
WHERE action_id = sqlc.arg(action_id) AND status IN ('launching','running')
  AND slot = sqlc.arg(slot) AND launch_fence = sqlc.arg(launch_fence)
  AND runtime_id IN ('', sqlc.arg(runtime_id));

-- name: InsertDCPV2ModelRuntime :exec
INSERT INTO dcp_v2_model_runtime (
    runtime_id, action_id, command_id, task_id, revision_id, slot,
    launch_fence, provider_request_id, provider_request_digest, worktree_path,
    worktree_digest, state,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2ModelRuntime :one
SELECT * FROM dcp_v2_model_runtime WHERE runtime_id = ?;

-- name: GetDCPV2ModelRuntimeByAction :one
SELECT * FROM dcp_v2_model_runtime WHERE action_id = ?;

-- name: ListActiveDCPV2ModelRuntimes :many
SELECT * FROM dcp_v2_model_runtime WHERE state IN ('reserved','running') ORDER BY slot;

-- name: StartDCPV2ModelRuntime :execrows
UPDATE dcp_v2_model_runtime SET
    provider_request_id = ?, provider_request_digest = ?, state = 'running', updated_at = ?
WHERE runtime_id = ? AND action_id = ? AND launch_fence = ? AND state = 'reserved';

-- name: FinishDCPV2ModelRuntime :execrows
UPDATE dcp_v2_model_runtime SET state = ?, updated_at = ?
WHERE runtime_id = ? AND action_id = ? AND launch_fence = ? AND state = 'running';

-- name: InsertDCPV2ModelTerminalReceipt :exec
INSERT INTO dcp_v2_model_terminal_receipt (
    receipt_id, action_id, command_id, task_id, revision_id, runtime_id,
    launch_fence, status, result_digest, error_code, output_json,
    output_digest, head_ref, head_sha, tree_sha, base_sha, worktree_path, worktree_digest,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2ModelTerminalReceipt :one
SELECT * FROM dcp_v2_model_terminal_receipt WHERE receipt_id = ?;

-- name: GetDCPV2ModelTerminalReceiptByAction :one
SELECT * FROM dcp_v2_model_terminal_receipt WHERE action_id = ?;

-- name: InsertDCPV2Stage6WorkerAdoption :exec
INSERT INTO dcp_v2_stage6_worker_adoption_v1 (
    adoption_id, task_id, revision_id, command_id, action_id, runtime_id,
    native_action_id, native_sequence, legacy_evidence_digest, commit_sha,
    tree_sha, branch, worktree_digest, output_digest, receipt_id, consumed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Stage6WorkerAdoption :one
SELECT * FROM dcp_v2_stage6_worker_adoption_v1 WHERE adoption_id = ?;

-- name: InsertDCPV2Admission :exec
INSERT INTO dcp_v2_admission (
    admission_id, line_key, task_id, revision_id, pr_number, head_sha, base_sha,
    main_sha, required_check_id, review_id, manifest_digest, status, lease_token,
    lease_owner, lease_epoch, dispatch_fence, recovery_generation,
    result_id, error_code, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Admission :one
SELECT * FROM dcp_v2_admission WHERE admission_id = ?;

-- name: ListDCPV2AdmissionsByTask :many
SELECT * FROM dcp_v2_admission WHERE task_id = ? ORDER BY sequence;

-- name: ListDCPV2AdmissionsByLine :many
SELECT * FROM dcp_v2_admission WHERE line_key = ? ORDER BY sequence;

-- name: ListLeasedDCPV2Admissions :many
SELECT * FROM dcp_v2_admission WHERE status IN ('leased','dispatched') ORDER BY line_key, sequence;

-- name: GetNextWaitingDCPV2Admission :one
SELECT * FROM dcp_v2_admission
WHERE line_key = ? AND status = 'waiting'
ORDER BY sequence LIMIT 1;

-- name: LeaseDCPV2Admission :execrows
UPDATE dcp_v2_admission SET status = 'leased', lease_owner = ?, lease_epoch = ?, lease_token = ?, updated_at = ?
WHERE admission_id = ? AND status = 'waiting' AND lease_token = '';

-- name: FenceDCPV2AdmissionDispatch :execrows
UPDATE dcp_v2_admission SET dispatch_fence = ?, updated_at = ?
WHERE admission_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ?
  AND lease_token = ? AND dispatch_fence = '';

-- name: RecoverDCPV2AdmissionLease :execrows
UPDATE dcp_v2_admission SET
    lease_owner = ?, lease_epoch = ?, lease_token = ?,
    recovery_generation = recovery_generation + 1, updated_at = ?
WHERE admission_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ?
  AND lease_token = ? AND dispatch_fence = '' AND recovery_generation = ?;

-- name: FinishDCPV2Admission :execrows
UPDATE dcp_v2_admission SET status = ?, result_id = ?, error_code = ?, updated_at = ?
WHERE admission_id = ? AND status IN ('leased','dispatched') AND lease_owner = ? AND lease_epoch = ? AND lease_token = ?;

-- name: DispatchDCPV2Admission :execrows
UPDATE dcp_v2_admission SET status = 'dispatched', updated_at = ?
WHERE admission_id = ? AND status = 'leased' AND lease_owner = ? AND lease_epoch = ? AND lease_token = ?
  AND dispatch_fence <> '';

-- name: InsertDCPV2ExternalEvent :exec
INSERT INTO dcp_v2_external_event (
    delivery_id, provider, task_id, revision_id, kind, provider_sequence,
    payload_digest, prerequisite_digest, status, command_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2ExternalEvent :one
SELECT * FROM dcp_v2_external_event WHERE delivery_id = ?;

-- name: ListDCPV2ExternalEventsByTask :many
SELECT * FROM dcp_v2_external_event WHERE task_id = ? ORDER BY provider_sequence, created_at, delivery_id;

-- name: ApplyDCPV2ExternalEvent :execrows
UPDATE dcp_v2_external_event SET status = ?, command_id = ?, updated_at = ?
WHERE delivery_id = ? AND status = 'retained';

-- name: InsertDCPV2Incident :exec
INSERT INTO dcp_v2_incident (
    incident_id, task_id, revision_id, cause_command_id, kind,
    evidence_digest, disposition, owner_question, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDCPV2IncidentsByTask :many
SELECT * FROM dcp_v2_incident WHERE task_id = ? ORDER BY created_at, incident_id;

-- name: InsertDCPV2Result :exec
INSERT INTO dcp_v2_result (
    result_id, task_id, revision_id, admission_id, command_id, kind,
    provider, proof_id, run_id, actor, manifest_digest, proof_digest,
    merge_sha, artifact_digest, deployed_sha, environment,
    service, probe_digest, verified, error_code, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPV2Result :one
SELECT * FROM dcp_v2_result WHERE result_id = ?;

-- name: ListDCPV2ResultsByTask :many
SELECT * FROM dcp_v2_result WHERE task_id = ? ORDER BY created_at, result_id;
