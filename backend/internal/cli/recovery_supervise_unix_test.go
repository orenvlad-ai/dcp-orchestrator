//go:build !windows

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverySuperviseSealsIdentityUsageAndStripsCredentials(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_EXTRA_SECRET", "do-not-leak")
	t.Setenv("DCP_CARD12_RECOVERY_PRIVATE", "do-not-leak")
	t.Setenv("GH_TOKEN", "do-not-leak")
	t.Setenv("GITHUB_TOKEN", "do-not-leak")

	var processExit recoveryProcessExitRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/dcp/review-lab/card12-recovery/process-exit" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &processExit); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	root := filepath.Join(cfg.dataDir, "runtime", "dcp-card12-fresh-worker-recovery", card12RecoveryID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	resultPath, logPath := filepath.Join(root, "worker-result.json"), filepath.Join(root, "worker-events.jsonl")
	threadID := "019ff589-0f9e-75f1-9abb-384bd9d8db46"
	events := `{"type":"thread.started","thread_id":"` + threadID + `"}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":900,"output_tokens":345}}` + "\n"
	out, _, err := executeCLI(t, Deps{In: strings.NewReader(""), ProcessAlive: func(int) bool { return true }},
		"recovery", "supervise", "--recovery", card12RecoveryID,
		"--identity-digest", card12RecoveryDigest, "--input-digest", strings.Repeat("a", 64),
		"--result-file", resultPath, "--log-file", logPath,
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--", "sh", "-c", `printf '%s' "$1"; printf '%s|%s|%s|%s' "${AO_DATA_DIR-unset}" "${DCP_CARD12_RECOVERY_PRIVATE-unset}" "${GH_TOKEN-unset}" "${GITHUB_TOKEN-unset}" >&2`, "sh", events)
	if err != nil {
		t.Fatalf("recovery supervise: %v", err)
	}
	if out != events {
		t.Fatalf("stdout = %q, want events", out)
	}
	if processExit.RecoveryID != card12RecoveryID || !processExit.Started || processExit.ExitCode != 0 {
		t.Fatalf("process exit = %+v", processExit)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil || string(logBytes) != events {
		t.Fatalf("sealed log = %q err=%v", logBytes, err)
	}
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result recoveryResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	if result.CodexSessionID != threadID || result.TokenCount != 1545 || result.LogDigest != digestRecoveryBytes(logBytes) || result.LogOverflow {
		t.Fatalf("sealed result = %+v", result)
	}
}

func TestRecoverySupervisorRejectsForeignIdentityAndExistingArtifacts(t *testing.T) {
	cfg := setConfigEnv(t)
	root := filepath.Join(cfg.dataDir, "runtime", "dcp-card12-fresh-worker-recovery", card12RecoveryID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	resultPath, logPath := filepath.Join(root, "worker-result.json"), filepath.Join(root, "worker-events.jsonl")
	_, _, err := executeCLI(t, Deps{}, "recovery", "supervise", "--recovery", card12RecoveryID,
		"--identity-digest", strings.Repeat("f", 64), "--input-digest", strings.Repeat("a", 64),
		"--result-file", resultPath, "--log-file", logPath,
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile, "--", "true")
	if err == nil || !strings.Contains(err.Error(), "invalid bounded card-12 recovery supervisor identity") {
		t.Fatalf("foreign identity error = %v", err)
	}
	if err := os.WriteFile(logPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = executeCLI(t, Deps{}, "recovery", "supervise", "--recovery", card12RecoveryID,
		"--identity-digest", card12RecoveryDigest, "--input-digest", strings.Repeat("a", 64),
		"--result-file", resultPath, "--log-file", logPath,
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile, "--", "true")
	if err == nil || !strings.Contains(err.Error(), "output exists before process start") {
		t.Fatalf("existing artifact error = %v", err)
	}
}
