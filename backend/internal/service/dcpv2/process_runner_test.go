package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestProcessModelRunnerReadsOnlyExactBoundedTerminalReceipt(t *testing.T) {
	dataDir := t.TempDir()
	actionID := "v2-" + strings.Repeat("a", 40)
	runtimeID := "v2-" + strings.Repeat("b", 40)
	head := strings.Repeat("c", 40)
	taskInput := `{"baseSha":"` + strings.Repeat("f", 40) + `","prompt":"test task"}`
	commandInput := `{"head":"` + head + `"}`
	request := ports.DCPV2ModelLaunchRequest{TaskID: "task", RevisionID: "revision", CommandID: "command", ActionID: actionID,
		Role: domain.DCPV2ActionReviewer, Attempt: 1, Model: "codex/default", Reasoning: "high", TokenBudget: 100,
		TimeBudgetSec: 60, InputDigest: strings.Repeat("d", 64), PromptDigest: digestCanonical(json.RawMessage(commandInput)),
		TaskInputJSON: taskInput, TaskInputDigest: digestCanonical(json.RawMessage(taskInput)), CommandPayloadJSON: commandInput,
		Repository: TwinRepository, BaseRef: TwinBase, BaseSHA: strings.Repeat("f", 40), HeadRef: "codex/direct", HeadSHA: head,
		Branch: "codex/direct", Worktree: filepath.Join(dataDir, "worktree"), WorktreeDigest: strings.Repeat("1", 64),
		LaunchFence: "model:" + actionID + ":" + runtimeID, EffectFence: "model:" + actionID + ":" + runtimeID,
		RuntimeID: runtimeID, ExpectedOldHead: head}
	runner := &processModelRunner{dataDir: dataDir, command: func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, "\x00") {
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return request.Branch, nil
		case "rev-parse\x00HEAD":
			return request.HeadSHA, nil
		default:
			return "", errors.New("unexpected review Git check")
		}
	}}
	root, _, _, resultPath := runner.artifactPaths(request)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	output := `{"verdict":"approved","headSha":"` + head + `","findings":[]}`
	result := directSupervisorResult{ActionID: actionID, RuntimeID: runtimeID, LaunchFence: request.LaunchFence,
		Started: true, ExitCode: 0, OutputJSON: output, OutputDigest: digestCanonical(json.RawMessage(output)),
		CompletedAt: time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC)}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := runner.Terminal(t.Context(), request)
	if err != nil || !found || receipt.ActionID != actionID || receipt.RuntimeID != runtimeID ||
		receipt.Status != domain.DCPV2ModelTerminalSucceeded || receipt.OutputJSON != output || receipt.ResultDigest == "" {
		t.Fatalf("terminal receipt=%+v found=%t err=%v", receipt, found, err)
	}
	result.RuntimeID = "v2-" + strings.Repeat("9", 40)
	data, _ = json.Marshal(result)
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.Terminal(t.Context(), request); err == nil {
		t.Fatal("crossed supervisor runtime identity was accepted")
	}
	crossedInput := request
	crossedInput.CommandPayloadJSON = `{"head":"` + strings.Repeat("9", 40) + `"}`
	if _, _, err := runner.Terminal(t.Context(), crossedInput); err == nil {
		t.Fatal("crossed digest-bound Command input was accepted")
	}
}

func TestDirectReadOnlyModelArgvRejectsBypassAndRemovesWritableAuthority(t *testing.T) {
	argv := []string{"codex", "exec", "-c", `approval_policy="on-request"`, "-c", `approvals_reviewer="auto_review"`,
		"--sandbox", "workspace-write", "--add-dir", "/tmp/git", "--", "prompt"}
	got, err := directReadOnlyModelArgv(argv)
	if err != nil || strings.Join(got, " ") != "codex exec -- prompt" {
		t.Fatalf("read-only argv=%q err=%v", got, err)
	}
	if _, err := directReadOnlyModelArgv([]string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--", "prompt"}); err == nil {
		t.Fatal("read-only direct role accepted a sandbox bypass")
	}
	if _, err := directReadOnlyModelArgv([]string{"codex", "exec", "-c", "sandbox_workspace_write.network_access=true", "--", "prompt"}); err == nil {
		t.Fatal("read-only direct role accepted network access")
	}
}
