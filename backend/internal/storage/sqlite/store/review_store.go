package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// UpsertReview inserts the per-worker review row, or reuses the existing one
// (session_id is unique) by refreshing its harness/pr_url/updated_at.
func (s *Store) UpsertReview(ctx context.Context, r domain.Review) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.UpsertReview(ctx, gen.UpsertReviewParams{
		ID:               r.ID,
		SessionID:        r.SessionID,
		ProjectID:        r.ProjectID,
		Harness:          r.Harness,
		PRURL:            r.PRURL,
		ReviewerHandleID: r.ReviewerHandleID,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	})
}

// GetReviewBySession returns the review row for a worker session, ok=false if none.
func (s *Store) GetReviewBySession(ctx context.Context, id domain.SessionID) (domain.Review, bool, error) {
	row, err := s.qr.GetReviewBySession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Review{}, false, nil
	}
	if err != nil {
		return domain.Review{}, false, fmt.Errorf("get review by session %s: %w", id, err)
	}
	return reviewFromRow(row), true, nil
}

// InsertReviewRun records a new review pass. A unique-constraint hit on the
// (session_id, pr_url, target_sha) index (migration 0020) is surfaced as the sentinel
// domain.ErrDuplicateReviewRun so the engine can fall back to the existing run.
func (s *Store) InsertReviewRun(ctx context.Context, r domain.ReviewRun) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "insert review run", func(q *gen.Queries) error {
		if err := q.InsertReviewRun(ctx, gen.InsertReviewRunParams{
			ID: r.ID, ReviewID: r.ReviewID, SessionID: r.SessionID, BatchID: r.BatchID,
			Harness: r.Harness, PRURL: r.PRURL, TargetSha: r.TargetSHA,
			Status: r.Status, Verdict: r.Verdict, Body: r.Body,
			GithubReviewID: r.GithubReviewID, CreatedAt: r.CreatedAt,
		}); err != nil {
			return err
		}
		finalization, finalizationErr := q.GetDCPCard12RebaseHeadFinalization(ctx, dcpCard12RebaseHeadFinalizationID)
		if finalizationErr == nil && finalization.SessionID == string(r.SessionID) && finalization.PRURL == r.PRURL && finalization.CandidateHead == r.TargetSHA {
			if finalization.Status != string(domain.DCPRebaseHeadFinalizationCandidateReady) || finalization.ReviewerModelCallCount != 0 {
				return errors.New("card-12 REBASE_HEAD finalization reviewer fence is already consumed")
			}
			n, fenceErr := q.FenceDCPCard12RebaseHeadFinalizationReview(ctx, gen.FenceDCPCard12RebaseHeadFinalizationReviewParams{
				ReviewRunID: r.ID, ReviewID: r.ReviewID, BatchID: r.BatchID,
				UpdatedAt: r.CreatedAt, SessionID: string(r.SessionID), PRURL: r.PRURL, TargetSha: r.TargetSHA,
			})
			if fenceErr != nil || n != 1 {
				return errors.Join(fenceErr, errors.New("card-12 REBASE_HEAD finalization reviewer fence was unavailable"))
			}
			return nil
		}
		if finalizationErr != nil && !errors.Is(finalizationErr, sql.ErrNoRows) {
			return finalizationErr
		}
		coldStart, coldStartErr := q.GetDCPCard12ColdStartRecovery(ctx, dcpCard12ColdStartRecoveryID)
		if coldStartErr == nil && coldStart.SessionID == string(r.SessionID) && coldStart.PRURL == r.PRURL && coldStart.NewHead == r.TargetSHA {
			if coldStart.Status != string(domain.DCPColdStartRecoveryCandidateReady) || coldStart.ReviewerModelCallCount != 0 {
				return errors.New("card-12 cold-start reviewer fence is already consumed")
			}
			n, fenceErr := q.FenceDCPCard12ColdStartRecoveryReview(ctx, gen.FenceDCPCard12ColdStartRecoveryReviewParams{
				ReviewRunID: r.ID, ReviewID: r.ReviewID, BatchID: r.BatchID,
				UpdatedAt: r.CreatedAt, SessionID: string(r.SessionID), PRURL: r.PRURL, TargetSha: r.TargetSHA,
			})
			if fenceErr != nil || n != 1 {
				return errors.Join(fenceErr, errors.New("card-12 cold-start reviewer fence was unavailable"))
			}
			return nil
		}
		if coldStartErr != nil && !errors.Is(coldStartErr, sql.ErrNoRows) {
			return coldStartErr
		}
		continuation, continuationErr := q.GetDCPCard12ModelFreeRebaseContinuation(ctx, dcpCard12ModelFreeRebaseContinuationID)
		if continuationErr == nil && continuation.SessionID == string(r.SessionID) && continuation.PRURL == r.PRURL && continuation.NewHead == r.TargetSHA {
			if continuation.Status != string(domain.DCPModelFreeRebaseCandidateReady) || continuation.ReviewerModelCallCount != 0 {
				return errors.New("card-12 model-free reviewer fence is already consumed")
			}
			n, fenceErr := q.FenceDCPCard12ModelFreeRebaseReview(ctx, gen.FenceDCPCard12ModelFreeRebaseReviewParams{
				ReviewRunID: r.ID, ReviewID: r.ReviewID, BatchID: r.BatchID,
				UpdatedAt: r.CreatedAt, SessionID: string(r.SessionID), PRURL: r.PRURL, TargetSha: r.TargetSHA,
			})
			if fenceErr != nil || n != 1 {
				return errors.Join(fenceErr, errors.New("card-12 model-free reviewer fence was unavailable"))
			}
			return nil
		}
		if continuationErr != nil && !errors.Is(continuationErr, sql.ErrNoRows) {
			return continuationErr
		}
		row, getErr := q.GetDCPCard12FreshWorkerRecovery(ctx, dcpCard12FreshWorkerRecoveryID)
		if errors.Is(getErr, sql.ErrNoRows) {
			return nil
		}
		if getErr != nil {
			return getErr
		}
		if row.SessionID != string(r.SessionID) || row.PRURL != r.PRURL || row.NewHead != r.TargetSHA {
			return nil
		}
		if row.Status != string(domain.DCPFreshWorkerSucceeded) || row.ReviewerModelCallCount != 0 {
			return errors.New("card-12 fresh recovery reviewer fence is already consumed")
		}
		n, fenceErr := q.FenceDCPCard12FreshRecoveryReview(ctx, gen.FenceDCPCard12FreshRecoveryReviewParams{
			ReviewRunID: r.ID, ReviewID: r.ReviewID, BatchID: r.BatchID,
			UpdatedAt: r.CreatedAt, SessionID: string(r.SessionID), PRURL: r.PRURL, TargetSha: r.TargetSHA,
		})
		if fenceErr != nil || n != 1 {
			return errors.Join(fenceErr, errors.New("card-12 fresh recovery reviewer fence was unavailable"))
		}
		return nil
	})
	if isSQLiteUnique(err) {
		return fmt.Errorf("insert review run for session %s pr %s sha %s: %w", r.SessionID, r.PRURL, r.TargetSHA, domain.ErrDuplicateReviewRun)
	}
	return err
}

