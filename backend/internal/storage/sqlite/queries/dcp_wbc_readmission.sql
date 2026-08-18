-- name: InsertDCPWBCReadmissionGeneration :execrows
INSERT INTO dcp_wbc_readmission_generation (
    generation_id, marker_digest, marker_version, marker_comment_id,
    marker_author, marker_created_at, marker_updated_at, marker_main_sha, task_id, session_id,
    old_admission_id, pr_url, pr_number, repository, base_branch, scope,
    head_ref, session_number, admitted_head_sha, admitted_base_sha, observed_head_sha,
    current_main_sha, ready_event_id, admission_check_id, handoff_proof_id,
    reason, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'observed', ?, ?)
ON CONFLICT (marker_comment_id) DO NOTHING;

-- name: GetDCPWBCReadmissionGenerationByID :one
SELECT * FROM dcp_wbc_readmission_generation WHERE generation_id = ?;

-- name: GetDCPWBCReadmissionGenerationByMarkerComment :one
SELECT * FROM dcp_wbc_readmission_generation WHERE marker_comment_id = ?;

-- name: GetOpenDCPWBCReadmissionGenerationByTask :one
SELECT * FROM dcp_wbc_readmission_generation
WHERE task_id = ? AND status NOT IN ('terminal', 'conflict', 'failed')
ORDER BY sequence DESC LIMIT 1;

-- name: GetLatestDCPWBCReadmissionGenerationByTask :one
SELECT * FROM dcp_wbc_readmission_generation
WHERE task_id = ? ORDER BY sequence DESC LIMIT 1;

-- name: ListDCPWBCReadmissionGenerations :many
SELECT * FROM dcp_wbc_readmission_generation ORDER BY sequence;

-- name: ClaimDCPWBCReadmissionGeneration :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'claimed', lease_id = ?, updated_at = ?
WHERE generation_id = ? AND status = 'observed' AND lease_id = '';

-- name: PrepareDCPWBCReadmissionGeneration :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'prepared', merge_tree_sha = ?, new_head_sha = ?, current_main_sha = ?, updated_at = ?
WHERE generation_id = ? AND status = 'claimed' AND lease_id = ?
  AND merge_tree_sha = '' AND new_head_sha = '' AND current_main_sha = ?;

-- name: MarkDCPWBCReadmissionHeadPushed :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'head_pushed', updated_at = ?
WHERE generation_id = ? AND status = 'prepared' AND lease_id = ?
  AND new_head_sha = ?;

-- name: BindDCPWBCReadmissionReviewAction :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'review_queued', review_action_id = ?, updated_at = ?
WHERE generation_id = ? AND new_head_sha <> ''
  AND ((status = 'head_pushed' AND review_action_id = '')
    OR (status = 'review_queued' AND review_action_id <> '' AND review_action_id <> ?));

-- name: BindDCPWBCReadmissionReviewRun :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'reviewed', review_run_id = ?, updated_at = ?
WHERE generation_id = ? AND status = 'review_queued'
  AND review_action_id = ? AND review_run_id = '';

-- name: BindDCPWBCReadmissionAdmission :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'admitted', admission_id = ?, updated_at = ?
WHERE generation_id = ? AND status = 'reviewed' AND new_head_sha = ?
  AND review_run_id = ? AND admission_id = '';

-- name: MarkDCPWBCReadmissionReleaseWaiting :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'release_waiting', updated_at = ?
WHERE generation_id = ? AND status = 'admitted' AND admission_id = ?;

-- name: CompleteDCPWBCReadmissionGeneration :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'terminal', updated_at = ?
WHERE generation_id = ? AND status = 'release_waiting' AND admission_id = ?;

-- name: FailDCPWBCReadmissionGeneration :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'failed', error_code = ?, updated_at = ?
WHERE generation_id = ? AND status NOT IN ('terminal', 'conflict', 'failed');

-- name: ConflictDCPWBCReadmissionGeneration :execrows
UPDATE dcp_wbc_readmission_generation
SET status = 'conflict', error_code = ?, updated_at = ?
WHERE generation_id = ? AND status = 'claimed';
