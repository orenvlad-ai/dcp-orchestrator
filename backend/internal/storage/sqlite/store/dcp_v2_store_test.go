package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const (
	v2BaseSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	v2HeadSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func v2Digest(letter string) string { return strings.Repeat(letter, 64) }

func v2Stage5Activation(now time.Time) domain.DCPV2Stage5Activation {
	return domain.DCPV2Stage5Activation{
		ActivationID: "dcp-v2-twin-stage5", AuthorityCommit: "4143982eb054a40537d963356c209bfe8447ba31",
		SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		InstallReceiptSHA: strings.Repeat("c", 64), TargetSpecVersion: "dcp-wbc-integration-lab/v2",
		TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(),
		Repository:         "orenvlad-ai/dcp-wbc-integration-lab", RepositoryID: 1340359100, OwnerID: 237411244,
		BaseRef: "main", RequiredCheck: "baseline", IssuerKind: "dcp/v2", IssuerActor: "orenvlad-ai",
		IssuerEvent: "repository_dispatch", IssuerEventType: "dcp-admission-v2", WorkflowID: 338377713,
		Environment: "dcp-wbc-integration-lab-selectel", Service: "dcp-wbc-integration-lab",
		Adapter: "selectel-systemd/v1", ActivatedAt: now.UTC(),
	}
}

func TestDCPV2Stage5ActivationIsAtomicImmutableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	activation := v2Stage5Activation(now)
	created, err := s.ActivateDCPV2Stage5(ctx, activation)
	if err != nil || !created {
		t.Fatalf("first activation: created=%v err=%v", created, err)
	}
	activation.ActivatedAt = now.Add(time.Hour)
	created, err = s.ActivateDCPV2Stage5(ctx, activation)
	if err != nil || created {
		t.Fatalf("exact replay: created=%v err=%v", created, err)
	}
	stored, err := s.GetDCPV2Stage5Activation(ctx)
	if err != nil || !stored.ActivatedAt.Equal(now) {
		t.Fatalf("stored activation drifted: activation=%+v err=%v", stored, err)
	}
	activation.SourceTree = strings.Repeat("d", 40)
	if _, err := s.ActivateDCPV2Stage5(ctx, activation); !errors.Is(err, sqlitestore.ErrDCPV2IdentityConflict) {
		t.Fatalf("conflicting replay err=%v, want identity conflict", err)
	}
}

func TestDCPV2Stage5ActivationRefusesExistingLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _, _, now := createV2Fixture(t, s, "activation-conflict")
	if _, err := s.ActivateDCPV2Stage5(ctx, v2Stage5Activation(now)); !errors.Is(err, sqlitestore.ErrDCPV2ProtocolViolation) {
		t.Fatalf("activation with lifecycle rows err=%v, want protocol violation", err)
	}
	if _, err := s.GetDCPV2Stage5Activation(ctx); !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		t.Fatalf("failed activation left a row: %v", err)
	}
}

func actionForV2Command(command domain.DCPV2Command, now time.Time) domain.DCPV2Action {
	return domain.DCPV2Action{
		ActionID: command.CommandID + "-action", CommandID: command.CommandID, TaskID: command.TaskID, RevisionID: command.RevisionID,
		Role: roleForV2Command(command.Kind), Model: "model", Reasoning: "bounded", TokenBudget: 100, TimeBudgetSec: 60,
		InputDigest: command.PayloadDigest, Attempt: 1, Status: domain.DCPV2ActionQueued, CreatedAt: now, UpdatedAt: now,
	}
}

