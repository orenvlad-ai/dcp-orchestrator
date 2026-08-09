-- name: GetDCPTaskByID :one
SELECT * FROM dcp_tasks WHERE task_id = ?;

-- name: GetDCPTaskByIdempotencyKey :one
SELECT * FROM dcp_tasks WHERE idempotency_key = ?;

-- name: ListDCPTasks :many
SELECT * FROM dcp_tasks
WHERE (CAST(sqlc.arg(target_project_id) AS TEXT) = '' OR target_project_id = CAST(sqlc.arg(target_project_id) AS TEXT))
ORDER BY created_at DESC, task_id DESC;

-- name: InsertDCPTask :exec
INSERT INTO dcp_tasks (
    task_id, idempotency_key, approved_task_json, approved_scope_json,
    approved_digest, target_project_id, target_repository, target_path,
    target_head_sha, target_marker_digest, target_identity_digest,
    state, revision, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateDCPTaskCAS :execrows
UPDATE dcp_tasks
SET state = ?, revision = revision + 1, updated_at = ?
WHERE task_id = ? AND state = ? AND revision = ?;

-- name: NextDCPTaskEventSequence :one
SELECT CAST(COALESCE(MAX(sequence), 0) + 1 AS INTEGER) AS sequence
FROM dcp_task_events WHERE task_id = ?;

-- name: InsertDCPTaskEvent :exec
INSERT INTO dcp_task_events (
    task_id, sequence, event_id, schema_version, event_type, source_kind,
    source_id, correlation_id, causation_id, idempotency_key, from_state,
    to_state, task_revision, occurred_at, recorded_at, payload_json,
    evidence_digest, integrity_digest
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDCPTaskEvents :many
SELECT * FROM dcp_task_events WHERE task_id = ? ORDER BY sequence;

-- name: CountDCPTaskEvents :one
SELECT COUNT(*) FROM dcp_task_events WHERE task_id = ?;
