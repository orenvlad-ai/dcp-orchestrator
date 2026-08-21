package dcpv2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const dcpV2Stage6Schema85SnapshotEnv = "DCP_V2_STAGE6_SCHEMA85_DB"

type fakeDirectRunner struct {
	workspace  ports.DCPV2ModelWorkspaceReceipt
	launches   int
	requests   []ports.DCPV2ModelLaunchRequest
	alive      bool
	terminal   *domain.DCPV2ModelTerminalReceipt
	observeErr error
	startedAt  time.Time
}

func (f *fakeDirectRunner) Prepare(context.Context, ports.DCPV2ModelPrepareRequest) (ports.DCPV2ModelWorkspaceReceipt, error) {
	return f.workspace, nil
}

func (f *fakeDirectRunner) Launch(_ context.Context, request ports.DCPV2ModelLaunchRequest) (ports.DCPV2ModelLaunchReceipt, error) {
	f.launches++
	f.requests = append(f.requests, request)
	f.alive = true
	startedAt := f.startedAt
	if startedAt.IsZero() {
		startedAt = time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)
	}
	return ports.DCPV2ModelLaunchReceipt{ActionID: request.ActionID, LaunchFence: request.LaunchFence,
		RuntimeID: request.RuntimeID, ProviderRequestID: "fake-request-1", ProviderRequestDigest: strings.Repeat("f", 64),
		StartedAt: startedAt}, nil
}

func (f *fakeDirectRunner) Observe(_ context.Context, request ports.DCPV2ModelLaunchRequest) (domain.DCPV2RuntimeObservation, error) {
	return domain.DCPV2RuntimeObservation{ActionID: request.ActionID, RuntimeID: request.RuntimeID, Alive: f.alive,
		ObservedAt: time.Date(2026, 8, 21, 13, 1, 0, 0, time.UTC)}, f.observeErr
}

func (f *fakeDirectRunner) Terminal(_ context.Context, _ ports.DCPV2ModelLaunchRequest) (domain.DCPV2ModelTerminalReceipt, bool, error) {
	if f.terminal == nil {
		return domain.DCPV2ModelTerminalReceipt{}, false, nil
	}
	return *f.terminal, true, nil
}

func activateDirectTwinForTest(t *testing.T, store *sqlite.Store, projectPath string, now time.Time) {
	t.Helper()
	spec, ok := domain.DCPPolicyTarget(TwinTarget, TwinProfile)
	if !ok {
		t.Fatal("exact twin target is absent")
	}
	activation := domain.DCPV2Stage5Activation{ActivationID: "dcp-v2-twin-stage5",
		AuthorityCommit: "4143982eb054a40537d963356c209bfe8447ba31", SourceCommit: strings.Repeat("a", 40),
		SourceTree: strings.Repeat("b", 40), InstallReceiptSHA: strings.Repeat("c", 64), TargetSpecVersion: TwinTargetSpec,
		TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(), Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, RequiredCheck: TwinRequiredCheck,
		IssuerKind: TwinIssuerKind, IssuerActor: TwinIssuerActor, IssuerEvent: TwinIssuerEvent,
		IssuerEventType: TwinDispatchEvent, WorkflowID: TwinWorkflowID, Environment: TwinEnvironment,
		Service: TwinServiceName, Adapter: TwinAdapterVersion, ActivatedAt: now}
	project := domain.ProjectRecord{ID: spec.Target, Path: projectPath, RepoOriginURL: spec.OriginURL, DisplayName: spec.Target,
		RegisteredAt: now, Kind: domain.ProjectKindSingleRepo, Config: domain.ProjectConfig{DefaultBranch: spec.DefaultBranch,
			SessionPrefix: spec.SessionPrefix, AgentRules: spec.AgentRules,
			Worker: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{
				Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}},
			Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}}}}
	if created, projectCreated, err := store.ActivateDCPV2Stage5WithProject(t.Context(), activation, project); err != nil || !created || !projectCreated {
		t.Fatalf("activate direct twin: created=%t project=%t err=%v", created, projectCreated, err)
	}
}

