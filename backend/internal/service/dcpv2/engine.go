// Package dcpv2 implements the dormant provider-neutral Stage 4 command
// drain. It has no timer, watcher, heartbeat, adapter registration or live
// startup wiring: a caller may invoke Startup once or Event for one concrete
// provider delivery only after a later activation stage.
package dcpv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

var (
	ErrDrainLimit                         = errors.New("dcp v2 finite command drain limit reached")
	ErrModelRuntimeReconciliationRequired = errors.New("dcp v2 active model runtime requires exact reconciliation")
)

type Store interface {
	ClaimNextDCPV2Command(context.Context, string, string, string, time.Time) (*domain.DCPV2Command, error)
	ListLeasedDCPV2Commands(context.Context) ([]domain.DCPV2Command, error)
	RecoverDCPV2CommandLease(context.Context, domain.DCPV2Command, string, string, string, time.Time) (domain.DCPV2Command, error)
	FenceDCPV2CommandEffect(context.Context, string, string, string, string, string, time.Time) error
	TransitionDCPV2(context.Context, sqlitestore.DCPV2Transition) error
	GetDCPV2Task(context.Context, string) (domain.DCPV2Task, error)
	GetDCPV2ActionByCommand(context.Context, string) (domain.DCPV2Action, error)
	RecordDCPV2ExternalEvent(context.Context, domain.DCPV2ExternalEvent) (sqlitestore.DCPV2ExternalEventOutcome, error)
}

// Processor is target-neutral. Any external side effect must call fence with
// its immutable provider id before crossing the boundary. The persisted fence
// makes ambiguous restart reconciliation fail closed instead of retrying.
type Processor interface {
	Process(context.Context, domain.DCPV2Command, *domain.DCPV2Action, func(string) error) (Outcome, error)
}

type Outcome struct {
	NextTaskState           domain.DCPV2TaskState
	RepairIncrement         bool
	ReadmissionIncrement    bool
	TerminalResultID        string
	HumanGateQuestion       string
	TaskErrorCode           string
	CommandResultDigest     string
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
}

type IdentitySource interface {
	Token(kind, id string) string
}

type Engine struct {
	store       Store
	processor   Processor
	identities  IdentitySource
	owner       string
	epoch       string
	maxCommands int
	now         func() time.Time
}

func New(store Store, processor Processor, identities IdentitySource, owner, epoch string, maxCommands int, now func() time.Time) (*Engine, error) {
	if store == nil || processor == nil || identities == nil || owner == "" || epoch == "" || maxCommands < 1 || now == nil {
		return nil, sqlitestore.ErrDCPV2ProtocolViolation
	}
	return &Engine{store: store, processor: processor, identities: identities, owner: owner, epoch: epoch, maxCommands: maxCommands, now: now}, nil
}

// Startup performs one finite snapshot reconciliation and then invokes the
// same bounded Drain used by provider Event wakes. A leased command without an
// effect fence can be recovered exactly. A deterministic fenced effect becomes
// a steady Human Gate; a model command is reconciled against its exact durable
// Action and never relaunched after that Action crossed its launch fence.
func (e *Engine) Startup(ctx context.Context) error {
	leased, err := e.store.ListLeasedDCPV2Commands(ctx)
	if err != nil {
		return fmt.Errorf("list startup commands: %w", err)
	}
	for _, command := range leased {
		if command.Kind.ModelBacked() {
			if command.EffectFence == "" {
				recovered, err := e.store.RecoverDCPV2CommandLease(ctx, command, e.owner, e.epoch, e.identities.Token("command-recovery", command.CommandID), e.now())
				if err != nil {
					return fmt.Errorf("recover model command %s: %w", command.CommandID, err)
				}
				command = recovered
			}
			if err := e.reconcileModelCommand(ctx, command); err != nil {
				return err
			}
			continue
		}
		if command.EffectFence != "" {
			if err := e.failClosed(ctx, command, "external effect "+command.EffectFence+" requires exact reconciliation", "effect_reconciliation_required"); err != nil {
				return err
			}
			continue
		}
		recovered, err := e.store.RecoverDCPV2CommandLease(ctx, command, e.owner, e.epoch, e.identities.Token("command-recovery", command.CommandID), e.now())
		if err != nil {
			return fmt.Errorf("recover command %s: %w", command.CommandID, err)
		}
		if err := e.process(ctx, recovered, nil); err != nil {
			return err
		}
	}
	return e.Drain(ctx)
}

// Event records the immutable delivery, optionally binds it to a pending wake
// Command, and enters the same Drain. A nil wake is retained evidence only.
func (e *Engine) Event(ctx context.Context, event domain.DCPV2ExternalEvent) error {
	_, err := e.store.RecordDCPV2ExternalEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("record provider event: %w", err)
	}
	if err := e.reconcileCompletedModelCommands(ctx); err != nil {
		return err
	}
	return e.Drain(ctx)
}

// Drain handles at most maxCommands already-durable commands and never waits.
// A later startup or concrete event is the only next wake.
func (e *Engine) Drain(ctx context.Context) error {
	for handled := 0; handled < e.maxCommands; handled++ {
		command, err := e.store.ClaimNextDCPV2Command(ctx, e.owner, e.epoch, e.identities.Token("command-lease", fmt.Sprintf("%d", handled)), e.now())
		if err != nil {
			return fmt.Errorf("claim command: %w", err)
		}
		if command == nil {
			return nil
		}
		if command.Kind.ModelBacked() {
			continue
		}
		if err := e.process(ctx, *command, nil); err != nil {
			return err
		}
	}
	return ErrDrainLimit
}

