package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func futureArbiterFromGen(row gen.DcpFutureCardArbiterV1) domain.DCPFutureArbiterIncident {
	result := domain.DCPFutureArbiterIncident{
		IncidentID: row.IncidentID, Generation: row.Generation, IdentityDigest: row.IdentityDigest,
		TaskID: row.TaskID, SessionID: domain.SessionID(row.SessionID), AdmissionID: row.AdmissionID,
		AdmissionSequence: row.AdmissionSequence, IncidentLeaseID: row.IncidentLeaseID, IncidentKind: row.IncidentKind,
		SourcePacketJSON: row.SourcePacketJson, SourcePacketDigest: row.SourcePacketDigest,
		PRURL: row.PRURL, PRNumber: row.PRNumber, CandidateHeadSHA: row.CandidateHeadSha,
		ReviewedBaseSHA: row.ReviewedBaseSha, CurrentMainSHA: row.CurrentMainSha, ReviewRunID: row.ReviewRunID,
		AffectedPathsJSON: row.AffectedPathsJson, CohortJSON: row.CohortJson, CohortDigest: row.CohortDigest,
		EvidenceJSON: row.EvidenceJson, EvidenceDigest: row.EvidenceDigest, InputJSON: row.InputJson, InputDigest: row.InputDigest,
		ModelActionID: row.ModelActionID, RuntimeHandleID: row.RuntimeHandleID,
		Status: domain.DCPFutureArbiterStatus(row.Status), ModelCallCount: row.ModelCallCount,
		DecisionJSON: row.DecisionJson, DecisionDigest: row.DecisionDigest, Verdict: domain.DCPFutureArbiterVerdict(row.Verdict),
		OrderJSON: row.OrderJson, RepairTaskID: row.RepairTaskID, RepairObjective: row.RepairObjective,
		RepairPathsJSON: row.RepairPathsJson, HumanQuestion: row.HumanQuestion, RepairActionID: row.RepairActionID,
		RecoveryReviewRunID: row.RecoveryReviewRunID, RecoveryHeadSHA: row.RecoveryHeadSha,
		MergeCommitSHA: row.MergeCommitSha, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.DecisionAt.Valid {
		t := row.DecisionAt.Time
		result.DecisionAt = &t
	}
	if row.FinishedAt.Valid {
		t := row.FinishedAt.Time
		result.FinishedAt = &t
	}
	return result
}

func futureArbiterSchemaRecoveryFromGen(row gen.DcpFutureCardArbiterSchemaRecoveryV1) domain.DCPFutureArbiterSchemaRecovery {
	result := domain.DCPFutureArbiterSchemaRecovery{
		RecoveryID: row.RecoveryID, PredecessorIncidentID: row.PredecessorIncidentID,
		PredecessorIdentityDigest: row.PredecessorIdentityDigest, PredecessorInputDigest: row.PredecessorInputDigest,
		PredecessorModelActionID: row.PredecessorModelActionID, PredecessorSchemaDigest: row.PredecessorSchemaDigest,
		ProviderErrorJSON: row.ProviderErrorJson, ProviderErrorDigest: row.ProviderErrorDigest,
		ProviderInferenceTokens: row.ProviderInferenceTokens, SuccessorGeneration: row.SuccessorGeneration,
		Status: row.Status, SuccessorIncidentID: row.SuccessorIncidentID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ConsumedAt.Valid {
		t := row.ConsumedAt.Time
		result.ConsumedAt = &t
	}
	return result
}

func futureArbiterResultRecoveryFromGen(row gen.DcpFutureCardArbiterResultValidationRecoveryV1) domain.DCPFutureArbiterResultRecovery {
	result := domain.DCPFutureArbiterResultRecovery{
		RecoveryID: row.RecoveryID, IncidentID: row.IncidentID, IdentityDigest: row.IdentityDigest,
		InputDigest: row.InputDigest, ModelActionID: row.ModelActionID, PriorStatus: row.PriorStatus,
		PriorErrorCode: row.PriorErrorCode, PriorFinishedAt: row.PriorFinishedAt,
		PriorModelCallCount: row.PriorModelCallCount, PriorDecisionDigest: row.PriorDecisionDigest,
		RuntimeHandleID: row.RuntimeHandleID, PhysicalRuntimeHandle: row.PhysicalRuntimeHandle,
		InputArtifactDigest: row.InputArtifactDigest, InputArtifactSize: row.InputArtifactSize,
		SchemaArtifactDigest: row.SchemaArtifactDigest, SchemaArtifactSize: row.SchemaArtifactSize,
		ResultArtifactDigest: row.ResultArtifactDigest, ResultArtifactSize: row.ResultArtifactSize,
		CodexSessionID: row.CodexSessionID, InferenceTokens: row.InferenceTokens,
		ContractCommit: row.ContractCommit, Status: row.Status, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt,
	}
	if row.FinishedAt.Valid {
		value := row.FinishedAt.Time
		result.FinishedAt = &value
	}
	return result
}

//nolint:dupl // Separate generated row types preserve additive immutable audit tables.
func futureArbiterHumanGateRecoveryFromGen(row gen.DcpFutureCardArbiterHumanGateResultRecoveryV1) domain.DCPFutureArbiterResultRecovery {
	result := domain.DCPFutureArbiterResultRecovery{
		RecoveryID: row.RecoveryID, IncidentID: row.IncidentID, IdentityDigest: row.IdentityDigest,
		InputDigest: row.InputDigest, ModelActionID: row.ModelActionID, PriorStatus: row.PriorStatus,
		PriorErrorCode: row.PriorErrorCode, PriorFinishedAt: row.PriorFinishedAt,
		PriorModelCallCount: row.PriorModelCallCount, PriorDecisionDigest: row.PriorDecisionDigest,
		RuntimeHandleID: row.RuntimeHandleID, PhysicalRuntimeHandle: row.PhysicalRuntimeHandle,
		InputArtifactDigest: row.InputArtifactDigest, InputArtifactSize: row.InputArtifactSize,
		SchemaArtifactDigest: row.SchemaArtifactDigest, SchemaArtifactSize: row.SchemaArtifactSize,
		ResultArtifactDigest: row.ResultArtifactDigest, ResultArtifactSize: row.ResultArtifactSize,
		CodexSessionID: row.CodexSessionID, InferenceTokens: row.InferenceTokens,
		ContractCommit: row.ContractCommit, Status: row.Status, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt,
	}
	if row.FinishedAt.Valid {
		value := row.FinishedAt.Time
		result.FinishedAt = &value
	}
	return result
}

// GetDCPFutureArbiterIncidentByID reads one exact ordinary-card incident generation.
func (s *Store) GetDCPFutureArbiterIncidentByID(ctx context.Context, id string) (domain.DCPFutureArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPFutureArbiterIncidentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterIncident{}, false, nil
	}
	return futureArbiterFromGen(row), err == nil, err
}

// GetDCPFutureArbiterIncidentByAdmission reads the latest generation for an admission.
func (s *Store) GetDCPFutureArbiterIncidentByAdmission(ctx context.Context, id string) (domain.DCPFutureArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPFutureArbiterIncidentByAdmission(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterIncident{}, false, nil
	}
	return futureArbiterFromGen(row), err == nil, err
}

// GetDCPFutureArbiterIncidentByTask reads the latest generation for a policy task.
func (s *Store) GetDCPFutureArbiterIncidentByTask(ctx context.Context, id string) (domain.DCPFutureArbiterIncident, bool, error) {
	row, err := s.qr.GetDCPFutureArbiterIncidentByTask(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterIncident{}, false, nil
	}
	return futureArbiterFromGen(row), err == nil, err
}

// ListDCPFutureArbiterIncidents returns all immutable ordinary-card incident generations.
func (s *Store) ListDCPFutureArbiterIncidents(ctx context.Context) ([]domain.DCPFutureArbiterIncident, error) {
	rows, err := s.qr.ListDCPFutureArbiterIncidents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPFutureArbiterIncident, 0, len(rows))
	for _, row := range rows {
		out = append(out, futureArbiterFromGen(row))
	}
	return out, nil
}

