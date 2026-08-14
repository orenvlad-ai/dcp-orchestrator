-- name: GetDCPReviewLabPolicyTaskByTaskID :one
SELECT * FROM dcp_review_lab_policy_task WHERE task_id = ?;

-- name: GetDCPReviewLabPolicyTaskBySessionID :one
SELECT * FROM dcp_review_lab_policy_task WHERE session_id = ?;

-- name: ListDCPReviewLabPolicyTasks :many
SELECT * FROM dcp_review_lab_policy_task ORDER BY created_at, task_id;

-- name: InsertDCPReviewLabPolicyTask :exec
INSERT INTO dcp_review_lab_policy_task (
    task_id, payload_json, payload_digest, target, profile, repository,
    policy_version, session_id, card_number, worktree_path, source_branch,
    prompt, state, revision, repair_count, pr_url, pr_number,
    current_head_sha, previous_head_sha, review_run_id, admission_id,
    merge_commit_sha, error_code, incident_packet, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateDCPReviewLabPolicyTask :execrows
UPDATE dcp_review_lab_policy_task SET
    state = ?, revision = revision + 1, repair_count = ?, pr_url = ?,
    pr_number = ?, current_head_sha = ?, previous_head_sha = ?,
    review_run_id = ?, admission_id = ?, merge_commit_sha = ?,
    error_code = ?, incident_packet = ?, updated_at = ?
WHERE task_id = ? AND state = ? AND revision = ?;

-- name: InsertDCPModelAction :exec
INSERT INTO dcp_model_action (
    id, task_id, session_id, kind, exact_head_sha, status, slot,
    launch_id, review_run_id, incident_id, error_code, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDCPModelActionByID :one
SELECT * FROM dcp_model_action WHERE id = ?;

-- name: GetDCPModelActionByIdentity :one
SELECT * FROM dcp_model_action
WHERE task_id = ? AND kind = ? AND exact_head_sha = ? AND incident_id = ?;

-- name: GetActiveDCPModelActionBySession :one
SELECT * FROM dcp_model_action
WHERE session_id = ? AND status IN ('claimed', 'running')
ORDER BY sequence LIMIT 1;

-- name: ListDCPModelActions :many
SELECT * FROM dcp_model_action ORDER BY sequence;

-- name: ListQueuedDCPModelActions :many
SELECT * FROM dcp_model_action WHERE status = 'queued' ORDER BY sequence;

-- name: ListActiveDCPModelActions :many
SELECT * FROM dcp_model_action WHERE status IN ('claimed', 'running') ORDER BY slot;

-- name: ClaimDCPModelAction :execrows
UPDATE dcp_model_action SET status = 'claimed', slot = ?, updated_at = ?
WHERE id = ? AND status = 'queued' AND slot = 0;

-- name: StartDCPModelAction :execrows
UPDATE dcp_model_action SET status = 'running', launch_id = ?, review_run_id = ?, updated_at = ?
WHERE id = ? AND status = 'claimed' AND slot = ?;

-- name: FinishDCPModelAction :execrows
UPDATE dcp_model_action SET status = ?, slot = 0, error_code = ?, updated_at = ?
WHERE id = ? AND status IN ('claimed', 'running') AND slot = ?;
