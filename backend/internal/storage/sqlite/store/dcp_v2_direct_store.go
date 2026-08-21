package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

type DCPV2DirectTransition struct {
	Transition DCPV2Transition
	Receipt    domain.DCPV2ModelTerminalReceipt
	Adoption   *domain.DCPV2Stage6WorkerAdoption
}

// ReserveDCPV2ModelLaunch atomically fences the leased Command, assigns the
// lowest free global slot, moves its Action to launching, and reserves the
// DCP-owned runtime identity before transport is called.
func (s *Store) ReserveDCPV2ModelLaunch(ctx context.Context, commandID, owner, epoch, token, runtimeID, worktreePath, worktreeDigest string, at time.Time) (domain.DCPV2Action, domain.DCPV2ModelRuntime, bool, error) {
	if commandID == "" || owner == "" || epoch == "" || token == "" || runtimeID == "" || worktreePath == "" || len(worktreeDigest) != 64 || at.IsZero() {
		return domain.DCPV2Action{}, domain.DCPV2ModelRuntime{}, false, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var action domain.DCPV2Action
	var runtime domain.DCPV2ModelRuntime
	created := false
	err := s.inTx(ctx, "reserve dcp v2 direct model launch", func(q *gen.Queries) error {
		commandRow, err := q.GetDCPV2Command(ctx, commandID)
		if err != nil {
			return err
		}
		actionRow, err := q.GetDCPV2ActionByCommand(ctx, commandID)
		if err != nil {
			return err
		}
		if existing, getErr := q.GetDCPV2ModelRuntimeByAction(ctx, actionRow.ActionID); getErr == nil {
			if existing.RuntimeID != runtimeID || existing.CommandID != commandID || existing.LaunchFence != commandRow.EffectFence ||
				commandRow.EffectFence == "" || actionRow.LaunchFence != existing.LaunchFence {
				return ErrDCPV2IdentityConflict
			}
			action, runtime = dcpV2ActionFromGen(actionRow), dcpV2ModelRuntimeFromGen(existing)
			return nil
		} else if !errors.Is(getErr, sql.ErrNoRows) {
			return getErr
		}
		if commandRow.Status != string(domain.DCPV2CommandLeased) || commandRow.LeaseOwner != owner ||
			commandRow.LeaseEpoch != epoch || commandRow.LeaseToken != token || commandRow.EffectFence != "" ||
			actionRow.Status != string(domain.DCPV2ActionQueued) || actionRow.Slot != 0 || actionRow.LaunchFence != "" ||
			actionRow.TaskID != commandRow.TaskID || actionRow.RevisionID != commandRow.RevisionID {
			return ErrDCPV2StaleTransition
		}
		active, err := q.ListActiveDCPV2Actions(ctx)
		if err != nil {
			return err
		}
		used := map[int64]bool{}
		for _, candidate := range active {
			used[candidate.Slot] = true
		}
		var slot int64
		for candidate := int64(1); candidate <= 3; candidate++ {
			if !used[candidate] {
				slot = candidate
				break
			}
		}
		if slot == 0 {
			return nil
		}
		fence := "model:" + actionRow.ActionID + ":" + runtimeID
		rows, err := q.FenceDCPV2CommandEffect(ctx, gen.FenceDCPV2CommandEffectParams{
			EffectFence: fence, UpdatedAt: at, CommandID: commandID, LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token,
		})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2LeaseLost)
		}
		rows, err = q.LaunchDCPV2Action(ctx, gen.LaunchDCPV2ActionParams{Slot: slot, LaunchFence: fence, UpdatedAt: at, ActionID: actionRow.ActionID})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2LeaseLost)
		}
		runtime = domain.DCPV2ModelRuntime{RuntimeID: runtimeID, ActionID: actionRow.ActionID, CommandID: commandID,
			TaskID: actionRow.TaskID, RevisionID: actionRow.RevisionID, Slot: slot, LaunchFence: fence,
			WorktreePath: worktreePath, WorktreeDigest: worktreeDigest,
			State: domain.DCPV2ModelRuntimeReserved, CreatedAt: at, UpdatedAt: at}
		if err := q.InsertDCPV2ModelRuntime(ctx, dcpV2ModelRuntimeParams(runtime)); err != nil {
			return err
		}
		actionRow.Status, actionRow.Slot, actionRow.LaunchFence, actionRow.UpdatedAt = string(domain.DCPV2ActionLaunching), slot, fence, at
		action = dcpV2ActionFromGen(actionRow)
		created = true
		return nil
	})
	return action, runtime, created, err
}

