package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectModelSupervisorWritesOneExactTerminalArtifact(t *testing.T) {
	dataDir := t.TempDir()
	action := "v2-" + strings.Repeat("a", 40)
	runtime := "v2-" + strings.Repeat("b", 40)
	root := filepath.Join(dataDir, "runtime", "dcp-v2", action, runtime)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	opts := directModelSuperviseOptions{action: action, runtime: runtime, fence: "model:" + action + ":" + runtime,
		role: "reviewer", messageFile: filepath.Join(root, "last-message.json"), resultFile: filepath.Join(root, "terminal.json"),
		supervisorDataDir: dataDir, supervisorRun: filepath.Join(dataDir, "ao.run")}
	if err := validateDirectModelSupervisorOptions(opts); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC)
	ctx := &commandContext{deps: Deps{In: bytes.NewReader(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: func() time.Time { return now }}.withDefaults()}
	command := `printf '%s' '{"verdict":"approved","headSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}' > "$1"`
	if err := ctx.runDirectModelSupervisor(t.Context(), opts, []string{"/bin/sh", "-c", command, "supervisor", opts.messageFile}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(opts.resultFile)
	if err != nil {
		t.Fatal(err)
	}
	var result directModelSupervisorResult
	if err := json.Unmarshal(data, &result); err != nil || !result.Started || result.ExitCode != 0 || result.ActionID != action ||
		result.RuntimeID != runtime || result.LaunchFence != opts.fence || result.CompletedAt != now ||
		result.OutputDigest != directModelOutputDigest(json.RawMessage(result.OutputJSON)) {
		t.Fatalf("terminal result=%+v err=%v", result, err)
	}
	if err := ctx.runDirectModelSupervisor(t.Context(), opts, []string{"/bin/true"}); err == nil {
		t.Fatal("equal supervisor replay overwrote the immutable terminal artifact")
	}
}

func TestDirectModelSupervisorRejectsUnstructuredDecisionAndForeignArtifactPath(t *testing.T) {
	dataDir := t.TempDir()
	action := "v2-" + strings.Repeat("c", 40)
	runtime := "v2-" + strings.Repeat("d", 40)
	root := filepath.Join(dataDir, "runtime", "dcp-v2", action, runtime)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	opts := directModelSuperviseOptions{action: action, runtime: runtime, fence: "model:" + action + ":" + runtime,
		role: "arbiter", messageFile: filepath.Join(root, "last-message.json"), resultFile: filepath.Join(root, "terminal.json"),
		supervisorDataDir: dataDir, supervisorRun: filepath.Join(dataDir, "ao.run")}
	foreign := opts
	foreign.resultFile = filepath.Join(dataDir, "foreign.json")
	if err := validateDirectModelSupervisorOptions(foreign); err == nil {
		t.Fatal("foreign terminal artifact path was accepted")
	}
	ctx := &commandContext{deps: Deps{In: bytes.NewReader(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Now: time.Now}.withDefaults()}
	command := `printf '%s' 'not-json' > "$1"`
	if err := ctx.runDirectModelSupervisor(t.Context(), opts, []string{"/bin/sh", "-c", command, "supervisor", opts.messageFile}); err == nil {
		t.Fatal("unstructured arbiter decision unexpectedly succeeded")
	}
	data, err := os.ReadFile(opts.resultFile)
	if err != nil {
		t.Fatal(err)
	}
	var result directModelSupervisorResult
	if err := json.Unmarshal(data, &result); err != nil || result.ExitCode == 0 || result.OutputJSON != `{}` {
		t.Fatalf("fail-closed terminal result=%+v err=%v", result, err)
	}
}

func TestDirectModelChildEnvironmentExposesOnlyRuntimeAndCodexAuthLocation(t *testing.T) {
	got := directModelChildEnv([]string{"HOME=/home/test", "PATH=/bin", "CODEX_HOME=/auth", "LC_ALL=C",
		"GH_TOKEN=secret", "GITHUB_TOKEN=secret", "OPENAI_API_KEY=secret", "AWS_SECRET_ACCESS_KEY=secret",
		"AO_DATA_DIR=/private", "DCP_V2_ACTION_ID=private", "SSH_AUTH_SOCK=/agent", "GIT_CONFIG_KEY_0=credential.helper"})
	joined := strings.Join(got, "\n")
	for _, want := range []string{"HOME=/home/test", "PATH=/bin", "CODEX_HOME=/auth", "LC_ALL=C"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("safe child environment lost %q: %q", want, joined)
		}
	}
	for _, secret := range []string{"GH_TOKEN", "GITHUB_TOKEN", "OPENAI_API_KEY", "AWS_SECRET_ACCESS_KEY",
		"AO_DATA_DIR", "DCP_V2_ACTION_ID", "SSH_AUTH_SOCK", "GIT_CONFIG_KEY_0"} {
		if strings.Contains(joined, secret+"=") {
			t.Fatalf("child environment exposed %s: %q", secret, joined)
		}
	}
}

func TestDirectModelOutputRejectsDuplicateJSONKeys(t *testing.T) {
	if err := rejectDuplicateDirectModelJSONKeys([]byte(`{"verdict":"approved","verdict":"changes_requested"}`)); err == nil {
		t.Fatal("ambiguous duplicate-key model output was accepted")
	}
}
