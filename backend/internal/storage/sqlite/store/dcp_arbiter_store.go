package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func (s *Store) OpenDCPReleaseArbiterIncident(ctx context.Context, a domain.DCPReviewLabAdmission, incident domain.DCPReleaseArbiterIncident) (domain.DCPReleaseArbiterIncident, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.InsertDCPReleaseArbiterIncident(ctx, gen.InsertDCPReleaseArbiterIncidentParams{
		IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest,
		SourcePacketDigest: incident.SourcePacketDigest, InputJson: incident.InputJSON,
		InputDigest: incident.InputDigest, TaskID: incident.TaskID, WorktreePath: incident.WorktreePath,
		SourceBranch: incident.SourceBranch, CurrentBaseSha: incident.CurrentBaseSHA, BatchID: incident.BatchID,
		ScopeDigest: incident.ScopeDigest, HistoryDigest: incident.HistoryDigest, DiffDigest: incident.DiffDigest,
		CheckSetDigest: incident.CheckSetDigest, ReviewSetDigest: incident.ReviewSetDigest,
		FrozenQueueDigest: incident.FrozenQueueDigest, MechanicalDigest: incident.MechanicalDigest,
		CreatedAt: incident.CreatedAt, UpdatedAt: incident.UpdatedAt, AdmissionID: a.ID,
		AdmissionSequence: a.Sequence, SessionID: string(a.SessionID), ReviewRunID: a.ReviewRunID,
		TargetSha: a.TargetSHA, IncidentLeaseID: a.LeaseID, SourcePacketJson: a.IncidentPacket,
	})
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, false, err
	}
	row, err := s.qw.GetDCPReleaseArbiterIncidentByAdmission(ctx, a.ID)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, false, err
	}
	got := dcpArbiterFromRow(row)
	if !sameDCPArbiterImmutable(got, incident) {
		return domain.DCPReleaseArbiterIncident{}, false, errors.New("DCP arbiter incident identity replay drift")
	}
	return got, n == 1, nil
}

func (s *Store) GetDCPReleaseArbiterIncidentByID(ctx context.Context, id string) (domain.DCPReleaseArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPReleaseArbiterIncidentByID(ctx, id)
	return dcpArbiterResult(row, err)
}

func (s *Store) GetDCPReleaseArbiterIncidentByAdmission(ctx context.Context, id string) (domain.DCPReleaseArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPReleaseArbiterIncidentByAdmission(ctx, id)
	return dcpArbiterResult(row, err)
}

func (s *Store) GetDCPReleaseArbiterIncidentBySession(ctx context.Context, id domain.SessionID) (domain.DCPReleaseArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPReleaseArbiterIncidentBySession(ctx, string(id))
	return dcpArbiterResult(row, err)
}

func (s *Store) ListDCPReleaseArbiterIncidents(ctx context.Context) ([]domain.DCPReleaseArbiterIncident, error) {
	rows, err := s.qr.ListDCPReleaseArbiterIncidents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPReleaseArbiterIncident, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpArbiterFromRow(row))
	}
	return out, nil
}

