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

var (
	ErrDCPV2NotFound           = errors.New("dcp v2 record not found")
	ErrDCPV2IdentityConflict   = errors.New("dcp v2 immutable identity conflict")
	ErrDCPV2StaleTransition    = errors.New("dcp v2 stale transition")
	ErrDCPV2LeaseLost          = errors.New("dcp v2 lease lost")
	ErrDCPV2EffectFenced       = errors.New("dcp v2 external effect is already fenced")
	ErrDCPV2BudgetExhausted    = errors.New("dcp v2 bounded allowance exhausted")
	ErrDCPV2ProtocolViolation  = errors.New("dcp v2 protocol violation")
	ErrDCPV2ExternalEventDrift = errors.New("dcp v2 external event identity conflict")
)

// DCPV2Transition is the single state-plus-next-command transaction. A
// successor Revision, Admission or Result is immutable and is committed in
// the same transaction. Nonterminal states other than Human Gate require
// exactly one NextCommand; terminal/Human Gate states forbid it.
type DCPV2Transition struct {
	CommandID               string
	LeaseOwner              string
	LeaseEpoch              string
	LeaseToken              string
	ExpectedTaskState       domain.DCPV2TaskState
	ExpectedStateRevision   int64
	ExpectedRevisionID      string
	NextTaskState           domain.DCPV2TaskState
	RepairUsed              int64
	ReadmissionCount        int64
	TerminalResultID        string
	HumanGateQuestion       string
	TaskErrorCode           string
	CommandResultDigest     string
	CommandErrorCode        string
	ExternalEventDeliveryID string
	NextRevision            *domain.DCPV2Revision
	NextCommand             *domain.DCPV2Command
	NextAction              *domain.DCPV2Action
	Admission               *domain.DCPV2Admission
	CompleteAdmissionID     string
	AdmissionLeaseOwner     string
	AdmissionLeaseEpoch     string
	AdmissionLeaseToken     string
	AdmissionCompletion     domain.DCPV2AdmissionStatus
	AdmissionResultID       string
	AdmissionErrorCode      string
	Incident                *domain.DCPV2Incident
	Result                  *domain.DCPV2Result
	UpdatedAt               time.Time
}

// DCPV2ExternalEventOutcome reports whether a delivery was newly retained or
// was an exact replay of the same immutable provider fact.
type DCPV2ExternalEventOutcome struct {
	Event   domain.DCPV2ExternalEvent
	Created bool
}

func (s *Store) CreateDCPV2Task(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, action domain.DCPV2Action) (bool, error) {
	if err := validateInitialDCPV2(task, revision, command, action); err != nil {
		return false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "create dcp v2 task", func(q *gen.Queries) error {
		existing, err := q.GetDCPV2Task(ctx, task.TaskID)
		switch {
		case err == nil:
			if !sameDCPV2Task(existing, task) {
				return ErrDCPV2IdentityConflict
			}
			existingRevision, revisionErr := q.GetDCPV2Revision(ctx, revision.RevisionID)
			existingCommand, commandErr := q.GetDCPV2CommandByIdempotencyKey(ctx, command.IdempotencyKey)
			existingAction, actionErr := q.GetDCPV2ActionByCommand(ctx, command.CommandID)
			if revisionErr != nil || commandErr != nil || actionErr != nil || !sameDCPV2Revision(existingRevision, revision) ||
				!sameDCPV2Command(existingCommand, command) || !sameDCPV2Action(existingAction, action) {
				return ErrDCPV2IdentityConflict
			}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read existing task: %w", err)
		}
		if err := q.InsertDCPV2Task(ctx, dcpV2TaskParams(task)); err != nil {
			return fmt.Errorf("insert task: %w", err)
		}
		if err := q.InsertDCPV2Revision(ctx, dcpV2RevisionParams(revision)); err != nil {
			return fmt.Errorf("insert initial revision: %w", err)
		}
		if err := q.InsertDCPV2Command(ctx, dcpV2CommandParams(command)); err != nil {
			return fmt.Errorf("insert initial command: %w", err)
		}
		if err := q.InsertDCPV2Action(ctx, dcpV2ActionParams(action)); err != nil {
			return fmt.Errorf("insert initial action: %w", err)
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) GetDCPV2Task(ctx context.Context, taskID string) (domain.DCPV2Task, error) {
	row, err := s.qr.GetDCPV2Task(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2Task{}, ErrDCPV2NotFound
	}
	if err != nil {
		return domain.DCPV2Task{}, fmt.Errorf("get dcp v2 task: %w", err)
	}
	return dcpV2TaskFromGen(row), nil
}

func (s *Store) ListDCPV2Tasks(ctx context.Context) ([]domain.DCPV2Task, error) {
	rows, err := s.qr.ListDCPV2Tasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dcp v2 tasks: %w", err)
	}
	out := make([]domain.DCPV2Task, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2TaskFromGen(row))
	}
	return out, nil
}

func (s *Store) GetDCPV2Command(ctx context.Context, commandID string) (domain.DCPV2Command, error) {
	row, err := s.qr.GetDCPV2Command(ctx, commandID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2Command{}, ErrDCPV2NotFound
	}
	if err != nil {
		return domain.DCPV2Command{}, fmt.Errorf("get dcp v2 command: %w", err)
	}
	return dcpV2CommandFromGen(row), nil
}

func (s *Store) ListDCPV2Commands(ctx context.Context, taskID string) ([]domain.DCPV2Command, error) {
	rows, err := s.qr.ListDCPV2CommandsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list dcp v2 commands: %w", err)
	}
	out := make([]domain.DCPV2Command, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2CommandFromGen(row))
	}
	return out, nil
}

func (s *Store) ListDCPV2Revisions(ctx context.Context, taskID string) ([]domain.DCPV2Revision, error) {
	rows, err := s.qr.ListDCPV2RevisionsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list dcp v2 revisions: %w", err)
	}
	out := make([]domain.DCPV2Revision, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2RevisionFromGen(row))
	}
	return out, nil
}

// ClaimNextDCPV2Command leases the globally oldest pending command once. It
// performs no waiting or polling; callers drain again only on startup or a
// concrete provider/event wake.
func (s *Store) ClaimNextDCPV2Command(ctx context.Context, owner, epoch, token string, at time.Time) (*domain.DCPV2Command, error) {
	if owner == "" || epoch == "" || token == "" || at.IsZero() {
		return nil, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var claimed *domain.DCPV2Command
	err := s.inTx(ctx, "claim next dcp v2 command", func(q *gen.Queries) error {
		rows, err := q.ListPendingDCPV2Commands(ctx)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		row := rows[0]
		updated, err := q.LeaseDCPV2Command(ctx, gen.LeaseDCPV2CommandParams{
			LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token, UpdatedAt: at, CommandID: row.CommandID,
		})
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrDCPV2LeaseLost
		}
		row.Status, row.LeaseOwner, row.LeaseEpoch, row.LeaseToken, row.UpdatedAt = "leased", owner, epoch, token, at
		mapped := dcpV2CommandFromGen(row)
		claimed = &mapped
		return nil
	})
	return claimed, err
}

func (s *Store) ListLeasedDCPV2Commands(ctx context.Context) ([]domain.DCPV2Command, error) {
	rows, err := s.qr.ListLeasedDCPV2Commands(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Command, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2CommandFromGen(row))
	}
	return out, nil
}