// CountDCPFutureArbiterGenerationsForTask returns the durable generation count.
func (s *Store) CountDCPFutureArbiterGenerationsForTask(ctx context.Context, taskID string) (int64, error) {
	return s.qr.CountDCPFutureArbiterGenerationsForTask(ctx, taskID)
}

// GetDCPFutureArbiterSchemaRecoveryByPredecessor reads the sole separately
// authorized recovery, if any, for an immutable terminal generation.
func (s *Store) GetDCPFutureArbiterSchemaRecoveryByPredecessor(ctx context.Context, incidentID string) (domain.DCPFutureArbiterSchemaRecovery, bool, error) {
	row, err := s.qr.GetDCPFutureArbiterSchemaRecoveryByPredecessor(ctx, incidentID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterSchemaRecovery{}, false, nil
	}
	return futureArbiterSchemaRecoveryFromGen(row), err == nil, err
}

// GetDCPFutureArbiterResultRecovery reads the one exact model-free result
// validation audit, if this database contains its frozen predecessor.
func (s *Store) GetDCPFutureArbiterResultRecovery(ctx context.Context, incidentID string) (domain.DCPFutureArbiterResultRecovery, bool, error) {
	row, err := s.qr.GetDCPFutureArbiterResultValidationRecovery(ctx, incidentID)
	if err == nil {
		return futureArbiterResultRecoveryFromGen(row), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterResultRecovery{}, false, err
	}
	humanGate, err := s.qr.GetDCPFutureArbiterHumanGateResultRecovery(ctx, incidentID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPFutureArbiterResultRecovery{}, false, nil
	}
	if err != nil {
		return domain.DCPFutureArbiterResultRecovery{}, false, err
	}
	return futureArbiterHumanGateRecoveryFromGen(humanGate), true, nil
}