func (s *Store) StartDCPReleaseArbiterCall(ctx context.Context, incident domain.DCPReleaseArbiterIncident, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPReleaseArbiterCall(ctx, gen.StartDCPReleaseArbiterCallParams{
		UpdatedAt: now, IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterPreflight(ctx context.Context, incidentID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterPreflight(ctx, gen.FailDCPReleaseArbiterPreflightParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incidentID,
	})
	return n == 1, err
}

func (s *Store) RecordDCPReleaseArbiterDecision(ctx context.Context, incident domain.DCPReleaseArbiterIncident, decisionJSON, decisionDigest string, safeStop bool, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	status := string(domain.DCPArbiterDecided)
	finished := sql.NullTime{}
	if safeStop {
		status = string(domain.DCPArbiterSafeStopped)
		finished = sql.NullTime{Time: now, Valid: true}
	}
	n, err := s.qw.RecordDCPReleaseArbiterDecision(ctx, gen.RecordDCPReleaseArbiterDecisionParams{
		Status: status, DecisionJson: decisionJSON, DecisionDigest: decisionDigest, ErrorCode: errorCode,
		DecisionAt: sql.NullTime{Time: now, Valid: true}, FinishedAt: finished,
		IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
	})
	return n == 1, err
}

func (s *Store) ConsumeDCPReleaseArbiterRepair(ctx context.Context, incident domain.DCPReleaseArbiterIncident, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ConsumeDCPReleaseArbiterRepair(ctx, gen.ConsumeDCPReleaseArbiterRepairParams{
		UpdatedAt: now, IncidentID: incident.IncidentID, DecisionDigest: incident.DecisionDigest,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterCall(ctx context.Context, incidentID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterCall(ctx, gen.FailDCPReleaseArbiterCallParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incidentID,
	})
	return n == 1, err
}

func (s *Store) FailDCPReleaseArbiterAfterDecision(ctx context.Context, incidentID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPReleaseArbiterAfterDecision(ctx, gen.FailDCPReleaseArbiterAfterDecisionParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incidentID,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterArbiterRepair(ctx context.Context, a domain.DCPReviewLabAdmission, incident domain.DCPReleaseArbiterIncident, run domain.ReviewRun, reviewBaseSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after arbiter repair", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterArbiterRepair(ctx, gen.RebindDCPAdmissionAfterArbiterRepairParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: a.ID,
			OldReviewRunID: a.ReviewRunID, SessionID: string(a.SessionID), PRURL: a.PRURL,
			OldTargetSha: a.TargetSHA, IncidentLeaseID: a.LeaseID, IncidentID: incident.IncidentID,
		})
		if err != nil || n == 0 {
			return err
		}
		n, err = q.MarkDCPReleaseArbiterRecoveryReviewed(ctx, gen.MarkDCPReleaseArbiterRecoveryReviewedParams{
			RecoveryReviewRunID: run.ID, RecoveryTargetSha: run.TargetSHA, UpdatedAt: now, IncidentID: incident.IncidentID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact DCP arbiter recovery review transition was unavailable")
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpArbiterResult(row gen.DcpReviewLabArbiterV1, err error) (domain.DCPReleaseArbiterIncident, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPReleaseArbiterIncident{}, false, nil
	}
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, false, err
	}
	return dcpArbiterFromRow(row), true, nil
}

func dcpArbiterFromRow(row gen.DcpReviewLabArbiterV1) domain.DCPReleaseArbiterIncident {
	result := domain.DCPReleaseArbiterIncident{
		IncidentID: row.IncidentID, Generation: row.Generation, IdentityDigest: row.IdentityDigest,
		AdmissionID: row.AdmissionID, IncidentLeaseID: row.IncidentLeaseID,
		SourcePacketJSON: row.SourcePacketJson, SourcePacketDigest: row.SourcePacketDigest,
		InputJSON: row.InputJson, InputDigest: row.InputDigest, TaskID: row.TaskID,
		SessionID: domain.SessionID(row.SessionID), WorktreePath: row.WorktreePath, SourceBranch: row.SourceBranch,
		PRURL: row.PRURL, PRNumber: row.PRNumber, TargetSHA: row.TargetSha,
		ReviewedBaseSHA: row.ReviewedBaseSha, CurrentBaseSHA: row.CurrentBaseSha,
		ReviewID: row.ReviewID, ReviewRunID: row.ReviewRunID, BatchID: row.BatchID,
		ScopeDigest: row.ScopeDigest, HistoryDigest: row.HistoryDigest, DiffDigest: row.DiffDigest,
		CheckSetDigest: row.CheckSetDigest, ReviewSetDigest: row.ReviewSetDigest,
		FrozenQueueDigest: row.FrozenQueueDigest, MechanicalDigest: row.MechanicalDigest,
		Model: row.Model, Reasoning: row.Reasoning, TokenBudget: row.TokenBudget,
		RuntimeHandleID: row.RuntimeHandleID, LaunchID: row.LaunchID,
		Status: domain.DCPReleaseArbiterStatus(row.Status), ModelCallCount: row.ModelCallCount,
		DecisionJSON: row.DecisionJson, DecisionDigest: row.DecisionDigest,
		RecoveryOwnerSessionID: domain.SessionID(row.RecoveryOwnerSessionID), RecoveryPath: row.RecoveryPath,
		RecoveryWakeCount: row.RecoveryWakeCount, RecoveryReviewRunID: row.RecoveryReviewRunID,
		RecoveryTargetSHA: row.RecoveryTargetSha, ErrorCode: row.ErrorCode,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
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

func sameDCPArbiterImmutable(a, b domain.DCPReleaseArbiterIncident) bool {
	return a.IncidentID == b.IncidentID && a.Generation == b.Generation && a.IdentityDigest == b.IdentityDigest &&
		a.AdmissionID == b.AdmissionID && a.IncidentLeaseID == b.IncidentLeaseID &&
		a.SourcePacketJSON == b.SourcePacketJSON && a.SourcePacketDigest == b.SourcePacketDigest &&
		a.InputJSON == b.InputJSON && a.InputDigest == b.InputDigest && a.TaskID == b.TaskID &&
		a.SessionID == b.SessionID && a.WorktreePath == b.WorktreePath && a.SourceBranch == b.SourceBranch &&
		a.PRURL == b.PRURL && a.PRNumber == b.PRNumber && a.TargetSHA == b.TargetSHA &&
		a.ReviewedBaseSHA == b.ReviewedBaseSHA && a.CurrentBaseSHA == b.CurrentBaseSHA &&
		a.ReviewID == b.ReviewID && a.ReviewRunID == b.ReviewRunID && a.BatchID == b.BatchID &&
		a.ScopeDigest == b.ScopeDigest && a.HistoryDigest == b.HistoryDigest && a.DiffDigest == b.DiffDigest &&
		a.CheckSetDigest == b.CheckSetDigest && a.ReviewSetDigest == b.ReviewSetDigest &&
		a.FrozenQueueDigest == b.FrozenQueueDigest && a.MechanicalDigest == b.MechanicalDigest &&
		a.Model == b.Model && a.Reasoning == b.Reasoning && a.TokenBudget == b.TokenBudget &&
		a.RuntimeHandleID == b.RuntimeHandleID && a.LaunchID == b.LaunchID
}

func requireDCPArbiterCompletion(q *gen.Queries, ctx context.Context, admission domain.DCPReviewLabAdmission, mergeSHA string, now time.Time) error {
	row, err := q.GetDCPReleaseArbiterIncidentByAdmission(ctx, admission.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	incident := dcpArbiterFromRow(row)
	coldStartRow, coldStartErr := q.GetDCPCard12ColdStartRecovery(ctx, dcpCard12ColdStartRecoveryID)
	if coldStartErr == nil {
		coldStart := dcpCard12ColdStartRecoveryFromRow(coldStartRow)
		if coldStart.AdmissionID == admission.ID && coldStart.IncidentID == incident.IncidentID &&
			coldStart.Status == domain.DCPColdStartRecoveryRecoveryReviewed &&
			coldStart.RecoveryReviewRunID == admission.ReviewRunID && coldStart.NewHead == admission.TargetSHA {
			if coldStart.ModelFreeActionCount != 1 || coldStart.ReviewerModelCallCount != 1 ||
				coldStart.WorkerModelCallCount != 0 || coldStart.ArbiterModelCallCount != 0 {
				return errors.New("card-12 cold-start terminal identity is not exact")
			}
			n, completeErr := q.CompleteDCPCard12ColdStartRecovery(ctx, gen.CompleteDCPCard12ColdStartRecoveryParams{
				MergeCommitSha: mergeSHA, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
				RecoveryID: coldStart.RecoveryID, ReviewRunID: admission.ReviewRunID, TargetSha: admission.TargetSHA,
			})
			if completeErr != nil || n != 1 {
				return errors.Join(completeErr, errors.New("card-12 cold-start terminal completion was unavailable"))
			}
			return nil
		}
	} else if !errors.Is(coldStartErr, sql.ErrNoRows) {
		return coldStartErr
	}
	continuationRow, continuationErr := q.GetDCPCard12ModelFreeRebaseContinuation(ctx, dcpCard12ModelFreeRebaseContinuationID)
	if continuationErr == nil {
		continuation := dcpCard12ModelFreeRebaseContinuationFromRow(continuationRow)
		if continuation.AdmissionID != admission.ID || continuation.IncidentID != incident.IncidentID ||
			continuation.Status != domain.DCPModelFreeRebaseRecoveryReviewed ||
			continuation.RecoveryReviewRunID != admission.ReviewRunID || continuation.NewHead != admission.TargetSHA ||
			continuation.ModelFreeActionCount != 1 || continuation.ReviewerModelCallCount != 1 {
			return errors.New("card-12 model-free terminal identity is not exact")
		}
		n, completeErr := q.CompleteDCPCard12ModelFreeRebase(ctx, gen.CompleteDCPCard12ModelFreeRebaseParams{
			MergeCommitSha: mergeSHA, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
			ContinuationID: continuation.ContinuationID, ReviewRunID: admission.ReviewRunID, TargetSha: admission.TargetSHA,
		})
		if completeErr != nil {
			return completeErr
		}
		if n != 1 {
			return errors.New("card-12 model-free terminal completion was unavailable")
		}
		return nil
	}
	if !errors.Is(continuationErr, sql.ErrNoRows) {
		return continuationErr
	}
	freshRow, freshErr := q.GetDCPCard12FreshWorkerRecovery(ctx, dcpCard12FreshWorkerRecoveryID)
	if freshErr == nil {
		fresh := dcpCard12FreshWorkerRecoveryFromRow(freshRow)
		if fresh.AdmissionID != admission.ID || fresh.IncidentID != incident.IncidentID ||
			fresh.Status != domain.DCPFreshWorkerRecoveryReviewed ||
			fresh.RecoveryReviewRunID != admission.ReviewRunID || fresh.NewHead != admission.TargetSHA ||
			fresh.WorkerModelCallCount != 1 || fresh.ReviewerModelCallCount != 1 ||
			fresh.PredecessorStatus != "failed" || fresh.PredecessorError != "repair_launch_failed" {
			return errors.New("card-12 fresh recovery terminal identity is not exact")
		}
		n, completeErr := q.CompleteDCPCard12FreshWorkerRecovery(ctx, gen.CompleteDCPCard12FreshWorkerRecoveryParams{
			MergeCommitSha: mergeSHA, UpdatedAt: now,
			FinishedAt: sql.NullTime{Time: now, Valid: true}, RecoveryID: fresh.RecoveryID,
			ReviewRunID: admission.ReviewRunID, TargetSha: admission.TargetSHA,
		})
		if completeErr != nil {
			return completeErr
		}
		if n != 1 {
			return errors.New("card-12 fresh recovery terminal completion was unavailable")
		}
		return nil
	}
	if !errors.Is(freshErr, sql.ErrNoRows) {
		return freshErr
	}
	if incident.Status == domain.DCPArbiterFailed && incident.IncidentID == "dcp-global-release-2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60" {
		if incident.Generation != 1 || incident.IdentityDigest != "2694dbd8b3d4897063603d7a8607ca516aa2f8e05c5a3c39cf56d8e3f18c3c60" ||
			incident.InputDigest != "f618fa8a46715acce0958b592384f0d42c071562e36988163e2b96f2c157fc49" ||
			incident.AdmissionID != "dcp-admission-ecb500ad-f9f0-443b-9d73-2c8a6350ce34" || incident.SessionID != "dcp-review-lab-12" ||
			incident.PRURL != "https://github.com/orenvlad-ai/dcp-review-lab/pull/9" || incident.PRNumber != 9 ||
			incident.ModelCallCount != 1 || incident.DecisionJSON != "" || incident.DecisionDigest != "" ||
			incident.RecoveryWakeCount != 0 || incident.ErrorCode != "submit_failed" {
			return errors.New("rejected DCP arbiter evidence is not exact")
		}
		successorRow, successorErr := q.GetDCPReleaseArbiterSuccessorAttemptByIncident(ctx, incident.IncidentID)
		if successorErr != nil {
			return successorErr
		}
		successor := dcpArbiterSuccessorFromRow(successorRow)
		if successor.AttemptID != "dcp-arbiter-successor-3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d" ||
			successor.AttemptIdentityDigest != "3c62ea80b56ef94165519d4f01e4c449c320bff22d16b902dd68d4a1a355ea7d" ||
			successor.Status != domain.DCPArbiterSuccessorRecoveryReviewed || successor.AttemptGeneration != 2 || successor.ModelCallCount != 1 ||
			successor.PolicyMaxWorkerCalls != 1 || successor.PolicyMaxFreshReviews != 1 || successor.RecoveryWakeCount != 1 ||
			successor.RecoveryReviewRunID != admission.ReviewRunID || successor.RecoveryTargetSHA != admission.TargetSHA {
			return fmt.Errorf("DCP arbiter successor terminal identity is not exact")
		}
		n, completeErr := q.CompleteDCPReleaseArbiterSuccessorAttempt(ctx, gen.CompleteDCPReleaseArbiterSuccessorAttemptParams{
			FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incident.IncidentID,
			RecoveryReviewRunID: admission.ReviewRunID, RecoveryTargetSha: admission.TargetSHA,
		})
		if completeErr != nil {
			return completeErr
		}
		if n != 1 {
			return errors.New("exact DCP arbiter successor terminal completion was unavailable")
		}
		return nil
	}
	if incident.Status != domain.DCPArbiterRecoveryReviewed || incident.RecoveryReviewRunID != admission.ReviewRunID || incident.RecoveryTargetSHA != admission.TargetSHA {
		return fmt.Errorf("DCP arbiter terminal identity is not exact")
	}
	n, err := q.CompleteDCPReleaseArbiterIncident(ctx, gen.CompleteDCPReleaseArbiterIncidentParams{
		FinishedAt: sql.NullTime{Time: now, Valid: true}, AdmissionID: admission.ID,
		RecoveryReviewRunID: admission.ReviewRunID, RecoveryTargetSha: admission.TargetSHA,
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("exact DCP arbiter terminal completion was unavailable")
	}
	return nil
}