func (s *Store) FenceDCPV2CommandEffect(ctx context.Context, commandID, owner, epoch, token, fence string, at time.Time) error {
	if commandID == "" || owner == "" || epoch == "" || token == "" || fence == "" || at.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.FenceDCPV2CommandEffect(ctx, gen.FenceDCPV2CommandEffectParams{
		EffectFence: fence, UpdatedAt: at, CommandID: commandID, LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token,
	})
	if err != nil {
		return fmt.Errorf("fence dcp v2 command effect: %w", err)
	}
	if rows != 1 {
		current, getErr := s.qw.GetDCPV2Command(ctx, commandID)
		if getErr == nil && current.EffectFence != "" {
			return ErrDCPV2EffectFenced
		}
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) RecoverDCPV2CommandLease(ctx context.Context, command domain.DCPV2Command, owner, epoch, token string, at time.Time) (domain.DCPV2Command, error) {
	if command.EffectFence != "" {
		return domain.DCPV2Command{}, ErrDCPV2EffectFenced
	}
	if command.CommandID == "" || command.LeaseOwner == "" || command.LeaseEpoch == "" || command.LeaseToken == "" ||
		owner == "" || epoch == "" || token == "" || at.IsZero() {
		return domain.DCPV2Command{}, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.RecoverDCPV2CommandLease(ctx, gen.RecoverDCPV2CommandLeaseParams{
		LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token, UpdatedAt: at, CommandID: command.CommandID,
		LeaseOwner_2: command.LeaseOwner, LeaseEpoch_2: command.LeaseEpoch, LeaseToken_2: command.LeaseToken,
		RecoveryGeneration: command.RecoveryGeneration,
	})
	if err != nil {
		return domain.DCPV2Command{}, fmt.Errorf("recover dcp v2 command lease: %w", err)
	}
	if rows != 1 {
		return domain.DCPV2Command{}, ErrDCPV2LeaseLost
	}
	command.LeaseOwner, command.LeaseEpoch, command.LeaseToken = owner, epoch, token
	command.RecoveryGeneration++
	command.UpdatedAt = at
	return command, nil
}

func (s *Store) TransitionDCPV2(ctx context.Context, tr DCPV2Transition) error {
	if tr.UpdatedAt.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "transition dcp v2", func(q *gen.Queries) error {
		commandRow, err := q.GetDCPV2Command(ctx, tr.CommandID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDCPV2NotFound
		}
		if err != nil {
			return err
		}
		if commandRow.Status != string(domain.DCPV2CommandLeased) || commandRow.LeaseOwner != tr.LeaseOwner ||
			commandRow.LeaseEpoch != tr.LeaseEpoch || commandRow.LeaseToken != tr.LeaseToken {
			return ErrDCPV2LeaseLost
		}
		taskRow, err := q.GetDCPV2Task(ctx, commandRow.TaskID)
		if err != nil {
			return err
		}
		task := dcpV2TaskFromGen(taskRow)
		if task.State != tr.ExpectedTaskState || task.StateRevision != tr.ExpectedStateRevision ||
			task.CurrentRevisionID != tr.ExpectedRevisionID || commandRow.RevisionID != tr.ExpectedRevisionID {
			return ErrDCPV2StaleTransition
		}
		currentRevisionRow, err := q.GetDCPV2Revision(ctx, task.CurrentRevisionID)
		if err != nil {
			return err
		}
		if err := validateDCPV2Transition(task, dcpV2RevisionFromGen(currentRevisionRow), dcpV2CommandFromGen(commandRow), tr); err != nil {
			return err
		}
		if domain.DCPV2CommandKind(commandRow.Kind).ModelBacked() {
			action, err := q.GetDCPV2ActionByCommand(ctx, commandRow.CommandID)
			if err != nil {
				return err
			}
			if action.TaskID != task.TaskID || action.RevisionID != task.CurrentRevisionID {
				return ErrDCPV2IdentityConflict
			}
			if tr.CommandErrorCode == "" {
				if action.Status != string(domain.DCPV2ActionSucceeded) || action.LaunchFence == "" ||
					action.LaunchFence != commandRow.EffectFence || action.ResultDigest != tr.CommandResultDigest {
					return ErrDCPV2IdentityConflict
				}
			} else if action.Status != string(domain.DCPV2ActionSucceeded) && action.Status != string(domain.DCPV2ActionFailed) {
				return ErrDCPV2StaleTransition
			}
		}
		if tr.ExternalEventDeliveryID != "" {
			event, err := q.GetDCPV2ExternalEvent(ctx, tr.ExternalEventDeliveryID)
			if err != nil {
				return err
			}
			if event.Status != "retained" || event.TaskID != task.TaskID || event.RevisionID != task.CurrentRevisionID ||
				event.PrerequisiteDigest != commandRow.PrerequisiteDigest {
				return ErrDCPV2StaleTransition
			}
		}
		commandStatus := string(domain.DCPV2CommandSucceeded)
		if tr.CommandErrorCode != "" {
			commandStatus = string(domain.DCPV2CommandFailed)
		}
		rows, err := q.FinishDCPV2Command(ctx, gen.FinishDCPV2CommandParams{
			Status: commandStatus, ResultDigest: tr.CommandResultDigest, ErrorCode: tr.CommandErrorCode, UpdatedAt: tr.UpdatedAt,
			CommandID: tr.CommandID, LeaseOwner: tr.LeaseOwner, LeaseEpoch: tr.LeaseEpoch, LeaseToken: tr.LeaseToken,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrDCPV2LeaseLost
		}
		if tr.ExternalEventDeliveryID != "" {
			rows, err := q.ApplyDCPV2ExternalEvent(ctx, gen.ApplyDCPV2ExternalEventParams{
				Status: "applied", CommandID: tr.CommandID, UpdatedAt: tr.UpdatedAt, DeliveryID: tr.ExternalEventDeliveryID,
			})
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrDCPV2StaleTransition
			}
		}
		nextRevisionID := task.CurrentRevisionID
		if tr.NextRevision != nil {
			if err := q.InsertDCPV2Revision(ctx, dcpV2RevisionParams(*tr.NextRevision)); err != nil {
				return fmt.Errorf("insert successor revision: %w", err)
			}
			nextRevisionID = tr.NextRevision.RevisionID
		}
		if tr.Admission != nil {
			if err := q.InsertDCPV2Admission(ctx, dcpV2AdmissionParams(*tr.Admission)); err != nil {
				return fmt.Errorf("insert admission: %w", err)
			}
		}
		if err := validateDCPV2AdmissionCompletion(ctx, q, task, nextRevisionID, tr); err != nil {
			return err
		}
		if tr.Incident != nil {
			if err := q.InsertDCPV2Incident(ctx, dcpV2IncidentParams(*tr.Incident)); err != nil {
				return fmt.Errorf("insert incident: %w", err)
			}
		}
		if tr.Result != nil {
			if err := validateDCPV2ResultBinding(ctx, q, task, nextRevisionID, tr); err != nil {
				return err
			}
			if err := q.InsertDCPV2Result(ctx, dcpV2ResultParams(*tr.Result)); err != nil {
				return fmt.Errorf("insert result: %w", err)
			}
		}
		if err := validateDCPV2TerminalResultBinding(ctx, q, task, nextRevisionID, tr); err != nil {
			return err
		}
		if tr.CompleteAdmissionID != "" {
			rows, err := q.FinishDCPV2Admission(ctx, gen.FinishDCPV2AdmissionParams{
				Status: string(tr.AdmissionCompletion), ResultID: tr.AdmissionResultID, ErrorCode: tr.AdmissionErrorCode,
				UpdatedAt: tr.UpdatedAt, AdmissionID: tr.CompleteAdmissionID, LeaseOwner: tr.AdmissionLeaseOwner,
				LeaseEpoch: tr.AdmissionLeaseEpoch, LeaseToken: tr.AdmissionLeaseToken,
			})
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrDCPV2LeaseLost
			}
		}
		rows, err = q.UpdateDCPV2TaskCAS(ctx, gen.UpdateDCPV2TaskCASParams{
			RepairUsed: tr.RepairUsed, ReadmissionCount: tr.ReadmissionCount, CurrentRevisionID: nextRevisionID,
			State: string(tr.NextTaskState), TerminalResultID: tr.TerminalResultID,
			HumanGateQuestion: tr.HumanGateQuestion, ErrorCode: tr.TaskErrorCode, UpdatedAt: tr.UpdatedAt,
			TaskID: task.TaskID, StateRevision: task.StateRevision, CurrentRevisionID_2: task.CurrentRevisionID, State_2: string(task.State),
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrDCPV2StaleTransition
		}
		if tr.NextCommand != nil {
			if err := q.InsertDCPV2Command(ctx, dcpV2CommandParams(*tr.NextCommand)); err != nil {
				return fmt.Errorf("insert next command: %w", err)
			}
			if tr.NextAction != nil {
				if err := q.InsertDCPV2Action(ctx, dcpV2ActionParams(*tr.NextAction)); err != nil {
					return fmt.Errorf("insert next action: %w", err)
				}
			}
		}
		return nil
	})
}

// ClaimNextDCPV2Action assigns the globally oldest queued model Action to the
// lowest free physical slot. Actions are inserted atomically with their owning
// model-backed Command; there are exactly three slots and no logical counter
// that can drift from their rows.
func (s *Store) ClaimNextDCPV2Action(ctx context.Context, launchFence string, at time.Time) (*domain.DCPV2Action, error) {
	if launchFence == "" || at.IsZero() {
		return nil, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var claimed *domain.DCPV2Action
	err := s.inTx(ctx, "claim next dcp v2 action", func(q *gen.Queries) error {
		active, err := q.ListActiveDCPV2Actions(ctx)
		if err != nil {
			return err
		}
		used := map[int64]bool{}
		for _, row := range active {
			used[row.Slot] = true
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
		queued, err := q.ListQueuedDCPV2Actions(ctx)
		if err != nil {
			return err
		}
		if len(queued) == 0 {
			return nil
		}
		row := queued[0]
		command, err := q.GetDCPV2Command(ctx, row.CommandID)
		if err != nil {
			return err
		}
		if command.TaskID != row.TaskID || command.RevisionID != row.RevisionID ||
			command.Status != string(domain.DCPV2CommandLeased) || command.EffectFence == "" || command.EffectFence != launchFence {
			return nil
		}
		updated, err := q.LaunchDCPV2Action(ctx, gen.LaunchDCPV2ActionParams{
			Slot: slot, LaunchFence: launchFence, UpdatedAt: at, ActionID: row.ActionID,
		})
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrDCPV2LeaseLost
		}
		row.Status, row.Slot, row.LaunchFence, row.UpdatedAt = "launching", slot, launchFence, at
		mapped := dcpV2ActionFromGen(row)
		claimed = &mapped
		return nil
	})
	return claimed, err
}

func (s *Store) StartDCPV2Action(ctx context.Context, actionID string, slot int64, launchFence, runtimeID string, at time.Time) error {
	if actionID == "" || slot < 1 || slot > 3 || launchFence == "" || runtimeID == "" || at.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.StartDCPV2Action(ctx, gen.StartDCPV2ActionParams{
		RuntimeID: runtimeID, UpdatedAt: at, ActionID: actionID, Slot: slot, LaunchFence: launchFence,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) FinishDCPV2Action(ctx context.Context, actionID string, slot int64, launchFence string, succeeded bool, resultDigest, errorCode string, at time.Time) error {
	if actionID == "" || slot < 1 || slot > 3 || launchFence == "" || at.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	status := string(domain.DCPV2ActionFailed)
	if succeeded {
		status = string(domain.DCPV2ActionSucceeded)
		if len(resultDigest) != 64 || errorCode != "" {
			return ErrDCPV2ProtocolViolation
		}
	} else if errorCode == "" || resultDigest != "" {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.FinishDCPV2Action(ctx, gen.FinishDCPV2ActionParams{
		Status: status, ResultDigest: resultDigest, ErrorCode: errorCode, UpdatedAt: at,
		ActionID: actionID, Slot: slot, LaunchFence: launchFence,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) ListDCPV2Actions(ctx context.Context, taskID string) ([]domain.DCPV2Action, error) {
	rows, err := s.qr.ListDCPV2ActionsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2ActionFromGen(row))
	}
	return out, nil
}

func (s *Store) GetDCPV2ActionByCommand(ctx context.Context, commandID string) (domain.DCPV2Action, error) {
	row, err := s.qr.GetDCPV2ActionByCommand(ctx, commandID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPV2Action{}, ErrDCPV2NotFound
	}
	if err != nil {
		return domain.DCPV2Action{}, err
	}
	return dcpV2ActionFromGen(row), nil
}

func (s *Store) ClaimNextDCPV2Admission(ctx context.Context, lineKey, owner, epoch, token string, at time.Time) (*domain.DCPV2Admission, error) {
	if lineKey == "" || owner == "" || epoch == "" || token == "" || at.IsZero() {
		return nil, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var claimed *domain.DCPV2Admission
	err := s.inTx(ctx, "claim next dcp v2 admission", func(q *gen.Queries) error {
		line, err := q.ListDCPV2AdmissionsByLine(ctx, lineKey)
		if err != nil {
			return err
		}
		for _, existing := range line {
			if existing.Status == string(domain.DCPV2AdmissionLeased) || existing.Status == string(domain.DCPV2AdmissionDispatched) {
				return nil
			}
		}
		row, err := q.GetNextWaitingDCPV2Admission(ctx, lineKey)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		updated, err := q.LeaseDCPV2Admission(ctx, gen.LeaseDCPV2AdmissionParams{
			LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token, UpdatedAt: at, AdmissionID: row.AdmissionID,
		})
		if err != nil {
			return err
		}
		if updated != 1 {
			return ErrDCPV2LeaseLost
		}
		row.Status, row.LeaseOwner, row.LeaseEpoch, row.LeaseToken, row.UpdatedAt = "leased", owner, epoch, token, at
		mapped := dcpV2AdmissionFromGen(row)
		claimed = &mapped
		return nil
	})
	return claimed, err
}

func (s *Store) ListLeasedDCPV2Admissions(ctx context.Context) ([]domain.DCPV2Admission, error) {
	rows, err := s.qr.ListLeasedDCPV2Admissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Admission, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2AdmissionFromGen(row))
	}
	return out, nil
}

func (s *Store) FenceDCPV2AdmissionDispatch(ctx context.Context, admissionID, owner, epoch, token, fence string, at time.Time) error {
	if admissionID == "" || owner == "" || epoch == "" || token == "" || fence == "" || at.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.FenceDCPV2AdmissionDispatch(ctx, gen.FenceDCPV2AdmissionDispatchParams{
		DispatchFence: fence, UpdatedAt: at, AdmissionID: admissionID,
		LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		current, getErr := s.qw.GetDCPV2Admission(ctx, admissionID)
		if getErr == nil && current.DispatchFence != "" {
			return ErrDCPV2EffectFenced
		}
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) RecoverDCPV2AdmissionLease(ctx context.Context, admission domain.DCPV2Admission, owner, epoch, token string, at time.Time) (domain.DCPV2Admission, error) {
	if admission.DispatchFence != "" {
		return domain.DCPV2Admission{}, ErrDCPV2EffectFenced
	}
	if owner == "" || epoch == "" || token == "" || at.IsZero() {
		return domain.DCPV2Admission{}, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.RecoverDCPV2AdmissionLease(ctx, gen.RecoverDCPV2AdmissionLeaseParams{
		LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token, UpdatedAt: at, AdmissionID: admission.AdmissionID,
		LeaseOwner_2: admission.LeaseOwner, LeaseEpoch_2: admission.LeaseEpoch, LeaseToken_2: admission.LeaseToken,
		RecoveryGeneration: admission.RecoveryGeneration,
	})
	if err != nil {
		return domain.DCPV2Admission{}, err
	}
	if rows != 1 {
		return domain.DCPV2Admission{}, ErrDCPV2LeaseLost
	}
	admission.LeaseOwner, admission.LeaseEpoch, admission.LeaseToken = owner, epoch, token
	admission.RecoveryGeneration++
	admission.UpdatedAt = at
	return admission, nil
}

func (s *Store) DispatchDCPV2Admission(ctx context.Context, admissionID, owner, epoch, token string, at time.Time) error {
	if admissionID == "" || owner == "" || epoch == "" || token == "" || at.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.DispatchDCPV2Admission(ctx, gen.DispatchDCPV2AdmissionParams{
		UpdatedAt: at, AdmissionID: admissionID, LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) FinishDCPV2Admission(ctx context.Context, admissionID, owner, epoch, token string, status domain.DCPV2AdmissionStatus, resultID, errorCode string, at time.Time) error {
	if status != domain.DCPV2AdmissionReadmissionRequired && status != domain.DCPV2AdmissionSucceeded && status != domain.DCPV2AdmissionFailed {
		return ErrDCPV2ProtocolViolation
	}
	if admissionID == "" || owner == "" || epoch == "" || token == "" || at.IsZero() ||
		(status == domain.DCPV2AdmissionSucceeded) != (resultID != "") ||
		(status == domain.DCPV2AdmissionFailed && errorCode == "") ||
		(status != domain.DCPV2AdmissionFailed && errorCode != "") {
		return ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.FinishDCPV2Admission(ctx, gen.FinishDCPV2AdmissionParams{
		Status: string(status), ResultID: resultID, ErrorCode: errorCode, UpdatedAt: at,
		AdmissionID: admissionID, LeaseOwner: owner, LeaseEpoch: epoch, LeaseToken: token,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrDCPV2LeaseLost
	}
	return nil
}

func (s *Store) ListDCPV2Admissions(ctx context.Context, taskID string) ([]domain.DCPV2Admission, error) {
	rows, err := s.qr.ListDCPV2AdmissionsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Admission, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2AdmissionFromGen(row))
	}
	return out, nil
}

func (s *Store) ListDCPV2Results(ctx context.Context, taskID string) ([]domain.DCPV2Result, error) {
	rows, err := s.qr.ListDCPV2ResultsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Result, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2ResultFromGen(row))
	}
	return out, nil
}

func (s *Store) ListDCPV2Incidents(ctx context.Context, taskID string) ([]domain.DCPV2Incident, error) {
	rows, err := s.qr.ListDCPV2IncidentsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2Incident, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2IncidentFromGen(row))
	}
	return out, nil
}

// RecordDCPV2ExternalEvent persists a provider delivery exactly once. Events
// for a stale/unknown Revision remain immutable retained evidence and cannot
// enqueue work. Equal duplicate deliveries return the existing fact; a
// different body under the same delivery/provider sequence fails closed.
func (s *Store) RecordDCPV2ExternalEvent(ctx context.Context, event domain.DCPV2ExternalEvent) (DCPV2ExternalEventOutcome, error) {
	if event.DeliveryID == "" || event.Provider == "" || len(event.PayloadDigest) != 64 || len(event.PrerequisiteDigest) != 64 || event.CreatedAt.IsZero() {
		return DCPV2ExternalEventOutcome{}, ErrDCPV2ProtocolViolation
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	outcome := DCPV2ExternalEventOutcome{}
	err := s.inTx(ctx, "record dcp v2 external event", func(q *gen.Queries) error {
		existing, err := q.GetDCPV2ExternalEvent(ctx, event.DeliveryID)
		switch {
		case err == nil:
			if !sameDCPV2ExternalEvent(existing, event) {
				return ErrDCPV2ExternalEventDrift
			}
			outcome.Event = dcpV2ExternalEventFromGen(existing)
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		event.Status = "retained"
		event.CommandID = ""
		if event.UpdatedAt.IsZero() {
			event.UpdatedAt = event.CreatedAt
		}
		if err := q.InsertDCPV2ExternalEvent(ctx, dcpV2ExternalEventParams(event)); err != nil {
			// The provider/task/kind/sequence unique constraint gives the same
			// fail-closed result when a new delivery ID reuses a sequence.
			return fmt.Errorf("insert provider event: %w", err)
		}
		outcome = DCPV2ExternalEventOutcome{Event: event, Created: true}
		return nil
	})
	return outcome, err
}

func (s *Store) ListDCPV2ExternalEvents(ctx context.Context, taskID string) ([]domain.DCPV2ExternalEvent, error) {
	rows, err := s.qr.ListDCPV2ExternalEventsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPV2ExternalEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpV2ExternalEventFromGen(row))
	}
	return out, nil
}

func validateInitialDCPV2(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, action domain.DCPV2Action) error {
	if task.TaskID == "" || task.State != domain.DCPV2TaskWorkerQueued || task.StateRevision != 1 ||
		task.InitialWorkerBudget != 1 || task.RepairBudget != 1 || task.RepairUsed != 0 || task.ReadmissionCount != 0 ||
		(task.Profile != "repo-only" && task.Profile != "live-runtime") ||
		task.CurrentRevisionID != revision.RevisionID || task.TerminalResultID != "" || task.HumanGateQuestion != "" || task.ErrorCode != "" ||
		task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	if revision.TaskID != task.TaskID || revision.Sequence != 1 || revision.Kind != domain.DCPV2RevisionWorkInput ||
		revision.Repository != task.Repository || revision.BaseRef != task.BaseRef || revision.BaseSHA != revision.HeadSHA ||
		revision.PredecessorRevisionID != "" || revision.CauseCommandID != "" || revision.CreatedAt.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	if command.TaskID != task.TaskID || command.RevisionID != revision.RevisionID || command.Kind != domain.DCPV2CommandWorkerExecute ||
		command.Status != domain.DCPV2CommandPending || command.LeaseOwner != "" || command.LeaseEpoch != "" || command.LeaseToken != "" ||
		command.EffectFence != "" || command.RecoveryGeneration != 0 || command.CreatedAt.IsZero() || command.UpdatedAt.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	if err := validateDCPV2ActionForCommand(action, command); err != nil {
		return err
	}
	return nil
}

func validateDCPV2Transition(task domain.DCPV2Task, currentRevision domain.DCPV2Revision, command domain.DCPV2Command, tr DCPV2Transition) error {
	if tr.RepairUsed > task.RepairBudget || tr.ReadmissionCount > task.MaxReadmissions {
		return ErrDCPV2BudgetExhausted
	}
	if task.State.Terminal() || command.Status != domain.DCPV2CommandLeased || tr.NextTaskState == "" ||
		tr.RepairUsed < task.RepairUsed || tr.RepairUsed > task.RepairUsed+1 || tr.RepairUsed > task.RepairBudget ||
		tr.ReadmissionCount < task.ReadmissionCount || tr.ReadmissionCount > task.ReadmissionCount+1 || tr.ReadmissionCount > task.MaxReadmissions {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState.RequiresCommand() != (tr.NextCommand != nil) {
		return ErrDCPV2ProtocolViolation
	}
	if !command.Kind.AllowsTransition(task.State, tr.NextTaskState, tr.NextRevision != nil) {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskHumanGate {
		if tr.HumanGateQuestion == "" || tr.TaskErrorCode != "" || tr.TerminalResultID != "" {
			return ErrDCPV2ProtocolViolation
		}
	} else if tr.HumanGateQuestion != "" {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskFailed {
		if tr.TaskErrorCode == "" || tr.TerminalResultID != "" {
			return ErrDCPV2ProtocolViolation
		}
	} else if tr.TaskErrorCode != "" {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskMerged || tr.NextTaskState == domain.DCPV2TaskDeployed {
		if tr.TerminalResultID == "" || tr.Result != nil || tr.CompleteAdmissionID != "" || command.Kind != domain.DCPV2CommandTerminalVerify {
			return ErrDCPV2ProtocolViolation
		}
	} else if tr.TerminalResultID != "" {
		return ErrDCPV2ProtocolViolation
	}
	if tr.CommandErrorCode == "" {
		if len(tr.CommandResultDigest) != 64 {
			return ErrDCPV2ProtocolViolation
		}
		if command.Kind.RequiresEffectFence() && command.EffectFence == "" {
			return ErrDCPV2ProtocolViolation
		}
	} else if tr.NextTaskState != domain.DCPV2TaskFailed && tr.NextTaskState != domain.DCPV2TaskHumanGate {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskRepairQueued {
		if task.RepairUsed >= task.RepairBudget || tr.RepairUsed != task.RepairUsed+1 {
			return ErrDCPV2BudgetExhausted
		}
	} else if tr.RepairUsed != task.RepairUsed {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskReadmission {
		if task.ReadmissionCount >= task.MaxReadmissions || tr.ReadmissionCount != task.ReadmissionCount+1 {
			return ErrDCPV2BudgetExhausted
		}
	} else if tr.ReadmissionCount != task.ReadmissionCount {
		return ErrDCPV2ProtocolViolation
	}
	nextRevisionID := currentRevision.RevisionID
	if tr.NextRevision != nil {
		revision := tr.NextRevision
		if revision.TaskID != task.TaskID || revision.Sequence != currentRevision.Sequence+1 || revision.Repository != task.Repository ||
			revision.BaseRef != task.BaseRef || revision.PredecessorRevisionID != currentRevision.RevisionID ||
			revision.CauseCommandID != command.CommandID || revision.BaseSHA == "" || revision.HeadSHA == "" || revision.CreatedAt.IsZero() {
			return ErrDCPV2ProtocolViolation
		}
		switch revision.Kind {
		case domain.DCPV2RevisionWorker:
			if command.Kind != domain.DCPV2CommandWorkerExecute {
				return ErrDCPV2ProtocolViolation
			}
		case domain.DCPV2RevisionRepair:
			if command.Kind != domain.DCPV2CommandRepairExecute {
				return ErrDCPV2ProtocolViolation
			}
		case domain.DCPV2RevisionReadmission:
			if command.Kind != domain.DCPV2CommandReadmission {
				return ErrDCPV2ProtocolViolation
			}
		default:
			return ErrDCPV2ProtocolViolation
		}
		nextRevisionID = revision.RevisionID
	}
	if tr.NextCommand != nil {
		next := tr.NextCommand
		if next.TaskID != task.TaskID || next.RevisionID != nextRevisionID || next.Status != domain.DCPV2CommandPending ||
			next.LeaseOwner != "" || next.LeaseEpoch != "" || next.LeaseToken != "" || next.EffectFence != "" ||
			next.RecoveryGeneration != 0 || next.ResultDigest != "" || next.ErrorCode != "" || next.CreatedAt.IsZero() || next.UpdatedAt.IsZero() {
			return ErrDCPV2ProtocolViolation
		}
		if next.Kind.ModelBacked() != (tr.NextAction != nil) {
			return ErrDCPV2ProtocolViolation
		}
		if tr.NextAction != nil {
			if err := validateDCPV2ActionForCommand(*tr.NextAction, *next); err != nil {
				return err
			}
		}
	} else if tr.NextAction != nil {
		return ErrDCPV2ProtocolViolation
	}
	if tr.Admission != nil {
		a := tr.Admission
		if a.TaskID != task.TaskID || a.RevisionID != nextRevisionID || a.Status != domain.DCPV2AdmissionWaiting ||
			a.LeaseOwner != "" || a.LeaseEpoch != "" || a.LeaseToken != "" || a.DispatchFence != "" || a.RecoveryGeneration != 0 ||
			a.ResultID != "" || a.ErrorCode != "" || a.HeadSHA == "" || a.ManifestDigest == "" {
			return ErrDCPV2ProtocolViolation
		}
	}
	if tr.CompleteAdmissionID == "" {
		if tr.AdmissionLeaseOwner != "" || tr.AdmissionLeaseEpoch != "" || tr.AdmissionLeaseToken != "" ||
			tr.AdmissionCompletion != "" || tr.AdmissionResultID != "" || tr.AdmissionErrorCode != "" {
			return ErrDCPV2ProtocolViolation
		}
	} else {
		if tr.AdmissionLeaseOwner == "" || tr.AdmissionLeaseEpoch == "" || tr.AdmissionLeaseToken == "" ||
			(tr.AdmissionCompletion != domain.DCPV2AdmissionReadmissionRequired &&
				tr.AdmissionCompletion != domain.DCPV2AdmissionSucceeded && tr.AdmissionCompletion != domain.DCPV2AdmissionFailed) ||
			(tr.AdmissionCompletion == domain.DCPV2AdmissionSucceeded) != (tr.AdmissionResultID != "") ||
			(tr.AdmissionCompletion == domain.DCPV2AdmissionFailed && tr.AdmissionErrorCode == "") ||
			(tr.AdmissionCompletion != domain.DCPV2AdmissionFailed && tr.AdmissionErrorCode != "") {
			return ErrDCPV2ProtocolViolation
		}
	}
	if tr.Incident != nil {
		i := tr.Incident
		if i.IncidentID == "" || i.TaskID != task.TaskID || i.RevisionID != nextRevisionID ||
			i.CauseCommandID != command.CommandID || i.Kind == "" || len(i.EvidenceDigest) != 64 || i.CreatedAt.IsZero() {
			return ErrDCPV2ProtocolViolation
		}
		switch i.Disposition {
		case domain.DCPV2IncidentArbiter:
			if tr.NextTaskState != domain.DCPV2TaskArbiterQueued || i.OwnerQuestion != "" {
				return ErrDCPV2ProtocolViolation
			}
		case domain.DCPV2IncidentHumanGate:
			if tr.NextTaskState != domain.DCPV2TaskHumanGate || i.OwnerQuestion == "" || i.OwnerQuestion != tr.HumanGateQuestion {
				return ErrDCPV2ProtocolViolation
			}
		case domain.DCPV2IncidentTerminal:
			if tr.NextTaskState != domain.DCPV2TaskFailed || i.OwnerQuestion != "" {
				return ErrDCPV2ProtocolViolation
			}
		default:
			return ErrDCPV2ProtocolViolation
		}
	}
	if tr.Result != nil {
		r := tr.Result
		if r.TaskID != task.TaskID || r.RevisionID != nextRevisionID || r.CommandID != command.CommandID || r.CreatedAt.IsZero() {
			return ErrDCPV2ProtocolViolation
		}
		if r.Verified && (r.Provider == "" || r.ProofID == "" || r.RunID == "" || r.Actor == "" || len(r.ManifestDigest) != 64) {
			return ErrDCPV2ProtocolViolation
		}
		if r.Kind == domain.DCPV2ResultDeployment && r.Verified && (r.MergeSHA == "" || r.DeployedSHA != r.MergeSHA || r.ArtifactDigest == "" || r.Environment == "" || r.Service == "" || r.ProbeDigest == "") {
			return ErrDCPV2ProtocolViolation
		}
		if r.Kind == domain.DCPV2ResultRelease && r.Verified && (r.MergeSHA == "" || r.ArtifactDigest == "") {
			return ErrDCPV2ProtocolViolation
		}
	}
	if (tr.NextTaskState == domain.DCPV2TaskReleaseVerified || tr.NextTaskState == domain.DCPV2TaskDeploymentWaiting) &&
		(tr.Result == nil || tr.Result.Kind != domain.DCPV2ResultRelease || !tr.Result.Verified) {
		return ErrDCPV2ProtocolViolation
	}
	if tr.NextTaskState == domain.DCPV2TaskDeploymentObserve &&
		(tr.Result == nil || tr.Result.Kind != domain.DCPV2ResultDeployment || !tr.Result.Verified) {
		return ErrDCPV2ProtocolViolation
	}
	if (tr.NextTaskState == domain.DCPV2TaskReleaseVerified || tr.NextTaskState == domain.DCPV2TaskMerged) && task.Profile != "repo-only" {
		return ErrDCPV2ProtocolViolation
	}
	if (tr.NextTaskState == domain.DCPV2TaskDeploymentWaiting || tr.NextTaskState == domain.DCPV2TaskDeploymentObserve ||
		tr.NextTaskState == domain.DCPV2TaskDeployed) && task.Profile != "live-runtime" {
		return ErrDCPV2ProtocolViolation
	}
	return nil
}

func validateDCPV2ActionForCommand(action domain.DCPV2Action, command domain.DCPV2Command) error {
	if !command.Kind.ModelBacked() || action.CommandID != command.CommandID || action.TaskID != command.TaskID ||
		action.RevisionID != command.RevisionID || action.Role != roleForDCPV2Command(command.Kind) ||
		action.ActionID == "" || action.Model == "" || action.Reasoning == "" || action.TokenBudget <= 0 || action.TimeBudgetSec <= 0 ||
		len(action.InputDigest) != 64 || action.Attempt != 1 || action.Status != domain.DCPV2ActionQueued || action.Slot != 0 ||
		action.LaunchFence != "" || action.RuntimeID != "" || action.ResultDigest != "" || action.ErrorCode != "" ||
		action.CreatedAt.IsZero() || action.UpdatedAt.IsZero() {
		return ErrDCPV2ProtocolViolation
	}
	return nil
}

func validateDCPV2AdmissionCompletion(ctx context.Context, q *gen.Queries, task domain.DCPV2Task, revisionID string, tr DCPV2Transition) error {
	if tr.CompleteAdmissionID == "" {
		return nil
	}
	admission, err := q.GetDCPV2Admission(ctx, tr.CompleteAdmissionID)
	if err != nil {
		return err
	}
	if admission.TaskID != task.TaskID || admission.RevisionID != revisionID ||
		admission.LeaseOwner != tr.AdmissionLeaseOwner || admission.LeaseEpoch != tr.AdmissionLeaseEpoch ||
		admission.LeaseToken != tr.AdmissionLeaseToken {
		return ErrDCPV2IdentityConflict
	}
	if tr.AdmissionCompletion == domain.DCPV2AdmissionSucceeded {
		if admission.Status != string(domain.DCPV2AdmissionDispatched) || tr.Result == nil ||
			tr.Result.AdmissionID != admission.AdmissionID || tr.Result.ResultID != tr.AdmissionResultID {
			return ErrDCPV2StaleTransition
		}
	} else if admission.Status != string(domain.DCPV2AdmissionLeased) && admission.Status != string(domain.DCPV2AdmissionDispatched) {
		return ErrDCPV2StaleTransition
	}
	return nil
}

func validateDCPV2ResultBinding(ctx context.Context, q *gen.Queries, task domain.DCPV2Task, revisionID string, tr DCPV2Transition) error {
	result := tr.Result
	if result == nil {
		return nil
	}
	if result.Verified && (result.Kind == domain.DCPV2ResultRelease || result.Kind == domain.DCPV2ResultDeployment) && result.AdmissionID == "" {
		return ErrDCPV2ProtocolViolation
	}
	if result.AdmissionID != "" {
		admission, err := q.GetDCPV2Admission(ctx, result.AdmissionID)
		if err != nil {
			return err
		}
		if admission.TaskID != task.TaskID || admission.RevisionID != revisionID {
			return ErrDCPV2IdentityConflict
		}
		if result.Verified && result.ManifestDigest != admission.ManifestDigest {
			return ErrDCPV2IdentityConflict
		}
		if tr.CompleteAdmissionID != "" {
			if tr.CompleteAdmissionID != admission.AdmissionID || tr.AdmissionResultID != result.ResultID ||
				tr.AdmissionCompletion != domain.DCPV2AdmissionSucceeded {
				return ErrDCPV2IdentityConflict
			}
		} else if result.Verified && admission.Status != string(domain.DCPV2AdmissionSucceeded) {
			return ErrDCPV2StaleTransition
		}
	}
	if result.Kind == domain.DCPV2ResultDeployment && result.Verified {
		prior, err := q.ListDCPV2ResultsByTask(ctx, task.TaskID)
		if err != nil {
			return err
		}
		matched := false
		for _, row := range prior {
			if row.Kind == string(domain.DCPV2ResultRelease) && row.Verified == 1 && row.RevisionID == revisionID &&
				row.AdmissionID.Valid && row.AdmissionID.String == result.AdmissionID && row.MergeSha == result.MergeSHA &&
				row.ArtifactDigest == result.ArtifactDigest {
				matched = true
				break
			}
		}
		if !matched {
			return ErrDCPV2IdentityConflict
		}
	}
	return nil
}

func validateDCPV2TerminalResultBinding(ctx context.Context, q *gen.Queries, task domain.DCPV2Task, revisionID string, tr DCPV2Transition) error {
	if tr.NextTaskState != domain.DCPV2TaskMerged && tr.NextTaskState != domain.DCPV2TaskDeployed {
		return nil
	}
	result, err := q.GetDCPV2Result(ctx, tr.TerminalResultID)
	if err != nil {
		return err
	}
	wantKind := domain.DCPV2ResultRelease
	if tr.NextTaskState == domain.DCPV2TaskDeployed {
		wantKind = domain.DCPV2ResultDeployment
	}
	if result.TaskID != task.TaskID || result.RevisionID != revisionID || result.Kind != string(wantKind) || result.Verified != 1 ||
		!result.AdmissionID.Valid {
		return ErrDCPV2IdentityConflict
	}
	admission, err := q.GetDCPV2Admission(ctx, result.AdmissionID.String)
	if err != nil {
		return err
	}
	if admission.TaskID != task.TaskID || admission.RevisionID != revisionID || admission.Status != string(domain.DCPV2AdmissionSucceeded) ||
		admission.ResultID == "" {
		return ErrDCPV2IdentityConflict
	}
	return nil
}

func roleForDCPV2Command(kind domain.DCPV2CommandKind) domain.DCPV2ActionRole {
	switch kind {
	case domain.DCPV2CommandWorkerExecute:
		return domain.DCPV2ActionWorker
	case domain.DCPV2CommandReviewExecute:
		return domain.DCPV2ActionReviewer
	case domain.DCPV2CommandRepairExecute:
		return domain.DCPV2ActionRepair
	case domain.DCPV2CommandArbiterExecute:
		return domain.DCPV2ActionArbiter
	default:
		return ""
	}
}

func dcpV2TaskParams(v domain.DCPV2Task) gen.InsertDCPV2TaskParams {
	return gen.InsertDCPV2TaskParams{
		TaskID: v.TaskID, TargetSpecVersion: v.TargetSpecVersion, Repository: v.Repository, RepositoryID: v.RepositoryID,
		OwnerID: v.OwnerID, BaseRef: v.BaseRef, Profile: v.Profile, RequestDigest: v.RequestDigest,
		ScopeDigest: v.ScopeDigest, PolicyDigest: v.PolicyDigest, InitialWorkerBudget: v.InitialWorkerBudget,
		RepairBudget: v.RepairBudget, RepairUsed: v.RepairUsed, MaxReadmissions: v.MaxReadmissions,
		ReadmissionCount: v.ReadmissionCount, CurrentRevisionID: v.CurrentRevisionID, State: string(v.State),
		StateRevision: v.StateRevision, TerminalResultID: v.TerminalResultID, HumanGateQuestion: v.HumanGateQuestion,
		ErrorCode: v.ErrorCode, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2TaskFromGen(v gen.DcpV2Task) domain.DCPV2Task {
	return domain.DCPV2Task{
		TaskID: v.TaskID, TargetSpecVersion: v.TargetSpecVersion, Repository: v.Repository, RepositoryID: v.RepositoryID,
		OwnerID: v.OwnerID, BaseRef: v.BaseRef, Profile: v.Profile, RequestDigest: v.RequestDigest,
		ScopeDigest: v.ScopeDigest, PolicyDigest: v.PolicyDigest, InitialWorkerBudget: v.InitialWorkerBudget,
		RepairBudget: v.RepairBudget, RepairUsed: v.RepairUsed, MaxReadmissions: v.MaxReadmissions,
		ReadmissionCount: v.ReadmissionCount, CurrentRevisionID: v.CurrentRevisionID, State: domain.DCPV2TaskState(v.State),
		StateRevision: v.StateRevision, TerminalResultID: v.TerminalResultID, HumanGateQuestion: v.HumanGateQuestion,
		ErrorCode: v.ErrorCode, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func sameDCPV2Task(row gen.DcpV2Task, v domain.DCPV2Task) bool {
	return row.TaskID == v.TaskID && row.TargetSpecVersion == v.TargetSpecVersion && row.Repository == v.Repository &&
		row.RepositoryID == v.RepositoryID && row.OwnerID == v.OwnerID && row.BaseRef == v.BaseRef && row.Profile == v.Profile &&
		row.RequestDigest == v.RequestDigest && row.ScopeDigest == v.ScopeDigest && row.PolicyDigest == v.PolicyDigest &&
		row.InitialWorkerBudget == v.InitialWorkerBudget && row.RepairBudget == v.RepairBudget && row.MaxReadmissions == v.MaxReadmissions
}

func dcpV2RevisionParams(v domain.DCPV2Revision) gen.InsertDCPV2RevisionParams {
	return gen.InsertDCPV2RevisionParams{
		RevisionID: v.RevisionID, TaskID: v.TaskID, Sequence: v.Sequence, Kind: string(v.Kind), Repository: v.Repository,
		BaseRef: v.BaseRef, BaseSha: v.BaseSHA, HeadRef: v.HeadRef, HeadSha: v.HeadSHA,
		PredecessorRevisionID: v.PredecessorRevisionID, CauseCommandID: v.CauseCommandID, PRNumber: v.PRNumber,
		EvidenceDigest: v.EvidenceDigest, CreatedAt: v.CreatedAt,
	}
}

func dcpV2RevisionFromGen(v gen.DcpV2Revision) domain.DCPV2Revision {
	return domain.DCPV2Revision{
		RevisionID: v.RevisionID, TaskID: v.TaskID, Sequence: v.Sequence, Kind: domain.DCPV2RevisionKind(v.Kind),
		Repository: v.Repository, BaseRef: v.BaseRef, BaseSHA: v.BaseSha, HeadRef: v.HeadRef, HeadSHA: v.HeadSha,
		PredecessorRevisionID: v.PredecessorRevisionID, CauseCommandID: v.CauseCommandID, PRNumber: v.PRNumber,
		EvidenceDigest: v.EvidenceDigest, CreatedAt: v.CreatedAt,
	}
}

func sameDCPV2Revision(row gen.DcpV2Revision, v domain.DCPV2Revision) bool {
	return row.RevisionID == v.RevisionID && row.TaskID == v.TaskID && row.Sequence == v.Sequence && row.Kind == string(v.Kind) &&
		row.Repository == v.Repository && row.BaseRef == v.BaseRef && row.BaseSha == v.BaseSHA && row.HeadRef == v.HeadRef &&
		row.HeadSha == v.HeadSHA && row.PredecessorRevisionID == v.PredecessorRevisionID && row.CauseCommandID == v.CauseCommandID &&
		row.PRNumber == v.PRNumber && row.EvidenceDigest == v.EvidenceDigest
}

func dcpV2CommandParams(v domain.DCPV2Command) gen.InsertDCPV2CommandParams {
	return gen.InsertDCPV2CommandParams{
		CommandID: v.CommandID, TaskID: v.TaskID, RevisionID: v.RevisionID, Kind: string(v.Kind), PayloadJson: v.PayloadJSON,
		PayloadDigest: v.PayloadDigest, PrerequisiteDigest: v.PrerequisiteDigest, IdempotencyKey: v.IdempotencyKey,
		Status: string(v.Status), LeaseOwner: v.LeaseOwner, LeaseEpoch: v.LeaseEpoch, LeaseToken: v.LeaseToken,
		EffectFence: v.EffectFence, RecoveryGeneration: v.RecoveryGeneration, ResultDigest: v.ResultDigest,
		ErrorCode: v.ErrorCode, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2CommandFromGen(v gen.DcpV2Command) domain.DCPV2Command {
	return domain.DCPV2Command{
		Sequence: v.Sequence, CommandID: v.CommandID, TaskID: v.TaskID, RevisionID: v.RevisionID,
		Kind: domain.DCPV2CommandKind(v.Kind), PayloadJSON: v.PayloadJson, PayloadDigest: v.PayloadDigest,
		PrerequisiteDigest: v.PrerequisiteDigest, IdempotencyKey: v.IdempotencyKey, Status: domain.DCPV2CommandStatus(v.Status),
		LeaseOwner: v.LeaseOwner, LeaseEpoch: v.LeaseEpoch, LeaseToken: v.LeaseToken, EffectFence: v.EffectFence,
		RecoveryGeneration: v.RecoveryGeneration, ResultDigest: v.ResultDigest, ErrorCode: v.ErrorCode,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func sameDCPV2Command(row gen.DcpV2Command, v domain.DCPV2Command) bool {
	return row.CommandID == v.CommandID && row.TaskID == v.TaskID && row.RevisionID == v.RevisionID &&
		row.Kind == string(v.Kind) && row.PayloadJson == v.PayloadJSON && row.PayloadDigest == v.PayloadDigest &&
		row.PrerequisiteDigest == v.PrerequisiteDigest && row.IdempotencyKey == v.IdempotencyKey
}

func dcpV2ActionParams(v domain.DCPV2Action) gen.InsertDCPV2ActionParams {
	return gen.InsertDCPV2ActionParams{
		ActionID: v.ActionID, CommandID: v.CommandID, TaskID: v.TaskID, RevisionID: v.RevisionID, Role: string(v.Role),
		Model: v.Model, Reasoning: v.Reasoning, TokenBudget: v.TokenBudget, TimeBudgetSec: v.TimeBudgetSec,
		InputDigest: v.InputDigest, Attempt: v.Attempt, Status: string(v.Status), Slot: v.Slot,
		LaunchFence: v.LaunchFence, RuntimeID: v.RuntimeID, ResultDigest: v.ResultDigest, ErrorCode: v.ErrorCode,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2ActionFromGen(v gen.DcpV2Action) domain.DCPV2Action {
	return domain.DCPV2Action{
		Sequence: v.Sequence, ActionID: v.ActionID, CommandID: v.CommandID, TaskID: v.TaskID, RevisionID: v.RevisionID,
		Role: domain.DCPV2ActionRole(v.Role), Model: v.Model, Reasoning: v.Reasoning, TokenBudget: v.TokenBudget,
		TimeBudgetSec: v.TimeBudgetSec, InputDigest: v.InputDigest, Attempt: v.Attempt, Status: domain.DCPV2ActionStatus(v.Status),
		Slot: v.Slot, LaunchFence: v.LaunchFence, RuntimeID: v.RuntimeID, ResultDigest: v.ResultDigest, ErrorCode: v.ErrorCode,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func sameDCPV2Action(row gen.DcpV2Action, v domain.DCPV2Action) bool {
	return row.ActionID == v.ActionID && row.CommandID == v.CommandID && row.TaskID == v.TaskID &&
		row.RevisionID == v.RevisionID && row.Role == string(v.Role) && row.Model == v.Model && row.Reasoning == v.Reasoning &&
		row.TokenBudget == v.TokenBudget && row.TimeBudgetSec == v.TimeBudgetSec && row.InputDigest == v.InputDigest &&
		row.Attempt == v.Attempt
}

func dcpV2AdmissionParams(v domain.DCPV2Admission) gen.InsertDCPV2AdmissionParams {
	return gen.InsertDCPV2AdmissionParams{
		AdmissionID: v.AdmissionID, LineKey: v.LineKey, TaskID: v.TaskID, RevisionID: v.RevisionID,
		PRNumber: v.PRNumber, HeadSha: v.HeadSHA, BaseSha: v.BaseSHA, MainSha: v.MainSHA,
		RequiredCheckID: v.RequiredCheckID, ReviewID: v.ReviewID, ManifestDigest: v.ManifestDigest,
		Status: string(v.Status), LeaseOwner: v.LeaseOwner, LeaseEpoch: v.LeaseEpoch, LeaseToken: v.LeaseToken,
		DispatchFence: v.DispatchFence, RecoveryGeneration: v.RecoveryGeneration, ResultID: v.ResultID, ErrorCode: v.ErrorCode,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2AdmissionFromGen(v gen.DcpV2Admission) domain.DCPV2Admission {
	return domain.DCPV2Admission{
		Sequence: v.Sequence, AdmissionID: v.AdmissionID, LineKey: v.LineKey, TaskID: v.TaskID, RevisionID: v.RevisionID,
		PRNumber: v.PRNumber, HeadSHA: v.HeadSha, BaseSHA: v.BaseSha, MainSHA: v.MainSha,
		RequiredCheckID: v.RequiredCheckID, ReviewID: v.ReviewID, ManifestDigest: v.ManifestDigest,
		Status: domain.DCPV2AdmissionStatus(v.Status), LeaseOwner: v.LeaseOwner, LeaseEpoch: v.LeaseEpoch,
		LeaseToken: v.LeaseToken, DispatchFence: v.DispatchFence, RecoveryGeneration: v.RecoveryGeneration,
		ResultID: v.ResultID, ErrorCode: v.ErrorCode,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2ExternalEventParams(v domain.DCPV2ExternalEvent) gen.InsertDCPV2ExternalEventParams {
	return gen.InsertDCPV2ExternalEventParams{
		DeliveryID: v.DeliveryID, Provider: v.Provider, TaskID: v.TaskID, RevisionID: v.RevisionID, Kind: v.Kind,
		ProviderSequence: v.ProviderSequence, PayloadDigest: v.PayloadDigest, PrerequisiteDigest: v.PrerequisiteDigest,
		Status: v.Status, CommandID: v.CommandID, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func dcpV2ExternalEventFromGen(v gen.DcpV2ExternalEvent) domain.DCPV2ExternalEvent {
	return domain.DCPV2ExternalEvent{
		DeliveryID: v.DeliveryID, Provider: v.Provider, TaskID: v.TaskID, RevisionID: v.RevisionID, Kind: v.Kind,
		ProviderSequence: v.ProviderSequence, PayloadDigest: v.PayloadDigest, PrerequisiteDigest: v.PrerequisiteDigest,
		Status: v.Status, CommandID: v.CommandID, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func sameDCPV2ExternalEvent(row gen.DcpV2ExternalEvent, v domain.DCPV2ExternalEvent) bool {
	return row.DeliveryID == v.DeliveryID && row.Provider == v.Provider && row.TaskID == v.TaskID &&
		row.RevisionID == v.RevisionID && row.Kind == v.Kind && row.ProviderSequence == v.ProviderSequence &&
		row.PayloadDigest == v.PayloadDigest && row.PrerequisiteDigest == v.PrerequisiteDigest
}

func dcpV2IncidentParams(v domain.DCPV2Incident) gen.InsertDCPV2IncidentParams {
	return gen.InsertDCPV2IncidentParams{
		IncidentID: v.IncidentID, TaskID: v.TaskID, RevisionID: v.RevisionID, CauseCommandID: v.CauseCommandID,
		Kind: v.Kind, EvidenceDigest: v.EvidenceDigest, Disposition: string(v.Disposition),
		OwnerQuestion: v.OwnerQuestion, CreatedAt: v.CreatedAt,
	}
}

func dcpV2IncidentFromGen(v gen.DcpV2Incident) domain.DCPV2Incident {
	return domain.DCPV2Incident{
		IncidentID: v.IncidentID, TaskID: v.TaskID, RevisionID: v.RevisionID, CauseCommandID: v.CauseCommandID,
		Kind: v.Kind, EvidenceDigest: v.EvidenceDigest, Disposition: domain.DCPV2IncidentDisposition(v.Disposition),
		OwnerQuestion: v.OwnerQuestion, CreatedAt: v.CreatedAt,
	}
}

func dcpV2ResultParams(v domain.DCPV2Result) gen.InsertDCPV2ResultParams {
	admissionID := sql.NullString{String: v.AdmissionID, Valid: v.AdmissionID != ""}
	verified := int64(0)
	if v.Verified {
		verified = 1
	}
	return gen.InsertDCPV2ResultParams{
		ResultID: v.ResultID, TaskID: v.TaskID, RevisionID: v.RevisionID, AdmissionID: admissionID,
		CommandID: v.CommandID, Kind: string(v.Kind), Provider: v.Provider, ProofID: v.ProofID, RunID: v.RunID,
		Actor: v.Actor, ManifestDigest: v.ManifestDigest, ProofDigest: v.ProofDigest, MergeSha: v.MergeSHA,
		ArtifactDigest: v.ArtifactDigest, DeployedSha: v.DeployedSHA, Environment: v.Environment,
		Service: v.Service, ProbeDigest: v.ProbeDigest, Verified: verified, ErrorCode: v.ErrorCode, CreatedAt: v.CreatedAt,
	}
}

func dcpV2ResultFromGen(v gen.DcpV2Result) domain.DCPV2Result {
	admissionID := ""
	if v.AdmissionID.Valid {
		admissionID = v.AdmissionID.String
	}
	return domain.DCPV2Result{
		ResultID: v.ResultID, TaskID: v.TaskID, RevisionID: v.RevisionID, AdmissionID: admissionID,
		CommandID: v.CommandID, Kind: domain.DCPV2ResultKind(v.Kind), Provider: v.Provider, ProofID: v.ProofID,
		RunID: v.RunID, Actor: v.Actor, ManifestDigest: v.ManifestDigest, ProofDigest: v.ProofDigest,
		MergeSHA: v.MergeSha, ArtifactDigest: v.ArtifactDigest, DeployedSHA: v.DeployedSha,
		Environment: v.Environment, Service: v.Service, ProbeDigest: v.ProbeDigest,
		Verified: v.Verified == 1, ErrorCode: v.ErrorCode, CreatedAt: v.CreatedAt,
	}
}
