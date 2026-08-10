// Package codex adapts the codex worker agent for code-review sessions.
package codex

import (
	"context"
	"fmt"
	"strings"

	workeragent "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/codex"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Reviewer is the codex code-review adapter.
type Reviewer struct {
	agent ports.Agent
}

// New builds the codex reviewer adapter.
func New() *Reviewer {
	return &Reviewer{agent: workeragent.New()}
}

// Harness identifies this reviewer in the reviewer registry.
func (r *Reviewer) Harness() domain.ReviewerHarness {
	return domain.ReviewerCodex
}

var _ ports.Reviewer = (*Reviewer)(nil)
var _ ports.ReviewerCanceller = (*Reviewer)(nil)
var _ ports.StructuredResultReviewer = (*Reviewer)(nil)

// RequiresStructuredResult selects Codex's native JSON Schema and last-message
// output instead of asking the model to invoke an AO bookkeeping command.
func (r *Reviewer) RequiresStructuredResult() bool { return true }

// ReviewCommand launches the reviewer with an enforced read-only filesystem
// sandbox and no interactive approval prompts. The installed Codex CLI accepts
// approval_policy through -c, but does not accept --ask-for-approval after the
// exec subcommand. Keep using the stock worker command builder for isolation,
// then replace only its reviewer-specific approval arguments here.
func (r *Reviewer) ReviewCommand(ctx context.Context, inv ports.ReviewInvocation) (ports.ReviewCommandSpec, error) {
	argv, err := r.agent.GetLaunchCommand(ctx, ports.LaunchConfig{
		SessionID:        inv.ReviewerID,
		WorkspacePath:    inv.WorkspacePath,
		Prompt:           inv.Prompt,
		SystemPrompt:     inv.SystemPrompt,
		SystemPromptFile: inv.SystemPromptFile,
		Permissions:      ports.PermissionModeAuto,
	})
	if err != nil {
		return ports.ReviewCommandSpec{}, err
	}
	argv, err = reviewerArgv(argv)
	if err != nil {
		return ports.ReviewCommandSpec{}, err
	}
	extra := []string{
		"-c", `approval_policy="never"`,
		"-c", `web_search="disabled"`,
		"--sandbox", "read-only",
	}
	if (inv.ResultSchemaFile == "") != (inv.ResultFile == "") {
		return ports.ReviewCommandSpec{}, fmt.Errorf("codex structured reviewer requires both schema and result paths")
	}
	if inv.ResultSchemaFile != "" {
		extra = append(extra, "--output-schema", inv.ResultSchemaFile, "--output-last-message", inv.ResultFile)
	}
	return ports.ReviewCommandSpec{Argv: insertBeforePrompt(argv, extra...)}, nil
}

func reviewerArgv(argv []string) ([]string, error) {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--dangerously-bypass-approvals-and-sandbox":
			return nil, fmt.Errorf("codex reviewer command attempted to bypass its read-only sandbox")
		case "--ask-for-approval":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("codex reviewer command has an incomplete --ask-for-approval option")
			}
			i++
			continue
		case "-c", "--config":
			if i+1 < len(argv) && strings.HasPrefix(argv[i+1], "approvals_reviewer=") {
				i++
				continue
			}
		}
		out = append(out, argv[i])
	}
	return out, nil
}

// ReviewMessage returns the centrally-authored task for an existing pane.
func (r *Reviewer) ReviewMessage(_ context.Context, inv ports.ReviewInvocation) (string, error) {
	return inv.Prompt, nil
}

// ReviewCancel stops the active Codex reviewer turn while preserving the
// terminal pane for inspection.
func (r *Reviewer) ReviewCancel(context.Context) (ports.ReviewCancelSpec, error) {
	return ports.ReviewCancelSpec{Mode: ports.ReviewCancelInterrupt, Interrupts: 2}, nil
}

func insertBeforePrompt(argv []string, extra ...string) []string {
	for i, arg := range argv {
		if arg == "--" {
			out := make([]string, 0, len(argv)+len(extra))
			out = append(out, argv[:i]...)
			out = append(out, extra...)
			return append(out, argv[i:]...)
		}
	}
	return append(argv, extra...)
}