func roleForV2Command(kind domain.DCPV2CommandKind) domain.DCPV2ActionRole {
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

func createV2Fixture(t *testing.T, s *sqlite.Store, id string) (domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Command, time.Time) {
	return createV2FixtureWithProfile(t, s, id, "repo-only")
}

func createV2FixtureWithProfile(t *testing.T, s *sqlite.Store, id, profile string) (domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Command, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	revision := domain.DCPV2Revision{
		RevisionID: id + "-r1", TaskID: id, Sequence: 1, Kind: domain.DCPV2RevisionWorkInput,
		Repository: "owner/repo", BaseRef: "main", BaseSHA: v2BaseSHA, HeadRef: "main", HeadSHA: v2BaseSHA,
		EvidenceDigest: v2Digest("e"), CreatedAt: now,
	}
	task := domain.DCPV2Task{
		TaskID: id, TargetSpecVersion: "target/v1", Repository: "owner/repo", RepositoryID: 1, OwnerID: 2,
		BaseRef: "main", Profile: profile, RequestDigest: v2Digest("a"), ScopeDigest: v2Digest("b"),
		PolicyDigest: v2Digest("c"), InitialWorkerBudget: 1, RepairBudget: 1, MaxReadmissions: 2,
		CurrentRevisionID: revision.RevisionID, State: domain.DCPV2TaskWorkerQueued, StateRevision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	command := domain.DCPV2Command{
		CommandID: id + "-c1", TaskID: id, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandWorkerExecute,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("d"), PrerequisiteDigest: v2Digest("p"),
		IdempotencyKey: id + "/worker/1", Status: domain.DCPV2CommandPending, CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.CreateDCPV2Task(context.Background(), task, revision, command, actionForV2Command(command, now))
	if err != nil || !created {
		t.Fatalf("create fixture %s: created=%v err=%v", id, created, err)
	}
	return task, revision, command, now
}

func claimV2Command(t *testing.T, s *sqlite.Store, wantID string, now time.Time) domain.DCPV2Command {
	t.Helper()
	command, err := s.ClaimNextDCPV2Command(context.Background(), "owner", "epoch", "lease-"+wantID, now)
	if err != nil || command == nil || command.CommandID != wantID {
		t.Fatalf("claim %s: command=%+v err=%v", wantID, command, err)
	}
	return *command
}

func fenceV2Command(t *testing.T, s *sqlite.Store, command domain.DCPV2Command, now time.Time) {
	t.Helper()
	if err := s.FenceDCPV2CommandEffect(context.Background(), command.CommandID, command.LeaseOwner, command.LeaseEpoch, command.LeaseToken, "effect-"+command.CommandID, now); err != nil {
		t.Fatalf("fence %s: %v", command.CommandID, err)
	}
	command.EffectFence = "effect-" + command.CommandID
}

func finishV2ModelAction(t *testing.T, s *sqlite.Store, command domain.DCPV2Command, resultDigest string, now time.Time) {
	t.Helper()
	fence := "effect-" + command.CommandID
	action, err := s.ClaimNextDCPV2Action(context.Background(), fence, now)
	if err != nil || action == nil || action.CommandID != command.CommandID {
		t.Fatalf("claim action for %s: action=%+v err=%v", command.CommandID, action, err)
	}
	if err := s.StartDCPV2Action(context.Background(), action.ActionID, action.Slot, action.LaunchFence, "runtime-"+action.ActionID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDCPV2Action(context.Background(), action.ActionID, action.Slot, action.LaunchFence, true, resultDigest, "", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func completeWorkerV2(t *testing.T, s *sqlite.Store, task domain.DCPV2Task, command domain.DCPV2Command, now time.Time) (domain.DCPV2Task, domain.DCPV2Revision, domain.DCPV2Command) {
	t.Helper()
	leased := claimV2Command(t, s, command.CommandID, now.Add(time.Second))
	fenceV2Command(t, s, leased, now.Add(2*time.Second))
	finishV2ModelAction(t, s, leased, v2Digest("r"), now.Add(2*time.Second))
	revision := domain.DCPV2Revision{
		RevisionID: task.TaskID + "-r2", TaskID: task.TaskID, Sequence: 2, Kind: domain.DCPV2RevisionWorker,
		Repository: task.Repository, BaseRef: task.BaseRef, BaseSHA: v2BaseSHA, HeadRef: "work", HeadSHA: v2HeadSHA,
		PredecessorRevisionID: task.CurrentRevisionID, CauseCommandID: command.CommandID,
		PRNumber: 1, EvidenceDigest: v2Digest("f"), CreatedAt: now.Add(3 * time.Second),
	}
	next := domain.DCPV2Command{
		CommandID: task.TaskID + "-c2", TaskID: task.TaskID, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandChecksObserve,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("g"), PrerequisiteDigest: v2Digest("q"),
		IdempotencyKey: task.TaskID + "/checks/2", Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second),
	}
	err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskChecksWaiting, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("r"), NextRevision: &revision, NextCommand: &next, UpdatedAt: now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("complete worker: %v", err)
	}
	updated, err := s.GetDCPV2Task(context.Background(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return updated, revision, next
}

func transitionV2NoRevision(t *testing.T, s *sqlite.Store, task domain.DCPV2Task, command domain.DCPV2Command, nextState domain.DCPV2TaskState, nextKind domain.DCPV2CommandKind, repairIncrement bool, now time.Time) (domain.DCPV2Task, domain.DCPV2Command) {
	t.Helper()
	leased := claimV2Command(t, s, command.CommandID, now)
	if command.Kind.RequiresEffectFence() {
		fenceV2Command(t, s, leased, now.Add(time.Second))
	}
	if command.Kind.ModelBacked() {
		finishV2ModelAction(t, s, leased, v2Digest("t"), now.Add(time.Second))
	}
	next := domain.DCPV2Command{
		CommandID: fmt.Sprintf("%s-c%d", task.TaskID, task.StateRevision+1), TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		Kind: nextKind, PayloadJSON: `{}`, PayloadDigest: v2Digest("h"), PrerequisiteDigest: v2Digest("s"),
		IdempotencyKey: fmt.Sprintf("%s/%s/%d", task.TaskID, nextKind, task.StateRevision+1), Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	var nextAction *domain.DCPV2Action
	if next.Kind.ModelBacked() {
		action := actionForV2Command(next, next.CreatedAt)
		nextAction = &action
	}
	repairUsed := task.RepairUsed
	if repairIncrement {
		repairUsed++
	}
	err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: nextState, RepairUsed: repairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("t"), NextCommand: &next, NextAction: nextAction, UpdatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("transition %s -> %s: %v", task.State, nextState, err)
	}
	updated, err := s.GetDCPV2Task(context.Background(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return updated, next
}

func TestDCPV2CreateIsAtomicAndIdempotent(t *testing.T) {
	s := newTestStore(t)
	task, revision, command, _ := createV2Fixture(t, s, "atomic")
	action := actionForV2Command(command, command.CreatedAt)
	created, err := s.CreateDCPV2Task(context.Background(), task, revision, command, action)
	if err != nil || created {
		t.Fatalf("exact replay created=%v err=%v", created, err)
	}
	retryTask, retryRevision, retryCommand, retryAction := task, revision, command, action
	retryTask.CreatedAt, retryTask.UpdatedAt = task.CreatedAt.Add(time.Minute), task.UpdatedAt.Add(time.Minute)
	retryRevision.CreatedAt = revision.CreatedAt.Add(time.Minute)
	retryCommand.CreatedAt, retryCommand.UpdatedAt = command.CreatedAt.Add(time.Minute), command.UpdatedAt.Add(time.Minute)
	retryAction.CreatedAt, retryAction.UpdatedAt = action.CreatedAt.Add(time.Minute), action.UpdatedAt.Add(time.Minute)
	if created, err := s.CreateDCPV2Task(context.Background(), retryTask, retryRevision, retryCommand, retryAction); err != nil || created {
		t.Fatalf("equal submit retry with new transport timestamps created=%v err=%v", created, err)
	}
	task.PolicyDigest = v2Digest("z")
	if _, err := s.CreateDCPV2Task(context.Background(), task, revision, command, action); !errors.Is(err, sqlitestore.ErrDCPV2IdentityConflict) {
		t.Fatalf("conflicting replay err=%v", err)
	}

	badTask := task
	badTask.TaskID, badTask.CurrentRevisionID = "rollback", "rollback-r1"
	badTask.PolicyDigest = v2Digest("c")
	badRevision := revision
	badRevision.TaskID, badRevision.RevisionID = badTask.TaskID, badTask.CurrentRevisionID
	badCommand := command
	badCommand.TaskID, badCommand.RevisionID, badCommand.CommandID, badCommand.IdempotencyKey = badTask.TaskID, badRevision.RevisionID, "rollback-c1", "rollback/worker/1"
	badCommand.PayloadJSON = `not-json`
	badAction := actionForV2Command(badCommand, badCommand.CreatedAt)
	if _, err := s.CreateDCPV2Task(context.Background(), badTask, badRevision, badCommand, badAction); err == nil {
		t.Fatal("malformed command unexpectedly committed")
	}
	if _, err := s.GetDCPV2Task(context.Background(), badTask.TaskID); !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		t.Fatalf("rolled-back task survived: %v", err)
	}
}

func TestDCPV2TransitionCommitsStateRevisionAndNextCommandTogether(t *testing.T) {
	s := newTestStore(t)
	task, initialRevision, command, now := createV2Fixture(t, s, "transition")
	updated, revision, next := completeWorkerV2(t, s, task, command, now)
	if updated.State != domain.DCPV2TaskChecksWaiting || updated.StateRevision != 2 || updated.CurrentRevisionID != revision.RevisionID {
		t.Fatalf("updated task=%+v", updated)
	}
	commands, err := s.ListDCPV2Commands(context.Background(), task.TaskID)
	if err != nil || len(commands) != 2 || commands[0].Status != domain.DCPV2CommandSucceeded || commands[1].CommandID != next.CommandID || commands[1].Status != domain.DCPV2CommandPending {
		t.Fatalf("commands=%+v err=%v", commands, err)
	}
	revisions, err := s.ListDCPV2Revisions(context.Background(), task.TaskID)
	if err != nil || len(revisions) != 2 || revisions[1].PredecessorRevisionID != revisions[0].RevisionID {
		t.Fatalf("revisions=%+v err=%v", revisions, err)
	}
	if created, err := s.CreateDCPV2Task(context.Background(), task, initialRevision, command, actionForV2Command(command, command.CreatedAt)); err != nil || created {
		t.Fatalf("immutable submit replay after progress created=%v err=%v", created, err)
	}
}

func TestDCPV2TransitionRollbackLosesNothingAndDuplicatesNothing(t *testing.T) {
	s := newTestStore(t)
	task, _, command, now := createV2Fixture(t, s, "tx-rollback")
	leased := claimV2Command(t, s, command.CommandID, now.Add(time.Second))
	fenceV2Command(t, s, leased, now.Add(2*time.Second))
	finishV2ModelAction(t, s, leased, v2Digest("x"), now.Add(2*time.Second))
	revision := domain.DCPV2Revision{
		RevisionID: "tx-rollback-r2", TaskID: task.TaskID, Sequence: 2, Kind: domain.DCPV2RevisionWorker,
		Repository: task.Repository, BaseRef: task.BaseRef, BaseSHA: v2BaseSHA, HeadRef: "work", HeadSHA: v2HeadSHA,
		PredecessorRevisionID: task.CurrentRevisionID, CauseCommandID: command.CommandID, EvidenceDigest: v2Digest("u"), CreatedAt: now.Add(3 * time.Second),
	}
	next := domain.DCPV2Command{
		CommandID: "tx-rollback-c2", TaskID: task.TaskID, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandChecksObserve,
		PayloadJSON: `bad-json`, PayloadDigest: v2Digest("v"), PrerequisiteDigest: v2Digest("w"), IdempotencyKey: "tx-rollback/checks/2",
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second),
	}
	err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskChecksWaiting, CommandResultDigest: v2Digest("x"), NextRevision: &revision, NextCommand: &next,
		UpdatedAt: now.Add(3 * time.Second),
	})
	if err == nil {
		t.Fatal("malformed successor unexpectedly committed")
	}
	gotTask, _ := s.GetDCPV2Task(context.Background(), task.TaskID)
	gotCommand, _ := s.GetDCPV2Command(context.Background(), command.CommandID)
	revisions, _ := s.ListDCPV2Revisions(context.Background(), task.TaskID)
	if gotTask.StateRevision != 1 || gotCommand.Status != domain.DCPV2CommandLeased || len(revisions) != 1 {
		t.Fatalf("rollback drift task=%+v command=%+v revisions=%d", gotTask, gotCommand, len(revisions))
	}
}

func TestDCPV2GlobalThreeActionSlots(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("slot-%d", i)
		_, _, command, _ := createV2Fixture(t, s, id)
		leased := claimV2Command(t, s, command.CommandID, now.Add(time.Duration(i)*time.Second))
		fenceV2Command(t, s, leased, now.Add(time.Duration(i+4)*time.Second))
	}
	claimed := make([]domain.DCPV2Action, 0, 3)
	for i := 1; i <= 3; i++ {
		action, err := s.ClaimNextDCPV2Action(ctx, fmt.Sprintf("effect-slot-%d-c1", i), now.Add(time.Duration(i)*time.Second))
		if err != nil || action == nil || action.Slot != int64(i) {
			t.Fatalf("claim %d action=%+v err=%v", i, action, err)
		}
		claimed = append(claimed, *action)
	}
	if action, err := s.ClaimNextDCPV2Action(ctx, "effect-slot-4-c1", now.Add(4*time.Second)); err != nil || action != nil {
		t.Fatalf("fourth action crossed global ceiling: action=%+v err=%v", action, err)
	}
	first := claimed[0]
	if err := s.StartDCPV2Action(ctx, first.ActionID, first.Slot, first.LaunchFence, "runtime-1", now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDCPV2Action(ctx, first.ActionID, first.Slot, first.LaunchFence, true, v2Digest("j"), "", now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if action, err := s.ClaimNextDCPV2Action(ctx, "effect-slot-4-c1", now.Add(7*time.Second)); err != nil || action == nil || action.ActionID != "slot-4-c1-action" || action.Slot != 1 {
		t.Fatalf("released slot not reused FIFO: action=%+v err=%v", action, err)
	}
}

func TestDCPV2ActionCannotLaunchBeforeExactCommandFence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, command, now := createV2Fixture(t, s, "action-fence")
	if action, err := s.ClaimNextDCPV2Action(ctx, "effect-action-fence-c1", now.Add(time.Second)); err != nil || action != nil {
		t.Fatalf("pending command exposed action=%+v err=%v", action, err)
	}
	leased := claimV2Command(t, s, command.CommandID, now.Add(2*time.Second))
	if action, err := s.ClaimNextDCPV2Action(ctx, "effect-action-fence-c1", now.Add(3*time.Second)); err != nil || action != nil {
		t.Fatalf("unfenced command exposed action=%+v err=%v", action, err)
	}
	fenceV2Command(t, s, leased, now.Add(4*time.Second))
	if action, err := s.ClaimNextDCPV2Action(ctx, "wrong-fence", now.Add(5*time.Second)); err != nil || action != nil {
		t.Fatalf("wrong fence exposed action=%+v err=%v", action, err)
	}
	action, err := s.ClaimNextDCPV2Action(ctx, "effect-action-fence-c1", now.Add(6*time.Second))
	if err != nil || action == nil || action.CommandID != command.CommandID {
		t.Fatalf("exact fence did not launch action=%+v err=%v", action, err)
	}
}

func TestDCPV2ProviderEventReplayAndExactBinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task, _, command, now := createV2Fixture(t, s, "event")
	task, _, checks := completeWorkerV2(t, s, task, command, now)
	event := domain.DCPV2ExternalEvent{
		DeliveryID: "delivery-1", Provider: "github", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		Kind: "check.completed", ProviderSequence: 1, PayloadDigest: v2Digest("k"), PrerequisiteDigest: checks.PrerequisiteDigest,
		CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second),
	}
	if outcome, err := s.RecordDCPV2ExternalEvent(ctx, event); err != nil || !outcome.Created {
		t.Fatalf("record event outcome=%+v err=%v", outcome, err)
	}
	if outcome, err := s.RecordDCPV2ExternalEvent(ctx, event); err != nil || outcome.Created {
		t.Fatalf("duplicate event outcome=%+v err=%v", outcome, err)
	}
	drift := event
	drift.PayloadDigest = v2Digest("l")
	if _, err := s.RecordDCPV2ExternalEvent(ctx, drift); !errors.Is(err, sqlitestore.ErrDCPV2ExternalEventDrift) {
		t.Fatalf("conflicting duplicate err=%v", err)
	}
	stale := event
	stale.DeliveryID, stale.ProviderSequence, stale.RevisionID = "delivery-stale", 2, "unknown-revision"
	if _, err := s.RecordDCPV2ExternalEvent(ctx, stale); err != nil {
		t.Fatal(err)
	}
	leased := claimV2Command(t, s, checks.CommandID, now.Add(5*time.Second))
	next := domain.DCPV2Command{
		CommandID: "event-c3", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandReviewExecute,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("m"), PrerequisiteDigest: v2Digest("n"), IdempotencyKey: "event/review/2",
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(6 * time.Second), UpdatedAt: now.Add(6 * time.Second),
	}
	nextAction := actionForV2Command(next, next.CreatedAt)
	err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskReviewQueued, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("o"), ExternalEventDeliveryID: event.DeliveryID,
		NextCommand: &next, NextAction: &nextAction, UpdatedAt: now.Add(6 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.ListDCPV2ExternalEvents(ctx, task.TaskID)
	if err != nil || len(events) != 2 || events[0].Status != "applied" || events[0].CommandID != checks.CommandID || events[1].Status != "retained" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestDCPV2SharedRepairAllowanceIsTaskLevelAndFinite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task, _, worker, now := createV2Fixture(t, s, "repair-ceiling")
	task, _, checks := completeWorkerV2(t, s, task, worker, now)
	task, review := transitionV2NoRevision(t, s, task, checks, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(10*time.Second))
	task, repair := transitionV2NoRevision(t, s, task, review, domain.DCPV2TaskRepairQueued, domain.DCPV2CommandRepairExecute, true, now.Add(20*time.Second))
	leasedRepair := claimV2Command(t, s, repair.CommandID, now.Add(30*time.Second))
	fenceV2Command(t, s, leasedRepair, now.Add(31*time.Second))
	finishV2ModelAction(t, s, leasedRepair, v2Digest("c"), now.Add(31*time.Second))
	revision := domain.DCPV2Revision{
		RevisionID: "repair-ceiling-r3", TaskID: task.TaskID, Sequence: 3, Kind: domain.DCPV2RevisionRepair,
		Repository: task.Repository, BaseRef: task.BaseRef, BaseSHA: v2BaseSHA, HeadRef: "repair", HeadSHA: strings.Repeat("c", 40),
		PredecessorRevisionID: task.CurrentRevisionID, CauseCommandID: repair.CommandID, PRNumber: 1,
		EvidenceDigest: v2Digest("y"), CreatedAt: now.Add(32 * time.Second),
	}
	nextChecks := domain.DCPV2Command{
		CommandID: "repair-ceiling-c5", TaskID: task.TaskID, RevisionID: revision.RevisionID, Kind: domain.DCPV2CommandChecksObserve,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("a"), PrerequisiteDigest: v2Digest("b"), IdempotencyKey: "repair-ceiling/checks/3",
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(32 * time.Second), UpdatedAt: now.Add(32 * time.Second),
	}
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leasedRepair.CommandID, LeaseOwner: leasedRepair.LeaseOwner, LeaseEpoch: leasedRepair.LeaseEpoch, LeaseToken: leasedRepair.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskChecksWaiting, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("c"), NextRevision: &revision, NextCommand: &nextChecks, UpdatedAt: now.Add(32 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	task, secondReview := transitionV2NoRevision(t, s, task, nextChecks, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(40*time.Second))
	leasedReview := claimV2Command(t, s, secondReview.CommandID, now.Add(50*time.Second))
	fenceV2Command(t, s, leasedReview, now.Add(51*time.Second))
	finishV2ModelAction(t, s, leasedReview, v2Digest("f"), now.Add(51*time.Second))
	secondRepair := domain.DCPV2Command{
		CommandID: "repair-ceiling-forbidden", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandRepairExecute,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("d"), PrerequisiteDigest: v2Digest("e"), IdempotencyKey: "repair-ceiling/repair/2",
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(52 * time.Second), UpdatedAt: now.Add(52 * time.Second),
	}
	err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leasedReview.CommandID, LeaseOwner: leasedReview.LeaseOwner, LeaseEpoch: leasedReview.LeaseEpoch, LeaseToken: leasedReview.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskRepairQueued, RepairUsed: task.RepairUsed + 1, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("f"), NextCommand: &secondRepair, UpdatedAt: now.Add(52 * time.Second),
	})
	if !errors.Is(err, sqlitestore.ErrDCPV2BudgetExhausted) {
		t.Fatalf("second repair err=%v", err)
	}
	unchanged, _ := s.GetDCPV2Task(ctx, task.TaskID)
	if unchanged.State != domain.DCPV2TaskReviewQueued || unchanged.RepairUsed != 1 {
		t.Fatalf("repair ceiling transition partially committed: %+v", unchanged)
	}
}

func TestDCPV2ArbiterCanOnlyOpenTypedSteadyHumanGate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task, _, worker, now := createV2Fixture(t, s, "arbiter-gate")
	task, _, checks := completeWorkerV2(t, s, task, worker, now)
	task, review := transitionV2NoRevision(t, s, task, checks, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(10*time.Second))
	leasedReview := claimV2Command(t, s, review.CommandID, now.Add(20*time.Second))
	fenceV2Command(t, s, leasedReview, now.Add(21*time.Second))
	finishV2ModelAction(t, s, leasedReview, v2Digest("t"), now.Add(21*time.Second))
	arbiter := domain.DCPV2Command{
		CommandID: task.TaskID + "-arbiter", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandArbiterExecute,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("a"), PrerequisiteDigest: v2Digest("b"), IdempotencyKey: task.TaskID + "/arbiter",
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(22 * time.Second), UpdatedAt: now.Add(22 * time.Second),
	}
	arbiterAction := actionForV2Command(arbiter, arbiter.CreatedAt)
	incident := domain.DCPV2Incident{
		IncidentID: task.TaskID + "-incident", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		CauseCommandID: review.CommandID, Kind: "policy_ambiguity", EvidenceDigest: v2Digest("i"),
		Disposition: domain.DCPV2IncidentArbiter, CreatedAt: now.Add(22 * time.Second),
	}
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leasedReview.CommandID, LeaseOwner: leasedReview.LeaseOwner, LeaseEpoch: leasedReview.LeaseEpoch, LeaseToken: leasedReview.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskArbiterQueued, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("t"), NextCommand: &arbiter, NextAction: &arbiterAction,
		Incident: &incident, UpdatedAt: now.Add(22 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	leased := claimV2Command(t, s, arbiter.CommandID, now.Add(30*time.Second))
	fenceV2Command(t, s, leased, now.Add(31*time.Second))
	finishV2ModelAction(t, s, leased, v2Digest("g"), now.Add(31*time.Second))
	question := "Choose whether this exact immutable conflict may consume the shared repair allowance."
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskHumanGate, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		HumanGateQuestion: question, CommandResultDigest: v2Digest("g"), UpdatedAt: now.Add(32 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	commands, _ := s.ListDCPV2Commands(ctx, task.TaskID)
	incidents, _ := s.ListDCPV2Incidents(ctx, task.TaskID)
	if task.State != domain.DCPV2TaskHumanGate || task.HumanGateQuestion != question || len(commands) != 4 ||
		commands[len(commands)-1].Status != domain.DCPV2CommandSucceeded || len(incidents) != 1 ||
		incidents[0].Disposition != domain.DCPV2IncidentArbiter || incidents[0].CauseCommandID != review.CommandID {
		t.Fatalf("typed Human Gate task=%+v commands=%+v incidents=%+v", task, commands, incidents)
	}
}

func enqueueAdmissionV2(t *testing.T, s *sqlite.Store, task domain.DCPV2Task, command domain.DCPV2Command, suffix string, now time.Time) domain.DCPV2Admission {
	t.Helper()
	leased := claimV2Command(t, s, command.CommandID, now)
	admission := domain.DCPV2Admission{
		AdmissionID: "admission-" + suffix, LineKey: "owner/repo:main", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		PRNumber: 1, HeadSHA: v2HeadSHA, BaseSHA: v2BaseSHA, MainSHA: v2BaseSHA,
		RequiredCheckID: "baseline/run", ReviewID: "review-" + suffix, ManifestDigest: v2Digest(suffix),
		Status: domain.DCPV2AdmissionWaiting, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	next := domain.DCPV2Command{
		CommandID: task.TaskID + "-release-" + suffix, TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandReleaseDispatch,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("n"), PrerequisiteDigest: v2Digest("o"), IdempotencyKey: task.TaskID + "/release/" + suffix,
		Status: domain.DCPV2CommandPending, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	if err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskReleaseWaiting, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("p"), NextCommand: &next, Admission: &admission, UpdatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("enqueue admission: %v", err)
	}
	return admission
}

func TestDCPV2AdmissionLeaseIsDurableFIFO(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task1, _, worker1, now := createV2Fixture(t, s, "fifo-1")
	task2, _, worker2, _ := createV2Fixture(t, s, "fifo-2")
	task1, _, checks1 := completeWorkerV2(t, s, task1, worker1, now)
	task2, _, checks2 := completeWorkerV2(t, s, task2, worker2, now)
	task1, review1 := transitionV2NoRevision(t, s, task1, checks1, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(10*time.Second))
	task2, review2 := transitionV2NoRevision(t, s, task2, checks2, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(11*time.Second))
	task1, admissionCommand1 := transitionV2NoRevision(t, s, task1, review1, domain.DCPV2TaskAdmissionWaiting, domain.DCPV2CommandAdmissionEnqueue, false, now.Add(20*time.Second))
	task2, admissionCommand2 := transitionV2NoRevision(t, s, task2, review2, domain.DCPV2TaskAdmissionWaiting, domain.DCPV2CommandAdmissionEnqueue, false, now.Add(21*time.Second))
	admission1 := enqueueAdmissionV2(t, s, task1, admissionCommand1, "1", now.Add(30*time.Second))
	admission2 := enqueueAdmissionV2(t, s, task2, admissionCommand2, "2", now.Add(31*time.Second))

	first, err := s.ClaimNextDCPV2Admission(ctx, admission1.LineKey, "owner-1", "epoch-1", "fifo-lease-1", now.Add(40*time.Second))
	if err != nil || first == nil || first.AdmissionID != admission1.AdmissionID {
		t.Fatalf("first admission=%+v err=%v", first, err)
	}
	if second, err := s.ClaimNextDCPV2Admission(ctx, admission1.LineKey, "owner-2", "epoch-2", "fifo-lease-2", now.Add(41*time.Second)); err != nil || second != nil {
		t.Fatalf("second crossed durable lease: %+v err=%v", second, err)
	}
	recovered, err := s.RecoverDCPV2AdmissionLease(ctx, *first, "owner-r", "epoch-r", "fifo-lease-r", now.Add(42*time.Second))
	if err != nil || recovered.RecoveryGeneration != 1 {
		t.Fatalf("recover unfenced admission=%+v err=%v", recovered, err)
	}
	first = &recovered
	if err := s.FenceDCPV2AdmissionDispatch(ctx, first.AdmissionID, first.LeaseOwner, first.LeaseEpoch, first.LeaseToken, "dispatch-1", now.Add(43*time.Second)); err != nil {
		t.Fatal(err)
	}
	first.DispatchFence = "dispatch-1"
	if _, err := s.RecoverDCPV2AdmissionLease(ctx, *first, "owner-x", "epoch-x", "fifo-lease-x", now.Add(44*time.Second)); !errors.Is(err, sqlitestore.ErrDCPV2EffectFenced) {
		t.Fatalf("fenced admission recovered: %v", err)
	}
	if err := s.DispatchDCPV2Admission(ctx, first.AdmissionID, first.LeaseOwner, first.LeaseEpoch, first.LeaseToken, now.Add(45*time.Second)); err != nil {
		t.Fatal(err)
	}
	if second, err := s.ClaimNextDCPV2Admission(ctx, admission1.LineKey, "owner-2", "epoch-2", "fifo-lease-2", now.Add(46*time.Second)); err != nil || second != nil {
		t.Fatalf("second crossed dispatched fence: %+v err=%v", second, err)
	}
	if err := s.FinishDCPV2Admission(ctx, first.AdmissionID, first.LeaseOwner, first.LeaseEpoch, first.LeaseToken, domain.DCPV2AdmissionFailed, "", "bounded-test-stop", now.Add(47*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimNextDCPV2Admission(ctx, admission1.LineKey, "owner-2", "epoch-2", "fifo-lease-2", now.Add(48*time.Second))
	if err != nil || second == nil || second.AdmissionID != admission2.AdmissionID {
		t.Fatalf("second admission=%+v err=%v", second, err)
	}
}

func prepareV2ReleaseObservation(t *testing.T, s *sqlite.Store, id, profile, suffix string) (domain.DCPV2Task, domain.DCPV2Admission, domain.DCPV2Command, time.Time) {
	t.Helper()
	task, _, worker, now := createV2FixtureWithProfile(t, s, id, profile)
	task, _, checks := completeWorkerV2(t, s, task, worker, now)
	task, review := transitionV2NoRevision(t, s, task, checks, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now.Add(10*time.Second))
	task, admissionCommand := transitionV2NoRevision(t, s, task, review, domain.DCPV2TaskAdmissionWaiting, domain.DCPV2CommandAdmissionEnqueue, false, now.Add(20*time.Second))
	admission := enqueueAdmissionV2(t, s, task, admissionCommand, suffix, now.Add(30*time.Second))
	task, err := s.GetDCPV2Task(context.Background(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := s.ListDCPV2Commands(context.Background(), task.TaskID)
	if err != nil || len(commands) == 0 {
		t.Fatalf("release commands=%+v err=%v", commands, err)
	}
	releaseCommand := commands[len(commands)-1]
	leasedAdmission, err := s.ClaimNextDCPV2Admission(context.Background(), admission.LineKey, "release-owner", "release-epoch", "release-token-"+suffix, now.Add(32*time.Second))
	if err != nil || leasedAdmission == nil || leasedAdmission.AdmissionID != admission.AdmissionID {
		t.Fatalf("lease admission=%+v err=%v", leasedAdmission, err)
	}
	if err := s.FenceDCPV2AdmissionDispatch(context.Background(), admission.AdmissionID, leasedAdmission.LeaseOwner, leasedAdmission.LeaseEpoch, leasedAdmission.LeaseToken, "dispatch-"+suffix, now.Add(33*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchDCPV2Admission(context.Background(), admission.AdmissionID, leasedAdmission.LeaseOwner, leasedAdmission.LeaseEpoch, leasedAdmission.LeaseToken, now.Add(34*time.Second)); err != nil {
		t.Fatal(err)
	}
	leasedAdmission.Status = domain.DCPV2AdmissionDispatched
	leasedAdmission.DispatchFence = "dispatch-" + suffix
	task, mergeCommand := transitionV2NoRevision(t, s, task, releaseCommand, domain.DCPV2TaskMergeObserving, domain.DCPV2CommandMergeObserve, false, now.Add(35*time.Second))
	return task, *leasedAdmission, mergeCommand, now
}

func advanceV2ChecksToMergeObservation(t *testing.T, s *sqlite.Store, task domain.DCPV2Task, checks domain.DCPV2Command, suffix string, now time.Time) (domain.DCPV2Task, domain.DCPV2Admission, domain.DCPV2Command) {
	t.Helper()
	task, review := transitionV2NoRevision(t, s, task, checks, domain.DCPV2TaskReviewQueued, domain.DCPV2CommandReviewExecute, false, now)
	task, admissionCommand := transitionV2NoRevision(t, s, task, review, domain.DCPV2TaskAdmissionWaiting, domain.DCPV2CommandAdmissionEnqueue, false, now.Add(10*time.Second))
	admission := enqueueAdmissionV2(t, s, task, admissionCommand, suffix, now.Add(20*time.Second))
	task, err := s.GetDCPV2Task(context.Background(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := s.ListDCPV2Commands(context.Background(), task.TaskID)
	if err != nil || len(commands) == 0 {
		t.Fatalf("commands=%+v err=%v", commands, err)
	}
	releaseCommand := commands[len(commands)-1]
	leasedAdmission, err := s.ClaimNextDCPV2Admission(context.Background(), admission.LineKey, "generation-owner", "generation-epoch", "generation-token-"+suffix, now.Add(22*time.Second))
	if err != nil || leasedAdmission == nil || leasedAdmission.AdmissionID != admission.AdmissionID {
		t.Fatalf("generation admission=%+v err=%v", leasedAdmission, err)
	}
	if err := s.FenceDCPV2AdmissionDispatch(context.Background(), admission.AdmissionID, leasedAdmission.LeaseOwner, leasedAdmission.LeaseEpoch, leasedAdmission.LeaseToken, "generation-dispatch-"+suffix, now.Add(23*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchDCPV2Admission(context.Background(), admission.AdmissionID, leasedAdmission.LeaseOwner, leasedAdmission.LeaseEpoch, leasedAdmission.LeaseToken, now.Add(24*time.Second)); err != nil {
		t.Fatal(err)
	}
	leasedAdmission.Status = domain.DCPV2AdmissionDispatched
	leasedAdmission.DispatchFence = "generation-dispatch-" + suffix
	task, mergeCommand := transitionV2NoRevision(t, s, task, releaseCommand, domain.DCPV2TaskMergeObserving, domain.DCPV2CommandMergeObserve, false, now.Add(25*time.Second))
	return task, *leasedAdmission, mergeCommand
}

func materializeV2Readmission(t *testing.T, s *sqlite.Store, task domain.DCPV2Task, admission domain.DCPV2Admission, mergeCommand domain.DCPV2Command, generation int64, now time.Time) (domain.DCPV2Task, domain.DCPV2Command) {
	t.Helper()
	merge := claimV2Command(t, s, mergeCommand.CommandID, now)
	readmission := domain.DCPV2Command{
		CommandID: fmt.Sprintf("%s-readmission-%d", task.TaskID, generation), TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		Kind: domain.DCPV2CommandReadmission, PayloadJSON: `{}`, PayloadDigest: v2Digest("j"), PrerequisiteDigest: v2Digest("k"),
		IdempotencyKey: fmt.Sprintf("%s/readmission/%d", task.TaskID, generation), Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	if err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: merge.CommandID, LeaseOwner: merge.LeaseOwner, LeaseEpoch: merge.LeaseEpoch, LeaseToken: merge.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskReadmission, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount + 1,
		CommandResultDigest: v2Digest("l"), NextCommand: &readmission,
		CompleteAdmissionID: admission.AdmissionID, AdmissionLeaseOwner: admission.LeaseOwner,
		AdmissionLeaseEpoch: admission.LeaseEpoch, AdmissionLeaseToken: admission.LeaseToken,
		AdmissionCompletion: domain.DCPV2AdmissionReadmissionRequired, UpdatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("generation %d enter readmission: %v task=%+v command=%+v", generation, err, task, merge)
	}
	task, _ = s.GetDCPV2Task(context.Background(), task.TaskID)
	leased := claimV2Command(t, s, readmission.CommandID, now.Add(2*time.Second))
	fenceV2Command(t, s, leased, now.Add(3*time.Second))
	revisions, _ := s.ListDCPV2Revisions(context.Background(), task.TaskID)
	headLetters := []string{"e", "f"}
	revision := domain.DCPV2Revision{
		RevisionID: fmt.Sprintf("%s-r%d", task.TaskID, len(revisions)+1), TaskID: task.TaskID, Sequence: int64(len(revisions) + 1),
		Kind: domain.DCPV2RevisionReadmission, Repository: task.Repository, BaseRef: task.BaseRef,
		BaseSHA: strings.Repeat("c", 40), HeadRef: "work", HeadSHA: strings.Repeat(headLetters[generation-1], 40),
		PredecessorRevisionID: task.CurrentRevisionID, CauseCommandID: readmission.CommandID, PRNumber: 1,
		EvidenceDigest: v2Digest("m"), CreatedAt: now.Add(4 * time.Second),
	}
	checks := domain.DCPV2Command{
		CommandID: fmt.Sprintf("%s-checks-generation-%d", task.TaskID, generation), TaskID: task.TaskID, RevisionID: revision.RevisionID,
		Kind: domain.DCPV2CommandChecksObserve, PayloadJSON: `{}`, PayloadDigest: v2Digest("n"), PrerequisiteDigest: v2Digest("o"),
		IdempotencyKey: fmt.Sprintf("%s/checks-generation/%d", task.TaskID, generation), Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second),
	}
	if err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskChecksWaiting, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: v2Digest("p"), NextRevision: &revision, NextCommand: &checks, UpdatedAt: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("generation %d materialize: %v task=%+v command=%+v", generation, err, task, leased)
	}
	task, _ = s.GetDCPV2Task(context.Background(), task.TaskID)
	return task, checks
}

func TestDCPV2ReadmissionGenerationsAreFiniteAndNeverCreateSecondWorker(t *testing.T) {
	s := newTestStore(t)
	task, admission, mergeCommand, now := prepareV2ReleaseObservation(t, s, "readmission-finite", "repo-only", "5")
	for generation := int64(1); generation <= 2; generation++ {
		var checks domain.DCPV2Command
		task, checks = materializeV2Readmission(t, s, task, admission, mergeCommand, generation, now.Add(time.Duration(generation)*100*time.Second))
		task, admission, mergeCommand = advanceV2ChecksToMergeObservation(t, s, task, checks, fmt.Sprint(generation+5), now.Add(time.Duration(generation)*100*time.Second+10*time.Second))
	}
	merge := claimV2Command(t, s, mergeCommand.CommandID, now.Add(400*time.Second))
	forbidden := domain.DCPV2Command{
		CommandID: task.TaskID + "-readmission-3", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID,
		Kind: domain.DCPV2CommandReadmission, PayloadJSON: `{}`, PayloadDigest: v2Digest("q"), PrerequisiteDigest: v2Digest("r"),
		IdempotencyKey: task.TaskID + "/readmission/3", Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(401 * time.Second), UpdatedAt: now.Add(401 * time.Second),
	}
	err := s.TransitionDCPV2(context.Background(), sqlitestore.DCPV2Transition{
		CommandID: merge.CommandID, LeaseOwner: merge.LeaseOwner, LeaseEpoch: merge.LeaseEpoch, LeaseToken: merge.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskReadmission, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount + 1,
		CommandResultDigest: v2Digest("s"), NextCommand: &forbidden, UpdatedAt: now.Add(401 * time.Second),
	})
	if !errors.Is(err, sqlitestore.ErrDCPV2BudgetExhausted) {
		t.Fatalf("third readmission err=%v", err)
	}
	actions, _ := s.ListDCPV2Actions(context.Background(), task.TaskID)
	revisions, _ := s.ListDCPV2Revisions(context.Background(), task.TaskID)
	workerCount := 0
	for _, action := range actions {
		if action.Role == domain.DCPV2ActionWorker {
			workerCount++
		}
	}
	if task.ReadmissionCount != 2 || len(revisions) != 4 || workerCount != 1 {
		t.Fatalf("finite readmission task=%+v revisions=%d workerActions=%d", task, len(revisions), workerCount)
	}
}

func TestDCPV2VerifiedReleaseAndTerminalVerificationAreExactAndAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task, admission, mergeCommand, now := prepareV2ReleaseObservation(t, s, "repo-terminal", "repo-only", "3")
	leased := claimV2Command(t, s, mergeCommand.CommandID, now.Add(50*time.Second))
	result := domain.DCPV2Result{
		ResultID: "repo-release-result", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, AdmissionID: admission.AdmissionID,
		CommandID: mergeCommand.CommandID, Kind: domain.DCPV2ResultRelease, Provider: "github", ProofID: "release-proof-3",
		RunID: "run-3", Actor: "release-train", ManifestDigest: admission.ManifestDigest, ProofDigest: v2Digest("u"),
		MergeSHA: strings.Repeat("d", 40), ArtifactDigest: v2Digest("a"), Verified: true, CreatedAt: now.Add(51 * time.Second),
	}
	terminalCommand := domain.DCPV2Command{
		CommandID: task.TaskID + "-terminal", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandTerminalVerify,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("v"), PrerequisiteDigest: result.ProofDigest,
		IdempotencyKey: task.TaskID + "/terminal", Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(51 * time.Second), UpdatedAt: now.Add(51 * time.Second),
	}
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: leased.CommandID, LeaseOwner: leased.LeaseOwner, LeaseEpoch: leased.LeaseEpoch, LeaseToken: leased.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskReleaseVerified, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: result.ProofDigest, NextCommand: &terminalCommand, Result: &result,
		CompleteAdmissionID: admission.AdmissionID, AdmissionLeaseOwner: admission.LeaseOwner,
		AdmissionLeaseEpoch: admission.LeaseEpoch, AdmissionLeaseToken: admission.LeaseToken,
		AdmissionCompletion: domain.DCPV2AdmissionSucceeded, AdmissionResultID: result.ResultID,
		UpdatedAt: now.Add(51 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	terminal := claimV2Command(t, s, terminalCommand.CommandID, now.Add(52*time.Second))
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: terminal.CommandID, LeaseOwner: terminal.LeaseOwner, LeaseEpoch: terminal.LeaseEpoch, LeaseToken: terminal.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskMerged, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		TerminalResultID: result.ResultID, CommandResultDigest: result.ProofDigest, UpdatedAt: now.Add(53 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	admissions, _ := s.ListDCPV2Admissions(ctx, task.TaskID)
	results, _ := s.ListDCPV2Results(ctx, task.TaskID)
	if task.State != domain.DCPV2TaskMerged || task.TerminalResultID != result.ResultID || len(results) != 1 ||
		len(admissions) != 1 || admissions[0].Status != domain.DCPV2AdmissionSucceeded || admissions[0].ResultID != result.ResultID {
		t.Fatalf("terminal release task=%+v admissions=%+v results=%+v", task, admissions, results)
	}
}

func TestDCPV2DeploymentMustMatchVerifiedReleaseBeforeTerminalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task, admission, mergeCommand, now := prepareV2ReleaseObservation(t, s, "deploy-terminal", "live-runtime", "4")
	merge := claimV2Command(t, s, mergeCommand.CommandID, now.Add(50*time.Second))
	release := domain.DCPV2Result{
		ResultID: "deploy-release-result", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, AdmissionID: admission.AdmissionID,
		CommandID: merge.CommandID, Kind: domain.DCPV2ResultRelease, Provider: "github", ProofID: "release-proof-4",
		RunID: "run-4", Actor: "release-train", ManifestDigest: admission.ManifestDigest, ProofDigest: v2Digest("w"),
		MergeSHA: strings.Repeat("d", 40), ArtifactDigest: v2Digest("a"), Verified: true, CreatedAt: now.Add(51 * time.Second),
	}
	deploymentCommand := domain.DCPV2Command{
		CommandID: task.TaskID + "-deployment", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandDeploymentObserve,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("x"), PrerequisiteDigest: release.ProofDigest,
		IdempotencyKey: task.TaskID + "/deployment", Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(51 * time.Second), UpdatedAt: now.Add(51 * time.Second),
	}
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: merge.CommandID, LeaseOwner: merge.LeaseOwner, LeaseEpoch: merge.LeaseEpoch, LeaseToken: merge.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskDeploymentWaiting, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: release.ProofDigest, NextCommand: &deploymentCommand, Result: &release,
		CompleteAdmissionID: admission.AdmissionID, AdmissionLeaseOwner: admission.LeaseOwner,
		AdmissionLeaseEpoch: admission.LeaseEpoch, AdmissionLeaseToken: admission.LeaseToken,
		AdmissionCompletion: domain.DCPV2AdmissionSucceeded, AdmissionResultID: release.ResultID,
		UpdatedAt: now.Add(51 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	deployment := claimV2Command(t, s, deploymentCommand.CommandID, now.Add(52*time.Second))
	terminalCommand := domain.DCPV2Command{
		CommandID: task.TaskID + "-terminal", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, Kind: domain.DCPV2CommandTerminalVerify,
		PayloadJSON: `{}`, PayloadDigest: v2Digest("y"), PrerequisiteDigest: v2Digest("z"),
		IdempotencyKey: task.TaskID + "/terminal", Status: domain.DCPV2CommandPending,
		CreatedAt: now.Add(53 * time.Second), UpdatedAt: now.Add(53 * time.Second),
	}
	deploymentResult := domain.DCPV2Result{
		ResultID: "deployment-result", TaskID: task.TaskID, RevisionID: task.CurrentRevisionID, AdmissionID: admission.AdmissionID,
		CommandID: deployment.CommandID, Kind: domain.DCPV2ResultDeployment, Provider: "github", ProofID: "deployment-proof-4",
		RunID: "deploy-run-4", Actor: "release-train", ManifestDigest: admission.ManifestDigest, ProofDigest: terminalCommand.PrerequisiteDigest,
		MergeSHA: release.MergeSHA, ArtifactDigest: v2Digest("q"), DeployedSHA: release.MergeSHA,
		Environment: "lab", Service: "inert", ProbeDigest: v2Digest("h"), Verified: true, CreatedAt: now.Add(53 * time.Second),
	}
	transition := sqlitestore.DCPV2Transition{
		CommandID: deployment.CommandID, LeaseOwner: deployment.LeaseOwner, LeaseEpoch: deployment.LeaseEpoch, LeaseToken: deployment.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskDeploymentObserve, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		CommandResultDigest: deploymentResult.ProofDigest, NextCommand: &terminalCommand, Result: &deploymentResult,
		UpdatedAt: now.Add(53 * time.Second),
	}
	if err := s.TransitionDCPV2(ctx, transition); !errors.Is(err, sqlitestore.ErrDCPV2IdentityConflict) {
		t.Fatalf("mismatched artifact transition err=%v", err)
	}
	unchanged, _ := s.GetDCPV2Task(ctx, task.TaskID)
	results, _ := s.ListDCPV2Results(ctx, task.TaskID)
	if unchanged.State != domain.DCPV2TaskDeploymentWaiting || len(results) != 1 {
		t.Fatalf("mismatch partially committed task=%+v results=%+v", unchanged, results)
	}
	deploymentResult.ArtifactDigest = release.ArtifactDigest
	if err := s.TransitionDCPV2(ctx, transition); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	terminal := claimV2Command(t, s, terminalCommand.CommandID, now.Add(54*time.Second))
	if err := s.TransitionDCPV2(ctx, sqlitestore.DCPV2Transition{
		CommandID: terminal.CommandID, LeaseOwner: terminal.LeaseOwner, LeaseEpoch: terminal.LeaseEpoch, LeaseToken: terminal.LeaseToken,
		ExpectedTaskState: task.State, ExpectedStateRevision: task.StateRevision, ExpectedRevisionID: task.CurrentRevisionID,
		NextTaskState: domain.DCPV2TaskDeployed, RepairUsed: task.RepairUsed, ReadmissionCount: task.ReadmissionCount,
		TerminalResultID: deploymentResult.ResultID, CommandResultDigest: deploymentResult.ProofDigest, UpdatedAt: now.Add(55 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = s.GetDCPV2Task(ctx, task.TaskID)
	results, _ = s.ListDCPV2Results(ctx, task.TaskID)
	if task.State != domain.DCPV2TaskDeployed || task.TerminalResultID != deploymentResult.ResultID || len(results) != 2 {
		t.Fatalf("deployment terminal task=%+v results=%+v", task, results)
	}
}
