package codex

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type captureAgent struct {
	got  ports.LaunchConfig
	argv []string
}

func (a *captureAgent) GetConfigSpec(context.Context) (ports.ConfigSpec, error) {
	return ports.ConfigSpec{}, nil
}
func (a *captureAgent) GetLaunchCommand(_ context.Context, cfg ports.LaunchConfig) ([]string, error) {
	a.got = cfg
	if a.argv != nil {
		return a.argv, nil
	}
	return []string{"agent", "exec", "--ask-for-approval", "on-request", "-c", `approvals_reviewer="auto_review"`, "--", cfg.Prompt}, nil
}
func (a *captureAgent) GetPromptDeliveryStrategy(context.Context, ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	return ports.PromptDeliveryInCommand, nil
}
func (a *captureAgent) GetAgentHooks(context.Context, ports.WorkspaceHookConfig) error { return nil }
func (a *captureAgent) GetRestoreCommand(context.Context, ports.RestoreConfig) ([]string, bool, error) {
	return nil, false, nil
}
func (a *captureAgent) SessionInfo(context.Context, ports.SessionRef) (ports.SessionInfo, bool, error) {
	return ports.SessionInfo{}, false, nil
}

func TestReviewCommandUsesReadOnlySandbox(t *testing.T) {
	t.Setenv("AO_PORT", "3103")
	t.Setenv("AO_DATA_DIR", "/tmp/ao data")
	t.Setenv("AO_RUN_FILE", "/tmp/ao data/running.json")
	agent := &captureAgent{}
	r := &Reviewer{agent: agent}

	got, err := r.ReviewCommand(context.Background(), ports.ReviewInvocation{
		ReviewerID:       "review-w1",
		WorkspacePath:    "/ws/w1",
		Prompt:           "review it",
		SystemPrompt:     "review only",
		ResultSchemaFile: "/ao/results/schema.json",
		ResultFile:       "/ao/results/result.json",
	})
	if err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}

	want := []string{
		"agent", "exec",
		"-c", `approval_policy="never"`,
		"-c", `web_search="disabled"`,
		"--sandbox", "read-only",
		"--output-schema", "/ao/results/schema.json",
		"--output-last-message", "/ao/results/result.json",
		"--", "review it",
	}
	if !slices.Equal(got.Argv, want) {
		t.Fatalf("argv = %#v, want %#v", got.Argv, want)
	}
	if agent.got.Permissions != ports.PermissionModeAuto {
		t.Fatalf("permissions = %q, want auto", agent.got.Permissions)
	}
	if agent.got.SystemPrompt != "review only" {
		t.Fatalf("system prompt = %q", agent.got.SystemPrompt)
	}
	for _, arg := range got.Argv {
		if strings.Contains(arg, "AO_PORT") || strings.Contains(arg, "AO_DATA_DIR") || strings.Contains(arg, "AO_RUN_FILE") || strings.Contains(arg, "network_access=true") {
			t.Fatalf("reviewer command leaks control-plane/network configuration: %#v", got.Argv)
		}
	}
}

func TestReviewCommandRejectsPartialStructuredPaths(t *testing.T) {
	agent := &captureAgent{}
	_, err := (&Reviewer{agent: agent}).ReviewCommand(context.Background(), ports.ReviewInvocation{
		Prompt: "review", ResultFile: "/ao/results/result.json",
	})
	if err == nil || !strings.Contains(err.Error(), "both schema and result") {
		t.Fatalf("ReviewCommand error = %v, want partial structured path rejection", err)
	}
}

func TestReviewCommandRejectsSandboxBypass(t *testing.T) {
	agent := &captureAgent{argv: []string{"agent", "exec", "--dangerously-bypass-approvals-and-sandbox", "--", "review"}}
	_, err := (&Reviewer{agent: agent}).ReviewCommand(context.Background(), ports.ReviewInvocation{Prompt: "review"})
	if err == nil || !strings.Contains(err.Error(), "bypass") {
		t.Fatalf("ReviewCommand error = %v, want sandbox bypass rejection", err)
	}
}

func TestReviewCommandReplacesWorkerConfigApprovalAndSandbox(t *testing.T) {
	agent := &captureAgent{argv: []string{
		"agent", "exec",
		"-c", `approval_policy="on-request"`,
		"-c", `approvals_reviewer="auto_review"`,
		"--sandbox", "workspace-write",
		"--add-dir", "/repo/.git/worktrees/worker",
		"--add-dir", "/repo/.git",
		"--", "review",
	}}
	got, err := (&Reviewer{agent: agent}).ReviewCommand(context.Background(), ports.ReviewInvocation{Prompt: "review"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Argv, " ")
	if strings.Contains(joined, `approval_policy="on-request"`) || strings.Contains(joined, "workspace-write") || strings.Contains(joined, "--add-dir") || strings.Contains(joined, "/repo/.git") {
		t.Fatalf("reviewer retained worker approval/sandbox: %#v", got.Argv)
	}
	if !strings.Contains(joined, `approval_policy="never"`) || !strings.Contains(joined, "--sandbox read-only") {
		t.Fatalf("reviewer command lost enforced read-only policy: %#v", got.Argv)
	}
}

func TestReviewCommandRejectsIncompleteWorkerGitMetadataFlag(t *testing.T) {
	agent := &captureAgent{argv: []string{"agent", "exec", "--add-dir"}}
	_, err := (&Reviewer{agent: agent}).ReviewCommand(context.Background(), ports.ReviewInvocation{Prompt: "review"})
	if err == nil || !strings.Contains(err.Error(), "incomplete --add-dir") {
		t.Fatalf("ReviewCommand error = %v, want incomplete Git metadata flag rejection", err)
	}
}

func TestReviewMessageReturnsTaskPrompt(t *testing.T) {
	got, err := (&Reviewer{}).ReviewMessage(context.Background(), ports.ReviewInvocation{Prompt: "next review"})
	if err != nil {
		t.Fatalf("ReviewMessage: %v", err)
	}
	if got != "next review" {
		t.Fatalf("message = %q", got)
	}
}

func TestReviewCommandUsesHiddenSystemPromptFile(t *testing.T) {
	agent := &captureAgent{}
	r := &Reviewer{agent: agent}

	if _, err := r.ReviewCommand(context.Background(), ports.ReviewInvocation{
		Prompt:           "Start the AO review task.",
		SystemPromptFile: "/ao/prompts/reviewer/system.md",
	}); err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}
	if agent.got.Prompt != "Start the AO review task." || agent.got.SystemPrompt != "" || agent.got.SystemPromptFile != "/ao/prompts/reviewer/system.md" {
		t.Fatalf("launch config = %+v", agent.got)
	}
}
