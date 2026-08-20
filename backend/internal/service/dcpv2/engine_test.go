package dcpv2_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/dcpv2"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type identities struct{ next int }

func (i *identities) Token(kind, id string) string {
	i.next++
	return fmt.Sprintf("%s/%s/%d", kind, id, i.next)
}

type scriptedProcessor struct {
	calls      []string
	failWorker bool
	eventID    string
	now        time.Time
}

func (p *scriptedProcessor) Process(_ context.Context, command domain.DCPV2Command, action *domain.DCPV2Action, fence func(string) error) (dcpv2.Outcome, error) {
	p.calls = append(p.calls, command.CommandID)
	switch command.Kind {
	case domain.DCPV2CommandWorkerExecute:
		if p.failWorker {
			return dcpv2.Outcome{}, errors.New("model adapter failed")
		}
		if action == nil || action.Status != domain.DCPV2ActionSucceeded {
			return dcpv2.Outcome{}, errors.New("missing completed model action")
		}
		revision := &domain.DCPV2Revision{
			RevisionID: command.TaskID + "-r2", TaskID: command.TaskID, Sequence: 2, Kind: domain.DCPV2RevisionWorker,
			Repository: "owner/repo", BaseRef: "main", BaseSHA: strings.Repeat("a", 40), HeadRef: "work", HeadSHA: strings.Repeat("b", 40),
			PredecessorRevisionID: command.RevisionID, CauseCommandID: command.CommandID, PRNumber: 1,
			EvidenceDigest: strings.Repeat("e", 64), CreatedAt: p.now.Add(time.Second),
		}
		next := &domain.DCPV2Command{
			CommandID: command.TaskID + "-checks", TaskID: command.TaskID, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandChecksObserve,
			PayloadJSON: `{}`, PayloadDigest: strings.Repeat("f", 64), PrerequisiteDigest: strings.Repeat("g", 64),
			IdempotencyKey: command.TaskID + "/checks", Status: domain.DCPV2CommandPending,
			CreatedAt: p.now.Add(time.Second), UpdatedAt: p.now.Add(time.Second),
		}
		return dcpv2.Outcome{
			NextTaskState: domain.DCPV2TaskChecksWaiting, CommandResultDigest: action.ResultDigest,
			NextRevision: revision, NextCommand: next, ExternalEventDeliveryID: p.eventID,
		}, nil
	case domain.DCPV2CommandChecksObserve:
		if action != nil {
			return dcpv2.Outcome{}, errors.New("deterministic command received model action")
		}
		return dcpv2.Outcome{
			NextTaskState: domain.DCPV2TaskHumanGate, HumanGateQuestion: "bounded test stop",
			CommandResultDigest: strings.Repeat("i", 64),
		}, nil
	default:
		return dcpv2.Outcome{}, errors.New("unexpected command")
	}
}

