package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

const dcpCard12RebaseHeadFinalizationID = "dcp-card12-rebase-head-finalization-a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b"

func (s *Store) GetDCPCard12RebaseHeadFinalization(ctx context.Context, id string) (domain.DCPCard12RebaseHeadFinalization, bool, error) {
	row, err := s.qr.GetDCPCard12RebaseHeadFinalization(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPCard12RebaseHeadFinalization{}, false, nil
	}
	if err != nil {
		return domain.DCPCard12RebaseHeadFinalization{}, false, err
	}
	return dcpCard12RebaseHeadFinalizationFromRow(row), true, nil
}

func (s *Store) ListDCPCard12RebaseHeadFinalizations(ctx context.Context) ([]domain.DCPCard12RebaseHeadFinalization, error) {
	rows, err := s.qr.ListDCPCard12RebaseHeadFinalizations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPCard12RebaseHeadFinalization, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpCard12RebaseHeadFinalizationFromRow(row))
	}
	return out, nil
}

func (s *Store) HasExactDCPFinalizationQuarantine(ctx context.Context) (bool, error) {
	count, err := s.qr.CountExactDCPFinalizationQuarantine(ctx)
	return count == 2, err
}

func (s *Store) StartDCPCard12RebaseHeadFinalization(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPCard12RebaseHeadFinalization(ctx, gen.StartDCPCard12RebaseHeadFinalizationParams{
		UpdatedAt: now, FinalizationID: row.FinalizationID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) CompleteDCPCard12RebaseHeadFinalizationAction(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.CompleteDCPCard12RebaseHeadFinalizationAction(ctx, gen.CompleteDCPCard12RebaseHeadFinalizationActionParams{
		UpdatedAt: now, FinalizationID: row.FinalizationID, Revision: row.Revision, CandidateHead: row.CandidateHead,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12RebaseHeadFinalization(ctx context.Context, id, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12RebaseHeadFinalization(ctx, gen.FailDCPCard12RebaseHeadFinalizationParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, FinalizationID: id,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12RebaseHeadFinalizationReview(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, run domain.ReviewRun, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12RebaseHeadFinalizationReview(ctx, gen.FailDCPCard12RebaseHeadFinalizationReviewParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
		FinalizationID: row.FinalizationID, ReviewRunID: run.ID, TargetSha: run.TargetSHA,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterCard12RebaseHeadFinalization(ctx context.Context, admission domain.DCPReviewLabAdmission, row domain.DCPCard12RebaseHeadFinalization, run domain.ReviewRun, reviewBaseSHA, checkID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after card-12 REBASE_HEAD finalization", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterCard12RebaseHeadFinalization(ctx, gen.RebindDCPAdmissionAfterCard12RebaseHeadFinalizationParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: admission.ID,
			OldReviewRunID: admission.ReviewRunID, FinalizationID: row.FinalizationID,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 REBASE_HEAD admission rebind was unavailable"))
		}
		n, err = q.MarkDCPCard12RebaseHeadFinalizationReviewed(ctx, gen.MarkDCPCard12RebaseHeadFinalizationReviewedParams{
			CheckID: checkID, UpdatedAt: now, FinalizationID: row.FinalizationID,
			ReviewRunID: run.ID, TargetSha: run.TargetSHA,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 REBASE_HEAD review transition was unavailable"))
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpCard12RebaseHeadFinalizationFromRow(row gen.DcpReviewLabCard12RebaseHeadFinalization) domain.DCPCard12RebaseHeadFinalization {
	result := domain.DCPCard12RebaseHeadFinalization{
		FinalizationID: row.FinalizationID, Generation: row.Generation, IdentityDigest: row.IdentityDigest, ContractCommit: row.ContractCommit,
		PredecessorRecoveryID: row.PredecessorRecoveryID, IncidentID: row.IncidentID, AdmissionID: row.AdmissionID,
		SessionID: domain.SessionID(row.SessionID), TaskID: row.TaskID, ProjectID: row.ProjectID, Repository: row.Repository,
		WorktreePath: row.WorktreePath, SourceBranch: row.SourceBranch, PRURL: row.PRURL, PRNumber: row.PRNumber,
		OldHead: row.OldHead, CandidateHead: row.CandidateHead, CurrentMain: row.CurrentMain, ProviderBase: row.ProviderBase,
		ConflictPath: row.ConflictPath, ResolvedBytesDigest: row.ResolvedBytesDigest, ResolvedBlob: row.ResolvedBlob,
		CandidateDiffDigest: row.CandidateDiffDigest, CleanStatusDigest: row.CleanStatusDigest,
		RebaseHeadDigest: row.RebaseHeadDigest, OrigHeadDigest: row.OrigHeadDigest,
		BackupPath: row.BackupPath, BackupDigest: row.BackupDigest, PushRef: row.PushRef, PushLeaseOldHead: row.PushLeaseOldHead,
		ProviderNewHead: row.ProviderNewHead, UnauthorizedWorkerTokens11: row.UnauthorizedWorkerTokens11,
		UnauthorizedWorkerTokens12: row.UnauthorizedWorkerTokens12, WorkerModelCallCount: row.WorkerModelCallCount,
		ArbiterModelCallCount: row.ArbiterModelCallCount, ModelFreeActionCount: row.ModelFreeActionCount,
		ReviewerModelCallCount: row.ReviewerModelCallCount, ReviewRunID: row.ReviewRunID, ReviewID: row.ReviewID,
		ReviewBatchID: row.ReviewBatchID, CheckID: row.CheckID, MergeCommitSHA: row.MergeCommitSha,
		Status: domain.DCPCard12RebaseHeadFinalizationStatus(row.Status), Revision: row.Revision, ErrorCode: row.ErrorCode,
		AuthorizedAt: row.AuthorizedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.FinishedAt.Valid {
		finished := row.FinishedAt.Time
		result.FinishedAt = &finished
	}
	return result
}
