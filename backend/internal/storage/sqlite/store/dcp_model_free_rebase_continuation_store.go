package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

const dcpCard12ModelFreeRebaseContinuationID = "dcp-card12-model-free-rebase-continuation-66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060"

func (s *Store) ValidateDCPCard12ModelFreeRebaseDurableCounts(ctx context.Context) (bool, error) {
	counts, err := s.qr.GetDCPCard12ModelFreeRebaseDurableCounts(ctx)
	if err != nil {
		return false, err
	}
	return counts.SessionCount == 17 && counts.ReviewRunCount == 11 && counts.AdmissionCount == 4 &&
		counts.IncidentCount == 1 && counts.SuccessorCount == 1 && counts.FreshWorkerCount == 1 &&
		counts.PreflightAuditCount == 1 && counts.ContinuationCount == 1, nil
}

func (s *Store) ValidateDCPCard12ModelFreeProviderBaseCorrection(ctx context.Context) (bool, error) {
	count, err := s.qr.GetDCPCard12ModelFreeProviderBaseCorrectionCount(ctx)
	return count == 1, err
}

func (s *Store) GetDCPCard12ModelFreeRebaseContinuation(ctx context.Context, id string) (domain.DCPCard12ModelFreeRebaseContinuation, bool, error) {
	row, err := s.qr.GetDCPCard12ModelFreeRebaseContinuation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPCard12ModelFreeRebaseContinuation{}, false, nil
	}
	if err != nil {
		return domain.DCPCard12ModelFreeRebaseContinuation{}, false, err
	}
	return dcpCard12ModelFreeRebaseContinuationFromRow(row), true, nil
}

func (s *Store) ListDCPCard12ModelFreeRebaseContinuations(ctx context.Context) ([]domain.DCPCard12ModelFreeRebaseContinuation, error) {
	rows, err := s.qr.ListDCPCard12ModelFreeRebaseContinuations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPCard12ModelFreeRebaseContinuation, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpCard12ModelFreeRebaseContinuationFromRow(row))
	}
	return out, nil
}

func (s *Store) StartDCPCard12ModelFreeRebaseContinuation(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPCard12ModelFreeRebaseContinuation(ctx, gen.StartDCPCard12ModelFreeRebaseContinuationParams{
		UpdatedAt: now, ContinuationID: row.ContinuationID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) CompleteDCPCard12ModelFreeRebaseContinuation(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, newHead string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.CompleteDCPCard12ModelFreeRebaseContinuation(ctx, gen.CompleteDCPCard12ModelFreeRebaseContinuationParams{
		NewHead: newHead, UpdatedAt: now, ContinuationID: row.ContinuationID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12ModelFreeRebaseContinuation(ctx context.Context, id, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12ModelFreeRebaseContinuation(ctx, gen.FailDCPCard12ModelFreeRebaseContinuationParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, ContinuationID: id,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12ModelFreeRebaseReview(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, run domain.ReviewRun, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12ModelFreeRebaseReview(ctx, gen.FailDCPCard12ModelFreeRebaseReviewParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
		ContinuationID: row.ContinuationID, ReviewRunID: run.ID, TargetSha: run.TargetSHA,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterCard12ModelFreeRebase(ctx context.Context, admission domain.DCPReviewLabAdmission, continuation domain.DCPCard12ModelFreeRebaseContinuation, run domain.ReviewRun, reviewBaseSHA, checkID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after card-12 model-free rebase", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterCard12ModelFreeRebase(ctx, gen.RebindDCPAdmissionAfterCard12ModelFreeRebaseParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: admission.ID,
			OldReviewRunID: admission.ReviewRunID, ContinuationID: continuation.ContinuationID,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 model-free admission rebind was unavailable"))
		}
		n, err = q.MarkDCPCard12ModelFreeRebaseReviewed(ctx, gen.MarkDCPCard12ModelFreeRebaseReviewedParams{
			CheckID: checkID, UpdatedAt: now, ContinuationID: continuation.ContinuationID,
			ReviewRunID: run.ID, TargetSha: run.TargetSHA,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 model-free review transition was unavailable"))
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpCard12ModelFreeRebaseContinuationFromRow(row gen.DcpReviewLabCard12ModelFreeRebaseContinuation) domain.DCPCard12ModelFreeRebaseContinuation {
	result := domain.DCPCard12ModelFreeRebaseContinuation{
		ContinuationID: row.ContinuationID, Generation: row.Generation, IdentityDigest: row.IdentityDigest,
		ContractCommit: row.ContractCommit, PredecessorRecoveryID: row.PredecessorRecoveryID,
		IncidentID: row.IncidentID, AdmissionID: row.AdmissionID, SessionID: domain.SessionID(row.SessionID),
		TaskID: row.TaskID, ProjectID: row.ProjectID, Repository: row.Repository, WorktreePath: row.WorktreePath,
		SourceBranch: row.SourceBranch, PRURL: row.PRURL, PRNumber: row.PRNumber, OldHead: row.OldHead,
		CurrentMain: row.CurrentMain, PredecessorInputDigest: row.PredecessorInputDigest,
		InputArtifactDigest: row.InputArtifactDigest, ResultArtifactDigest: row.ResultArtifactDigest,
		LogArtifactDigest: row.LogArtifactDigest, RebaseMetadataDigest: row.RebaseMetadataDigest,
		ResolvedBytesDigest: row.ResolvedBytesDigest, ModelFreeActionCount: row.ModelFreeActionCount,
		ReviewerModelCallCount: row.ReviewerModelCallCount, LocalRefBefore: row.LocalRefBefore,
		LocalRefAfter: row.LocalRefAfter, PushRef: row.PushRef, PushLeaseOldHead: row.PushLeaseOldHead,
		NewHead: row.NewHead, NewCommit: row.NewCommit, ProviderNewHead: row.ProviderNewHead,
		RecoveryReviewRunID: row.RecoveryReviewRunID, RecoveryReviewID: row.RecoveryReviewID,
		RecoveryReviewBatchID: row.RecoveryReviewBatchID, RecoveryCheckID: row.RecoveryCheckID,
		MergeCommitSHA: row.MergeCommitSha, Status: domain.DCPCard12ModelFreeRebaseContinuationStatus(row.Status),
		Revision: row.Revision, ErrorCode: row.ErrorCode, AuthorizedAt: row.AuthorizedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.FinishedAt.Valid {
		value := row.FinishedAt.Time
		result.FinishedAt = &value
	}
	return result
}
