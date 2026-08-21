package dcpv2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

const dcpV2Stage6Schema85SnapshotEnv = "DCP_V2_STAGE6_SCHEMA85_DB"

type stage6RecoveryProvisioner struct {
	calls int
	want  dcptasksvc.PolicySubmitInput
}

func (p *stage6RecoveryProvisioner) ProvisionV2RuntimePolicy(_ context.Context, in dcptasksvc.PolicySubmitInput) (dcptasksvc.PolicySubmitResult, error) {
	p.calls++
	if in != p.want {
		return dcptasksvc.PolicySubmitResult{}, fmt.Errorf("recovery input=%+v want=%+v", in, p.want)
	}
	return dcptasksvc.PolicySubmitResult{Task: domain.DCPReviewLabPolicyTask{
		TaskID: in.TaskID, Target: in.Target, Profile: in.Profile, Repository: in.Repository, Prompt: in.Prompt,
		SessionID: domain.SessionID(TwinTarget + "-1"), CardNumber: 1, SourceBranch: "ao/" + TwinTarget + "-1/root",
	}}, nil
}

func seedExactStage6RecoveryFence(t *testing.T, store *sqlite.Store) string {
	t.Helper()
	ctx := t.Context()
	now := time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	activation := domain.DCPV2Stage5Activation{
		ActivationID: "dcp-v2-twin-stage5", AuthorityCommit: "4143982eb054a40537d963356c209bfe8447ba31",
		SourceCommit: "c1fc43d74cd517b7d73540f340058fa17b56ef15", SourceTree: "ff51ca2b1f6f9fa502b999f50a366a8e35035421",
		InstallReceiptSHA: "54dd88beef2e9c93ee86435df2645d6707acf2dc3e2c0c0b4dad6de9b40cc9c0",
		TargetSpecVersion: TwinTargetSpec, TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(),
		Repository: TwinRepository, RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase,
		RequiredCheck: TwinRequiredCheck, IssuerKind: TwinIssuerKind, IssuerActor: TwinIssuerActor,
		IssuerEvent: TwinIssuerEvent, IssuerEventType: TwinDispatchEvent, WorkflowID: TwinWorkflowID,
		Environment: TwinEnvironment, Service: TwinServiceName, Adapter: TwinAdapterVersion, ActivatedAt: now,
	}
	if created, err := store.ActivateDCPV2Stage5(ctx, activation); err != nil || !created {
		t.Fatalf("activate exact Stage 5 fixture: created=%t err=%v", created, err)
	}
	prompt := "Add docs/STAGE6_CANARY.md with the single line Stage 6 DCP v2 canary. Change no other file."
	requestDigest := digestCanonical(map[string]string{"taskId": TwinCanaryTaskID, "prompt": prompt})
	scopeDigest := digestCanonical(map[string]any{"repository": TwinRepository, "repositoryId": TwinRepositoryID,
		"ownerId": TwinOwnerID, "base": TwinBase, "profile": TwinProfile})
	revision := domain.DCPV2Revision{RevisionID: stage6RecoveryRevisionID, TaskID: TwinCanaryTaskID, Sequence: 1,
		Kind: domain.DCPV2RevisionWorkInput, Repository: TwinRepository, BaseRef: TwinBase,
		BaseSHA: stage6RecoveryBaseSHA, HeadRef: TwinBase, HeadSHA: stage6RecoveryBaseSHA,
		EvidenceDigest: digestCanonical(map[string]string{"main": stage6RecoveryBaseSHA}), CreatedAt: now}
	task := domain.DCPV2Task{TaskID: TwinCanaryTaskID, TargetSpecVersion: TwinTargetSpec, Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, Profile: TwinProfile,
		RequestDigest: requestDigest, ScopeDigest: scopeDigest, PolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(),
		InitialWorkerBudget: 1, RepairBudget: 1, MaxReadmissions: 2, CurrentRevisionID: revision.RevisionID,
		State: domain.DCPV2TaskWorkerQueued, StateRevision: 1, CreatedAt: now, UpdatedAt: now}
	command := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandWorkerExecute, 1,
		map[string]string{"prompt": prompt, "baseSha": stage6RecoveryBaseSHA}, requestDigest, now)
	action := newAction(command, domain.DCPV2ActionWorker, requestDigest, now)
	if command.CommandID != stage6RecoveryCommandID || action.ActionID != stage6RecoveryActionID {
		t.Fatalf("fixture identities drifted: command=%s action=%s", command.CommandID, action.ActionID)
	}
	if created, err := store.CreateDCPV2Task(ctx, task, revision, command, action); err != nil || !created {
		t.Fatalf("create exact Stage 6 fixture: created=%t err=%v", created, err)
	}
	leased, err := store.ClaimNextDCPV2Command(ctx, "dcp-v2-daemon", "pid-29329", "lease-token", now.Add(time.Second))
	if err != nil || leased == nil || leased.CommandID != command.CommandID {
		t.Fatalf("lease exact command: command=%+v err=%v", leased, err)
	}
	fence := "model:" + action.ActionID
	if err := store.FenceDCPV2CommandEffect(ctx, command.CommandID, leased.LeaseOwner, leased.LeaseEpoch, leased.LeaseToken, fence, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextDCPV2Action(ctx, fence, now.Add(3*time.Second))
	if err != nil || claimed == nil || claimed.ActionID != action.ActionID || claimed.Status != domain.DCPV2ActionLaunching || claimed.Slot != 1 {
		t.Fatalf("claim exact Action: action=%+v err=%v", claimed, err)
	}
	return prompt
}

