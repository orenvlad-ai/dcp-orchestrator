-- name: InsertDCPReviewLabAdmission :execrows
INSERT INTO dcp_review_lab_admission (
    id, review_run_id, review_id, session_id, pr_url, pr_number, target_sha,
    review_base_sha, status, created_at, updated_at
)
SELECT
    sqlc.arg(id), rr.id, rr.review_id, rr.session_id, rr.pr_url,
    sqlc.arg(pr_number), rr.target_sha, sqlc.arg(review_base_sha), 'waiting',
    sqlc.arg(created_at), sqlc.arg(updated_at)
FROM review_run rr
JOIN pr ON pr.url = rr.pr_url AND pr.session_id = rr.session_id
WHERE rr.id = sqlc.arg(review_run_id)
  AND rr.review_id = sqlc.arg(review_id)
  AND rr.session_id = sqlc.arg(session_id)
  AND rr.pr_url = sqlc.arg(pr_url)
  AND rr.target_sha = sqlc.arg(target_sha)
  AND rr.status = 'complete'
  AND rr.verdict = 'approved'
  AND rr.result_channel = 'structured_dcp_v1'
  AND rr.terminal_merge_status = ''
  AND pr.number = sqlc.arg(pr_number)
  AND pr.head_sha = rr.target_sha
  AND pr.pr_state = 'open'
  AND pr.is_draft = 0
  AND pr.is_merged = 0
  AND pr.is_closed = 0
ON CONFLICT (review_run_id) DO NOTHING;

-- name: GetDCPReviewLabAdmissionByRun :one
SELECT * FROM dcp_review_lab_admission WHERE review_run_id = ?;

-- name: GetDCPReviewLabAdmissionByID :one
SELECT * FROM dcp_review_lab_admission WHERE id = ?;

-- name: ListDCPReviewLabAdmissions :many
SELECT * FROM dcp_review_lab_admission ORDER BY sequence ASC;

-- name: GetClaimedDCPReviewLabAdmission :one
SELECT * FROM dcp_review_lab_admission WHERE status = 'claimed' LIMIT 1;

-- name: GetNextWaitingDCPReviewLabAdmission :one
SELECT * FROM dcp_review_lab_admission WHERE status = 'waiting' ORDER BY sequence ASC LIMIT 1;

-- name: GetRefreshingDCPReviewLabAdmissionBySession :one
SELECT * FROM dcp_review_lab_admission WHERE session_id = ? AND status = 'refreshing';

-- name: RecoverDCPReviewLabCanonicalBaseIncident :execrows
UPDATE dcp_review_lab_admission
SET status = 'waiting',
    lease_id = '',
    admitted_base_sha = '',
    error_code = '',
    recovered_incident_packet = incident_packet,
    incident_packet = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.pr_url = sqlc.arg(pr_url)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(target_sha)
  AND dcp_review_lab_admission.status = 'incident'
  AND dcp_review_lab_admission.error_code = 'canonical_main_diverged'
  AND dcp_review_lab_admission.lease_id = 'dcp-incident-' || dcp_review_lab_admission.id
  AND dcp_review_lab_admission.refresh_wake_count = 0
  AND dcp_review_lab_admission.recovered_incident_packet = ''
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.schemaVersion') = 'dcp.review-lab.arbiter-needed/v1'
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.reason') = 'canonical_main_diverged'
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.admissionId') = dcp_review_lab_admission.id
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.sessionId') = dcp_review_lab_admission.session_id
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.reviewRunId') = dcp_review_lab_admission.review_run_id
  AND json_extract(dcp_review_lab_admission.incident_packet, '$.targetSha') = dcp_review_lab_admission.target_sha
  AND EXISTS (
    SELECT 1 FROM review_run rr
    WHERE rr.id = dcp_review_lab_admission.review_run_id
      AND rr.session_id = dcp_review_lab_admission.session_id
      AND rr.pr_url = dcp_review_lab_admission.pr_url
      AND rr.target_sha = dcp_review_lab_admission.target_sha
      AND rr.status = 'complete'
      AND rr.verdict = 'approved'
      AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = ''
  );

