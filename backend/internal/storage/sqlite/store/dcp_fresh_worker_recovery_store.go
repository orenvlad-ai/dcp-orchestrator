package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

const dcpCard12FreshWorkerRecoveryID = "dcp-card12-fresh-worker-recovery-d2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b"

func (s *Store) GetDCPCard12FreshWorkerRecovery(ctx context.Context, id string) (domain.DCPCard12FreshWorkerRecovery, bool, error) {
	row, err := s.qr.GetDCPCard12FreshWorkerRecovery(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPCard12FreshWorkerRecovery{}, false, nil
	}
	if err != nil {
		return domain.DCPCard12FreshWorkerRecovery{}, false, err
	}
	return dcpCard12FreshWorkerRecoveryFromRow(row), true, nil
}

func (s *Store) ListDCPCard12FreshWorkerRecoveries(ctx context.Context) ([]domain.DCPCard12FreshWorkerRecovery, error) {
	rows, err := s.qr.ListDCPCard12FreshWorkerRecoveries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPCard12FreshWorkerRecovery, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpCard12FreshWorkerRecoveryFromRow(row))
	}
	return out, nil
}

func (s *Store) PrepareDCPCard12FreshWorkerRecovery(ctx context.Context, r domain.DCPCard12FreshWorkerRecovery, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.PrepareDCPCard12FreshWorkerRecovery(ctx, gen.PrepareDCPCard12FreshWorkerRecoveryParams{
		InputJson: r.InputJSON, InputDigest: r.InputDigest, InputPath: r.InputPath,
		ResultPath: r.ResultPath, LogPath: r.LogPath, UpdatedAt: now,
		RecoveryID: r.RecoveryID, Revision: r.Revision,
	})
	return n == 1, err
}

func (s *Store) StartDCPCard12FreshWorkerRecovery(ctx context.Context, r domain.DCPCard12FreshWorkerRecovery, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPCard12FreshWorkerRecovery(ctx, gen.StartDCPCard12FreshWorkerRecoveryParams{
		UpdatedAt: now, RecoveryID: r.RecoveryID, Revision: r.Revision,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12FreshWorkerPreflight(ctx context.Context, id, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12FreshWorkerPreflight(ctx, gen.FailDCPCard12FreshWorkerPreflightParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, RecoveryID: id,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12FreshWorkerCall(ctx context.Context, id, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12FreshWorkerCall(ctx, gen.FailDCPCard12FreshWorkerCallParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, RecoveryID: id,
	})
	return n == 1, err
}

func (s *Store) CompleteDCPCard12FreshWorkerCall(ctx context.Context, r domain.DCPCard12FreshWorkerRecovery, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.CompleteDCPCard12FreshWorkerCall(ctx, gen.CompleteDCPCard12FreshWorkerCallParams{
		WorkerCodexSessionID: r.WorkerCodexSessionID, WorkerTokenCount: r.WorkerTokenCount,
		WorkerResultDigest: r.WorkerResultDigest, WorkerLogDigest: r.WorkerLogDigest,
		NewHead: r.NewHead, NewCommit: r.NewCommit, UpdatedAt: now,
		RecoveryID: r.RecoveryID, Revision: r.Revision,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12FreshRecoveryReview(ctx context.Context, r domain.DCPCard12FreshWorkerRecovery, run domain.ReviewRun, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12FreshRecoveryReview(ctx, gen.FailDCPCard12FreshRecoveryReviewParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
		RecoveryID: r.RecoveryID, ReviewRunID: run.ID, TargetSha: run.TargetSHA,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterCard12FreshWorkerRecovery(ctx context.Context, admission domain.DCPReviewLabAdmission, recovery domain.DCPCard12FreshWorkerRecovery, run domain.ReviewRun, reviewBaseSHA, checkID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after card-12 fresh worker recovery", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterCard12FreshWorkerRecovery(ctx, gen.RebindDCPAdmissionAfterCard12FreshWorkerRecoveryParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: admission.ID,
			OldReviewRunID: admission.ReviewRunID, RecoveryID: recovery.RecoveryID,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 admission rebind was unavailable"))
		}
		n, err = q.MarkDCPCard12FreshWorkerRecoveryReviewed(ctx, gen.MarkDCPCard12FreshWorkerRecoveryReviewedParams{
			CheckID: checkID, UpdatedAt: now, RecoveryID: recovery.RecoveryID,
			ReviewRunID: run.ID, TargetSha: run.TargetSHA,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 recovery review transition was unavailable"))
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpCard12FreshWorkerRecoveryFromRow(row gen.DcpReviewLabCard12FreshWorkerRecovery) domain.DCPCard12FreshWorkerRecovery {
	r := domain.DCPCard12FreshWorkerRecovery{
		RecoveryID: row.RecoveryID, RecoveryGeneration: row.RecoveryGeneration, RecoveryIdentityDigest: row.RecoveryIdentityDigest,
		IncidentID: row.IncidentID, IncidentGeneration: row.IncidentGeneration,
		SuccessorAttemptID: row.SuccessorAttemptID, SuccessorAttemptGeneration: row.SuccessorAttemptGeneration,
		SuccessorIdentityDigest: row.SuccessorIdentityDigest, AcceptedDecisionDigest: row.AcceptedDecisionDigest,
		AdmissionID: row.AdmissionID, SessionID: domain.SessionID(row.SessionID), TaskID: row.TaskID, ProjectID: row.ProjectID,
		Repository: row.Repository, WorktreePath: row.WorktreePath, SourceBranch: row.SourceBranch,
		PRURL: row.PRURL, PRNumber: row.PRNumber, OldHead: row.OldHead, CurrentMain: row.CurrentMain,
		PredecessorStatus: row.PredecessorStatus, PredecessorError: row.PredecessorError,
		OldRuntimeHandleID: row.OldRuntimeHandleID, OldAgentSessionID: row.OldAgentSessionID,
		OldRuntimeLaunchID: row.OldRuntimeLaunchID, ContractCommit: row.ContractCommit,
		Model: row.Model, Reasoning: row.Reasoning, TokenBudget: row.TokenBudget,
		WorkerModelCallCount: row.WorkerModelCallCount, ReviewerModelCallCount: row.ReviewerModelCallCount,
		RuntimeActionID: row.RuntimeActionID, RuntimeHandleID: row.RuntimeHandleID, LaunchID: row.LaunchID,
		InputJSON: row.InputJson, InputDigest: row.InputDigest, InputPath: row.InputPath,
		ResultPath: row.ResultPath, LogPath: row.LogPath, WorkerCodexSessionID: row.WorkerCodexSessionID,
		WorkerTokenCount: row.WorkerTokenCount, WorkerResultDigest: row.WorkerResultDigest,
		WorkerLogDigest: row.WorkerLogDigest, NewHead: row.NewHead, NewCommit: row.NewCommit,
		RecoveryReviewRunID: row.RecoveryReviewRunID, RecoveryReviewID: row.RecoveryReviewID,
		RecoveryReviewBatchID: row.RecoveryReviewBatchID, RecoveryCheckID: row.RecoveryCheckID,
		MergeCommitSHA: row.MergeCommitSha, Status: domain.DCPCard12FreshWorkerRecoveryStatus(row.Status),
		Revision: row.Revision, ErrorCode: row.ErrorCode, AuthorizedAt: row.AuthorizedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.FinishedAt.Valid {
		value := row.FinishedAt.Time
		r.FinishedAt = &value
	}
	return r
}