// UpdateReviewRunResult sets the status/verdict/body and the GitHub review id of
// a running review pass.
func (s *Store) UpdateReviewRunResult(ctx context.Context, id string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, body, githubReviewID string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	updated := false
	err := s.inTx(ctx, "update review run result", func(q *gen.Queries) error {
		n, err := q.UpdateReviewRunResult(ctx, gen.UpdateReviewRunResultParams{
			Status: status, Verdict: verdict, Body: body, GithubReviewID: githubReviewID, ID: id,
		})
		if err != nil || n == 0 {
			return err
		}
		updated = true
		if status != domain.ReviewRunFailed {
			return nil
		}
		run, runErr := q.GetReviewRun(ctx, id)
		if runErr != nil {
			return runErr
		}
		finalization, finalizationErr := q.GetDCPCard12RebaseHeadFinalization(ctx, dcpCard12RebaseHeadFinalizationID)
		if finalizationErr == nil && finalization.Status == string(domain.DCPRebaseHeadFinalizationReviewRunning) && finalization.ReviewRunID == run.ID && finalization.CandidateHead == run.TargetSha {
			now := time.Now().UTC()
			n, failErr := q.FailDCPCard12RebaseHeadFinalizationReview(ctx, gen.FailDCPCard12RebaseHeadFinalizationReviewParams{
				ErrorCode: "reviewer_failed", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				FinalizationID: finalization.FinalizationID, ReviewRunID: run.ID, TargetSha: run.TargetSha,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 REBASE_HEAD failed reviewer fence could not be closed"))
			}
			return nil
		}
		if finalizationErr != nil && !errors.Is(finalizationErr, sql.ErrNoRows) {
			return finalizationErr
		}
		coldStart, coldStartErr := q.GetDCPCard12ColdStartRecovery(ctx, dcpCard12ColdStartRecoveryID)
		if coldStartErr == nil && coldStart.Status == string(domain.DCPColdStartRecoveryReviewRunning) && coldStart.RecoveryReviewRunID == run.ID && coldStart.NewHead == run.TargetSha {
			now := time.Now().UTC()
			n, failErr := q.FailDCPCard12ColdStartRecoveryReview(ctx, gen.FailDCPCard12ColdStartRecoveryReviewParams{
				ErrorCode: "reviewer_failed", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				RecoveryID: coldStart.RecoveryID, ReviewRunID: run.ID, TargetSha: run.TargetSha,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 cold-start failed reviewer fence could not be closed"))
			}
			return nil
		}
		if coldStartErr != nil && !errors.Is(coldStartErr, sql.ErrNoRows) {
			return coldStartErr
		}
		continuation, continuationErr := q.GetDCPCard12ModelFreeRebaseContinuation(ctx, dcpCard12ModelFreeRebaseContinuationID)
		if continuationErr == nil && continuation.Status == string(domain.DCPModelFreeRebaseReviewRunning) && continuation.RecoveryReviewRunID == run.ID && continuation.NewHead == run.TargetSha {
			n, failErr := q.FailDCPCard12ModelFreeRebaseReview(ctx, gen.FailDCPCard12ModelFreeRebaseReviewParams{
				ErrorCode: "reviewer_failed", UpdatedAt: time.Now().UTC(), FinishedAt: sql.NullTime{Time: time.Now().UTC(), Valid: true},
				ContinuationID: continuation.ContinuationID, ReviewRunID: run.ID, TargetSha: run.TargetSha,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 model-free failed reviewer fence could not be closed"))
			}
			return nil
		}
		if continuationErr != nil && !errors.Is(continuationErr, sql.ErrNoRows) {
			return continuationErr
		}
		recovery, recoveryErr := q.GetDCPCard12FreshWorkerRecovery(ctx, dcpCard12FreshWorkerRecoveryID)
		if errors.Is(recoveryErr, sql.ErrNoRows) {
			return nil
		}
		if recoveryErr != nil {
			return recoveryErr
		}
		if recovery.Status != string(domain.DCPFreshReviewerRunning) || recovery.RecoveryReviewRunID != run.ID || recovery.NewHead != run.TargetSha {
			return nil
		}
		n, failErr := q.FailDCPCard12FreshRecoveryReview(ctx, gen.FailDCPCard12FreshRecoveryReviewParams{
			ErrorCode: "reviewer_failed", UpdatedAt: time.Now().UTC(),
			FinishedAt: sql.NullTime{Time: time.Now().UTC(), Valid: true}, RecoveryID: recovery.RecoveryID,
			ReviewRunID: run.ID, TargetSha: run.TargetSha,
		})
		if failErr != nil || n != 1 {
			return errors.Join(failErr, errors.New("card-12 failed reviewer fence could not be closed"))
		}
		return nil
	})
	return updated, err
}

// UpdateBoundReviewRunResult atomically completes one still-running exact-head
// run only when its session, batch, PR, current PR head, and stable reviewer
// terminal all match the trusted supervisor contract.
func (s *Store) UpdateBoundReviewRunResult(ctx context.Context, expected reviewcore.StructuredResultExpected, verdict domain.ReviewVerdict, body string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	updated := false
	err := s.inTx(ctx, "update bound review run result", func(q *gen.Queries) error {
		n, err := q.UpdateBoundReviewRunResult(ctx, gen.UpdateBoundReviewRunResultParams{
			Verdict: verdict, Body: body, RunID: expected.RunID,
			SessionID: domain.SessionID(expected.WorkerSessionID), BatchID: expected.BatchID,
			PRURL: expected.PRURL, TargetSha: expected.TargetSHA, ReviewerHandleID: expected.ReviewerHandleID,
		})
		if err != nil || n == 0 {
			return err
		}
		updated = true
		if verdict != domain.VerdictChangesRequested {
			return nil
		}
		finalization, finalizationErr := q.GetDCPCard12RebaseHeadFinalization(ctx, dcpCard12RebaseHeadFinalizationID)
		if finalizationErr == nil && finalization.Status == string(domain.DCPRebaseHeadFinalizationReviewRunning) && finalization.ReviewRunID == expected.RunID && finalization.CandidateHead == expected.TargetSHA {
			now := time.Now().UTC()
			n, failErr := q.FailDCPCard12RebaseHeadFinalizationReview(ctx, gen.FailDCPCard12RebaseHeadFinalizationReviewParams{
				ErrorCode: "review_changes_requested", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				FinalizationID: finalization.FinalizationID, ReviewRunID: expected.RunID, TargetSha: expected.TargetSHA,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 REBASE_HEAD changes-requested reviewer fence could not be closed"))
			}
			return nil
		}
		if finalizationErr != nil && !errors.Is(finalizationErr, sql.ErrNoRows) {
			return finalizationErr
		}
		coldStart, coldStartErr := q.GetDCPCard12ColdStartRecovery(ctx, dcpCard12ColdStartRecoveryID)
		if coldStartErr == nil && coldStart.Status == string(domain.DCPColdStartRecoveryReviewRunning) && coldStart.RecoveryReviewRunID == expected.RunID && coldStart.NewHead == expected.TargetSHA {
			now := time.Now().UTC()
			n, failErr := q.FailDCPCard12ColdStartRecoveryReview(ctx, gen.FailDCPCard12ColdStartRecoveryReviewParams{
				ErrorCode: "review_changes_requested", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				RecoveryID: coldStart.RecoveryID, ReviewRunID: expected.RunID, TargetSha: expected.TargetSHA,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 cold-start changes-requested reviewer fence could not be closed"))
			}
			return nil
		}
		if coldStartErr != nil && !errors.Is(coldStartErr, sql.ErrNoRows) {
			return coldStartErr
		}
		continuation, continuationErr := q.GetDCPCard12ModelFreeRebaseContinuation(ctx, dcpCard12ModelFreeRebaseContinuationID)
		if continuationErr == nil && continuation.Status == string(domain.DCPModelFreeRebaseReviewRunning) && continuation.RecoveryReviewRunID == expected.RunID && continuation.NewHead == expected.TargetSHA {
			now := time.Now().UTC()
			n, failErr := q.FailDCPCard12ModelFreeRebaseReview(ctx, gen.FailDCPCard12ModelFreeRebaseReviewParams{
				ErrorCode: "review_changes_requested", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				ContinuationID: continuation.ContinuationID, ReviewRunID: expected.RunID, TargetSha: expected.TargetSHA,
			})
			if failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("card-12 model-free changes-requested reviewer fence could not be closed"))
			}
			return nil
		}
		if continuationErr != nil && !errors.Is(continuationErr, sql.ErrNoRows) {
			return continuationErr
		}
		recovery, recoveryErr := q.GetDCPCard12FreshWorkerRecovery(ctx, dcpCard12FreshWorkerRecoveryID)
		if errors.Is(recoveryErr, sql.ErrNoRows) {
			return nil
		}
		if recoveryErr != nil {
			return recoveryErr
		}
		if recovery.Status != string(domain.DCPFreshReviewerRunning) || recovery.RecoveryReviewRunID != expected.RunID || recovery.NewHead != expected.TargetSHA {
			return nil
		}
		now := time.Now().UTC()
		n, failErr := q.FailDCPCard12FreshRecoveryReview(ctx, gen.FailDCPCard12FreshRecoveryReviewParams{
			ErrorCode: "review_changes_requested", UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
			RecoveryID: recovery.RecoveryID, ReviewRunID: expected.RunID, TargetSha: expected.TargetSHA,
		})
		if failErr != nil || n != 1 {
			return errors.Join(failErr, errors.New("card-12 changes-requested reviewer fence could not be closed"))
		}
		return nil
	})
	return updated, err
}

// SupersedeStaleRunningReviewRuns marks older running unverdicted passes for a
// worker failed before starting a review for a newer commit.
func (s *Store) SupersedeStaleRunningReviewRuns(ctx context.Context, sessionID domain.SessionID, prURL, targetSHA, body string) (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.SupersedeStaleRunningReviewRuns(ctx, gen.SupersedeStaleRunningReviewRunsParams{
		Body:      body,
		SessionID: sessionID,
		PRURL:     prURL,
		TargetSha: targetSHA,
	})
}

// CancelRunningReviewRunsBySession marks all currently running review passes
// for a worker cancelled.
func (s *Store) CancelRunningReviewRunsBySession(ctx context.Context, sessionID domain.SessionID, body string) (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.CancelRunningReviewRunsBySession(ctx, gen.CancelRunningReviewRunsBySessionParams{
		Body:      body,
		SessionID: sessionID,
	})
}

// MarkReviewRunDelivered records that lifecycle delivered the worker nudge for
// a completed AO-internal review pass.
func (s *Store) MarkReviewRunDelivered(ctx context.Context, id string, deliveredAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.MarkReviewRunDelivered(ctx, gen.MarkReviewRunDeliveredParams{
		DeliveredAt: sql.NullTime{Time: deliveredAt, Valid: true},
		ID:          id,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ClaimDCPReviewLabTerminalMerge atomically consumes the one authorized merge
// opportunity on an approved structured exact-head run.
func (s *Store) ClaimDCPReviewLabTerminalMerge(ctx context.Context, run domain.ReviewRun) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ClaimDCPReviewLabTerminalMerge(ctx, gen.ClaimDCPReviewLabTerminalMergeParams{
		RunID:     run.ID,
		SessionID: run.SessionID,
		PRURL:     run.PRURL,
		TargetSha: run.TargetSHA,
	})
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// CompleteDCPReviewLabTerminalMerge records the provider's returned merge SHA.
func (s *Store) CompleteDCPReviewLabTerminalMerge(ctx context.Context, runID, mergeCommitSHA string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.CompleteDCPReviewLabTerminalMerge(ctx, gen.CompleteDCPReviewLabTerminalMergeParams{
		MergeCommitSha: mergeCommitSHA,
		RunID:          runID,
	})
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// FailDCPReviewLabTerminalMerge closes a claimed attempt without retry.
func (s *Store) FailDCPReviewLabTerminalMerge(ctx context.Context, runID, errorCode string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReviewLabTerminalMerge(ctx, gen.FailDCPReviewLabTerminalMergeParams{
		ErrorCode: errorCode,
		RunID:     runID,
	})
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetReviewRun returns one review pass by id.
func (s *Store) GetReviewRun(ctx context.Context, id string) (domain.ReviewRun, bool, error) {
	row, err := s.qr.GetReviewRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReviewRun{}, false, nil
	}
	if err != nil {
		return domain.ReviewRun{}, false, fmt.Errorf("get review run %s: %w", id, err)
	}
	return reviewRunFromRow(row), true, nil
}

// GetReviewRunBySessionPRAndSHA returns the most recent review pass for a
// worker session, PR, and commit, ok=false if none. It lets a repeat trigger for
// the same PR head short-circuit to the existing run without colliding with
// another PR that happens to share the same head commit.
func (s *Store) GetReviewRunBySessionPRAndSHA(ctx context.Context, id domain.SessionID, prURL, targetSHA string) (domain.ReviewRun, bool, error) {
	row, err := s.qr.GetReviewRunBySessionPRAndSHA(ctx, gen.GetReviewRunBySessionPRAndSHAParams{SessionID: id, PRURL: prURL, TargetSha: targetSHA})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReviewRun{}, false, nil
	}
	if err != nil {
		return domain.ReviewRun{}, false, fmt.Errorf("get review run for session %s pr %s sha %s: %w", id, prURL, targetSHA, err)
	}
	return reviewRunFromRow(row), true, nil
}

// GetReviewRunBySessionPRSHAAndHarness returns the most recent review pass for
// a worker session, PR, commit, and reviewer harness, ok=false if none.
func (s *Store) GetReviewRunBySessionPRSHAAndHarness(ctx context.Context, id domain.SessionID, prURL, targetSHA string, harness domain.ReviewerHarness) (domain.ReviewRun, bool, error) {
	row, err := s.qr.GetReviewRunBySessionPRSHAAndHarness(ctx, gen.GetReviewRunBySessionPRSHAAndHarnessParams{SessionID: id, PRURL: prURL, TargetSha: targetSHA, Harness: harness})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReviewRun{}, false, nil
	}
	if err != nil {
		return domain.ReviewRun{}, false, fmt.Errorf("get review run for session %s pr %s sha %s harness %s: %w", id, prURL, targetSHA, harness, err)
	}
	return reviewRunFromRow(row), true, nil
}

// ListReviewRunsBySession returns all review passes for a worker session, newest first.
func (s *Store) ListReviewRunsBySession(ctx context.Context, id domain.SessionID) ([]domain.ReviewRun, error) {
	rows, err := s.qr.ListReviewRunsBySession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list review runs for session %s: %w", id, err)
	}
	out := make([]domain.ReviewRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, reviewRunFromRow(row))
	}
	return out, nil
}

// ListRunningReviewRunsBySession returns only currently running unverdicted
// review passes for a worker session, newest first.
func (s *Store) ListRunningReviewRunsBySession(ctx context.Context, id domain.SessionID) ([]domain.ReviewRun, error) {
	rows, err := s.qr.ListRunningReviewRunsBySession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list running review runs for session %s: %w", id, err)
	}
	out := make([]domain.ReviewRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, reviewRunFromRow(row))
	}
	return out, nil
}

// ListReviewRunsByBatch returns all passes in one trigger-created batch, oldest first.
func (s *Store) ListReviewRunsByBatch(ctx context.Context, id domain.SessionID, batchID string) ([]domain.ReviewRun, error) {
	rows, err := s.qr.ListReviewRunsByBatch(ctx, gen.ListReviewRunsByBatchParams{SessionID: id, BatchID: batchID})
	if err != nil {
		return nil, fmt.Errorf("list review runs for session %s batch %s: %w", id, batchID, err)
	}
	out := make([]domain.ReviewRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, reviewRunFromRow(row))
	}
	return out, nil
}

