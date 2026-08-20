package dcpv2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

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
