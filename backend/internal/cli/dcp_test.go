package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestDCPPolicySubmitResponsePreservesImmutablePolicyIdentity(t *testing.T) {
	const serverResponse = `{"task":{"taskId":"price-arch-v1","target":"wb-price-extension","profile":"repo-only","repository":"orenvlad-ai/wb-price-extension","sessionId":"wb-price-extension-1","cardNumber":1,"worktreePath":"/tmp/wb-price-extension-1","sourceBranch":"ao/wb-price-extension-1/root","state":"worker_running","revision":3},"duplicate":false}`

	var response dcpPolicySubmitResponse
	if err := json.Unmarshal([]byte(serverResponse), &response); err != nil {
		t.Fatal(err)
	}
	if response.Task.TaskID != "price-arch-v1" || response.Task.Target != "wb-price-extension" ||
		response.Task.Profile != "repo-only" || response.Task.Repository != "orenvlad-ai/wb-price-extension" {
		t.Fatalf("immutable response identity was dropped: %+v", response.Task)
	}

	var output bytes.Buffer
	if err := writeJSON(&output, response); err != nil {
		t.Fatal(err)
	}
	var projected map[string]any
	if err := json.Unmarshal(output.Bytes(), &projected); err != nil {
		t.Fatal(err)
	}
	task, ok := projected["task"].(map[string]any)
	if !ok || task["taskId"] != "price-arch-v1" || task["target"] != "wb-price-extension" ||
		task["profile"] != "repo-only" || task["repository"] != "orenvlad-ai/wb-price-extension" {
		t.Fatalf("CLI JSON drifted from the immutable server tuple: %s", output.String())
	}
}

func TestDCPV2Stage5ActivateValidatesBeforeOpeningAndReplaysExactly(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("AO_DATA_DIR", dataDir)
	t.Setenv("AO_RUN_FILE", filepath.Join(t.TempDir(), "running.json"))
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC)
	targetPath := filepath.Join(t.TempDir(), "targets", "dcp-wbc-integration-lab")
	deps := Deps{In: bytes.NewReader(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: func() time.Time { return now }}

	invalid := NewRootCommand(deps)
	invalid.SetArgs([]string{"dcp", "stage5-activate", "--source-commit", "bad", "--source-tree", strings.Repeat("b", 40), "--install-receipt-sha", strings.Repeat("c", 64)})
	if err := invalid.Execute(); err == nil {
		t.Fatal("invalid source identity unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "ao.db")); !os.IsNotExist(err) {
		t.Fatalf("invalid input opened the database: %v", err)
	}

	for i, wantCreated := range []bool{true, false} {
		var out bytes.Buffer
		deps.Out = &out
		cmd := NewRootCommand(deps)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"dcp", "stage5-activate", "--source-commit", strings.Repeat("a", 40), "--source-tree", strings.Repeat("b", 40), "--install-receipt-sha", strings.Repeat("c", 64), "--target-path", targetPath, "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("activation attempt %d: %v\n%s", i+1, err, out.String())
		}
		var response dcpV2Stage5ActivateResponse
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatalf("decode activation attempt %d: %v\n%s", i+1, err, out.String())
		}
		if response.Created != wantCreated || response.ProjectCreated != wantCreated || response.ProjectID != "dcp-wbc-integration-lab" || response.ProjectPath != targetPath || response.Activation.TargetPolicyDigest != domain.DCPWBCIntegrationTwinPolicyDigest() {
			t.Fatalf("activation attempt %d response=%+v", i+1, response)
		}
	}
	store, err := sqlite.OpenReadOnly(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	activation, err := store.GetDCPV2Stage5Activation(t.Context())
	if err != nil || !activation.ActivatedAt.Equal(now) {
		t.Fatalf("read activation: activation=%+v err=%v", activation, err)
	}
	project, found, err := store.GetProject(t.Context(), "dcp-wbc-integration-lab")
	if err != nil || !found || project.Path != targetPath || project.RegisteredAt != activation.ActivatedAt {
		t.Fatalf("read twin project: project=%+v found=%t err=%v", project, found, err)
	}
}

func TestDCPV2Stage6RecoveryPreflightValidatesBeforeOpening(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("AO_DATA_DIR", dataDir)
	t.Setenv("AO_RUN_FILE", filepath.Join(t.TempDir(), "running.json"))
	cmd := NewRootCommand(Deps{In: bytes.NewReader(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	cmd.SetArgs([]string{"dcp", "stage6-recovery-preflight", "--source-commit", strings.Repeat("A", 40),
		"--source-tree", strings.Repeat("b", 40), "--install-receipt-sha", strings.Repeat("c", 64), "--json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("uppercase source identity unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "ao.db")); !os.IsNotExist(err) {
		t.Fatalf("invalid Stage 6 input opened the database: %v", err)
	}
}

func TestDCPV2Stage6RecoveryResponseUsesOnlyCanonicalLowerCamelFields(t *testing.T) {
	response := dcpV2Stage6RecoveryResponse{
		SchemaVersion: "dcp.v2.stage6-native-shell-recovery/v1", InstalledSourceCommit: strings.Repeat("a", 40),
		InstalledSourceTree: strings.Repeat("b", 40), InstallReceiptSHA: strings.Repeat("c", 64),
		Stage5ActivationID: "dcp-v2-twin-stage5", Stage5SourceCommit: dcpV2Stage5SourceCommit,
		Stage5SourceTree: dcpV2Stage5SourceTree, Stage5ReceiptSHA: dcpV2Stage5ReceiptSHA,
		TaskID: "dcp-v2-twin-canary-v1", RevisionID: "revision", CommandID: "command", ActionID: "action",
		BaseSHA: strings.Repeat("d", 40), Ready: true,
	}
	var out bytes.Buffer
	if err := writeJSON(&out, response); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"schemaVersion", "installedSourceCommit", "installedSourceTree", "installReceiptSha",
		"stage5ActivationId", "stage5SourceCommit", "stage5SourceTree", "stage5ReceiptSha", "taskId",
		"revisionId", "commandId", "actionId", "baseSha", "ready"}
	if len(fields) != len(want) {
		t.Fatalf("response fields=%v", fields)
	}
	for _, name := range want {
		if _, ok := fields[name]; !ok {
			t.Fatalf("canonical lower-camel field %q is absent: %s", name, out.String())
		}
	}
	for name := range fields {
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			t.Fatalf("uppercase response field %q is forbidden", name)
		}
	}
}
