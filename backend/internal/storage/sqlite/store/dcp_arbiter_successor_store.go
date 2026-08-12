package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func (s *Store) GetDCPReleaseArbiterSuccessorAttemptByID(ctx context.Context, id string) (domain.DCPReleaseArbiterSuccessorAttempt, bool, error) {
	row, err := s.qr.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, id)
	return dcpArbiterSuccessorResult(row, err)
}

func (s *Store) GetDCPReleaseArbiterSuccessorAttemptByIncident(ctx context.Context, id string) (domain.DCPReleaseArbiterSuccessorAttempt, bool, error) {
	row, err := s.qr.GetDCPReleaseArbiterSuccessorAttemptByIncident(ctx, id)
	return dcpArbiterSuccessorResult(row, err)
}

func (s *Store) ListDCPReleaseArbiterSuccessorAttempts(ctx context.Context) ([]domain.DCPReleaseArbiterSuccessorAttempt, error) {
	rows, err := s.qr.ListDCPReleaseArbiterSuccessorAttempts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPReleaseArbiterSuccessorAttempt, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpArbiterSuccessorFromRow(row))
	}
	return out, nil
}

func (s *Store) PrepareDCPReleaseArbiterSuccessorAttempt(ctx context.Context, attempt domain.DCPReleaseArbiterSuccessorAttempt, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.PrepareDCPReleaseArbiterSuccessorAttempt(ctx, gen.PrepareDCPReleaseArbiterSuccessorAttemptParams{
		InputJson: attempt.InputJSON, InputDigest: attempt.InputDigest, UpdatedAt: now,
		AttemptID: attempt.AttemptID, IncidentID: attempt.IncidentID,
		AttemptIdentityDigest: attempt.AttemptIdentityDigest, IncidentIdentityDigest: attempt.IncidentIdentityDigest,
		IncidentInputDigest: attempt.IncidentInputDigest,
	})
	return n == 1, err
}