func completeEngineAction(t *testing.T, s *sqlite.Store, commandID string, now time.Time) {
	t.Helper()
	command, err := s.GetDCPV2Command(context.Background(), commandID)
	if err != nil || command.Status != domain.DCPV2CommandLeased {
		t.Fatalf("leased model command=%+v err=%v", command, err)
	}
	fence := "model-effect-" + commandID
	if err := s.FenceDCPV2CommandEffect(context.Background(), commandID, command.LeaseOwner, command.LeaseEpoch, command.LeaseToken, fence, now); err != nil {
		t.Fatal(err)
	}
	action, err := s.ClaimNextDCPV2Action(context.Background(), fence, now.Add(time.Second))
	if err != nil || action == nil || action.CommandID != commandID {
		t.Fatalf("claim model action=%+v err=%v", action, err)
	}
	if err := s.StartDCPV2Action(context.Background(), action.ActionID, action.Slot, action.LaunchFence, "runtime-"+commandID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDCPV2Action(context.Background(), action.ActionID, action.Slot, action.LaunchFence, true, strings.Repeat("h", 64), "", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func newEngineStore(t *testing.T, id string) (*sqlite.Store, domain.DCPV2Task, domain.DCPV2Command, time.Time) {
	t.Helper()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	revision := domain.DCPV2Revision{
		RevisionID: id + "-r1", TaskID: id, Sequence: 1, Kind: domain.DCPV2RevisionWorkInput,
		Repository: "owner/repo", BaseRef: "main", BaseSHA: strings.Repeat("a", 40), HeadRef: "main", HeadSHA: strings.Repeat("a", 40),
		EvidenceDigest: strings.Repeat("b", 64), CreatedAt: now,
	}
	task := domain.DCPV2Task{
		TaskID: id, TargetSpecVersion: "target/v1", Repository: "owner/repo", RepositoryID: 1, OwnerID: 2,
		BaseRef: "main", Profile: "repo-only", RequestDigest: strings.Repeat("c", 64), ScopeDigest: strings.Repeat("d", 64),
		PolicyDigest: strings.Repeat("e", 64), InitialWorkerBudget: 1, RepairBudget: 1, MaxReadmissions: 2,
		CurrentRevisionID: revision.RevisionID, State: domain.DCPV2TaskWorkerQueued, StateRevision: 1, CreatedAt: now, UpdatedAt: now,
	}
	command := domain.DCPV2Command{
		CommandID: id + "-worker", TaskID: id, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandWorkerExecute,
		PayloadJSON: `{}`, PayloadDigest: strings.Repeat("f", 64), PrerequisiteDigest: strings.Repeat("g", 64),
		IdempotencyKey: id + "/worker", Status: domain.DCPV2CommandPending, CreatedAt: now, UpdatedAt: now,
	}
	action := domain.DCPV2Action{
		ActionID: id + "-worker-action", CommandID: command.CommandID, TaskID: task.TaskID, RevisionID: revision.RevisionID,
		Role: domain.DCPV2ActionWorker, Model: "model", Reasoning: "bounded", TokenBudget: 100, TimeBudgetSec: 60,
		InputDigest: command.PayloadDigest, Attempt: 1, Status: domain.DCPV2ActionQueued, CreatedAt: now, UpdatedAt: now,
	}
	if created, err := s.CreateDCPV2Task(context.Background(), task, revision, command, action); err != nil || !created {
		t.Fatalf("create task: %v/%v", created, err)
	}
	return s, task, command, now
}

func newEngine(t *testing.T, s *sqlite.Store, processor *scriptedProcessor, now time.Time, max int) *dcpv2.Engine {
	t.Helper()
	clock := now
	engine, err := dcpv2.New(s, processor, &identities{}, "engine", "epoch-2", max, func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestStartupRecoversUnfencedModelCommandWithoutLaunchingIt(t *testing.T) {
	s, _, command, now := newEngineStore(t, "recover-before-fence")
	old, err := s.ClaimNextDCPV2Command(context.Background(), "old", "epoch-1", "old-lease", now.Add(time.Second))
	if err != nil || old == nil {
		t.Fatalf("old claim=%+v err=%v", old, err)
	}
	processor := &scriptedProcessor{now: now.Add(10 * time.Second)}
	engine := newEngine(t, s, processor, now.Add(20*time.Second), 4)
	if err := engine.Startup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(processor.calls) != 0 {
		t.Fatalf("unfenced model Action launched during recovery: %v", processor.calls)
	}
	worker, err := s.GetDCPV2Command(context.Background(), command.CommandID)
	if err != nil || worker.RecoveryGeneration != 1 || worker.Status != domain.DCPV2CommandLeased || worker.EffectFence != "" {
		t.Fatalf("recovered worker=%+v err=%v", worker, err)
	}
	task, _ := s.GetDCPV2Task(context.Background(), command.TaskID)
	if task.State != domain.DCPV2TaskWorkerQueued || task.StateRevision != 1 {
		t.Fatalf("unfenced recovery advanced task=%+v", task)
	}
}

func TestStartupKeepsFencedButUnlaunchedActionDurablyQueued(t *testing.T) {
	s, _, command, now := newEngineStore(t, "recover-after-fence")
	old, err := s.ClaimNextDCPV2Command(context.Background(), "old", "epoch-1", "old-lease", now.Add(time.Second))
	if err != nil || old == nil {
		t.Fatal(err)
	}
	if err := s.FenceDCPV2CommandEffect(context.Background(), command.CommandID, old.LeaseOwner, old.LeaseEpoch, old.LeaseToken, "provider-effect-1", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	processor := &scriptedProcessor{now: now}
	engine := newEngine(t, s, processor, now.Add(20*time.Second), 4)
	if err := engine.Startup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(processor.calls) != 0 {
		t.Fatalf("fenced command was retried: %v", processor.calls)
	}
	task, _ := s.GetDCPV2Task(context.Background(), command.TaskID)
	action, _ := s.GetDCPV2ActionByCommand(context.Background(), command.CommandID)
	if task.State != domain.DCPV2TaskWorkerQueued || action.Status != domain.DCPV2ActionQueued || action.LaunchFence != "" {
		t.Fatalf("unlaunched fenced action drifted task=%+v action=%+v", task, action)
	}
}

func TestStartupStopsOnActiveModelRuntimeWithoutAdoptionProof(t *testing.T) {
	s, _, command, now := newEngineStore(t, "active-runtime")
	processor := &scriptedProcessor{now: now}
	engine := newEngine(t, s, processor, now.Add(10*time.Second), 4)
	if err := engine.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	leased, _ := s.GetDCPV2Command(context.Background(), command.CommandID)
	fence := "active-runtime-fence"
	if err := s.FenceDCPV2CommandEffect(context.Background(), command.CommandID, leased.LeaseOwner, leased.LeaseEpoch, leased.LeaseToken, fence, now.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	action, err := s.ClaimNextDCPV2Action(context.Background(), fence, now.Add(13*time.Second))
	if err != nil || action == nil {
		t.Fatalf("claim action=%+v err=%v", action, err)
	}
	if err := s.StartDCPV2Action(context.Background(), action.ActionID, action.Slot, action.LaunchFence, "live-runtime-id", now.Add(14*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := engine.Startup(context.Background()); !errors.Is(err, dcpv2.ErrModelRuntimeReconciliationRequired) {
		t.Fatalf("active runtime startup err=%v", err)
	}
	task, _ := s.GetDCPV2Task(context.Background(), command.TaskID)
	if task.State != domain.DCPV2TaskWorkerQueued || len(processor.calls) != 0 {
		t.Fatalf("active runtime advanced task=%+v calls=%v", task, processor.calls)
	}
}

func TestRestartAfterCommittedFenceDoesNotDuplicatePriorCommand(t *testing.T) {
	s, _, command, now := newEngineStore(t, "restart-after-commit")
	firstProcessor := &scriptedProcessor{now: now.Add(10 * time.Second)}
	first := newEngine(t, s, firstProcessor, now.Add(20*time.Second), 4)
	if err := first.Drain(context.Background()); err != nil {
		t.Fatalf("claim model command err=%v", err)
	}
	if len(firstProcessor.calls) != 0 {
		t.Fatalf("model command bypassed Action: %v", firstProcessor.calls)
	}
	completeEngineAction(t, s, command.CommandID, now.Add(22*time.Second))
	secondProcessor := &scriptedProcessor{now: now.Add(30 * time.Second)}
	second := newEngine(t, s, secondProcessor, now.Add(40*time.Second), 4)
	if err := second.Startup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(secondProcessor.calls) != 2 || secondProcessor.calls[0] != command.CommandID || secondProcessor.calls[1] != "restart-after-commit-checks" {
		t.Fatalf("restart duplicated or lost commands: %v", secondProcessor.calls)
	}
	commands, _ := s.ListDCPV2Commands(context.Background(), command.TaskID)
	if len(commands) != 2 {
		t.Fatalf("command count=%d want=2", len(commands))
	}
}

func TestProcessorFailureHasNoAutomaticRetryOrFalseSuccess(t *testing.T) {
	s, _, command, now := newEngineStore(t, "processor-failure")
	processor := &scriptedProcessor{now: now, failWorker: true}
	engine := newEngine(t, s, processor, now.Add(10*time.Second), 4)
	if err := engine.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	completeEngineAction(t, s, command.CommandID, now.Add(12*time.Second))
	if err := engine.Startup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(processor.calls) != 1 {
		t.Fatalf("processor retried %d times: %v", len(processor.calls), processor.calls)
	}
	task, _ := s.GetDCPV2Task(context.Background(), command.TaskID)
	worker, _ := s.GetDCPV2Command(context.Background(), command.CommandID)
	incidents, _ := s.ListDCPV2Incidents(context.Background(), command.TaskID)
	if task.State != domain.DCPV2TaskHumanGate || worker.Status != domain.DCPV2CommandFailed || task.TerminalResultID != "" ||
		len(incidents) != 1 || incidents[0].Disposition != domain.DCPV2IncidentHumanGate {
		t.Fatalf("false terminal success task=%+v command=%+v incidents=%+v", task, worker, incidents)
	}
}

func TestProviderEventUsesSameDrainAndBindsAtomically(t *testing.T) {
	s, task, command, now := newEngineStore(t, "event-drain")
	event := domain.DCPV2ExternalEvent{
		DeliveryID: "event-1", Provider: "github", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		Kind: "provider.wake", ProviderSequence: 1, PayloadDigest: strings.Repeat("h", 64),
		PrerequisiteDigest: command.PrerequisiteDigest, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	processor := &scriptedProcessor{now: now.Add(10 * time.Second), eventID: event.DeliveryID}
	engine := newEngine(t, s, processor, now.Add(20*time.Second), 4)
	if err := engine.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	completeEngineAction(t, s, command.CommandID, now.Add(22*time.Second))
	if err := engine.Event(context.Background(), event); err != nil {
		t.Fatalf("event drain err=%v", err)
	}
	events, err := s.ListDCPV2ExternalEvents(context.Background(), task.TaskID)
	if err != nil || len(events) != 1 || events[0].Status != "applied" || events[0].CommandID != command.CommandID {
		t.Fatalf("event binding=%+v err=%v", events, err)
	}
}
