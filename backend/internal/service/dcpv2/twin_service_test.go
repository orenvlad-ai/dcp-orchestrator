package dcpv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

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
