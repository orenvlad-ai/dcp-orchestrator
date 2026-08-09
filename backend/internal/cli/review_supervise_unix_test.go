//go:build !windows

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewSuperviseReportsExitAndStripsSupervisorConnection(t *testing.T) {
	cfg := setConfigEnv(t)
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
		"--", "sh", "-c", `printf '%s|%s' "${AO_DATA_DIR-unset}" "${AO_RUN_FILE-unset}"; exit 23`)
	if err == nil {
		t.Fatal("review supervisor should preserve the child failure exit")
	}
	if out != "unset|unset" {
		t.Fatalf("child connection env = %q, want unset|unset", out)
	}
	if gotPath != "/api/v1/sessions/ao-7/reviews/process-exit" || !got.Started || got.ExitCode != 23 || len(got.RunIDs) != 2 {
		t.Fatalf("process exit report path=%q body=%+v", gotPath, got)
	}
}