func TestTwinServiceIsDormantWithoutStage5Activation(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc, err := NewTwinService(store, &dcptasksvc.Service{}, newTwinGitHubAdapterForTest(nil), "test-epoch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Startup(context.Background()); err != nil {
		t.Fatalf("dormant startup: %v", err)
	}
	_, err = svc.SubmitTwin(context.Background(), TwinSubmitInput{TaskID: TwinCanaryTaskID, Prompt: "inert"})
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Code != "DCP_V2_NOT_ACTIVATED" {
		t.Fatalf("dormant submit err=%v", err)
	}
}

func TestValidateTwinActivationBindsExactInstalledTuple(t *testing.T) {
	activation := domain.DCPV2Stage5Activation{
		ActivationID: "dcp-v2-twin-stage5", AuthorityCommit: "4143982eb054a40537d963356c209bfe8447ba31",
		SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		InstallReceiptSHA: strings.Repeat("c", 64), TargetSpecVersion: TwinTargetSpec,
		TargetPolicyDigest: domain.DCPWBCIntegrationTwinPolicyDigest(), Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, RequiredCheck: TwinRequiredCheck,
		IssuerKind: TwinIssuerKind, IssuerActor: TwinIssuerActor, IssuerEvent: TwinIssuerEvent,
		IssuerEventType: TwinDispatchEvent, WorkflowID: TwinWorkflowID, Environment: TwinEnvironment,
		Service: TwinServiceName, Adapter: TwinAdapterVersion, ActivatedAt: time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC),
	}
	if err := validateTwinActivation(activation); err != nil {
		t.Fatal(err)
	}
	activation.TargetPolicyDigest = strings.Repeat("d", 64)
	if err := validateTwinActivation(activation); err == nil {
		t.Fatal("wrong installed policy digest was accepted")
	}
}