// OpenDCPFutureArbiterIncident atomically persists one incident and queued action.
func (s *Store) OpenDCPFutureArbiterIncident(ctx context.Context, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction) (domain.DCPFutureArbiterIncident, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result domain.DCPFutureArbiterIncident
	var created bool
	err := s.inTx(ctx, "open DCP future-card arbiter incident", func(q *gen.Queries) error {
		if existing, err := q.GetDCPFutureArbiterIncidentByAdmission(ctx, incident.AdmissionID); err == nil {
			latest := futureArbiterFromGen(existing)
			if latest.Generation == incident.Generation {
				if !sameFutureArbiterOpenIdentity(latest, incident) || action.ID != latest.ModelActionID ||
					action.IncidentID != latest.IncidentID || action.TaskID != latest.TaskID || action.SessionID != latest.SessionID {
					return ErrDCPPolicyStale
				}
				result = latest
				return nil
			}
			if latest.Status != domain.DCPFutureArbiterHold || incident.Generation != latest.Generation+1 ||
				latest.TaskID != incident.TaskID || latest.SessionID != incident.SessionID || latest.AdmissionID != incident.AdmissionID ||
				latest.SourcePacketJSON != incident.SourcePacketJSON || latest.CandidateHeadSHA != incident.CandidateHeadSHA ||
				latest.CurrentMainSHA == incident.CurrentMainSHA {
				return ErrDCPPolicyStale
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, incident.TaskID)
		if err != nil {
			return err
		}
		task := dcpPolicyTaskFromGen(taskRow)
		admissionRow, err := q.GetDCPReviewLabAdmissionByID(ctx, incident.AdmissionID)
		if err != nil {
			return err
		}
		admission := dcpAdmissionFromRow(admissionRow)
		if task.State != domain.DCPPolicyIncident || task.SessionID != incident.SessionID || task.AdmissionID != incident.AdmissionID ||
			task.CurrentHeadSHA != incident.CandidateHeadSHA || task.ReviewRunID != incident.ReviewRunID || task.IncidentPacket != incident.SourcePacketJSON ||
			admission.Status != domain.DCPAdmissionIncident || admission.Sequence != incident.AdmissionSequence || admission.LeaseID != incident.IncidentLeaseID ||
			admission.IncidentPacket != incident.SourcePacketJSON || admission.TargetSHA != incident.CandidateHeadSHA ||
			action.Kind != domain.DCPActionArbiter || action.IncidentID != incident.IncidentID || action.TaskID != incident.TaskID || action.SessionID != incident.SessionID {
			return ErrDCPPolicyStale
		}
		if err := insertFutureArbiterIncident(ctx, q, incident, action); err != nil {
			return err
		}
		result, created = incident, true
		return nil
	})
	return result, created, err
}

func insertFutureArbiterIncident(ctx context.Context, q *gen.Queries, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction) error {
	rows, err := q.InsertDCPFutureArbiterIncident(ctx, gen.InsertDCPFutureArbiterIncidentParams{
		IncidentID: incident.IncidentID, Generation: incident.Generation, IdentityDigest: incident.IdentityDigest,
		TaskID: incident.TaskID, SessionID: string(incident.SessionID), AdmissionID: incident.AdmissionID,
		AdmissionSequence: incident.AdmissionSequence, IncidentLeaseID: incident.IncidentLeaseID, IncidentKind: incident.IncidentKind,
		SourcePacketJson: incident.SourcePacketJSON, SourcePacketDigest: incident.SourcePacketDigest,
		PRURL: incident.PRURL, PRNumber: incident.PRNumber, CandidateHeadSha: incident.CandidateHeadSHA,
		ReviewedBaseSha: incident.ReviewedBaseSHA, CurrentMainSha: incident.CurrentMainSHA, ReviewRunID: incident.ReviewRunID,
		AffectedPathsJson: incident.AffectedPathsJSON, CohortJson: incident.CohortJSON, CohortDigest: incident.CohortDigest,
		EvidenceJson: incident.EvidenceJSON, EvidenceDigest: incident.EvidenceDigest, InputJson: incident.InputJSON,
		InputDigest: incident.InputDigest, ModelActionID: action.ID, RuntimeHandleID: incident.RuntimeHandleID,
		CreatedAt: incident.CreatedAt, UpdatedAt: incident.UpdatedAt,
	})
	if err != nil || rows != 1 {
		return errors.Join(err, ErrDCPPolicyStale)
	}
	return q.InsertDCPModelAction(ctx, modelActionInsertParams(action))
}

// OpenDCPFutureArbiterSchemaRecovery atomically consumes one exact recovery
// grant and opens its additive successor generation. The failed predecessor is
// read and compared but never updated.
func (s *Store) OpenDCPFutureArbiterSchemaRecovery(ctx context.Context, predecessor domain.DCPFutureArbiterIncident, recovery domain.DCPFutureArbiterSchemaRecovery, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction) (domain.DCPFutureArbiterIncident, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result domain.DCPFutureArbiterIncident
	var created bool
	err := s.inTx(ctx, "open DCP future-card arbiter schema recovery", func(q *gen.Queries) error {
		latestRow, err := q.GetDCPFutureArbiterIncidentByAdmission(ctx, predecessor.AdmissionID)
		if err != nil {
			return err
		}
		latest := futureArbiterFromGen(latestRow)
		if !sameFutureArbiterOpenIdentity(latest, predecessor) || latest.Status != domain.DCPFutureArbiterFailed || latest.ErrorCode != "launch_failed" ||
			latest.ModelCallCount != 1 || latest.DecisionJSON != "" || latest.DecisionDigest != "" ||
			recovery.Status != "authorized" || recovery.PredecessorIncidentID != predecessor.IncidentID ||
			recovery.PredecessorIdentityDigest != predecessor.IdentityDigest || recovery.PredecessorInputDigest != predecessor.InputDigest ||
			recovery.PredecessorModelActionID != predecessor.ModelActionID || recovery.ProviderInferenceTokens != 0 ||
			recovery.SuccessorGeneration != predecessor.Generation+1 || incident.Generation != recovery.SuccessorGeneration ||
			!sameFutureArbiterRecoveryEvidence(predecessor, incident) || action.ID != incident.ModelActionID || action.IncidentID != incident.IncidentID ||
			action.Kind != domain.DCPActionArbiter || action.TaskID != incident.TaskID || action.SessionID != incident.SessionID {
			return ErrDCPPolicyStale
		}
		actionRow, err := q.GetDCPModelActionByID(ctx, predecessor.ModelActionID)
		if err != nil || actionRow.Status != string(domain.DCPActionFailed) || actionRow.ErrorCode != "launch_failed" || actionRow.IncidentID != predecessor.IncidentID {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		if err := insertFutureArbiterIncident(ctx, q, incident, action); err != nil {
			return err
		}
		rows, err := q.ConsumeDCPFutureArbiterSchemaRecovery(ctx, gen.ConsumeDCPFutureArbiterSchemaRecoveryParams{
			SuccessorIncidentID: incident.IncidentID, ConsumedAt: incident.CreatedAt, RecoveryID: recovery.RecoveryID,
			PredecessorIncidentID: recovery.PredecessorIncidentID, PredecessorIdentityDigest: recovery.PredecessorIdentityDigest,
			PredecessorInputDigest: recovery.PredecessorInputDigest, PredecessorModelActionID: recovery.PredecessorModelActionID,
			PredecessorSchemaDigest: recovery.PredecessorSchemaDigest, ProviderErrorDigest: recovery.ProviderErrorDigest,
			SuccessorGeneration: recovery.SuccessorGeneration,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		result, created = incident, true
		return nil
	})
	return result, created, err
}

//nolint:dupl // Every frozen evidence field is an intentional recovery fence.
func sameFutureArbiterRecoveryEvidence(predecessor, successor domain.DCPFutureArbiterIncident) bool {
	return predecessor.TaskID == successor.TaskID && predecessor.SessionID == successor.SessionID &&
		predecessor.AdmissionID == successor.AdmissionID && predecessor.AdmissionSequence == successor.AdmissionSequence &&
		predecessor.IncidentLeaseID == successor.IncidentLeaseID && predecessor.IncidentKind == successor.IncidentKind &&
		predecessor.SourcePacketJSON == successor.SourcePacketJSON && predecessor.SourcePacketDigest == successor.SourcePacketDigest &&
		predecessor.PRURL == successor.PRURL && predecessor.PRNumber == successor.PRNumber &&
		predecessor.CandidateHeadSHA == successor.CandidateHeadSHA && predecessor.ReviewedBaseSHA == successor.ReviewedBaseSHA &&
		predecessor.CurrentMainSHA == successor.CurrentMainSHA && predecessor.ReviewRunID == successor.ReviewRunID &&
		predecessor.AffectedPathsJSON == successor.AffectedPathsJSON && predecessor.CohortJSON == successor.CohortJSON &&
		predecessor.CohortDigest == successor.CohortDigest && predecessor.EvidenceJSON == successor.EvidenceJSON &&
		predecessor.EvidenceDigest == successor.EvidenceDigest
}

//nolint:dupl // Explicit equality is the conflicting-replay fence for every immutable input field.
func sameFutureArbiterOpenIdentity(a, b domain.DCPFutureArbiterIncident) bool {
	return a.IncidentID == b.IncidentID && a.Generation == b.Generation && a.IdentityDigest == b.IdentityDigest &&
		a.TaskID == b.TaskID && a.SessionID == b.SessionID && a.AdmissionID == b.AdmissionID && a.AdmissionSequence == b.AdmissionSequence &&
		a.IncidentLeaseID == b.IncidentLeaseID && a.IncidentKind == b.IncidentKind && a.SourcePacketJSON == b.SourcePacketJSON &&
		a.SourcePacketDigest == b.SourcePacketDigest && a.PRURL == b.PRURL && a.PRNumber == b.PRNumber && a.CandidateHeadSHA == b.CandidateHeadSHA &&
		a.ReviewedBaseSHA == b.ReviewedBaseSHA && a.CurrentMainSHA == b.CurrentMainSHA && a.ReviewRunID == b.ReviewRunID &&
		a.AffectedPathsJSON == b.AffectedPathsJSON && a.CohortJSON == b.CohortJSON && a.CohortDigest == b.CohortDigest &&
		a.EvidenceJSON == b.EvidenceJSON && a.EvidenceDigest == b.EvidenceDigest && a.InputJSON == b.InputJSON && a.InputDigest == b.InputDigest &&
		a.ModelActionID == b.ModelActionID && a.RuntimeHandleID == b.RuntimeHandleID
}

// FailDCPFutureArbiterIncident closes one exact generation without retry.
func (s *Store) FailDCPFutureArbiterIncident(ctx context.Context, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	err := s.inTx(ctx, "fail DCP future-card arbiter", func(q *gen.Queries) error {
		rows, err := q.FailDCPFutureArbiterIncident(ctx, gen.FailDCPFutureArbiterIncidentParams{ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incident.IncidentID})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		rows, err = q.FinishDCPModelAction(ctx, gen.FinishDCPModelActionParams{Status: string(domain.DCPActionFailed), ErrorCode: code, UpdatedAt: now, ID: action.ID, Slot: action.Slot})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		changed = true
		return nil
	})
	return changed, err
}

// RecordDCPFutureArbiterDecision atomically consumes the one-call result and applies its bounded transition.
func (s *Store) RecordDCPFutureArbiterDecision(ctx context.Context, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction, decisionJSON, decisionDigest string, verdict domain.DCPFutureArbiterVerdict, orderJSON, repairObjective, repairPathsJSON, question string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	err := s.inTx(ctx, "record DCP future-card arbiter decision", func(q *gen.Queries) error {
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, incident.TaskID)
		if err != nil {
			return err
		}
		task := dcpPolicyTaskFromGen(taskRow)
		if task.State != domain.DCPPolicyIncident || task.AdmissionID != incident.AdmissionID || task.CurrentHeadSHA != incident.CandidateHeadSHA ||
			action.ID != incident.ModelActionID || action.IncidentID != incident.IncidentID || action.Status != domain.DCPActionRunning || action.Slot == 0 {
			return ErrDCPPolicyStale
		}
		status := domain.DCPFutureArbiterHumanGate
		repairActionID := ""
		finished := sql.NullTime{Time: now, Valid: true}
		if verdict == domain.DCPFutureVerdictOrderHold {
			status, finished = domain.DCPFutureArbiterHold, sql.NullTime{}
		}
		if verdict == domain.DCPFutureVerdictRepair {
			if task.RepairCount != 0 {
				return errors.New("DCP future-card repair allowance is consumed")
			}
			status, finished = domain.DCPFutureArbiterRepairQueued, sql.NullTime{}
			repairActionID = "dcp-model-" + task.TaskID + "-worker-2"
		}
		rows, err := q.DecideDCPFutureArbiterIncident(ctx, gen.DecideDCPFutureArbiterIncidentParams{
			Status: string(status), DecisionJson: decisionJSON, DecisionDigest: decisionDigest, Verdict: string(verdict),
			OrderJson: orderJSON, RepairTaskID: func() string {
				if verdict == domain.DCPFutureVerdictRepair {
					return task.TaskID
				}
				return ""
			}(),
			RepairObjective: repairObjective, RepairPathsJson: repairPathsJSON, HumanQuestion: question, RepairActionID: repairActionID,
			UpdatedAt: now, DecisionAt: sql.NullTime{Time: now, Valid: true}, FinishedAt: finished,
			IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		rows, err = q.FinishDCPModelAction(ctx, gen.FinishDCPModelActionParams{Status: string(domain.DCPActionSucceeded), ErrorCode: "", UpdatedAt: now, ID: action.ID, Slot: action.Slot})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		if verdict == domain.DCPFutureVerdictRepair {
			next := task
			next.State, next.RepairCount, next.UpdatedAt = domain.DCPPolicyRepairQueued, 1, now
			next.ErrorCode, next.IncidentPacket = "", ""
			rows, err = q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(task, next))
			if err != nil || rows != 1 {
				return errors.Join(err, ErrDCPPolicyStale)
			}
			repair := domain.DCPModelAction{ID: repairActionID, TaskID: task.TaskID, SessionID: task.SessionID, Kind: domain.DCPActionRepairWorker,
				ExactHeadSHA: task.CurrentHeadSHA, IncidentID: incident.IncidentID, Status: domain.DCPActionQueued, CreatedAt: now, UpdatedAt: now}
			if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(repair)); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	return changed, err
}

// RecoverDCPFutureArbiterExactDecision accepts one unchanged result without a
// second model call. The original failed action remains immutable; its exact
// failure facts live in the one-way validation audit before this transaction
// moves the incident and task to their ordinary bounded repair path.
func (s *Store) RecoverDCPFutureArbiterExactDecision(ctx context.Context, incident domain.DCPFutureArbiterIncident, decisionJSON, decisionDigest, orderJSON, repairObjective, repairPathsJSON string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	recovered := false
	err := s.inTx(ctx, "recover exact DCP future-card arbiter decision", func(q *gen.Queries) error {
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, incident.TaskID)
		if err != nil {
			return err
		}
		task := dcpPolicyTaskFromGen(taskRow)
		actionRow, err := q.GetDCPModelActionByID(ctx, incident.ModelActionID)
		if err != nil {
			return err
		}
		if task.State != domain.DCPPolicyIncident || task.AdmissionID != incident.AdmissionID ||
			task.CurrentHeadSHA != incident.CandidateHeadSHA || task.RepairCount != 0 ||
			actionRow.TaskID != incident.TaskID || actionRow.SessionID != string(incident.SessionID) ||
			actionRow.Kind != string(domain.DCPActionArbiter) || actionRow.IncidentID != incident.IncidentID ||
			actionRow.Status != string(domain.DCPActionFailed) || actionRow.ErrorCode != "launch_failed" || actionRow.Slot != 0 {
			return ErrDCPPolicyStale
		}
		repairActionID := "dcp-model-" + task.TaskID + "-worker-2"
		rows, err := q.RecoverDCPFutureArbiterExactDecision(ctx, gen.RecoverDCPFutureArbiterExactDecisionParams{
			DecisionJson: decisionJSON, DecisionDigest: decisionDigest, OrderJson: orderJSON,
			RepairTaskID: task.TaskID, RepairObjective: repairObjective, RepairPathsJson: repairPathsJSON,
			RepairActionID: repairActionID, DecisionAt: now, IncidentID: incident.IncidentID,
			IdentityDigest: incident.IdentityDigest, InputDigest: incident.InputDigest, ModelActionID: incident.ModelActionID,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		next := task
		next.State, next.RepairCount, next.UpdatedAt = domain.DCPPolicyRepairQueued, 1, now
		next.ErrorCode, next.IncidentPacket = "", ""
		rows, err = q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(task, next))
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		repair := domain.DCPModelAction{ID: repairActionID, TaskID: task.TaskID, SessionID: task.SessionID,
			Kind: domain.DCPActionRepairWorker, ExactHeadSHA: task.CurrentHeadSHA, IncidentID: incident.IncidentID,
			Status: domain.DCPActionQueued, CreatedAt: now, UpdatedAt: now}
		if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(repair)); err != nil {
			return err
		}
		rows, err = q.MarkDCPFutureArbiterResultValidationRecoveryApplied(ctx, gen.MarkDCPFutureArbiterResultValidationRecoveryAppliedParams{
			FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incident.IncidentID,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		recovered = true
		return nil
	})
	return recovered, err
}

// RecoverDCPFutureArbiterExactHumanGate accepts one unchanged result without a
// second model call and without authorizing code mutation. The policy task and
// admission remain in their existing incident state while the exact arbiter
// generation becomes the durable terminal owner question.
func (s *Store) RecoverDCPFutureArbiterExactHumanGate(ctx context.Context, incident domain.DCPFutureArbiterIncident, decisionJSON, decisionDigest, orderJSON, affectedPathsJSON, question string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	recovered := false
	err := s.inTx(ctx, "recover exact DCP future-card HumanGate decision", func(q *gen.Queries) error {
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, incident.TaskID)
		if err != nil {
			return err
		}
		task := dcpPolicyTaskFromGen(taskRow)
		actionRow, err := q.GetDCPModelActionByID(ctx, incident.ModelActionID)
		if err != nil {
			return err
		}
		if task.State != domain.DCPPolicyIncident || task.AdmissionID != incident.AdmissionID ||
			task.CurrentHeadSHA != incident.CandidateHeadSHA || task.RepairCount != 0 ||
			actionRow.TaskID != incident.TaskID || actionRow.SessionID != string(incident.SessionID) ||
			actionRow.Kind != string(domain.DCPActionArbiter) || actionRow.IncidentID != incident.IncidentID ||
			actionRow.Status != string(domain.DCPActionFailed) || actionRow.ErrorCode != "submit_failed" || actionRow.Slot != 0 {
			return ErrDCPPolicyStale
		}
		rows, err := q.RecoverDCPFutureArbiterExactHumanGate(ctx, gen.RecoverDCPFutureArbiterExactHumanGateParams{
			DecisionJson: decisionJSON, DecisionDigest: decisionDigest, OrderJson: orderJSON,
			AffectedPathsJson: affectedPathsJSON, HumanQuestion: question, DecisionAt: now,
			IncidentID: incident.IncidentID, IdentityDigest: incident.IdentityDigest,
			InputDigest: incident.InputDigest, ModelActionID: incident.ModelActionID,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		rows, err = q.MarkDCPFutureArbiterHumanGateResultRecoveryApplied(ctx, gen.MarkDCPFutureArbiterHumanGateResultRecoveryAppliedParams{
			FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incident.IncidentID,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		recovered = true
		return nil
	})
	return recovered, err
}

// FailDCPFutureArbiterResultRecovery makes a drifted one-time recovery inert.
func (s *Store) FailDCPFutureArbiterResultRecovery(ctx context.Context, incidentID, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.FailDCPFutureArbiterResultValidationRecovery(ctx, gen.FailDCPFutureArbiterResultValidationRecoveryParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incidentID,
	})
	if err != nil || rows == 1 {
		return rows == 1, err
	}
	rows, err = s.qw.FailDCPFutureArbiterHumanGateResultRecovery(ctx, gen.FailDCPFutureArbiterHumanGateResultRecoveryParams{
		ErrorCode: errorCode, FinishedAt: sql.NullTime{Time: now, Valid: true}, IncidentID: incidentID,
	})
	return rows == 1, err
}

// RebindDCPFutureArbiterAdmission binds the fresh reviewed successor head to the existing FIFO row.
func (s *Store) RebindDCPFutureArbiterAdmission(ctx context.Context, incident domain.DCPFutureArbiterIncident, task domain.DCPReviewLabPolicyTask, run domain.ReviewRun, reviewBase string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	err := s.inTx(ctx, "rebind DCP future-card arbiter admission", func(q *gen.Queries) error {
		if task.State != domain.DCPPolicyAdmissionWait || task.AdmissionID != incident.AdmissionID || task.SessionID != incident.SessionID ||
			task.CurrentHeadSHA != run.TargetSHA || task.ReviewRunID != run.ID || strings.EqualFold(run.TargetSHA, incident.CandidateHeadSHA) {
			return ErrDCPPolicyStale
		}
		rows, err := q.RebindDCPFutureArbiterAdmission(ctx, gen.RebindDCPFutureArbiterAdmissionParams{
			ReviewRunID: run.ID, ReviewID: run.ReviewID, TargetSha: strings.ToLower(run.TargetSHA), ReviewBaseSha: strings.ToLower(reviewBase), UpdatedAt: now,
			ID: incident.AdmissionID, ReviewRunID_2: incident.ReviewRunID, SessionID: string(incident.SessionID), PRURL: incident.PRURL,
			TargetSha_2: incident.CandidateHeadSHA, LeaseID: incident.IncidentLeaseID, ErrorCode: incident.IncidentKind,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		rows, err = q.MarkDCPFutureArbiterRecoveryReviewed(ctx, gen.MarkDCPFutureArbiterRecoveryReviewedParams{RecoveryReviewRunID: run.ID, RecoveryHeadSha: strings.ToLower(run.TargetSHA), UpdatedAt: now, IncidentID: incident.IncidentID})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		changed = true
		return nil
	})
	return changed, err
}
