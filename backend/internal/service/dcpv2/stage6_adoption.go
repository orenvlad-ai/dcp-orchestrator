package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const (
	stage6RecoveryBaseSHA    = "375b9b2d0b4c2fce6f2c417850553f79e24a0d92"
	stage6RecoveryRevisionID = "v2-13f81f321f99d1117dc931419e0bea3945ee35a5"
	stage6RecoveryCommandID  = "v2-e028f779a18417e990911057f7db7c666f7487ca"
	stage6RecoveryActionID   = "v2-40f87d048813533daa1108b4316c09139acf0a8f"
	stage6RecoveryRuntimeID  = "78535564-a2bc-478c-80b0-207753f2152c"
	stage6NativeActionID     = "dcp-model-dcp-v2-twin-canary-v1-worker-1"
	stage6CanaryCommit       = "bebbf8f617f1a6fa0b9e91698fe710fe0a2bad2c"
	stage6CanaryTree         = "2fda4cae71976fd701bf3a9ccca4031f7afb630d"
	stage6CanaryBranch       = "ao/dcp-wbc-integration-lab-1/root"
	stage6CanaryPrompt       = "Add docs/STAGE6_CANARY.md with the single line Stage 6 DCP v2 canary. Change no other file."
)

type Stage6WorkerOutput struct {
	CommitSHA, TreeSHA, Branch, WorktreePath, WorktreeDigest, OutputDigest string
	RemoteBranchAbsent                                                     bool
	OpenPRCount                                                            int
}

