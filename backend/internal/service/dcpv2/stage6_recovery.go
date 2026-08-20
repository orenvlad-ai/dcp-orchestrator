package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const (
	stage6RecoveryBaseSHA    = "375b9b2d0b4c2fce6f2c417850553f79e24a0d92"
	stage6RecoveryRevisionID = "v2-13f81f321f99d1117dc931419e0bea3945ee35a5"
	stage6RecoveryCommandID  = "v2-e028f779a18417e990911057f7db7c666f7487ca"
	stage6RecoveryActionID   = "v2-40f87d048813533daa1108b4316c09139acf0a8f"
)

// Stage6RecoveryFence is the exact stopped post-submit shape. Prompt is
// retained only for the same-identity native reservation and is not exposed by
// the stopped CLI response.
type Stage6RecoveryFence struct {
	TaskID     string `json:"taskId"`
	RevisionID string `json:"revisionId"`
	CommandID  string `json:"commandId"`
	ActionID   string `json:"actionId"`
	BaseSHA    string `json:"baseSha"`
	Prompt     string `json:"-"`
}

// InspectStage6RecoveryFence is read-only. It accepts only the one durable
// identity that crossed the submit boundary before the native shell existed.
func InspectStage6RecoveryFence(ctx context.Context, store *sqlite.Store) (Stage6RecoveryFence, error) {
	if store == nil {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	activation, err := store.GetDCPV2Stage5Activation(ctx)
	if err != nil || validateTwinActivation(activation) != nil {
		return Stage6RecoveryFence{}, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	tasks, err := store.ListDCPV2Tasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].TaskID != TwinCanaryTaskID {
		return Stage6RecoveryFence{}, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	task := tasks[0]
	if task.TargetSpecVersion != TwinTargetSpec || task.Repository != TwinRepository ||
		task.RepositoryID != TwinRepositoryID || task.OwnerID != TwinOwnerID || task.BaseRef != TwinBase ||
		task.Profile != TwinProfile || task.PolicyDigest != domain.DCPWBCIntegrationTwinPolicyDigest() ||
		task.InitialWorkerBudget != 1 || task.RepairBudget != 1 || task.RepairUsed != 0 ||
		task.MaxReadmissions != 2 || task.ReadmissionCount != 0 || task.State != domain.DCPV2TaskWorkerQueued ||
		task.StateRevision != 1 || task.CurrentRevisionID != stage6RecoveryRevisionID || task.TerminalResultID != "" ||
		task.HumanGateQuestion != "" || task.ErrorCode != "" {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	revisions, revisionErr := store.ListDCPV2Revisions(ctx, task.TaskID)
	commands, commandErr := store.ListDCPV2Commands(ctx, task.TaskID)
	actions, actionErr := store.ListDCPV2Actions(ctx, task.TaskID)
	admissions, admissionErr := store.ListDCPV2Admissions(ctx, task.TaskID)
	results, resultErr := store.ListDCPV2Results(ctx, task.TaskID)
	incidents, incidentErr := store.ListDCPV2Incidents(ctx, task.TaskID)
	events, eventErr := store.ListDCPV2ExternalEvents(ctx, task.TaskID)
	if err := errors.Join(revisionErr, commandErr, actionErr, admissionErr, resultErr, incidentErr, eventErr); err != nil {
		return Stage6RecoveryFence{}, err
	}
	if len(revisions) != 1 || len(commands) != 1 || len(actions) != 1 || len(admissions) != 0 ||
		len(results) != 0 || len(incidents) != 0 || len(events) != 0 {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	revision, command, action := revisions[0], commands[0], actions[0]
	if revision.RevisionID != stage6RecoveryRevisionID || revision.TaskID != task.TaskID || revision.Sequence != 1 ||
		revision.Kind != domain.DCPV2RevisionWorkInput || revision.Repository != TwinRepository || revision.BaseRef != TwinBase ||
		revision.BaseSHA != stage6RecoveryBaseSHA || revision.HeadRef != TwinBase || revision.HeadSHA != stage6RecoveryBaseSHA ||
		revision.PredecessorRevisionID != "" || revision.CauseCommandID != "" || revision.PRNumber != 0 ||
		revision.EvidenceDigest != digestCanonical(map[string]string{"main": stage6RecoveryBaseSHA}) {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	var payload struct {
		BaseSHA string `json:"baseSha"`
		Prompt  string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(command.PayloadJSON), &payload); err != nil || strings.TrimSpace(payload.Prompt) == "" ||
		payload.BaseSHA != stage6RecoveryBaseSHA {
		return Stage6RecoveryFence{}, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	canonicalPayload, _ := json.Marshal(map[string]string{"baseSha": payload.BaseSHA, "prompt": payload.Prompt})
	if command.PayloadJSON != string(canonicalPayload) {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	requestDigest := digestCanonical(map[string]string{"taskId": task.TaskID, "prompt": payload.Prompt})
	scopeDigest := digestCanonical(map[string]any{"repository": TwinRepository, "repositoryId": TwinRepositoryID,
		"ownerId": TwinOwnerID, "base": TwinBase, "profile": TwinProfile})
	expectedCommand := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandWorkerExecute, 1,
		map[string]string{"prompt": payload.Prompt, "baseSha": payload.BaseSHA}, requestDigest, task.CreatedAt)
	expectedAction := newAction(expectedCommand, domain.DCPV2ActionWorker, requestDigest, task.CreatedAt)
	if task.RequestDigest != requestDigest || task.ScopeDigest != scopeDigest ||
		command.CommandID != stage6RecoveryCommandID || command.CommandID != expectedCommand.CommandID ||
		command.TaskID != task.TaskID || command.RevisionID != revision.RevisionID || command.Kind != domain.DCPV2CommandWorkerExecute ||
		command.PayloadDigest != expectedCommand.PayloadDigest || command.PrerequisiteDigest != requestDigest ||
		command.IdempotencyKey != expectedCommand.IdempotencyKey || command.Status != domain.DCPV2CommandLeased ||
		command.LeaseOwner != "dcp-v2-daemon" || command.LeaseEpoch == "" || command.LeaseToken == "" ||
		command.EffectFence != "model:"+stage6RecoveryActionID || command.RecoveryGeneration != 0 ||
		command.ResultDigest != "" || command.ErrorCode != "" || action.ActionID != stage6RecoveryActionID ||
		action.ActionID != expectedAction.ActionID || action.CommandID != command.CommandID || action.TaskID != task.TaskID ||
		action.RevisionID != revision.RevisionID || action.Role != domain.DCPV2ActionWorker || action.Model != expectedAction.Model ||
		action.Reasoning != expectedAction.Reasoning || action.TokenBudget != expectedAction.TokenBudget ||
		action.TimeBudgetSec != expectedAction.TimeBudgetSec || action.InputDigest != requestDigest || action.Attempt != 1 ||
		action.Status != domain.DCPV2ActionLaunching || action.Slot != 1 || action.LaunchFence != command.EffectFence ||
		action.RuntimeID != "" || action.ResultDigest != "" || action.ErrorCode != "" {
		return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
	}
	if native, found, err := store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID); err != nil || found || native.TaskID != "" {
		return Stage6RecoveryFence{}, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	sessions, err := store.ListSessions(ctx, domain.ProjectID(TwinTarget))
	if err != nil || len(sessions) != 0 {
		return Stage6RecoveryFence{}, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	legacyActions, err := store.ListDCPModelActions(ctx)
	if err != nil {
		return Stage6RecoveryFence{}, err
	}
	for _, legacyAction := range legacyActions {
		if legacyAction.TaskID == task.TaskID || legacyAction.SessionID == domain.SessionID(TwinTarget+"-1") {
			return Stage6RecoveryFence{}, sqlitestore.ErrDCPV2ProtocolViolation
		}
	}
	return Stage6RecoveryFence{TaskID: task.TaskID, RevisionID: revision.RevisionID, CommandID: command.CommandID,
		ActionID: action.ActionID, BaseSHA: revision.BaseSHA, Prompt: payload.Prompt}, nil
}

func (s *TwinService) recoverStage6NativeShell(ctx context.Context) (bool, error) {
	if _, found, err := s.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, TwinCanaryTaskID); err != nil {
		return false, err
	} else if found {
		return false, nil
	}
	if _, err := s.store.GetDCPV2Task(ctx, TwinCanaryTaskID); errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	fence, err := InspectStage6RecoveryFence(ctx, s.store)
	if err != nil {
		return false, fmt.Errorf("inspect exact Stage 6 native-shell recovery: %w", err)
	}
	result, err := s.legacy.ProvisionV2RuntimePolicy(ctx, dcptasksvc.PolicySubmitInput{
		TaskID: fence.TaskID, Target: TwinTarget, Profile: TwinProfile, Repository: TwinRepository, Prompt: fence.Prompt,
	})
	if err != nil {
		return false, fmt.Errorf("provision exact Stage 6 native shell: %w", err)
	}
	if result.Duplicate || result.Task.TaskID != fence.TaskID || result.Task.Target != TwinTarget ||
		result.Task.Profile != TwinProfile || result.Task.Repository != TwinRepository || result.Task.Prompt != fence.Prompt ||
		result.Task.SessionID != domain.SessionID(TwinTarget+"-1") || result.Task.CardNumber != 1 ||
		result.Task.SourceBranch != "ao/"+TwinTarget+"-1/root" {
		return false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	return true, nil
}
