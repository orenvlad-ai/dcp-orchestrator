package dcpterminalmerge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestFutureArbiterSchemaRecoveryPreflightProvesNoResultAndExactRejectedSchema(t *testing.T) {
	incident := futureProtocolIncident(t)
	incident.Status = domain.DCPFutureArbiterFailed
	incident.ErrorCode = "launch_failed"
	incident.ModelCallCount = 1
	incident.ModelActionID = "dcp-model-night-arb-b-arbiter-1"
	incident.RuntimeHandleID = incident.IncidentID
	incident.InputJSON = `{"schemaVersion":"dcp.review-lab.future-arbiter-input/v1"}`

	dataDir := t.TempDir()
	launcher := &futureArbiterLauncher{dataDir: dataDir, runFile: filepath.Join(dataDir, "run.json")}
	artifacts, err := launcher.artifacts(incident)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artifacts.directory, 0o750); err != nil {
		t.Fatal(err)
	}
	input := append([]byte(incident.InputJSON), '\n')
	rejectedSchema := []byte(`{"type":"object","properties":{"affectedPaths":{"type":"array","uniqueItems":true}}}` + "\n")
	if err := os.WriteFile(artifacts.input, input, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifacts.schema, rejectedSchema, 0o600); err != nil {
		t.Fatal(err)
	}
	providerError := `{"type":"invalid_request_error","code":"invalid_json_schema","message":"uniqueItems is not permitted","status":400}`
	recovery := domain.DCPFutureArbiterSchemaRecovery{
		RecoveryID:            "dcp-future-arbiter-schema-recovery-" + incident.IdentityDigest,
		PredecessorIncidentID: incident.IncidentID, PredecessorIdentityDigest: incident.IdentityDigest,
		PredecessorInputDigest: incident.InputDigest, PredecessorModelActionID: incident.ModelActionID,
		PredecessorSchemaDigest: digestBytes(rejectedSchema), ProviderErrorJSON: providerError,
		ProviderErrorDigest: digestString(providerError), ProviderInferenceTokens: 0,
		SuccessorGeneration: 2, Status: "authorized",
	}
	if err := launcher.PreflightSchemaRecovery(context.Background(), incident, recovery); err != nil {
		t.Fatalf("exact schema recovery preflight = %v", err)
	}
	if err := os.WriteFile(artifacts.result, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := launcher.PreflightSchemaRecovery(context.Background(), incident, recovery); err == nil {
		t.Fatal("predecessor result crossed no-inference recovery fence")
	}
}