func TestExactActiveLegacyActionIgnoresTerminalHistoryAndBindsCurrentRole(t *testing.T) {
	taskID := TwinCanaryTaskID
	sessionID := domain.SessionID(TwinTarget + "-1")
	actions := []domain.DCPModelAction{
		{ID: "worker", TaskID: taskID, SessionID: sessionID, Kind: domain.DCPActionInitialWorker, Status: domain.DCPActionSucceeded},
		{ID: "reviewer", TaskID: taskID, SessionID: sessionID, Kind: domain.DCPActionReviewer, Status: domain.DCPActionRunning},
	}
	wantKind, err := legacyActionKindForV2(domain.DCPV2ActionReviewer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := exactActiveLegacyAction(actions, taskID, sessionID, wantKind)
	if err != nil || got.ID != "reviewer" {
		t.Fatalf("active reviewer=%+v err=%v", got, err)
	}
	actions = append(actions, domain.DCPModelAction{ID: "foreign", TaskID: "foreign", SessionID: sessionID,
		Kind: domain.DCPActionReviewer, Status: domain.DCPActionQueued})
	if _, err := exactActiveLegacyAction(actions, taskID, sessionID, wantKind); err == nil {
		t.Fatal("crossed active native identity was accepted")
	}
}

func TestLegacyActionKindForV2RejectsUnknownRole(t *testing.T) {
	for role, want := range map[domain.DCPV2ActionRole]domain.DCPModelActionKind{
		domain.DCPV2ActionWorker: domain.DCPActionInitialWorker, domain.DCPV2ActionReviewer: domain.DCPActionReviewer,
		domain.DCPV2ActionRepair: domain.DCPActionRepairWorker, domain.DCPV2ActionArbiter: domain.DCPActionArbiter,
	} {
		got, err := legacyActionKindForV2(role)
		if err != nil || got != want {
			t.Fatalf("role %s kind=%s err=%v", role, got, err)
		}
	}
	if _, err := legacyActionKindForV2("foreign"); err == nil {
		t.Fatal("foreign DCP v2 role was accepted")
	}
}

func TestInspectAndRecoverExactStage6NativeShellOnce(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prompt := seedExactStage6RecoveryFence(t, store)
	fence, err := InspectStage6RecoveryFence(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	if fence.TaskID != TwinCanaryTaskID || fence.RevisionID != stage6RecoveryRevisionID ||
		fence.CommandID != stage6RecoveryCommandID || fence.ActionID != stage6RecoveryActionID ||
		fence.BaseSHA != stage6RecoveryBaseSHA || fence.Prompt != prompt {
		t.Fatalf("recovery fence=%+v", fence)
	}
	provisioner := &stage6RecoveryProvisioner{want: dcptasksvc.PolicySubmitInput{
		TaskID: TwinCanaryTaskID, Target: TwinTarget, Profile: TwinProfile, Repository: TwinRepository, Prompt: prompt,
	}}
	svc, err := NewTwinService(store, provisioner, newTwinGitHubAdapterForTest(nil), "recovery-epoch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Startup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if provisioner.calls != 1 {
		t.Fatalf("native recovery calls=%d want=1", provisioner.calls)
	}
}

func TestStage6Schema85SnapshotRestartKeepsOnePrelaunchIdentity(t *testing.T) {
	destination := copyStage6Schema85Snapshot(t)
	store, err := sqlite.Open(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	provisioner := &stage6RecoveryProvisioner{}
	svc, err := NewTwinService(store, provisioner, newTwinGitHubAdapterForTest(nil), "snapshot-restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil {
		t.Fatal(err)
	}
	envelope := domain.CanonicalDCPPolicySpawnEnvelope(before.Native)
	if !strings.HasPrefix(envelope.Prompt, "DCP live-runtime task ") || before.Projection.ModelActive || !before.Projection.WorkflowActive {
		t.Fatalf("prelaunch envelope/projection drift envelope=%q projection=%+v", envelope.Prompt, before.Projection)
	}
	for restart := 1; restart <= 2; restart++ {
		if err := svc.Startup(t.Context()); err != nil {
			t.Fatalf("restart %d: %v", restart, err)
		}
	}
	after, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil {
		t.Fatal(err)
	}
	legacyActions, err := store.ListDCPModelActions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if provisioner.calls != 0 || len(before.Revisions) != 1 || len(after.Revisions) != 1 ||
		len(before.Commands) != 1 || len(after.Commands) != 1 || len(before.Actions) != 1 || len(after.Actions) != 1 ||
		len(legacyActions) != 74 || after.Actions[0].Status != domain.DCPV2ActionLaunching ||
		after.Actions[0].RuntimeID != "" || after.Projection.ModelActive || !after.Projection.WorkflowActive {
		t.Fatalf("restart duplicated or advanced effects provision=%d before=%+v after=%+v legacyActions=%d",
			provisioner.calls, before, after, len(legacyActions))
	}
}

func TestStage6Schema85SnapshotRejectsForeignPromptAndAsymmetricRuntime(t *testing.T) {
	for _, test := range []struct {
		name       string
		mutation   string
		wantError  string
		wantParams []any
		rejected   bool
	}{
		{name: "foreign prompt rejected by immutable schema", mutation: `UPDATE dcp_review_lab_policy_task SET prompt = ? WHERE task_id = ?`,
			wantParams: []any{"foreign prompt", TwinCanaryTaskID}, rejected: true},
		{name: "claimed without runtime", mutation: `UPDATE dcp_model_action SET status = 'claimed', slot = 1 WHERE task_id = ?`,
			wantParams: []any{TwinCanaryTaskID}, wantError: "runtime asymmetry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := copyStage6Schema85Snapshot(t)
			db, err := sql.Open("sqlite", destination)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.mutation, test.wantParams...); err != nil {
				if test.rejected && strings.Contains(err.Error(), "immutable identity") {
					_ = db.Close()
					return
				}
				t.Fatal(err)
			} else if test.rejected {
				t.Fatal("foreign immutable identity mutation was accepted")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := sqlite.Open(filepath.Dir(destination))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			svc, err := NewTwinService(store, &stage6RecoveryProvisioner{}, newTwinGitHubAdapterForTest(nil), "snapshot-negative", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Startup(t.Context()); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("startup error=%v want %q", err, test.wantError)
			}
		})
	}
}

func TestStage6Schema85SnapshotAdoptsOneExactRunningRuntime(t *testing.T) {
	destination := copyStage6Schema85Snapshot(t)
	db, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	var branch, worktree, prompt, sessionID string
	if err := db.QueryRow(`SELECT source_branch, worktree_path, 'DCP live-runtime task ' || task_id || ': ' || prompt, session_id
		FROM dcp_review_lab_policy_task WHERE task_id = ?`, TwinCanaryTaskID).Scan(&branch, &worktree, &prompt, &sessionID); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		query string
		args  []any
	}{
		{`UPDATE sessions SET branch = ?, workspace_path = ?, prompt = ?, runtime_handle_id = 'handle-1',
			runtime_launch_id = 'launch-1', activity_state = 'active' WHERE id = ?`, []any{branch, worktree, prompt, sessionID}},
		{`UPDATE dcp_model_action SET status = 'claimed', slot = 1 WHERE task_id = ?`, []any{TwinCanaryTaskID}},
		{`UPDATE dcp_model_action SET status = 'running', launch_id = 'launch-1' WHERE task_id = ?`, []any{TwinCanaryTaskID}},
		{`UPDATE dcp_review_lab_policy_task SET state = 'worker_running', revision = revision + 1 WHERE task_id = ?`, []any{TwinCanaryTaskID}},
	} {
		if _, err := db.Exec(mutation.query, mutation.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc, err := NewTwinService(store, &stage6RecoveryProvisioner{}, newTwinGitHubAdapterForTest(nil), "snapshot-running", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Startup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Startup(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Snapshot(t.Context(), TwinCanaryTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Actions) != 1 || snapshot.Actions[0].Status != domain.DCPV2ActionRunning ||
		snapshot.Actions[0].RuntimeID != "launch-1" || !snapshot.Projection.ModelActive || !snapshot.Projection.WorkflowActive {
		t.Fatalf("running adoption=%+v projection=%+v", snapshot.Actions, snapshot.Projection)
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
