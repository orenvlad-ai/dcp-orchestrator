//go:build !windows

package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAgentProcessSuperviseReportsExitAndPreservesOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--", "sh", "-c", "printf supervised; exit 23")
	if err != nil {
		t.Fatalf("supervise returned child exit as command failure: %v\nstderr=%s", err, errOut)
	}
	if out != "supervised" {
		t.Fatalf("stdout = %q, want supervised", out)
	}
	var req setActivityAPIRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatal(err)
	}
	want := setActivityAPIRequest{State: "exited", Event: "process-exited", LaunchID: "launch-3"}
	if req != want {
		t.Fatalf("exit report = %+v, want %+v", req, want)
	}
}

func TestAgentProcessSuperviseOneShotSuccessReportsActiveThenIdle(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--idle-on-success", "--", "sh", "-c", "exit 0")
	if err != nil {
		t.Fatalf("supervise failed: %v\nstderr=%s", err, errOut)
	}
	assertSupervisedStates(t, capture, "active", "idle")
}

func TestAgentProcessSupervisorConnectionIsNotInheritedByChild(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3",
		"--supervisor-data-dir", cfg.dataDir, "--supervisor-run-file", cfg.runFile,
		"--idle-on-success", "--", "sh", "-c", `printf '%s|%s' "${AO_DATA_DIR-unset}" "${AO_RUN_FILE-unset}"`)
	if err != nil {
		t.Fatalf("supervise failed: %v\nstderr=%s", err, errOut)
	}
	if out != "unset|unset" {
		t.Fatalf("child connection env = %q, want unset|unset", out)
	}
	assertSupervisedStates(t, capture, "active", "idle")
}

func TestAgentProcessSupervisorConnectionRequiresExactPair(t *testing.T) {
	_, _, err := executeCLI(t, Deps{}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3",
		"--supervisor-data-dir", "/absolute/data", "--", "true")
	if err == nil || !strings.Contains(err.Error(), "exact absolute data-dir and run-file") {
		t.Fatalf("partial supervisor connection error = %v", err)
	}
}

func TestAgentProcessSuperviseOneShotNonZeroReportsActiveThenExited(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--idle-on-success", "--", "sh", "-c", "exit 23")
	if err != nil {
		t.Fatalf("supervise returned child exit as command failure: %v", err)
	}
	assertSupervisedStates(t, capture, "active", "exited")
}

func TestAgentProcessSuperviseOneShotSignalReportsExited(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--idle-on-success", "--", "sh", "-c", "kill -TERM $$")
	if err != nil {
		t.Fatalf("supervise returned child signal as command failure: %v", err)
	}
	assertSupervisedStates(t, capture, "active", "exited")
}

func TestAgentProcessSuperviseOneShotLaunchFailureReportsExited(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--idle-on-success", "--", "/definitely/missing/dcp-one-shot")
	if err != nil {
		t.Fatalf("supervise returned launch failure as command failure: %v", err)
	}
	assertSupervisedStates(t, capture, "exited")
}

func assertSupervisedStates(t *testing.T, capture *activityCapture, want ...string) {
	t.Helper()
	if len(capture.bodies) != len(want) {
		t.Fatalf("activity reports = %d, want %d", len(capture.bodies), len(want))
	}
	for i, body := range capture.bodies {
		var req setActivityAPIRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		if req.State != want[i] {
			t.Fatalf("activity report %d state = %q, want %q", i, req.State, want[i])
		}
	}
}

func TestAgentProcessSuperviseRejectsInvalidGeneration(t *testing.T) {
	_, _, err := executeCLI(t, Deps{}, "agent-process", "supervise", "--session", "ao-7", "--launch", "../stale", "--", "true")
	if err == nil {
		t.Fatal("invalid launch id should be rejected before starting the child")
	}
}