func (s *Store) StartDCPV2ModelRuntime(ctx context.Context, receipt domain.DCPV2ModelRuntime, providerID, providerDigest string, at time.Time) (bool, error) {
	if receipt.RuntimeID == "" || receipt.ActionID == "" || receipt.LaunchFence == "" || providerID == "" || len(providerDigest) != 64 || at.IsZero() {
		return false, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	started := false
	err := s.inTx(ctx, "start dcp v2 direct model runtime", func(q *gen.Queries) error {
		current, err := q.GetDCPV2ModelRuntime(ctx, receipt.RuntimeID)
		if err != nil {
			return err
		}
		if current.ActionID != receipt.ActionID || current.LaunchFence != receipt.LaunchFence {
			return ErrDCPV2IdentityConflict
		}
		if current.State == string(domain.DCPV2ModelRuntimeRunning) {
			if current.ProviderRequestID == providerID && current.ProviderRequestDigest == providerDigest {
				return nil
			}
			return ErrDCPV2IdentityConflict
		}
		rows, err := q.StartDCPV2ModelRuntime(ctx, gen.StartDCPV2ModelRuntimeParams{ProviderRequestID: providerID,
			ProviderRequestDigest: providerDigest, UpdatedAt: at, RuntimeID: receipt.RuntimeID,
			ActionID: receipt.ActionID, LaunchFence: receipt.LaunchFence})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		rows, err = q.StartDCPV2Action(ctx, gen.StartDCPV2ActionParams{RuntimeID: receipt.RuntimeID, UpdatedAt: at,
			ActionID: receipt.ActionID, Slot: receipt.Slot, LaunchFence: receipt.LaunchFence})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		started = true
		return nil
	})
	return started, err
}

func (s *Store) GetDCPV2ModelRuntimeByAction(ctx context.Context, actionID string) (domain.DCPV2ModelRuntime, error) {
	row, err := s.qr.GetDCPV2ModelRuntimeByAction(ctx, actionID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2ModelRuntime{}, ErrDCPV2NotFound
	}
	return dcpV2ModelRuntimeFromGen(row), err
}

func (s *Store) ListActiveDCPV2ModelRuntimes(ctx context.Context) ([]domain.DCPV2ModelRuntime, error) {
	rows, err := s.qr.ListActiveDCPV2ModelRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2ModelRuntime, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2ModelRuntimeFromGen(row))
	}
	return out, nil
}

func (s *Store) GetDCPV2ModelTerminalReceiptByAction(ctx context.Context, actionID string) (domain.DCPV2ModelTerminalReceipt, error) {
	row, err := s.qr.GetDCPV2ModelTerminalReceiptByAction(ctx, actionID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2ModelTerminalReceipt{}, ErrDCPV2NotFound
	}
	return dcpV2ModelTerminalReceiptFromGen(row), err
}

func (s *Store) GetDCPV2Stage6WorkerAdoption(ctx context.Context) (domain.DCPV2Stage6WorkerAdoption, error) {
	row, err := s.qr.GetDCPV2Stage6WorkerAdoption(ctx, "dcp-v2-stage6-worker-adoption-v1")
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2Stage6WorkerAdoption{}, ErrDCPV2NotFound
	}
	return dcpV2Stage6WorkerAdoptionFromGen(row), err
}