-- name: ResumeDCPReviewLabAdmissionAfterRefresh :execrows
UPDATE dcp_review_lab_admission
SET review_run_id = sqlc.arg(new_review_run_id),
    review_id = sqlc.arg(new_review_id),
    target_sha = sqlc.arg(new_target_sha),
    review_base_sha = sqlc.arg(new_review_base_sha),
    status = 'waiting',
    lease_id = '',
    error_code = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(old_review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.pr_url = sqlc.arg(pr_url)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(old_target_sha)
  AND dcp_review_lab_admission.status = 'refreshing'
  AND dcp_review_lab_admission.lease_id = sqlc.arg(expected_lease_id)
  AND dcp_review_lab_admission.refresh_wake_count = 1
  AND sqlc.arg(new_target_sha) <> dcp_review_lab_admission.target_sha
  AND EXISTS (
    SELECT 1
    FROM review_run rr
    JOIN pr ON pr.url = rr.pr_url AND pr.session_id = rr.session_id
    WHERE rr.id = sqlc.arg(new_review_run_id)
      AND rr.review_id = sqlc.arg(new_review_id)
      AND rr.session_id = sqlc.arg(session_id)
      AND rr.pr_url = sqlc.arg(pr_url)
      AND rr.target_sha = sqlc.arg(new_target_sha)
      AND rr.status = 'complete'
      AND rr.verdict = 'approved'
      AND rr.result_channel = 'structured_dcp_v1'
      AND rr.terminal_merge_status = ''
      AND pr.head_sha = rr.target_sha
      AND pr.pr_state = 'open'
      AND pr.is_draft = 0
      AND pr.is_merged = 0
      AND pr.is_closed = 0
  );

-- name: ClaimDCPReviewLabAdmission :execrows
UPDATE dcp_review_lab_admission
SET status = 'claimed',
    lease_id = sqlc.arg(lease_id),
    admitted_base_sha = sqlc.arg(admitted_base_sha),
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.pr_url = sqlc.arg(pr_url)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(target_sha)
  AND dcp_review_lab_admission.status = 'waiting'
  AND dcp_review_lab_admission.lease_id = ''
  AND dcp_review_lab_admission.sequence = (SELECT MIN(sequence) FROM dcp_review_lab_admission WHERE status = 'waiting')
  AND NOT EXISTS (SELECT 1 FROM dcp_review_lab_admission WHERE status IN ('claimed', 'refreshing', 'incident'));

-- name: CompleteDCPReviewLabAdmission :execrows
UPDATE dcp_review_lab_admission
SET status = 'succeeded',
    merge_commit_sha = sqlc.arg(merge_commit_sha),
    error_code = '',
    incident_packet = '',
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.lease_id = sqlc.arg(lease_id)
  AND dcp_review_lab_admission.status = 'claimed';

-- name: FailDCPReviewLabAdmission :execrows
UPDATE dcp_review_lab_admission
SET status = 'failed',
    error_code = sqlc.arg(error_code),
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.lease_id = sqlc.arg(lease_id)
  AND dcp_review_lab_admission.status = 'claimed';

-- name: StartDCPReviewLabRefresh :execrows
UPDATE dcp_review_lab_admission
SET status = 'refreshing',
    lease_id = sqlc.arg(lease_id),
    admitted_base_sha = sqlc.arg(admitted_base_sha),
    refresh_wake_count = 1,
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(target_sha)
  AND dcp_review_lab_admission.status = 'waiting'
  AND dcp_review_lab_admission.lease_id = ''
  AND dcp_review_lab_admission.refresh_wake_count = 0
  AND dcp_review_lab_admission.sequence = (SELECT MIN(sequence) FROM dcp_review_lab_admission WHERE status = 'waiting')
  AND NOT EXISTS (SELECT 1 FROM dcp_review_lab_admission WHERE status IN ('claimed', 'refreshing', 'incident'))
  AND NOT EXISTS (
    SELECT 1 FROM dcp_review_lab_admission prior
    WHERE prior.session_id = dcp_review_lab_admission.session_id
      AND prior.refresh_wake_count = 1
  );

-- name: RecordDCPReviewLabIncident :execrows
UPDATE dcp_review_lab_admission
SET status = 'incident',
    lease_id = CASE WHEN lease_id = '' THEN sqlc.arg(lease_id) ELSE lease_id END,
    admitted_base_sha = CASE WHEN admitted_base_sha = '' THEN sqlc.arg(admitted_base_sha) ELSE admitted_base_sha END,
    error_code = sqlc.arg(error_code),
    incident_packet = sqlc.arg(incident_packet),
    updated_at = sqlc.arg(updated_at)
WHERE dcp_review_lab_admission.id = sqlc.arg(id)
  AND dcp_review_lab_admission.review_run_id = sqlc.arg(review_run_id)
  AND dcp_review_lab_admission.session_id = sqlc.arg(session_id)
  AND dcp_review_lab_admission.target_sha = sqlc.arg(target_sha)
  AND dcp_review_lab_admission.status IN ('waiting', 'claimed', 'refreshing')
  AND (dcp_review_lab_admission.lease_id = '' OR dcp_review_lab_admission.lease_id = sqlc.arg(expected_lease_id));