// AdoptStage6Worker consumes only the frozen historical native receipt. Once
// consumed, the immutable DCP adoption row is returned without consulting any
// legacy row again.
func (s *TwinService) AdoptStage6Worker(ctx context.Context) (domain.DCPV2Stage6WorkerAdoption, bool, error) {
	activation, err := s.store.GetDCPV2Stage5Activation(ctx)
	if err != nil || validateTwinActivation(activation) != nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	if existing, err := s.store.GetDCPV2Stage6WorkerAdoption(ctx); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		return domain.DCPV2Stage6WorkerAdoption{}, false, err
	}
	tasks, err := s.store.ListDCPV2Tasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].TaskID != TwinCanaryTaskID {
		return domain.DCPV2Stage6WorkerAdoption{}, false, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	task := tasks[0]
	revisions, revisionErr := s.store.ListDCPV2Revisions(ctx, task.TaskID)
	commands, commandErr := s.store.ListDCPV2Commands(ctx, task.TaskID)
	actions, actionErr := s.store.ListDCPV2Actions(ctx, task.TaskID)
	admissions, admissionErr := s.store.ListDCPV2Admissions(ctx, task.TaskID)
	results, resultErr := s.store.ListDCPV2Results(ctx, task.TaskID)
	incidents, incidentErr := s.store.ListDCPV2Incidents(ctx, task.TaskID)
	events, eventErr := s.store.ListDCPV2ExternalEvents(ctx, task.TaskID)
	if err := errors.Join(revisionErr, commandErr, actionErr, admissionErr, resultErr, incidentErr, eventErr); err != nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, err
	}
	requestDigest := digestCanonical(map[string]string{"taskId": task.TaskID, "prompt": stage6CanaryPrompt})
	scopeDigest := digestCanonical(map[string]any{"repository": TwinRepository, "repositoryId": TwinRepositoryID,
		"ownerId": TwinOwnerID, "base": TwinBase, "profile": TwinProfile})
	if task.TargetSpecVersion != TwinTargetSpec || task.Repository != TwinRepository || task.RepositoryID != TwinRepositoryID ||
		task.OwnerID != TwinOwnerID || task.BaseRef != TwinBase || task.Profile != TwinProfile || task.RequestDigest != requestDigest ||
		task.ScopeDigest != scopeDigest || task.PolicyDigest != domain.DCPWBCIntegrationTwinPolicyDigest() ||
		task.InitialWorkerBudget != 1 || task.RepairBudget != 1 || task.RepairUsed != 0 || task.MaxReadmissions != 2 ||
		task.ReadmissionCount != 0 || task.State != domain.DCPV2TaskWorkerQueued || task.StateRevision != 1 || task.CurrentRevisionID != stage6RecoveryRevisionID ||
		task.TerminalResultID != "" || task.HumanGateQuestion != "" || task.ErrorCode != "" ||
		len(revisions) != 1 || len(commands) != 1 || len(actions) != 1 || len(admissions) != 0 || len(results) != 0 || len(incidents) != 0 || len(events) != 0 {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	revision, command, action := revisions[0], commands[0], actions[0]
	if revision.RevisionID != stage6RecoveryRevisionID || revision.TaskID != task.TaskID || revision.Sequence != 1 ||
		revision.Kind != domain.DCPV2RevisionWorkInput || revision.Repository != TwinRepository || revision.BaseRef != TwinBase ||
		revision.BaseSHA != stage6RecoveryBaseSHA || revision.HeadRef != TwinBase || revision.HeadSHA != stage6RecoveryBaseSHA ||
		revision.PredecessorRevisionID != "" || revision.CauseCommandID != "" || revision.PRNumber != 0 ||
		revision.EvidenceDigest != digestCanonical(map[string]string{"main": stage6RecoveryBaseSHA}) {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	var payload struct {
		BaseSHA string `json:"baseSha"`
		Prompt  string `json:"prompt"`
	}
	if json.Unmarshal([]byte(command.PayloadJSON), &payload) != nil || payload.BaseSHA != stage6RecoveryBaseSHA || payload.Prompt != stage6CanaryPrompt {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	expectedCommand := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandWorkerExecute, 1,
		map[string]string{"prompt": stage6CanaryPrompt, "baseSha": stage6RecoveryBaseSHA}, requestDigest, task.CreatedAt)
	expectedAction := newAction(expectedCommand, domain.DCPV2ActionWorker, requestDigest, task.CreatedAt)
	if command.CommandID != stage6RecoveryCommandID || command.CommandID != expectedCommand.CommandID ||
		command.TaskID != task.TaskID || command.RevisionID != revision.RevisionID || command.Kind != domain.DCPV2CommandWorkerExecute ||
		command.PayloadJSON != expectedCommand.PayloadJSON || command.PayloadDigest != expectedCommand.PayloadDigest ||
		command.PrerequisiteDigest != requestDigest || command.IdempotencyKey != expectedCommand.IdempotencyKey ||
		command.Status != domain.DCPV2CommandLeased || command.LeaseOwner != "dcp-v2-daemon" || command.LeaseEpoch == "" || command.LeaseToken == "" ||
		command.EffectFence != "model:"+stage6RecoveryActionID || action.ActionID != stage6RecoveryActionID ||
		command.RecoveryGeneration != 0 || command.ResultDigest != "" || command.ErrorCode != "" ||
		action.ActionID != expectedAction.ActionID || action.CommandID != command.CommandID || action.TaskID != task.TaskID ||
		action.RevisionID != revision.RevisionID || action.Role != domain.DCPV2ActionWorker || action.Model != expectedAction.Model ||
		action.Reasoning != expectedAction.Reasoning || action.TokenBudget != expectedAction.TokenBudget ||
		action.TimeBudgetSec != expectedAction.TimeBudgetSec || action.InputDigest != requestDigest || action.Attempt != 1 ||
		action.Status != domain.DCPV2ActionRunning || action.Slot != 1 || action.RuntimeID != stage6RecoveryRuntimeID ||
		action.LaunchFence != command.EffectFence || action.ResultDigest != "" || action.ErrorCode != "" {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	native, found, err := s.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if err != nil || !found || native.TaskID != task.TaskID || native.Target != TwinTarget || native.Profile != TwinProfile ||
		native.Repository != TwinRepository || native.SessionID != domain.SessionID(TwinTarget+"-1") || native.CardNumber != 1 ||
		native.SourceBranch != stage6CanaryBranch || native.Prompt != stage6CanaryPrompt || native.State != domain.DCPPolicyCIWaiting || native.Revision != 4 ||
		native.RepairCount != 0 || native.PRNumber != 0 || native.CurrentHeadSHA != "" || native.PreviousHeadSHA != "" || native.ReviewRunID != "" ||
		native.AdmissionID != "" || native.ReleasePhase != "" || native.ErrorCode != "" {
		return domain.DCPV2Stage6WorkerAdoption{}, false, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	session, found, err := s.store.GetSession(ctx, native.SessionID)
	if err != nil || !found || session.ID != native.SessionID || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.Metadata.RuntimeHandleID != string(native.SessionID) || session.Metadata.RuntimeLaunchID != "" ||
		session.Metadata.Branch != native.SourceBranch || session.Metadata.WorkspacePath != native.WorktreePath {
		return domain.DCPV2Stage6WorkerAdoption{}, false, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	legacyActions, err := s.store.ListDCPModelActions(ctx)
	if err != nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, err
	}
	var nativeAction *domain.DCPModelAction
	for i := range legacyActions {
		if legacyActions[i].TaskID == task.TaskID || legacyActions[i].SessionID == native.SessionID {
			if nativeAction != nil {
				return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
			}
			nativeAction = &legacyActions[i]
		}
	}
	if nativeAction == nil || nativeAction.ID != stage6NativeActionID || nativeAction.Sequence != 74 ||
		nativeAction.Kind != domain.DCPActionInitialWorker || nativeAction.ExactHeadSHA != "" || nativeAction.Status != domain.DCPActionSucceeded ||
		nativeAction.Slot != 0 || nativeAction.LaunchID != stage6RecoveryRuntimeID || nativeAction.ReviewRunID != "" || nativeAction.ErrorCode != "" {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	output, err := s.adapter.InspectStage6WorkerOutput(ctx, native.WorktreePath, native.SourceBranch)
	if err != nil || output.CommitSHA != stage6CanaryCommit || output.TreeSHA != stage6CanaryTree || output.Branch != stage6CanaryBranch ||
		output.WorktreePath != native.WorktreePath || len(output.WorktreeDigest) != 64 || len(output.OutputDigest) != 64 ||
		!output.RemoteBranchAbsent || output.OpenPRCount != 0 {
		return domain.DCPV2Stage6WorkerAdoption{}, false, errors.Join(err, sqlitestore.ErrDCPV2ProtocolViolation)
	}
	now := s.now().UTC()
	legacyDigest := digestCanonical(map[string]any{"task": task.TaskID, "revision": revision.RevisionID, "command": command.CommandID,
		"action": action.ActionID, "runtime": action.RuntimeID, "nativeAction": nativeAction.ID, "nativeSequence": nativeAction.Sequence,
		"nativeStatus": nativeAction.Status, "nativeState": native.State, "nativeRevision": native.Revision,
		"commit": output.CommitSHA, "tree": output.TreeSHA, "branch": output.Branch, "worktreeDigest": output.WorktreeDigest,
		"outputDigest": output.OutputDigest, "remoteAbsent": output.RemoteBranchAbsent, "prCount": output.OpenPRCount})
	receiptID := stableID("terminal-receipt", action.ActionID, legacyDigest)
	resultDigest := digestCanonical(map[string]string{"commit": output.CommitSHA, "tree": output.TreeSHA, "output": output.OutputDigest})
	receipt := domain.DCPV2ModelTerminalReceipt{ReceiptID: receiptID, ActionID: action.ActionID, CommandID: command.CommandID,
		TaskID: task.TaskID, RevisionID: revision.RevisionID, RuntimeID: action.RuntimeID, LaunchFence: action.LaunchFence,
		Status: domain.DCPV2ModelTerminalSucceeded, ResultDigest: resultDigest, OutputJSON: `{}`,
		OutputDigest: output.OutputDigest, HeadRef: output.Branch, HeadSHA: output.CommitSHA, TreeSHA: output.TreeSHA,
		BaseSHA: revision.HeadSHA, WorktreePath: output.WorktreePath, WorktreeDigest: output.WorktreeDigest, CreatedAt: now}
	processorOutcome, err := s.processor.ProcessModelTerminal(ctx, command, action, receipt)
	if err != nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, err
	}
	adoption := domain.DCPV2Stage6WorkerAdoption{AdoptionID: "dcp-v2-stage6-worker-adoption-v1", TaskID: task.TaskID,
		RevisionID: revision.RevisionID, CommandID: command.CommandID, ActionID: action.ActionID, RuntimeID: action.RuntimeID,
		NativeActionID: nativeAction.ID, NativeSequence: nativeAction.Sequence, LegacyEvidenceDigest: legacyDigest,
		CommitSHA: output.CommitSHA, TreeSHA: output.TreeSHA, Branch: output.Branch, WorktreeDigest: output.WorktreeDigest,
		OutputDigest: output.OutputDigest, ReceiptID: receiptID, ConsumedAt: now}
	transition := sqlitestore.DCPV2Transition{CommandID: command.CommandID, LeaseOwner: command.LeaseOwner,
		LeaseEpoch: command.LeaseEpoch, LeaseToken: command.LeaseToken, ExpectedTaskState: task.State,
		ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: processorOutcome.NextTaskState, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: resultDigest, NextRevision: processorOutcome.NextRevision, NextCommand: processorOutcome.NextCommand,
		UpdatedAt: now}
	applied, err := s.store.CompleteDCPV2ModelTransition(ctx, sqlitestore.DCPV2DirectTransition{
		Transition: transition, Receipt: receipt, Adoption: &adoption})
	if err != nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, err
	}
	return adoption, applied, nil
}

// AdoptStage6WorkerExact is the stopped, one-shot installer boundary for the
// frozen Stage 6 Worker receipt. It has no model runner and no startup path;
// all provider access in the adapter is exact read-back performed before the
// single atomic DCP-owned adoption transition.
func AdoptStage6WorkerExact(ctx context.Context, store *sqlite.Store, adapter *TwinGitHubAdapter,
	now func() time.Time) (domain.DCPV2Stage6WorkerAdoption, bool, error) {
	if store == nil || adapter == nil {
		return domain.DCPV2Stage6WorkerAdoption{}, false, sqlitestore.ErrDCPV2ProtocolViolation
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	service := &TwinService{store: store, adapter: adapter, now: now}
	service.processor = &twinProcessor{store: store, adapter: adapter, now: now}
	return service.AdoptStage6Worker(ctx)
}