// CompleteDCPV2ModelTransition commits the terminal receipt, slot release,
// Action/Command completion, successor Revision, Task pointer and next Command
// as one transaction. Equal replay is inert; crossed evidence is rejected.
func (s *Store) CompleteDCPV2ModelTransition(ctx context.Context, direct DCPV2DirectTransition) (bool, error) {
	tr, receipt := direct.Transition, direct.Receipt
	if tr.UpdatedAt.IsZero() || receipt.CreatedAt.IsZero() || !json.Valid([]byte(receipt.OutputJSON)) || len(receipt.OutputDigest) != 64 {
		return false, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "complete dcp v2 direct model", func(q *gen.Queries) error {
		if existing, err := q.GetDCPV2ModelTerminalReceiptByAction(ctx, receipt.ActionID); err == nil {
			if !reflect.DeepEqual(dcpV2ModelTerminalReceiptFromGen(existing), receipt) {
				return ErrDCPV2IdentityConflict
			}
			if direct.Adoption != nil {
				row, getErr := q.GetDCPV2Stage6WorkerAdoption(ctx, direct.Adoption.AdoptionID)
				if getErr != nil || !reflect.DeepEqual(dcpV2Stage6WorkerAdoptionFromGen(row), *direct.Adoption) {
					return ErrDCPV2IdentityConflict
				}
			}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		commandRow, err := q.GetDCPV2Command(ctx, tr.CommandID)
		if err != nil || commandRow.Status != string(domain.DCPV2CommandLeased) || commandRow.LeaseOwner != tr.LeaseOwner ||
			commandRow.LeaseEpoch != tr.LeaseEpoch || commandRow.LeaseToken != tr.LeaseToken {
			return errors.Join(err, ErrDCPV2LeaseLost)
		}
		taskRow, err := q.GetDCPV2Task(ctx, commandRow.TaskID)
		if err != nil {
			return err
		}
		task := dcpV2TaskFromGen(taskRow)
		currentRevisionRow, err := q.GetDCPV2Revision(ctx, task.CurrentRevisionID)
		if err != nil || task.State != tr.ExpectedTaskState || task.StateRevision != tr.ExpectedStateRevision ||
			task.CurrentRevisionID != tr.ExpectedRevisionID || commandRow.RevisionID != tr.ExpectedRevisionID {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		if err := validateDCPV2Transition(task, dcpV2RevisionFromGen(currentRevisionRow), dcpV2CommandFromGen(commandRow), tr); err != nil {
			return err
		}
		actionRow, err := q.GetDCPV2ActionByCommand(ctx, commandRow.CommandID)
		if err != nil || actionRow.ActionID != receipt.ActionID || actionRow.TaskID != receipt.TaskID ||
			actionRow.RevisionID != receipt.RevisionID || actionRow.CommandID != receipt.CommandID ||
			actionRow.LaunchFence != receipt.LaunchFence ||
			commandRow.EffectFence != receipt.LaunchFence || (actionRow.Status != string(domain.DCPV2ActionLaunching) && actionRow.Status != string(domain.DCPV2ActionRunning)) {
			return errors.Join(err, ErrDCPV2IdentityConflict)
		}
		runtimeRow, runtimeErr := q.GetDCPV2ModelRuntime(ctx, receipt.RuntimeID)
		if errors.Is(runtimeErr, sql.ErrNoRows) && direct.Adoption != nil {
			runtime := domain.DCPV2ModelRuntime{RuntimeID: receipt.RuntimeID, ActionID: receipt.ActionID, CommandID: receipt.CommandID,
				TaskID: receipt.TaskID, RevisionID: receipt.RevisionID, Slot: actionRow.Slot, LaunchFence: receipt.LaunchFence,
				ProviderRequestID: direct.Adoption.NativeActionID, ProviderRequestDigest: direct.Adoption.LegacyEvidenceDigest,
				WorktreePath: receipt.WorktreePath, WorktreeDigest: receipt.WorktreeDigest,
				State: domain.DCPV2ModelRuntimeRunning, CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.CreatedAt}
			if err := q.InsertDCPV2ModelRuntime(ctx, dcpV2ModelRuntimeParams(runtime)); err != nil {
				return err
			}
			runtimeRow, runtimeErr = q.GetDCPV2ModelRuntime(ctx, receipt.RuntimeID)
		}
		if runtimeErr != nil || runtimeRow.ActionID != receipt.ActionID || runtimeRow.CommandID != receipt.CommandID ||
			runtimeRow.LaunchFence != receipt.LaunchFence || runtimeRow.State != string(domain.DCPV2ModelRuntimeRunning) {
			return errors.Join(runtimeErr, ErrDCPV2IdentityConflict)
		}
		if actionRow.Status != string(domain.DCPV2ActionRunning) || actionRow.RuntimeID != receipt.RuntimeID {
			return ErrDCPV2IdentityConflict
		}
		if (receipt.Status == domain.DCPV2ModelTerminalSucceeded) != (tr.CommandErrorCode == "") ||
			receipt.ResultDigest != tr.CommandResultDigest || (receipt.Status == domain.DCPV2ModelTerminalSucceeded && len(receipt.ResultDigest) != 64) ||
			(receipt.Status == domain.DCPV2ModelTerminalFailed && (receipt.ErrorCode == "" || receipt.ResultDigest != "")) {
			return ErrDCPV2ProtocolViolation
		}
		if err := q.InsertDCPV2ModelTerminalReceipt(ctx, dcpV2ModelTerminalReceiptParams(receipt)); err != nil {
			return err
		}
		runtimeState := string(domain.DCPV2ModelRuntimeFailed)
		actionStatus := string(domain.DCPV2ActionFailed)
		if receipt.Status == domain.DCPV2ModelTerminalSucceeded {
			runtimeState, actionStatus = string(domain.DCPV2ModelRuntimeSucceeded), string(domain.DCPV2ActionSucceeded)
		}
		rows, err := q.FinishDCPV2ModelRuntime(ctx, gen.FinishDCPV2ModelRuntimeParams{State: runtimeState, UpdatedAt: tr.UpdatedAt,
			RuntimeID: receipt.RuntimeID, ActionID: receipt.ActionID, LaunchFence: receipt.LaunchFence})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		rows, err = q.FinishDCPV2Action(ctx, gen.FinishDCPV2ActionParams{Status: actionStatus, RuntimeID: receipt.RuntimeID, ResultDigest: receipt.ResultDigest,
			ErrorCode: receipt.ErrorCode, UpdatedAt: tr.UpdatedAt, ActionID: receipt.ActionID, Slot: actionRow.Slot, LaunchFence: receipt.LaunchFence})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		commandStatus := string(domain.DCPV2CommandSucceeded)
		if tr.CommandErrorCode != "" {
			commandStatus = string(domain.DCPV2CommandFailed)
		}
		rows, err = q.FinishDCPV2Command(ctx, gen.FinishDCPV2CommandParams{Status: commandStatus,
			ResultDigest: tr.CommandResultDigest, ErrorCode: tr.CommandErrorCode, UpdatedAt: tr.UpdatedAt,
			CommandID: tr.CommandID, LeaseOwner: tr.LeaseOwner, LeaseEpoch: tr.LeaseEpoch, LeaseToken: tr.LeaseToken})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2LeaseLost)
		}
		nextRevisionID := task.CurrentRevisionID
		if tr.NextRevision != nil {
			if err := q.InsertDCPV2Revision(ctx, dcpV2RevisionParams(*tr.NextRevision)); err != nil {
				return err
			}
			nextRevisionID = tr.NextRevision.RevisionID
		}
		if tr.Incident != nil {
			if err := q.InsertDCPV2Incident(ctx, dcpV2IncidentParams(*tr.Incident)); err != nil {
				return err
			}
		}
		if rows, err = q.UpdateDCPV2TaskCAS(ctx, gen.UpdateDCPV2TaskCASParams{RepairUsed: tr.RepairUsed,
			ReadmissionCount: tr.ReadmissionCount, CurrentRevisionID: nextRevisionID, State: string(tr.NextTaskState),
			TerminalResultID: tr.TerminalResultID, HumanGateQuestion: tr.HumanGateQuestion, ErrorCode: tr.TaskErrorCode,
			UpdatedAt: tr.UpdatedAt, TaskID: task.TaskID, StateRevision: task.StateRevision,
			CurrentRevisionID_2: task.CurrentRevisionID, State_2: string(task.State)}); err != nil || rows != 1 {
			return errors.Join(err, ErrDCPV2StaleTransition)
		}
		if tr.NextCommand != nil {
			if err := q.InsertDCPV2Command(ctx, dcpV2CommandParams(*tr.NextCommand)); err != nil {
				return err
			}
			if tr.NextAction != nil {
				if err := q.InsertDCPV2Action(ctx, dcpV2ActionParams(*tr.NextAction)); err != nil {
					return err
				}
			}
		}
		if direct.Adoption != nil {
			if direct.Adoption.ReceiptID != receipt.ReceiptID || direct.Adoption.TaskID != receipt.TaskID ||
				direct.Adoption.RevisionID != receipt.RevisionID || direct.Adoption.CommandID != receipt.CommandID ||
				direct.Adoption.ActionID != receipt.ActionID || direct.Adoption.RuntimeID != receipt.RuntimeID {
				return ErrDCPV2IdentityConflict
			}
			if err := q.InsertDCPV2Stage6WorkerAdoption(ctx, dcpV2Stage6WorkerAdoptionParams(*direct.Adoption)); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	return created, err
}

func dcpV2ModelRuntimeParams(v domain.DCPV2ModelRuntime) gen.InsertDCPV2ModelRuntimeParams {
	return gen.InsertDCPV2ModelRuntimeParams{RuntimeID: v.RuntimeID, ActionID: v.ActionID, CommandID: v.CommandID,
		TaskID: v.TaskID, RevisionID: v.RevisionID, Slot: v.Slot, LaunchFence: v.LaunchFence,
		ProviderRequestID: v.ProviderRequestID, ProviderRequestDigest: v.ProviderRequestDigest, State: string(v.State),
		WorktreePath: v.WorktreePath, WorktreeDigest: v.WorktreeDigest,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func dcpV2ModelRuntimeFromGen(v gen.DcpV2ModelRuntime) domain.DCPV2ModelRuntime {
	return domain.DCPV2ModelRuntime{RuntimeID: v.RuntimeID, ActionID: v.ActionID, CommandID: v.CommandID,
		TaskID: v.TaskID, RevisionID: v.RevisionID, Slot: v.Slot, LaunchFence: v.LaunchFence,
		ProviderRequestID: v.ProviderRequestID, ProviderRequestDigest: v.ProviderRequestDigest,
		WorktreePath: v.WorktreePath, WorktreeDigest: v.WorktreeDigest,
		State: domain.DCPV2ModelRuntimeState(v.State), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

func dcpV2ModelTerminalReceiptParams(v domain.DCPV2ModelTerminalReceipt) gen.InsertDCPV2ModelTerminalReceiptParams {
	return gen.InsertDCPV2ModelTerminalReceiptParams{ReceiptID: v.ReceiptID, ActionID: v.ActionID, CommandID: v.CommandID,
		TaskID: v.TaskID, RevisionID: v.RevisionID, RuntimeID: v.RuntimeID, LaunchFence: v.LaunchFence,
		Status: string(v.Status), ResultDigest: v.ResultDigest, ErrorCode: v.ErrorCode, OutputJson: v.OutputJSON,
		OutputDigest: v.OutputDigest, HeadRef: v.HeadRef, HeadSha: v.HeadSHA, TreeSha: v.TreeSHA,
		BaseSha: v.BaseSHA, WorktreePath: v.WorktreePath, WorktreeDigest: v.WorktreeDigest, CreatedAt: v.CreatedAt}
}

func dcpV2ModelTerminalReceiptFromGen(v gen.DcpV2ModelTerminalReceipt) domain.DCPV2ModelTerminalReceipt {
	return domain.DCPV2ModelTerminalReceipt{ReceiptID: v.ReceiptID, ActionID: v.ActionID, CommandID: v.CommandID,
		TaskID: v.TaskID, RevisionID: v.RevisionID, RuntimeID: v.RuntimeID, LaunchFence: v.LaunchFence,
		Status: domain.DCPV2ModelTerminalStatus(v.Status), ResultDigest: v.ResultDigest, ErrorCode: v.ErrorCode,
		OutputJSON: v.OutputJson, OutputDigest: v.OutputDigest, HeadRef: v.HeadRef, HeadSHA: v.HeadSha,
		TreeSHA: v.TreeSha, BaseSHA: v.BaseSha, WorktreePath: v.WorktreePath, WorktreeDigest: v.WorktreeDigest, CreatedAt: v.CreatedAt}
}

func dcpV2Stage6WorkerAdoptionParams(v domain.DCPV2Stage6WorkerAdoption) gen.InsertDCPV2Stage6WorkerAdoptionParams {
	return gen.InsertDCPV2Stage6WorkerAdoptionParams{AdoptionID: v.AdoptionID, TaskID: v.TaskID, RevisionID: v.RevisionID,
		CommandID: v.CommandID, ActionID: v.ActionID, RuntimeID: v.RuntimeID, NativeActionID: v.NativeActionID,
		NativeSequence: v.NativeSequence, LegacyEvidenceDigest: v.LegacyEvidenceDigest, CommitSha: v.CommitSHA,
		TreeSha: v.TreeSHA, Branch: v.Branch, WorktreeDigest: v.WorktreeDigest, OutputDigest: v.OutputDigest,
		ReceiptID: v.ReceiptID, ConsumedAt: v.ConsumedAt}
}

func dcpV2Stage6WorkerAdoptionFromGen(v gen.DcpV2Stage6WorkerAdoptionV1) domain.DCPV2Stage6WorkerAdoption {
	return domain.DCPV2Stage6WorkerAdoption{AdoptionID: v.AdoptionID, TaskID: v.TaskID, RevisionID: v.RevisionID,
		CommandID: v.CommandID, ActionID: v.ActionID, RuntimeID: v.RuntimeID, NativeActionID: v.NativeActionID,
		NativeSequence: v.NativeSequence, LegacyEvidenceDigest: v.LegacyEvidenceDigest, CommitSHA: v.CommitSha,
		TreeSHA: v.TreeSha, Branch: v.Branch, WorktreeDigest: v.WorktreeDigest, OutputDigest: v.OutputDigest,
		ReceiptID: v.ReceiptID, ConsumedAt: v.ConsumedAt}
}
