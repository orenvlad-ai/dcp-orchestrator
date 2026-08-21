package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func (s *TwinService) driveDirectModels(ctx context.Context) error {
	tasks, err := s.store.ListDCPV2Tasks(ctx)
	if err != nil {
		return err
	}
	actions := make([]domain.DCPV2Action, 0)
	for _, task := range tasks {
		rows, err := s.store.ListDCPV2Actions(ctx, task.TaskID)
		if errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
			continue
		}
		if err != nil {
			return err
		}
		actions = append(actions, rows...)
	}
	for _, action := range actions {
		if action.Status != domain.DCPV2ActionQueued {
			continue
		}
		command, err := s.store.GetDCPV2Command(ctx, action.CommandID)
		if err != nil || command.Status != domain.DCPV2CommandLeased || command.EffectFence != "" {
			return errors.Join(err, errors.New("DCP v2 direct model Command is not an unfenced lease"))
		}
		task, err := s.store.GetDCPV2Task(ctx, action.TaskID)
		if err != nil {
			return err
		}
		revision, err := s.currentRevision(ctx, task)
		if err != nil {
			return err
		}
		taskInputJSON, taskInputDigest, err := s.directTaskInput(ctx, task)
		if err != nil {
			return err
		}
		project, found, err := s.store.GetProject(ctx, TwinTarget)
		if err != nil || !found {
			return errors.Join(err, errors.New("DCP v2 direct repository path is unavailable"))
		}
		workspace, err := s.runner.Prepare(ctx, ports.DCPV2ModelPrepareRequest{TaskID: task.TaskID,
			RevisionID: revision.RevisionID, CommandID: command.CommandID, ActionID: action.ActionID, Role: action.Role,
			Repository: task.Repository, RepositoryPath: project.Path, BaseRef: task.BaseRef, BaseSHA: revision.BaseSHA, HeadRef: revision.HeadRef, HeadSHA: revision.HeadSHA})
		if err != nil || workspace.Branch == "" || workspace.Worktree == "" || len(workspace.WorktreeDigest) != 64 {
			return errors.Join(err, errors.New("DCP v2 direct model workspace receipt drifted"))
		}
		runtimeID := stableID("runtime", action.ActionID, "attempt-1")
		claimed, runtime, created, err := s.store.ReserveDCPV2ModelLaunch(ctx, command.CommandID, command.LeaseOwner,
			command.LeaseEpoch, command.LeaseToken, runtimeID, workspace.Worktree, workspace.WorktreeDigest, s.now().UTC())
		if err != nil {
			return err
		}
		if !created {
			if claimed.ActionID == "" { // all three durable slots are occupied
				return nil
			}
			continue
		}
		request := s.directLaunchRequest(task, revision, command, claimed, runtime, workspace, taskInputJSON, taskInputDigest)
		launch, err := s.runner.Launch(ctx, request)
		if err != nil {
			return fmt.Errorf("DCP v2 direct model transport launch: %w", err)
		}
		if launch.ActionID != claimed.ActionID || launch.LaunchFence != runtime.LaunchFence || launch.RuntimeID != runtime.RuntimeID ||
			launch.ProviderRequestID == "" || len(launch.ProviderRequestDigest) != 64 || launch.StartedAt.IsZero() {
			return errors.New("DCP v2 direct model launch receipt drifted")
		}
		if _, err := s.store.StartDCPV2ModelRuntime(ctx, runtime, launch.ProviderRequestID, launch.ProviderRequestDigest, launch.StartedAt); err != nil {
			if _, receiptErr := s.store.GetDCPV2ModelTerminalReceiptByAction(ctx, action.ActionID); receiptErr == nil {
				continue
			}
			return err
		}
		// Runtime.Create can return after a bounded child has already written its
		// terminal artifact. Consume that durable fact only after the launch
		// receipt is committed, so the reserved fence never masquerades as a
		// provider start.
		terminal, found, err := s.runner.Terminal(ctx, request)
		if err != nil {
			return err
		}
		if found {
			if terminal.ActionID != claimed.ActionID || terminal.RuntimeID != runtime.RuntimeID || terminal.LaunchFence != runtime.LaunchFence {
				return errors.New("DCP v2 immediate terminal receipt drifted")
			}
			if _, err := s.CompleteDirectModel(ctx, terminal); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *TwinService) directLaunchRequest(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command,
	action domain.DCPV2Action, runtime domain.DCPV2ModelRuntime, workspace ports.DCPV2ModelWorkspaceReceipt,
	taskInputJSON, taskInputDigest string) ports.DCPV2ModelLaunchRequest {
	return ports.DCPV2ModelLaunchRequest{TaskID: task.TaskID, RevisionID: revision.RevisionID, CommandID: command.CommandID,
		ActionID: action.ActionID, Role: action.Role, Attempt: action.Attempt, Model: action.Model, Reasoning: action.Reasoning,
		TokenBudget: action.TokenBudget, TimeBudgetSec: action.TimeBudgetSec, InputDigest: action.InputDigest,
		PromptDigest: command.PayloadDigest, TaskInputJSON: taskInputJSON, TaskInputDigest: taskInputDigest,
		CommandPayloadJSON: command.PayloadJSON, Repository: task.Repository, BaseRef: task.BaseRef, BaseSHA: revision.BaseSHA,
		HeadRef: revision.HeadRef, HeadSHA: revision.HeadSHA, Branch: workspace.Branch, Worktree: runtime.WorktreePath,
		WorktreeDigest: runtime.WorktreeDigest, LaunchFence: runtime.LaunchFence, EffectFence: command.EffectFence,
		RuntimeID: runtime.RuntimeID, ExpectedOldHead: workspace.ExpectedOldHead}
}

func (s *TwinService) directRequestForRuntime(ctx context.Context, runtime domain.DCPV2ModelRuntime) (ports.DCPV2ModelLaunchRequest, error) {
	action, err := s.store.GetDCPV2ActionByCommand(ctx, runtime.CommandID)
	if err != nil || action.ActionID != runtime.ActionID {
		return ports.DCPV2ModelLaunchRequest{}, errors.Join(err, errors.New("DCP v2 runtime Action drifted"))
	}
	command, err := s.store.GetDCPV2Command(ctx, runtime.CommandID)
	if err != nil || command.EffectFence != runtime.LaunchFence {
		return ports.DCPV2ModelLaunchRequest{}, errors.Join(err, errors.New("DCP v2 runtime Command drifted"))
	}
	task, err := s.store.GetDCPV2Task(ctx, runtime.TaskID)
	if err != nil {
		return ports.DCPV2ModelLaunchRequest{}, err
	}
	taskInputJSON, taskInputDigest, err := s.directTaskInput(ctx, task)
	if err != nil {
		return ports.DCPV2ModelLaunchRequest{}, err
	}
	revision, err := s.currentRevision(ctx, task)
	if err != nil || revision.RevisionID != runtime.RevisionID {
		return ports.DCPV2ModelLaunchRequest{}, errors.Join(err, errors.New("DCP v2 runtime Revision drifted"))
	}
	branch, expectedOldHead := revision.HeadRef, revision.HeadSHA
	if action.Role == domain.DCPV2ActionWorker {
		branch, expectedOldHead = directWorkerBranch(task.TaskID), ""
	}
	return s.directLaunchRequest(task, revision, command, action, runtime, ports.DCPV2ModelWorkspaceReceipt{
		Branch: branch, Worktree: runtime.WorktreePath, WorktreeDigest: runtime.WorktreeDigest,
		ExpectedOldHead: expectedOldHead}, taskInputJSON, taskInputDigest), nil
}

func (s *TwinService) directTaskInput(ctx context.Context, task domain.DCPV2Task) (string, string, error) {
	commands, err := s.store.ListDCPV2Commands(ctx, task.TaskID)
	if err != nil {
		return "", "", err
	}
	var initial *domain.DCPV2Command
	for i := range commands {
		if commands[i].Kind != domain.DCPV2CommandWorkerExecute {
			continue
		}
		if initial != nil {
			return "", "", errors.New("DCP v2 initial Worker input cardinality drifted")
		}
		initial = &commands[i]
	}
	if initial == nil || initial.PayloadDigest != digestCanonical(json.RawMessage(initial.PayloadJSON)) {
		return "", "", errors.New("DCP v2 initial Worker input digest drifted")
	}
	var input struct {
		BaseSHA string `json:"baseSha"`
		Prompt  string `json:"prompt"`
	}
	if decodeExactDirectJSON([]byte(initial.PayloadJSON), &input) != nil || !validV2SHA(input.BaseSHA) ||
		strings.TrimSpace(input.Prompt) == "" || strings.ContainsAny(input.Prompt, "\x00\r\n") || len(input.Prompt) > 512 ||
		task.RequestDigest != digestCanonical(map[string]string{"taskId": task.TaskID, "prompt": input.Prompt}) {
		return "", "", errors.New("DCP v2 immutable Task input drifted")
	}
	return initial.PayloadJSON, initial.PayloadDigest, nil
}

func (s *TwinService) reconcileDirectModels(ctx context.Context) error {
	runtimes, err := s.store.ListActiveDCPV2ModelRuntimes(ctx)
	if err != nil {
		return err
	}
	activeActions := map[string]domain.DCPV2Action{}
	tasks, err := s.store.ListDCPV2Tasks(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		actions, err := s.store.ListDCPV2Actions(ctx, task.TaskID)
		if err != nil {
			return err
		}
		for _, action := range actions {
			switch action.Status {
			case domain.DCPV2ActionQueued, domain.DCPV2ActionLaunching, domain.DCPV2ActionRunning:
				activeActions[action.ActionID] = action
			}
		}
	}
	runtimeByAction := make(map[string]domain.DCPV2ModelRuntime, len(runtimes))
	for _, runtime := range runtimes {
		if _, duplicate := runtimeByAction[runtime.ActionID]; duplicate {
			return errors.New("DCP v2 direct Action has multiple active runtimes")
		}
		runtimeByAction[runtime.ActionID] = runtime
	}
	for actionID, action := range activeActions {
		runtime, found := runtimeByAction[actionID]
		if action.Status == domain.DCPV2ActionQueued {
			if found {
				return errors.New("DCP v2 queued Action already has an active runtime")
			}
			continue
		}
		if !found || runtime.TaskID != action.TaskID || runtime.RevisionID != action.RevisionID ||
			runtime.CommandID != action.CommandID || runtime.Slot != action.Slot || runtime.LaunchFence != action.LaunchFence ||
			(action.Status == domain.DCPV2ActionLaunching && (action.RuntimeID != "" || runtime.State != domain.DCPV2ModelRuntimeReserved)) ||
			(action.Status == domain.DCPV2ActionRunning && (action.RuntimeID != runtime.RuntimeID || runtime.State != domain.DCPV2ModelRuntimeRunning)) {
			return errors.New("DCP v2 active Action runtime identity is incomplete at startup")
		}
	}
	for _, runtime := range runtimes {
		if _, found := activeActions[runtime.ActionID]; !found {
			return errors.New("DCP v2 active runtime has no active Action at startup")
		}
	}
	for _, runtime := range runtimes {
		request, err := s.directRequestForRuntime(ctx, runtime)
		if err != nil {
			return err
		}
		observation, err := s.runner.Observe(ctx, request)
		if err != nil || observation.ActionID != runtime.ActionID || observation.RuntimeID != runtime.RuntimeID {
			return errors.Join(err, errors.New("DCP v2 direct runtime observation drifted"))
		}
		if observation.Alive {
			if runtime.State != domain.DCPV2ModelRuntimeRunning {
				return errors.New("DCP v2 live runtime is not durably running")
			}
			continue
		}
		receipt, found, err := s.runner.Terminal(ctx, request)
		if err != nil || !found {
			return errors.Join(err, errors.New("DCP v2 active Action has neither a live runtime nor terminal receipt"))
		}
		if _, err := s.CompleteDirectModel(ctx, receipt); err != nil {
			return err
		}
	}
	return nil
}

func (s *TwinService) CompleteDirectModel(ctx context.Context, receipt domain.DCPV2ModelTerminalReceipt) (bool, error) {
	if existing, err := s.store.GetDCPV2ModelTerminalReceiptByAction(ctx, receipt.ActionID); err == nil {
		if reflect.DeepEqual(existing, receipt) {
			return false, nil
		}
		return false, sqlitestore.ErrDCPV2IdentityConflict
	} else if !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		return false, err
	}
	action, err := s.store.GetDCPV2ActionByCommand(ctx, receipt.CommandID)
	if err != nil {
		return false, err
	}
	command, err := s.store.GetDCPV2Command(ctx, receipt.CommandID)
	if err != nil {
		return false, err
	}
	task, err := s.store.GetDCPV2Task(ctx, receipt.TaskID)
	if err != nil {
		return false, err
	}
	var outcome Outcome
	if receipt.Status == domain.DCPV2ModelTerminalSucceeded {
		outcome, err = s.processor.ProcessModelTerminal(ctx, command, action, receipt)
	} else {
		outcome = Outcome{NextTaskState: domain.DCPV2TaskFailed, TaskErrorCode: receipt.ErrorCode,
			CommandResultDigest: receipt.ResultDigest}
	}
	if err != nil {
		return false, err
	}
	repairUsed := task.RepairUsed
	if outcome.RepairIncrement {
		repairUsed++
	}
	transition := sqlitestore.DCPV2Transition{CommandID: command.CommandID, LeaseOwner: command.LeaseOwner,
		LeaseEpoch: command.LeaseEpoch, LeaseToken: command.LeaseToken, ExpectedTaskState: task.State,
		ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: outcome.NextTaskState, RepairUsed: repairUsed, ReadmissionCount: task.ReadmissionCount,
		TaskErrorCode: outcome.TaskErrorCode, CommandResultDigest: outcome.CommandResultDigest,
		CommandErrorCode: receipt.ErrorCode, NextRevision: outcome.NextRevision, NextCommand: outcome.NextCommand,
		NextAction: outcome.NextAction, Incident: outcome.Incident, UpdatedAt: s.now().UTC()}
	applied, err := s.store.CompleteDCPV2ModelTransition(ctx, sqlitestore.DCPV2DirectTransition{Transition: transition, Receipt: receipt})
	if err != nil || !applied {
		return applied, err
	}
	if err := s.engine.Drain(ctx); err != nil && !errors.Is(err, errPauseDrain) {
		return true, err
	}
	if err := s.driveDirectModels(ctx); err != nil {
		return true, err
	}
	return true, nil
}

