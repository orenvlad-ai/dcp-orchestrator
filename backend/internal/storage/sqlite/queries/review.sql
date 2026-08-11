-- name: UpsertReview :exec
INSERT INTO review (id, session_id, project_id, harness, pr_url, reviewer_handle_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (session_id) DO UPDATE SET
    harness = excluded.harness,
    pr_url = excluded.pr_url,
    reviewer_handle_id = excluded.reviewer_handle_id,
    updated_at = excluded.updated_at;

-- name: GetReviewBySession :one
SELECT id, session_id, project_id, harness, pr_url, reviewer_handle_id, created_at, updated_at
FROM review WHERE session_id = ?;

-- name: InsertReviewRun :exec
INSERT INTO review_run (id, review_id, session_id, batch_id, harness, pr_url, target_sha, status, verdict, body, github_review_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateReviewRunResult :execrows
UPDATE review_run SET status = ?, verdict = ?, body = ?, github_review_id = ? WHERE id = ? AND status = 'running';

-- name: UpdateBoundReviewRunResult :execrows
UPDATE review_run
SET status = 'complete', verdict = sqlc.arg(verdict), body = sqlc.arg(body), github_review_id = '', result_channel = 'structured_dcp_v1'
WHERE review_run.id = sqlc.arg(run_id)
  AND review_run.session_id = sqlc.arg(session_id)
  AND review_run.batch_id = sqlc.arg(batch_id)
  AND review_run.pr_url = sqlc.arg(pr_url)
  AND review_run.target_sha = sqlc.arg(target_sha)
  AND review_run.status = 'running'
  AND review_run.verdict = ''
  AND EXISTS (
    SELECT 1
    FROM review
    WHERE review.id = review_run.review_id
      AND review.session_id = review_run.session_id
      AND review.reviewer_handle_id = sqlc.arg(reviewer_handle_id)
  )
  AND EXISTS (
    SELECT 1
    FROM pr
    WHERE pr.url = review_run.pr_url
      AND pr.session_id = review_run.session_id
      AND pr.head_sha = review_run.target_sha
      AND pr.pr_state = 'open'
      AND pr.is_draft = 0
      AND pr.is_merged = 0
      AND pr.is_closed = 0
  );

-- name: SupersedeStaleRunningReviewRuns :execrows
UPDATE review_run SET status = 'failed', body = ? WHERE session_id = ? AND pr_url = ? AND target_sha != ? AND status = 'running' AND verdict = '';

-- name: CancelRunningReviewRunsBySession :execrows
UPDATE review_run SET status = 'cancelled', body = ? WHERE session_id = ? AND status = 'running' AND verdict = '';

-- name: MarkReviewRunDelivered :execrows
UPDATE review_run SET status = 'delivered', delivered_at = ? WHERE id = ? AND status = 'complete' AND delivered_at IS NULL;

-- name: GetReviewRun :one
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE id = ?;

-- name: GetReviewRunBySessionPRAndSHA :one
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE session_id = ? AND pr_url = ? AND target_sha = ? ORDER BY created_at DESC LIMIT 1;

-- name: GetReviewRunBySessionPRSHAAndHarness :one
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE session_id = ? AND pr_url = ? AND target_sha = ? AND harness = ? ORDER BY created_at DESC LIMIT 1;

-- name: ListReviewRunsBySession :many
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE session_id = ? ORDER BY created_at DESC;

-- name: ListRunningReviewRunsBySession :many
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE session_id = ? AND status = 'running' AND verdict = '' ORDER BY created_at DESC;

-- name: ListReviewRunsByBatch :many
SELECT id, review_id, session_id, harness, pr_url, target_sha, status, verdict, body, created_at, github_review_id, delivered_at, batch_id
     , result_channel, terminal_merge_status, terminal_merge_commit_sha, terminal_merge_error
FROM review_run WHERE session_id = ? AND batch_id = ? ORDER BY created_at ASC, id ASC;

-- name: ClaimDCPReviewLabTerminalMerge :execrows
UPDATE review_run
SET terminal_merge_status = 'running', terminal_merge_error = ''
WHERE review_run.id = sqlc.arg(run_id)
  AND review_run.session_id = sqlc.arg(session_id)
  AND review_run.pr_url = sqlc.arg(pr_url)
  AND review_run.target_sha = sqlc.arg(target_sha)
  AND review_run.status = 'complete'
  AND review_run.verdict = 'approved'
  AND review_run.result_channel = 'structured_dcp_v1'
  AND review_run.terminal_merge_status = ''
  AND EXISTS (
    SELECT 1
    FROM pr
    WHERE pr.url = review_run.pr_url
      AND pr.session_id = review_run.session_id
      AND pr.head_sha = review_run.target_sha
      AND pr.pr_state = 'open'
      AND pr.is_draft = 0
      AND pr.is_merged = 0
      AND pr.is_closed = 0
  );

-- name: CompleteDCPReviewLabTerminalMerge :execrows
UPDATE review_run
SET terminal_merge_status = 'succeeded',
    terminal_merge_commit_sha = sqlc.arg(merge_commit_sha),
    terminal_merge_error = ''
WHERE id = sqlc.arg(run_id)
  AND terminal_merge_status = 'running';

-- name: FailDCPReviewLabTerminalMerge :execrows
UPDATE review_run
SET terminal_merge_status = 'failed', terminal_merge_error = sqlc.arg(error_code)
WHERE id = sqlc.arg(run_id)
  AND terminal_merge_status = 'running';