func reviewFromRow(r gen.Review) domain.Review {
	return domain.Review{
		ID:               r.ID,
		SessionID:        r.SessionID,
		ProjectID:        r.ProjectID,
		Harness:          r.Harness,
		PRURL:            r.PRURL,
		ReviewerHandleID: r.ReviewerHandleID,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func reviewRunFromRow(r gen.ReviewRun) domain.ReviewRun {
	var deliveredAt *time.Time
	if r.DeliveredAt.Valid {
		t := r.DeliveredAt.Time
		deliveredAt = &t
	}
	return domain.ReviewRun{
		ID:                     r.ID,
		ReviewID:               r.ReviewID,
		SessionID:              r.SessionID,
		BatchID:                r.BatchID,
		Harness:                r.Harness,
		PRURL:                  r.PRURL,
		TargetSHA:              r.TargetSha,
		Status:                 r.Status,
		Verdict:                r.Verdict,
		Body:                   r.Body,
		GithubReviewID:         r.GithubReviewID,
		CreatedAt:              r.CreatedAt,
		DeliveredAt:            deliveredAt,
		ResultChannel:          r.ResultChannel,
		TerminalMergeStatus:    r.TerminalMergeStatus,
		TerminalMergeCommitSHA: r.TerminalMergeCommitSha,
		TerminalMergeError:     r.TerminalMergeError,
	}
}