func TestDirectTwinOwnsLaunchRestartTerminalAndCreatesNoLegacyRows(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	store, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectPath := filepath.Join(t.TempDir(), "targets", TwinTarget)
	activateDirectTwinForTest(t, store, projectPath, now)
	worktree := filepath.Join(t.TempDir(), "worktrees", "direct")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeDirectRunner{workspace: ports.DCPV2ModelWorkspaceReceipt{Branch: directWorkerBranch(TwinCanaryTaskID),
		Worktree: worktree, WorktreeDigest: strings.Repeat("d", 64)}, startedAt: now.Add(4 * time.Second)}
	pushes := 0
	adapter := &TwinGitHubAdapter{gh: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "repos/"+TwinRepository+"/git/ref/heads/"+TwinBase {
			return []byte(`{"object":{"sha":"` + stage6RecoveryBaseSHA + `"}}`), nil
		}
		if len(args) == 1 && strings.HasPrefix(args[0], "repos/"+TwinRepository+"/pulls?") {
			return []byte(`[]`), nil
		}
		if len(args) == 5 && args[0] == "--method" && args[1] == "POST" && args[2] == "repos/"+TwinRepository+"/pulls" {
			return []byte(`{"number":17,"state":"open","draft":false,"merged":false,"base":{"ref":"main","sha":"` + stage6RecoveryBaseSHA + `"},"head":{"ref":"` + directWorkerBranch(TwinCanaryTaskID) + `","sha":"` + strings.Repeat("e", 40) + `","repo":{"full_name":"` + TwinRepository + `"}}}`), nil
		}
		return nil, errors.New("unexpected GitHub read: " + strings.Join(args, " "))
	}, git: func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, "\x00") {
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return directWorkerBranch(TwinCanaryTaskID), nil
		case "rev-parse\x00HEAD":
			return strings.Repeat("e", 40), nil
		case "rev-parse\x00" + strings.Repeat("e", 40) + "^{tree}":
			return strings.Repeat("f", 40), nil
		case "merge-base\x00--is-ancestor\x00" + stage6RecoveryBaseSHA + "\x00" + strings.Repeat("e", 40):
			return "", nil
		case "ls-remote\x00--heads\x00origin\x00refs/heads/" + directWorkerBranch(TwinCanaryTaskID):
			return "", nil
		default:
			if len(args) == 4 && args[0] == "push" && strings.HasPrefix(args[1], "--force-with-lease=") {
				pushes++
				return "", nil
			}
			return "", errors.New("unexpected Git operation: " + strings.Join(args, " "))
		}
	}}
	clock := now
	svc, err := NewTwinService(store, runner, adapter, "direct-test-epoch", func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.SubmitTwin(t.Context(), TwinSubmitInput{TaskID: TwinCanaryTaskID, Prompt: "add one inert canary fixture"})
	if err != nil || result.Duplicate || runner.launches != 1 || len(runner.requests) != 1 ||
		!result.Projection.ModelActive || !result.Projection.WorkflowActive {
		t.Fatalf("direct submit result=%+v launches=%d requests=%d err=%v", result, runner.launches, len(runner.requests), err)
	}
	if err := svc.Startup(t.Context()); err != nil || runner.launches != 1 {
		t.Fatalf("live exact restart duplicated launch: launches=%d err=%v", runner.launches, err)
	}
	request := runner.requests[0]
	if request.Branch != directWorkerBranch(TwinCanaryTaskID) || request.ExpectedOldHead != "" ||
		!strings.Contains(request.TaskInputJSON, "add one inert canary fixture") ||
		request.TaskInputDigest != digestCanonical(json.RawMessage(request.TaskInputJSON)) ||
		request.PromptDigest != digestCanonical(json.RawMessage(request.CommandPayloadJSON)) {
		t.Fatalf("direct Worker restart identity=%+v", request)
	}
	outputDigest := digestCanonical(map[string]string{"worker": "done"})
	receipt := domain.DCPV2ModelTerminalReceipt{ReceiptID: stableID("terminal-receipt", request.ActionID, outputDigest),
		ActionID: request.ActionID, CommandID: request.CommandID, TaskID: request.TaskID, RevisionID: request.RevisionID,
		RuntimeID: request.RuntimeID, LaunchFence: request.LaunchFence, Status: domain.DCPV2ModelTerminalSucceeded,
		OutputJSON: `{}`, OutputDigest: outputDigest, HeadRef: request.Branch, HeadSHA: strings.Repeat("e", 40),
		TreeSHA: strings.Repeat("f", 40), BaseSHA: request.HeadSHA, WorktreePath: request.Worktree,
		WorktreeDigest: request.WorktreeDigest, CreatedAt: runner.startedAt.Add(time.Second)}
	receipt.ResultDigest = digestCanonical(map[string]string{"output": receipt.OutputDigest, "head": receipt.HeadSHA, "tree": receipt.TreeSHA})
	runner.alive, runner.terminal = false, &receipt
	if err := svc.ReportDirectProcessExit(t.Context(), request.ActionID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReportDirectProcessExit(t.Context(), request.ActionID); err != nil || runner.launches != 1 {
		t.Fatalf("equal terminal replay duplicated effect: launches=%d err=%v", runner.launches, err)
	}
	snapshot, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil || snapshot.Task.State != domain.DCPV2TaskChecksWaiting || snapshot.Task.CurrentRevisionID == request.RevisionID ||
		len(snapshot.Revisions) != 2 || len(snapshot.Commands) != 3 || snapshot.Commands[1].Kind != domain.DCPV2CommandPublication ||
		snapshot.Commands[1].Status != domain.DCPV2CommandSucceeded || snapshot.Commands[2].Kind != domain.DCPV2CommandChecksObserve || pushes != 1 ||
		len(snapshot.Actions) != 1 || snapshot.Actions[0].Status != domain.DCPV2ActionSucceeded || snapshot.Actions[0].Slot != 0 ||
		snapshot.Projection.ModelActive || !snapshot.Projection.WorkflowActive {
		t.Fatalf("direct terminal snapshot=%+v err=%v", snapshot, err)
	}
	legacyTasks, taskErr := store.ListDCPReviewLabPolicyTasks(t.Context())
	legacyActions, actionErr := store.ListDCPModelActions(t.Context())
	legacySessions, sessionErr := store.ListSessions(t.Context(), domain.ProjectID(TwinTarget))
	if err := errors.Join(taskErr, actionErr, sessionErr); err != nil || len(legacyTasks) != 0 || len(legacyActions) != 0 || len(legacySessions) != 0 {
		t.Fatalf("direct Task created legacy authority: tasks=%d actions=%d sessions=%d err=%v", len(legacyTasks), len(legacyActions), len(legacySessions), err)
	}
}

func TestDirectRestartAfterFenceWithoutRuntimeProofFailsClosedWithoutRelaunch(t *testing.T) {
	now := time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectPath := filepath.Join(t.TempDir(), "targets", TwinTarget)
	activateDirectTwinForTest(t, store, projectPath, now)
	runner := &fakeDirectRunner{workspace: ports.DCPV2ModelWorkspaceReceipt{Branch: directWorkerBranch(TwinCanaryTaskID),
		Worktree: t.TempDir(), WorktreeDigest: strings.Repeat("d", 64)}, startedAt: now.Add(4 * time.Second)}
	adapter := &TwinGitHubAdapter{gh: func(context.Context, []byte, ...string) ([]byte, error) {
		return []byte(`{"object":{"sha":"` + stage6RecoveryBaseSHA + `"}}`), nil
	}, git: runTwinGit}
	svc, err := NewTwinService(store, runner, adapter, "direct-fence-epoch", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitTwin(t.Context(), TwinSubmitInput{TaskID: TwinCanaryTaskID, Prompt: "inert"}); err != nil {
		t.Fatal(err)
	}
	runner.alive = false
	if err := svc.Startup(t.Context()); err == nil || runner.launches != 1 {
		t.Fatalf("ambiguous fenced restart did not fail closed: launches=%d err=%v", runner.launches, err)
	}
}

func TestDirectReviewerRepairAndArbiterShareOneDCPAuthority(t *testing.T) {
	now := time.Date(2026, 8, 21, 17, 0, 0, 0, time.UTC)
	processor := &twinProcessor{now: func() time.Time { return now }}
	revision := domain.DCPV2Revision{RevisionID: "revision-2", TaskID: "task", Sequence: 2,
		Kind: domain.DCPV2RevisionWorker, Repository: TwinRepository, BaseRef: TwinBase,
		BaseSHA: strings.Repeat("a", 40), HeadRef: "codex/direct", HeadSHA: strings.Repeat("b", 40), CreatedAt: now.Add(-time.Minute)}
	task := domain.DCPV2Task{TaskID: "task", BaseRef: TwinBase, CurrentRevisionID: revision.RevisionID,
		State: domain.DCPV2TaskReviewQueued, StateRevision: 4, RepairBudget: 1}
	command := domain.DCPV2Command{CommandID: "review-command", TaskID: task.TaskID, RevisionID: revision.RevisionID,
		Kind: domain.DCPV2CommandReviewExecute}
	receipt := domain.DCPV2ModelTerminalReceipt{ReceiptID: "review-receipt", OutputDigest: strings.Repeat("c", 64),
		ResultDigest: strings.Repeat("d", 64), OutputJSON: `{"verdict":"changes_requested","headSha":"` + revision.HeadSHA + `","findings":["fix the exact regression"]}`}
	outcome, err := processor.completeReviewReceipt(task, revision, command, receipt)
	if err != nil || !outcome.RepairIncrement || outcome.NextTaskState != domain.DCPV2TaskRepairQueued ||
		outcome.NextCommand == nil || outcome.NextCommand.Kind != domain.DCPV2CommandRepairExecute ||
		outcome.NextAction == nil || outcome.NextAction.Role != domain.DCPV2ActionRepair {
		t.Fatalf("direct repair decision outcome=%+v err=%v", outcome, err)
	}
	if !strings.Contains(outcome.NextCommand.PayloadJSON, "fix the exact regression") {
		t.Fatalf("repair Command lost the reviewed finding: %s", outcome.NextCommand.PayloadJSON)
	}
	receipt.OutputJSON = `{"verdict":"changes_requested","headSha":"` + revision.HeadSHA + `","findings":[]}`
	if _, err := processor.completeReviewReceipt(task, revision, command, receipt); err == nil {
		t.Fatal("changes_requested without findings was accepted")
	}
	receipt.OutputJSON = `{"verdict":"approved","verdict":"changes_requested","headSha":"` + revision.HeadSHA + `","findings":["ambiguous"]}`
	if _, err := processor.completeReviewReceipt(task, revision, command, receipt); err == nil {
		t.Fatal("duplicate-key Reviewer result was accepted")
	}
	receipt.OutputJSON = `{"verdict":"changes_requested","headSha":"` + revision.HeadSHA + `","findings":["fix the exact regression"]}`
	task.RepairUsed = 1
	if _, err := processor.completeReviewReceipt(task, revision, command, receipt); err == nil {
		t.Fatal("second task-level repair was accepted")
	}
	task.RepairUsed = 0
	receipt.OutputJSON = `{"verdict":"approved","headSha":"` + revision.HeadSHA + `","findings":[]}`
	outcome, err = processor.completeReviewReceipt(task, revision, command, receipt)
	if err != nil || outcome.NextTaskState != domain.DCPV2TaskAdmissionWaiting || outcome.NextCommand == nil ||
		outcome.NextCommand.Kind != domain.DCPV2CommandAdmissionEnqueue || outcome.NextAction != nil {
		t.Fatalf("direct approved review outcome=%+v err=%v", outcome, err)
	}

	arbiterCommand := command
	arbiterCommand.Kind = domain.DCPV2CommandArbiterExecute
	receipt.OutputJSON = `{"decision":"admit"}`
	outcome, err = processor.completeArbiterReceipt(task, revision, arbiterCommand, receipt)
	if err != nil || outcome.NextTaskState != domain.DCPV2TaskAdmissionWaiting || outcome.NextCommand == nil ||
		outcome.NextCommand.Kind != domain.DCPV2CommandAdmissionEnqueue {
		t.Fatalf("direct arbiter outcome=%+v err=%v", outcome, err)
	}

	repairCommand := command
	repairCommand.Kind = domain.DCPV2CommandRepairExecute
	repairReceipt := domain.DCPV2ModelTerminalReceipt{OutputDigest: strings.Repeat("e", 64), ResultDigest: strings.Repeat("f", 64),
		HeadRef: revision.HeadRef, HeadSHA: strings.Repeat("c", 40), TreeSHA: strings.Repeat("d", 40), BaseSHA: revision.HeadSHA,
		WorktreePath: filepath.Join(t.TempDir(), "direct-repair"), WorktreeDigest: strings.Repeat("e", 64)}
	outcome, err = processor.completeWorkerReceipt(task, revision, repairCommand, repairReceipt)
	if err != nil || outcome.NextRevision == nil || outcome.NextRevision.Kind != domain.DCPV2RevisionRepair ||
		outcome.NextRevision.PredecessorRevisionID != revision.RevisionID || outcome.NextCommand == nil ||
		outcome.NextCommand.Kind != domain.DCPV2CommandPublication {
		t.Fatalf("direct repair terminal outcome=%+v err=%v", outcome, err)
	}
}

func TestTwinServiceIsDormantWithoutStage5Activation(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &fakeDirectRunner{workspace: ports.DCPV2ModelWorkspaceReceipt{Branch: "codex/test", Worktree: t.TempDir(), WorktreeDigest: strings.Repeat("w", 64)}}
	svc, err := NewTwinService(store, runner, newTwinGitHubAdapterForTest(nil), "test-epoch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Startup(context.Background()); err != nil {
		t.Fatalf("dormant startup: %v", err)
	}
	_, err = svc.SubmitTwin(context.Background(), TwinSubmitInput{TaskID: TwinCanaryTaskID, Prompt: "inert"})
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Code != "DCP_V2_NOT_ACTIVATED" || runner.launches != 0 {
		t.Fatalf("dormant submit err=%v launches=%d", err, runner.launches)
	}
}

func TestValidateTwinActivationBindsExactInstalledTuple(t *testing.T) {
	activation := domain.DCPV2Stage5Activation{ActivationID: "dcp-v2-twin-stage5",
		AuthorityCommit: "4143982eb054a40537d963356c209bfe8447ba31", SourceCommit: strings.Repeat("a", 40),
		SourceTree: strings.Repeat("b", 40), InstallReceiptSHA: strings.Repeat("c", 64), TargetSpecVersion: TwinTargetSpec,
		TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(), Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, RequiredCheck: TwinRequiredCheck,
		IssuerKind: TwinIssuerKind, IssuerActor: TwinIssuerActor, IssuerEvent: TwinIssuerEvent,
		IssuerEventType: TwinDispatchEvent, WorkflowID: TwinWorkflowID, Environment: TwinEnvironment,
		Service: TwinServiceName, Adapter: TwinAdapterVersion, ActivatedAt: time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC)}
	if err := validateTwinActivation(activation); err != nil {
		t.Fatal(err)
	}
	activation.TargetPolicyDigest = strings.Repeat("d", 64)
	if err := validateTwinActivation(activation); err == nil {
		t.Fatal("wrong installed policy digest was accepted")
	}
}

func TestStage6Schema85SnapshotAdoptsFrozenWorkerOnceWithoutLegacyAuthority(t *testing.T) {
	destination := copyStage6Schema85Snapshot(t)
	store, err := sqlite.Open(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	driftedOutput := true
	git := func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, "\x00") {
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return stage6CanaryBranch, nil
		case "rev-parse\x00HEAD":
			if driftedOutput {
				return strings.Repeat("f", 40), nil
			}
			return stage6CanaryCommit, nil
		case "rev-parse\x00HEAD^{tree}":
			return stage6CanaryTree, nil
		case "diff-tree\x00--no-commit-id\x00--name-only\x00-r\x00HEAD":
			return "docs/STAGE6_CANARY.md", nil
		case "show\x00HEAD:docs/STAGE6_CANARY.md":
			return "Stage 6 DCP v2 canary.", nil
		case "merge-base\x00--is-ancestor\x00" + stage6RecoveryBaseSHA + "\x00" + stage6CanaryCommit:
			return "", nil
		case "ls-remote\x00--heads\x00origin\x00refs/heads/" + stage6CanaryBranch:
			return "", nil
		default:
			return "", errors.New("unexpected Git read: " + strings.Join(args, " "))
		}
	}
	gh := func(_ context.Context, _ []byte, _ ...string) ([]byte, error) { return []byte(`[]`), nil }
	adapter := &TwinGitHubAdapter{git: git, gh: gh}
	runner := &fakeDirectRunner{}
	svc, err := NewTwinService(store, runner, adapter, "adoption-epoch", func() time.Time {
		return time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := svc.AdoptStage6Worker(t.Context()); err == nil || applied {
		t.Fatalf("foreign Worker output was adopted: applied=%t err=%v", applied, err)
	}
	if _, err := store.GetDCPV2Stage6WorkerAdoption(t.Context()); !errors.Is(err, sqlitestore.ErrDCPV2NotFound) {
		t.Fatalf("failed adoption wrote a receipt: %v", err)
	}
	unchangedAction, err := store.GetDCPV2ActionByCommand(t.Context(), stage6RecoveryCommandID)
	if err != nil || unchangedAction.Status != domain.DCPV2ActionRunning || unchangedAction.RuntimeID != stage6RecoveryRuntimeID {
		t.Fatalf("failed adoption mutated frozen Action=%+v err=%v", unchangedAction, err)
	}
	if err := svc.Startup(t.Context()); err == nil || runner.launches != 0 {
		t.Fatalf("frozen pre-adoption Action did not block startup: launches=%d err=%v", runner.launches, err)
	}
	driftedOutput = false
	adoption, applied, err := svc.AdoptStage6Worker(t.Context())
	if err != nil || !applied || adoption.CommitSHA != stage6CanaryCommit || adoption.TreeSHA != stage6CanaryTree {
		t.Fatalf("adopt exact Worker: adoption=%+v applied=%t err=%v", adoption, applied, err)
	}
	replay, applied, err := svc.AdoptStage6Worker(t.Context())
	if err != nil || applied || replay != adoption {
		t.Fatalf("equal adoption replay: adoption=%+v applied=%t err=%v", replay, applied, err)
	}
	snapshot, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil || snapshot.Task.State != domain.DCPV2TaskChecksWaiting || len(snapshot.Revisions) != 2 ||
		len(snapshot.Commands) != 2 || snapshot.Commands[1].Kind != domain.DCPV2CommandPublication ||
		len(snapshot.Actions) != 1 || snapshot.Actions[0].Status != domain.DCPV2ActionSucceeded || snapshot.Actions[0].Slot != 0 || runner.launches != 0 {
		t.Fatalf("adopted DCP state drifted: snapshot=%+v launches=%d err=%v", snapshot, runner.launches, err)
	}
	db, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET branch='foreign-after-adoption' WHERE id=?`, domain.SessionID(TwinTarget+"-1")); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	after, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil || after.Task != snapshot.Task || len(after.Revisions) != len(snapshot.Revisions) || len(after.Commands) != len(snapshot.Commands) {
		t.Fatalf("legacy mutation changed DCP authority: before=%+v after=%+v err=%v", snapshot.Task, after.Task, err)
	}
}

func copyStage6Schema85Snapshot(t *testing.T) string {
	t.Helper()
	source := os.Getenv(dcpV2Stage6Schema85SnapshotEnv)
	if source == "" {
		t.Skip(dcpV2Stage6Schema85SnapshotEnv + " is not set")
	}
	readOnly, err := sql.Open("sqlite", "file:"+source+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "ao.db")
	if _, err := readOnly.Exec(`VACUUM INTO ?`, destination); err != nil {
		t.Fatalf("copy exact schema-85 snapshot: %v", err)
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}
	return destination
}