func (s *Store) StartDCPReleaseArbiterSuccessorCall(ctx context.Context, attempt domain.DCPReleaseArbiterSuccessorAttempt, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPReleaseArbiterSuccessorCall(ctx, gen.StartDCPReleaseArbiterSuccessorCallParams{
		UpdatedAt: now, AttemptID: attempt.AttemptID, AttemptIdentityDigest: attempt.AttemptIdentityDigest, InputDigest: attempt.InputDigest,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterSuccessorPreflight(ctx context.Context, attemptID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterSuccessorPreflight(ctx, gen.FailDCPReleaseArbiterSuccessorPreflightParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, AttemptID: attemptID,
	})
	return n == 1, err
}

func (s *Store) RecordDCPReleaseArbiterSuccessorDecision(ctx context.Context, attempt domain.DCPReleaseArbiterSuccessorAttempt, decisionJSON, decisionDigest string, safeStop bool, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	status := string(domain.DCPArbiterSuccessorDecided)
	finished := sql.NullTime{}
	if safeStop {
		status = string(domain.DCPArbiterSuccessorSafeStopped)
		finished = sql.NullTime{Time: now, Valid: true}
	}
	n, err := s.qw.RecordDCPReleaseArbiterSuccessorDecision(ctx, gen.RecordDCPReleaseArbiterSuccessorDecisionParams{
		Status: status, DecisionJson: decisionJSON, DecisionDigest: decisionDigest, ErrorCode: errorCode,
		DecisionAt: sql.NullTime{Time: now, Valid: true}, FinishedAt: finished,
		AttemptID: attempt.AttemptID, IncidentID: attempt.IncidentID,
		AttemptIdentityDigest: attempt.AttemptIdentityDigest, InputDigest: attempt.InputDigest,
	})
	return n == 1, err
}

func (s *Store) ConsumeDCPReleaseArbiterSuccessorRepair(ctx context.Context, attempt domain.DCPReleaseArbiterSuccessorAttempt, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ConsumeDCPReleaseArbiterSuccessorRepair(ctx, gen.ConsumeDCPReleaseArbiterSuccessorRepairParams{
		UpdatedAt: now, AttemptID: attempt.AttemptID, DecisionDigest: attempt.DecisionDigest,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterSuccessorCall(ctx context.Context, attemptID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterSuccessorCall(ctx, gen.FailDCPReleaseArbiterSuccessorCallParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, AttemptID: attemptID,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterSuccessorAfterDecision(ctx context.Context, attemptID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, gen.FailDCPReleaseArbiterSuccessorAfterDecisionParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, AttemptID: attemptID,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterArbiterSuccessorRepair(ctx context.Context, admission domain.DCPReviewLabAdmission, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt, run domain.ReviewRun, reviewBaseSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after arbiter successor repair", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterArbiterSuccessorRepair(ctx, gen.RebindDCPAdmissionAfterArbiterSuccessorRepairParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: admission.ID,
			OldReviewRunID: admission.ReviewRunID, SessionID: string(admission.SessionID), PRURL: admission.PRURL,
			OldTargetSha: admission.TargetSHA, IncidentLeaseID: admission.LeaseID,
			AttemptID: attempt.AttemptID, IncidentID: incident.IncidentID,
		})
		if err != nil || n == 0 {
			return err
		}
		n, err = q.MarkDCPReleaseArbiterSuccessorRecoveryReviewed(ctx, gen.MarkDCPReleaseArbiterSuccessorRecoveryReviewedParams{
			RecoveryReviewRunID: run.ID, RecoveryTargetSha: run.TargetSHA, UpdatedAt: now, AttemptID: attempt.AttemptID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact DCP arbiter successor recovery review transition was unavailable")
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpArbiterSuccessorResult(row gen.DcpReviewLabArbiterV1SuccessorAttempt, err error) (domain.DCPReleaseArbiterSuccessorAttempt, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, false, nil
	}
	if err != nil {
		return domain.DCPReleaseArbiterSuccessorAttempt{}, false, err
	}
	return dcpArbiterSuccessorFromRow(row), true, nil
}

func dcpArbiterSuccessorFromRow(row gen.DcpReviewLabArbiterV1SuccessorAttempt) domain.DCPReleaseArbiterSuccessorAttempt {
	result := domain.DCPReleaseArbiterSuccessorAttempt{
		AttemptID: row.AttemptID, IncidentID: row.IncidentID, IncidentGeneration: row.IncidentGeneration,
		AttemptGeneration: row.AttemptGeneration, AttemptIdentityDigest: row.AttemptIdentityDigest,
		IncidentIdentityDigest: row.IncidentIdentityDigest, IncidentInputDigest: row.IncidentInputDigest,
		OriginalInputArtifactDigest: row.OriginalInputArtifactDigest, OriginalSchemaArtifactDigest: row.OriginalSchemaArtifactDigest,
		OriginalResultArtifactDigest: row.OriginalResultArtifactDigest, OriginalCodexSessionID: row.OriginalCodexSessionID,
		OriginalTokenCount: row.OriginalTokenCount, ContractCommit: row.ContractCommit,
		InputJSON: row.InputJson, InputDigest: row.InputDigest, Model: row.Model, Reasoning: row.Reasoning,
		TokenBudget: row.TokenBudget, PolicyMaxWorkerCalls: row.PolicyMaxWorkerCalls,
		PolicyMaxFreshReviews: row.PolicyMaxFreshReviews, RuntimeHandleID: row.RuntimeHandleID, LaunchID: row.LaunchID,
		Status: domain.DCPReleaseArbiterSuccessorStatus(row.Status), ModelCallCount: row.ModelCallCount,
		DecisionJSON: row.DecisionJson, DecisionDigest: row.DecisionDigest,
		RecoveryOwnerSessionID: domain.SessionID(row.RecoveryOwnerSessionID), RecoveryPath: row.RecoveryPath,
		RecoveryWakeCount: row.RecoveryWakeCount, RecoveryReviewRunID: row.RecoveryReviewRunID,
		RecoveryTargetSHA: row.RecoveryTargetSha, ErrorCode: row.ErrorCode,
		AuthorizedAt: row.AuthorizedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.DecisionAt.Valid {
		value := row.DecisionAt.Time
		result.DecisionAt = &value
	}
	if row.FinishedAt.Valid {
		value := row.FinishedAt.Time
		result.FinishedAt = &value
	}
	return result
}
