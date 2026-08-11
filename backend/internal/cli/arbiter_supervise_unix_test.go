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

func TestArbiterSuperviseSubmitsOneBoundedResultAndStripsCredentials(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_EXTRA_SECRET", "do-not-leak")
	t.Setenv("DCP_ARBITER_PRIVATE", "do-not-leak")
	t.Setenv("GH_TOKEN", "do-not-leak")
	t.Setenv("GITHUB_TOKEN", "do-not-leak")

	incident := "dcp-global-release-" + strings.Repeat("a", 64)
	var decision arbiterDecisionRequest
	var processExit arbiterProcessExitRequest
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/internal/dcp/review-lab/arbiter/decision":
			if err := json.Unmarshal(body, &decision); err != nil {
				t.Fatal(err)
			}
		case "/internal/dcp/review-lab/arbiter/process-exit":
			if err := json.Unmarshal(body, &processExit); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	root := filepath.Join(cfg.dataDir, "runtime", "dcp-arbiter", incident)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, "schema.json")
	resultPath := filepath.Join(root, "result.json")
	if err := os.WriteFile(schemaPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resultJSON := `{"schemaVersion":"dcp.review-lab.global-release-arbiter-decision/v1","incidentId":"` + incident + `"}`

	out, _, err := executeCLI(t, Deps{In: strings.NewReader(""), ProcessAlive: func(int) bool { return true }},
		"arbiter", "supervise", "--handle", "dcp-global-release-arbiter-v1", "--incident", incident,
		"--identity-digest", strings.Repeat("b", 64), "--input-digest", strings.Repeat("c", 64),
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--result-file", resultPath, "--result-schema", schemaPath,
		"--", "sh", "-c", `printf '%s' "$1" > "$2"; printf '%s|%s|%s|%s|%s|%s' "${AO_DATA_DIR-unset}" "${AO_RUN_FILE-unset}" "${AO_EXTRA_SECRET-unset}" "${DCP_ARBITER_PRIVATE-unset}" "${GH_TOKEN-unset}" "${GITHUB_TOKEN-unset}"`,
		"sh", resultJSON, resultPath)
	if err != nil {
		t.Fatalf("arbiter supervise: %v", err)
	}
	if out != "unset|unset|unset|unset|unset|unset" {
		t.Fatalf("child connection/credential env = %q, want all unset", out)
	}
	if len(paths) != 2 || paths[0] != "/internal/dcp/review-lab/arbiter/decision" || paths[1] != "/internal/dcp/review-lab/arbiter/process-exit" {
		t.Fatalf("callback paths = %v", paths)
	}
	if decision.IncidentID != incident || string(decision.Decision) != resultJSON {
		t.Fatalf("decision = %+v", decision)
	}
	if processExit.IncidentID != incident || !processExit.Started || processExit.ExitCode != 0 || processExit.ResultFailure != "" {
		t.Fatalf("process exit = %+v", processExit)
	}
	for _, artifact := range []string{resultPath, schemaPath} {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("transient arbiter artifact remains at %s: %v", artifact, err)
		}
	}
}

func TestArbiterSuperviseRejectsPreexistingResultBeforeProcessStart(t *testing.T) {
	cfg := setConfigEnv(t)
	incident := "dcp-global-release-" + strings.Repeat("a", 64)
	root := filepath.Join(cfg.dataDir, "runtime", "dcp-arbiter", incident)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, "schema.json")
	resultPath := filepath.Join(root, "result.json")
	if err := os.WriteFile(schemaPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCLI(t, Deps{}, "arbiter", "supervise",
		"--handle", "dcp-global-release-arbiter-v1", "--incident", incident,
		"--identity-digest", strings.Repeat("b", 64), "--input-digest", strings.Repeat("c", 64),
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--result-file", resultPath, "--result-schema", schemaPath, "--", "true")
	if err == nil || !strings.Contains(err.Error(), "result exists before process start") {
		t.Fatalf("preexisting result error = %v", err)
	}
}
