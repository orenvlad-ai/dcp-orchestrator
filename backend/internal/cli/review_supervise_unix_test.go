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

func TestReviewSuperviseReportsExitAndStripsSupervisorConnection(t *testing.T) {
	cfg := setConfigEnv(t)
	t.Setenv("AO_EXTRA_SECRET", "do-not-leak")
	t.Setenv("GH_TOKEN", "do-not-leak")
	var got reviewProcessExitRequest
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reviews":[]}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, _, err := executeCLI(t, Deps{In: strings.NewReader(""), ProcessAlive: func(int) bool { return true }},
		"review", "supervise", "--session", "ao-7", "--run", "run-1", "--run", "run-2",
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--", "sh", "-c", `printf '%s|%s|%s|%s' "${AO_DATA_DIR-unset}" "${AO_RUN_FILE-unset}" "${AO_EXTRA_SECRET-unset}" "${GH_TOKEN-unset}"; exit 23`)
	if err == nil {
		t.Fatal("review supervisor should preserve the child failure exit")
	}
	if out != "unset|unset|unset|unset" {
		t.Fatalf("child connection/credential env = %q, want all unset", out)
	}
	if gotPath != "/api/v1/sessions/ao-7/reviews/process-exit" || !got.Started || got.ExitCode != 23 || len(got.RunIDs) != 2 {
		t.Fatalf("process exit report path=%q body=%+v", gotPath, got)
	}
}

func TestReviewSuperviseSubmitsOneValidatedStructuredResult(t *testing.T) {
	cfg := setConfigEnv(t)
	var submitted submitReviewRequest
	var exit reviewProcessExitRequest
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/v1/sessions/ao-7/reviews/submit":
			if err := json.Unmarshal(body, &submitted); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"review":{"id":"run-1","status":"complete","verdict":"approved"},"reviews":[]}`)
		case "/api/v1/sessions/ao-7/reviews/process-exit":
			if err := json.Unmarshal(body, &exit); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"reviews":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	root := filepath.Join(cfg.dataDir, "runtime", "reviewer-results", "ao-7", "batch-1", "run-1")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, "schema.json")
	resultPath := filepath.Join(root, "result.json")
	if err := os.WriteFile(schemaPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resultJSON := `{"version":1,"workerSessionId":"ao-7","reviewerHandleId":"review-ao-7","batchId":"batch-1","runId":"run-1","prUrl":"https://github.com/o/r/pull/1","targetSha":"1111111111111111111111111111111111111111","verdict":"approved","summary":"No blocking findings.","findings":[]}`

	_, _, err := executeCLI(t, Deps{In: strings.NewReader(""), ProcessAlive: func(int) bool { return true }},
		"review", "supervise", "--session", "ao-7", "--run", "run-1",
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--reviewer-handle", "review-ao-7", "--batch", "batch-1",
		"--pr-url", "https://github.com/o/r/pull/1", "--target-sha", "1111111111111111111111111111111111111111",
		"--result-file", resultPath, "--result-schema", schemaPath,
		"--", "sh", "-c", `printf '%s' "$1" > "$2"`, "sh", resultJSON, resultPath)
	if err != nil {
		t.Fatalf("review supervise: %v", err)
	}
	if submitted.StructuredResult == nil || submitted.StructuredResult.RunID != "run-1" || submitted.StructuredResult.Verdict != "approved" {
		t.Fatalf("submitted = %+v", submitted.StructuredResult)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/sessions/ao-7/reviews/submit" || paths[1] != "/api/v1/sessions/ao-7/reviews/process-exit" || exit.ResultFailure != "" || exit.ExitCode != 0 {
		t.Fatalf("paths=%v exit=%+v", paths, exit)
	}
	for _, artifact := range []string{resultPath, schemaPath} {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("transient artifact remains at %s: %v", artifact, err)
		}
	}
}

func TestReviewSuperviseRejectsMissingStructuredResultWithoutSubmit(t *testing.T) {
	cfg := setConfigEnv(t)
	var gotPath string
	var exit reviewProcessExitRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if gotPath == "/api/v1/sessions/ao-7/reviews/submit" {
			t.Fatal("missing result reached the verdict submit path")
		}
		if err := json.NewDecoder(r.Body).Decode(&exit); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"reviews":[]}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	root := filepath.Join(cfg.dataDir, "runtime", "reviewer-results", "ao-7", "batch-1", "run-1")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(root, "schema.json")
	resultPath := filepath.Join(root, "result.json")
	if err := os.WriteFile(schemaPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeCLI(t, Deps{In: strings.NewReader(""), ProcessAlive: func(int) bool { return true }},
		"review", "supervise", "--session", "ao-7", "--run", "run-1",
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--reviewer-handle", "review-ao-7", "--batch", "batch-1",
		"--pr-url", "https://github.com/o/r/pull/1", "--target-sha", "1111111111111111111111111111111111111111",
		"--result-file", resultPath, "--result-schema", schemaPath,
		"--", "sh", "-c", "true")
	if err == nil {
		t.Fatal("missing structured result should fail the supervisor")
	}
	if gotPath != "/api/v1/sessions/ao-7/reviews/process-exit" || exit.ResultFailure != "missing_result" {
		t.Fatalf("path=%q exit=%+v", gotPath, exit)
	}
}