// ReportDirectProcessExit consumes one supervisor artifact by its exact DCP
// Action identity. The supervisor supplies no Task transition data; the
// service reconstructs every binding from DCP-owned durable state.
func (s *TwinService) ReportDirectProcessExit(ctx context.Context, actionID string) error {
	if actionID == "" {
		return errors.New("DCP v2 process-exit Action is required")
	}
	if _, err := s.store.GetDCPV2ModelTerminalReceiptByAction(ctx, actionID); err == nil {
		return nil
	} else if !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		return err
	}
	runtime, err := s.store.GetDCPV2ModelRuntimeByAction(ctx, actionID)
	if err != nil || runtime.ActionID != actionID {
		return errors.Join(err, errors.New("DCP v2 process-exit runtime identity drifted"))
	}
	request, err := s.directRequestForRuntime(ctx, runtime)
	if err != nil {
		return err
	}
	receipt, found, err := s.runner.Terminal(ctx, request)
	if err != nil || !found || receipt.ActionID != actionID {
		return errors.Join(err, errors.New("DCP v2 process-exit terminal receipt is unavailable"))
	}
	_, err = s.CompleteDirectModel(ctx, receipt)
	return err
}

func (s *TwinService) currentRevision(ctx context.Context, task domain.DCPV2Task) (domain.DCPV2Revision, error) {
	revisions, err := s.store.ListDCPV2Revisions(ctx, task.TaskID)
	if err != nil {
		return domain.DCPV2Revision{}, err
	}
	for _, revision := range revisions {
		if revision.RevisionID == task.CurrentRevisionID {
			return revision, nil
		}
	}
	return domain.DCPV2Revision{}, errors.New("DCP v2 current Revision is unavailable")
}