func (e *Engine) process(ctx context.Context, command domain.DCPV2Command, action *domain.DCPV2Action) error {
	if command.Kind.ModelBacked() {
		if action == nil || action.CommandID != command.CommandID || action.TaskID != command.TaskID ||
			action.RevisionID != command.RevisionID || action.Status != domain.DCPV2ActionSucceeded ||
			action.LaunchFence == "" || action.LaunchFence != command.EffectFence || len(action.ResultDigest) != 64 {
			return e.failClosed(ctx, command, "model Action result does not bind the leased Command", "model_action_identity_drift")
		}
	}
	outcome, err := e.processor.Process(ctx, command, action, func(fence string) error {
		return e.store.FenceDCPV2CommandEffect(ctx, command.CommandID, command.LeaseOwner, command.LeaseEpoch, command.LeaseToken, fence, e.now())
	})
	if err != nil {
		return e.failClosed(ctx, command, "command processor failed; exact evidence is required", "processor_failed")
	}
	if action != nil && outcome.CommandResultDigest != action.ResultDigest {
		return e.failClosed(ctx, command, "model Action output digest does not match the command transition", "model_action_result_drift")
	}
	task, err := e.store.GetDCPV2Task(ctx, command.TaskID)
	if err != nil {
		return err
	}
	repairUsed := task.RepairUsed
	if outcome.RepairIncrement {
		repairUsed++
	}
	readmissions := task.ReadmissionCount
	if outcome.ReadmissionIncrement {
		readmissions++
	}
	transition := sqlitestore.DCPV2Transition{
		CommandID: command.CommandID, LeaseOwner: command.LeaseOwner, LeaseEpoch: command.LeaseEpoch, LeaseToken: command.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: outcome.NextTaskState, RepairUsed: repairUsed, ReadmissionCount: readmissions,
		TerminalResultID: outcome.TerminalResultID, HumanGateQuestion: outcome.HumanGateQuestion, TaskErrorCode: outcome.TaskErrorCode,
		CommandResultDigest: outcome.CommandResultDigest, NextRevision: outcome.NextRevision, NextCommand: outcome.NextCommand,
		NextAction: outcome.NextAction, Admission: outcome.Admission, CompleteAdmissionID: outcome.CompleteAdmissionID,
		AdmissionLeaseOwner: outcome.AdmissionLeaseOwner, AdmissionLeaseEpoch: outcome.AdmissionLeaseEpoch,
		AdmissionLeaseToken: outcome.AdmissionLeaseToken, AdmissionCompletion: outcome.AdmissionCompletion,
		AdmissionResultID: outcome.AdmissionResultID, AdmissionErrorCode: outcome.AdmissionErrorCode,
		Incident: outcome.Incident, Result: outcome.Result, ExternalEventDeliveryID: outcome.ExternalEventDeliveryID, UpdatedAt: e.now(),
	}
	if err := e.store.TransitionDCPV2(ctx, transition); err != nil {
		if failErr := e.failClosed(ctx, command, "state transition failed closed; inspect immutable command evidence", "transition_failed"); failErr != nil {
			return fmt.Errorf("transition command %s: %w (fail-closed transition: %v)", command.CommandID, err, failErr)
		}
		return fmt.Errorf("transition command %s: %w", command.CommandID, err)
	}
	return nil
}

func (e *Engine) reconcileCompletedModelCommands(ctx context.Context) error {
	leased, err := e.store.ListLeasedDCPV2Commands(ctx)
	if err != nil {
		return err
	}
	for _, command := range leased {
		if command.Kind.ModelBacked() {
			if err := e.reconcileModelCommand(ctx, command); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) reconcileModelCommand(ctx context.Context, command domain.DCPV2Command) error {
	action, err := e.store.GetDCPV2ActionByCommand(ctx, command.CommandID)
	if err != nil {
		return e.failClosed(ctx, command, "model-backed Command has no exact bounded Action", "model_action_missing")
	}
	switch action.Status {
	case domain.DCPV2ActionQueued:
		// The durable Action launch fence is still empty, so no provider
		// request crossed the boundary. A separately activated Action drain
		// may use the already-persisted command fence exactly once.
		return nil
	case domain.DCPV2ActionSucceeded:
		return e.process(ctx, command, &action)
	case domain.DCPV2ActionFailed:
		return e.failClosed(ctx, command, "bounded model Action failed and will not be retried", "model_action_failed")
	case domain.DCPV2ActionLaunching, domain.DCPV2ActionRunning:
		return ErrModelRuntimeReconciliationRequired
	default:
		return e.failClosed(ctx, command, "model Action has an invalid durable state", "model_action_state_invalid")
	}
}

func (e *Engine) failClosed(ctx context.Context, command domain.DCPV2Command, question, code string) error {
	task, err := e.store.GetDCPV2Task(ctx, command.TaskID)
	if err != nil {
		return err
	}
	at := e.now()
	evidence := sha256.Sum256([]byte(command.CommandID + "\x00" + command.RevisionID + "\x00" + code + "\x00" + question))
	incident := domain.DCPV2Incident{
		IncidentID: e.identities.Token("incident", command.CommandID+"/"+code), TaskID: command.TaskID,
		RevisionID: command.RevisionID, CauseCommandID: command.CommandID, Kind: "provider_failure",
		EvidenceDigest: hex.EncodeToString(evidence[:]), Disposition: domain.DCPV2IncidentHumanGate,
		OwnerQuestion: question, CreatedAt: at,
	}
	return e.store.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: command.CommandID, LeaseOwner: command.LeaseOwner, LeaseEpoch: command.LeaseEpoch, LeaseToken: command.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskHumanGate, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		HumanGateQuestion: question, CommandErrorCode: code, Incident: &incident, UpdatedAt: at,
	})
}
