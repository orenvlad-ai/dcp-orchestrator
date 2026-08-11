// Package codex implements the Codex agent adapter: launching new sessions,
// resuming hook-tracked sessions, installing workspace-local hooks, and reading
// hook-derived session info.
//
// AO-managed sessions derive native session identity and display
// metadata from Codex hooks instead of transcript/cache scans.
package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// Plugin is the Codex agent adapter. It is safe for concurrent use; the binary
// path is resolved once and cached under binaryMu.
type Plugin struct {
	agentbase.Base
	binaryMu       sync.Mutex
	resolvedBinary string
}

// New returns a ready-to-register Codex adapter.
func New() *Plugin {
	return &Plugin{}
}

// EmitsSubmitActivity is false for the DCP lab: its Codex worker disables all
// hooks and relies on the existing process supervisor for lifecycle state.
func (p *Plugin) EmitsSubmitActivity() bool { return false }

// EmitsBlockedActivity is false: codex reports permission prompts as
// waiting_input — it installs no post-tool-use hook, so a blocked state could
// never be cleared mid-turn. confirmActive must not nudge it (an Enter could
// answer a pending decision it cannot report as blocked). See
// ports.ActivitySignaler.
func (p *Plugin) EmitsBlockedActivity() bool { return false }

// ExitDetectionMode opts the DCP one-shot Codex worker into AO's process
// supervisor. A zero process exit is an ordinary completed turn; every other
// machine outcome remains exited.
func (p *Plugin) ExitDetectionMode() ports.AgentExitDetectionMode {
	return ports.AgentExitDetectionSupervisorIdleOnSuccess
}

// SteersActiveTurn is true: submitting input to the codex TUI mid-turn steers
// the running turn rather than being swallowed or queued, so AO may write an
// unsolicited coordination message into an active codex session. See
// ports.ActiveTurnSteerer.
func (p *Plugin) SteersActiveTurn() bool { return true }

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.ActiveTurnSteerer = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)
var _ ports.TerminalActivityDetector = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          "codex",
		Name:        "Codex",
		Description: "Run Codex worker sessions.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the per-project agent config keys Codex understands.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "model",
				Type:        ports.ConfigFieldString,
				Description: "Model override passed to `codex --model`.",
			},
		},
	}, nil
}

// GetLaunchCommand builds the argv to start a new Codex session. The DCP lab
// uses Codex's supported non-interactive, ephemeral surface so authentication
// remains available while user config, MCP, apps, plugins, and hooks do not.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	binary, err := p.codexBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary, "exec", "--ignore-user-config", "--ephemeral", "--strict-config"}
	appendWorkerIsolationFlags(&cmd)
	appendNoUpdateCheckFlag(&cmd)
	appendHideRateLimitNudgeFlag(&cmd)
	if err := appendApprovalFlags(&cmd, cfg.Permissions); err != nil {
		return nil, err
	}
	if err := appendWorkspaceGitMetadataFlags(ctx, &cmd, cfg.Permissions, cfg.WorkspacePath); err != nil {
		return nil, err
	}
	if err := appendDCPReviewLabNetworkFlag(ctx, &cmd, cfg.Config.DCPReviewLabNetwork, cfg.DataDir, cfg.SessionID, cfg.Kind, cfg.Permissions, cfg.WorkspacePath); err != nil {
		return nil, err
	}
	appendWorkspaceTrustFlag(&cmd, cfg.WorkspacePath)
	appendModelFlag(&cmd, cfg.Config)

	if cfg.SystemPrompt != "" {
		cmd = append(cmd, "-c", "developer_instructions="+codexTOMLConfigString(cfg.SystemPrompt))
	} else if cfg.SystemPromptFile != "" {
		cmd = append(cmd, "-c", "model_instructions_file="+cfg.SystemPromptFile)
	}

	if cfg.Prompt != "" {
		cmd = append(cmd, "--", cfg.Prompt)
	}

	return cmd, nil
}

// GetRestoreCommand rebuilds the argv that continues an existing Codex
// session: `codex resume <agentSessionId>`. ok is false when the hook-derived
// native session id has not landed yet, so callers can fall back to fresh
// launch behavior.
func (p *Plugin) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) (cmd []string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	agentSessionID := strings.TrimSpace(cfg.Session.Metadata[ports.MetadataKeyAgentSessionID])
	if agentSessionID == "" {
		return nil, false, nil
	}

	binary, err := p.codexBinary(ctx)
	if err != nil {
		return nil, false, err
	}

	cmd = make([]string, 0, 24)
	cmd = append(cmd, binary, "exec", "--ignore-user-config", "--ephemeral", "--strict-config")
	appendWorkerIsolationFlags(&cmd)
	appendNoUpdateCheckFlag(&cmd)
	appendHideRateLimitNudgeFlag(&cmd)
	if err := appendApprovalFlags(&cmd, cfg.Permissions); err != nil {
		return nil, false, err
	}
	if err := appendWorkspaceGitMetadataFlags(ctx, &cmd, cfg.Permissions, cfg.Session.WorkspacePath); err != nil {
		return nil, false, err
	}
	if err := appendDCPReviewLabNetworkFlag(ctx, &cmd, cfg.Config.DCPReviewLabNetwork, cfg.DataDir, cfg.Session.ID, cfg.Kind, cfg.Permissions, cfg.Session.WorkspacePath); err != nil {
		return nil, false, err
	}
	appendWorkspaceTrustFlag(&cmd, cfg.Session.WorkspacePath)
	appendModelFlag(&cmd, cfg.Config)
	if cfg.SystemPrompt != "" {
		cmd = append(cmd, "-c", "developer_instructions="+codexTOMLConfigString(cfg.SystemPrompt))
	} else if cfg.SystemPromptFile != "" {
		cmd = append(cmd, "-c", "model_instructions_file="+cfg.SystemPromptFile)
	}
	cmd = append(cmd, "resume", agentSessionID)
	if cfg.Prompt != "" {
		cmd = append(cmd, cfg.Prompt)
	}
	return cmd, true, nil
}

// SessionInfo surfaces Codex hook-derived metadata. Metadata is intentionally
// nil for Codex: callers get the normalized fields directly.
func (p *Plugin) SessionInfo(ctx context.Context, session ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	info, ok := agentbase.StandardSessionInfo(session)
	return info, ok, nil
}

// AuthStatus checks Codex's local login state without making a model call.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	binary, err := p.codexBinary(ctx)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := aoprocess.CommandContext(probeCtx, binary, "login", "status").CombinedOutput()
	if probeCtx.Err() != nil {
		return ports.AgentAuthStatusUnknown, probeCtx.Err()
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "not logged in") || strings.Contains(text, "logged out") {
		return ports.AgentAuthStatusUnauthorized, nil
	}
	if strings.Contains(text, "logged in") {
		return ports.AgentAuthStatusAuthorized, nil
	}
	if err != nil {
		return ports.AgentAuthStatusUnauthorized, nil
	}
	return ports.AgentAuthStatusUnknown, nil
}

// ResolveCodexBinary returns the path to the codex binary on this machine,
// searching platform-specific well-known install locations and PATH.
func ResolveCodexBinary(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		candidates := []string{}
		if appData := os.Getenv("APPDATA"); appData != "" {
			shim := filepath.Join(appData, "npm", "codex.cmd")
			candidates = append(candidates, windowsNativeCodexCandidatesForShim(shim)...)
			candidates = append(candidates,
				filepath.Join(appData, "npm", "codex.exe"),
				shim,
			)
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".cargo", "bin", "codex.exe"))
		}
		for _, candidate := range candidates {
			if fileExists(candidate) {
				return resolveNativeWindowsCodex(candidate), nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		for _, name := range []string{"codex.cmd", "codex", "codex.exe"} {
			path, err := exec.LookPath(name)
			if err == nil && path != "" {
				if isWindowsAppsCodexExecutable(path) {
					continue
				}
				return resolveNativeWindowsCodex(path), nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		return "", fmt.Errorf("codex: %w", ports.ErrAgentBinaryNotFound)
	}

	if path, err := exec.LookPath("codex"); err == nil && path != "" {
		return path, nil
	}

	candidates := []string{
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".npm-global", "bin", "codex"),
			filepath.Join(home, ".npm", "bin", "codex"),
			filepath.Join(home, ".local", "bin", "codex"),
			filepath.Join(home, ".cargo", "bin", "codex"),
		)
		nodeManagerCandidates, err := binaryutil.UnixNodeManagerBinCandidates(ctx, home, "codex")
		if err != nil {
			return "", err
		}
		candidates = append(candidates, nodeManagerCandidates...)
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("codex: %w", ports.ErrAgentBinaryNotFound)
}

func resolveNativeWindowsCodex(path string) string {
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(path), ".cmd") {
		return path
	}
	for _, candidate := range windowsNativeCodexCandidatesForShim(path) {
		if fileExists(candidate) {
			return candidate
		}
	}
	return path
}

func windowsNativeCodexCandidatesForShim(shim string) []string {
	dir := filepath.Dir(shim)
	return []string{
		filepath.Join(dir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"),
		filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.exe"),
	}
}

func isWindowsAppsCodexExecutable(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	clean := strings.ToLower(filepath.Clean(path))
	base := filepath.Base(clean)
	return (base == "codex.exe" || base == "codex") &&
		strings.Contains(clean, string(filepath.Separator)+"windowsapps"+string(filepath.Separator)+"openai.codex_")
}

func (p *Plugin) codexBinary(ctx context.Context) (string, error) {
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()

	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}

	binary, err := ResolveCodexBinary(ctx)
	if err != nil {
		return "", err
	}
	p.resolvedBinary = binary
	return binary, nil
}

// DoctorLaunchProbes returns offline argv tails that exercise the same worker
// isolation and config overrides as the real DCP launch command.
func DoctorLaunchProbes() [][]string {
	execProbe := []string{"exec", "--ignore-user-config", "--ephemeral", "--strict-config"}
	appendWorkerIsolationFlags(&execProbe)
	appendNoUpdateCheckFlag(&execProbe)
	appendHideRateLimitNudgeFlag(&execProbe)
	appendInteractiveWorkspaceFlags(&execProbe, false)
	appendWorkspaceTrustFlag(&execProbe, os.TempDir())
	execProbe = append(execProbe, "--help")

	featureProbe := make([]string, 0, 24)
	appendWorkerIsolationFlags(&featureProbe)
	appendNoUpdateCheckFlag(&featureProbe)
	appendHideRateLimitNudgeFlag(&featureProbe)
	appendInteractiveWorkspaceFlags(&featureProbe, false)
	appendWorkspaceTrustFlag(&featureProbe, os.TempDir())
	featureProbe = append(featureProbe, "features", "list")
	return [][]string{execProbe, featureProbe}
}

func appendNoUpdateCheckFlag(cmd *[]string) {
	*cmd = append(*cmd, "-c", "check_for_update_on_startup=false")
}

func appendHideRateLimitNudgeFlag(cmd *[]string) {
	// When the account nears its rate limit, the Codex TUI interposes an
	// interactive "switch to a cheaper model?" dialog before the first turn.
	// In a headless AO pane that dialog hangs the session invisibly and
	// swallows the auto-submitted spawn prompt, so suppress it.
	*cmd = append(*cmd, "-c", "notice.hide_rate_limit_model_nudge=true")
}

func appendWorkerIsolationFlags(cmd *[]string) {
	for _, feature := range []string{"hooks", "apps", "plugins", "multi_agent"} {
		*cmd = append(*cmd, "--disable", feature)
	}
}

func appendTerminalCompatibilityFlags(cmd *[]string) {
	if runtime.GOOS == "windows" {
		*cmd = append(*cmd, "--no-alt-screen")
	}
}

func appendModelFlag(cmd *[]string, cfg ports.AgentConfig) {
	if model := strings.TrimSpace(cfg.Model); model != "" {
		*cmd = append(*cmd, "--model", model)
	}
}

func appendApprovalFlags(cmd *[]string, permissions ports.PermissionMode) error {
	switch permissions {
	case "", ports.PermissionModeDefault:
		// Codex sessions are AO-managed and run headlessly inside a terminal
		// mux pane; default to no approval prompts unless project settings
		// explicitly choose a more restrictive mode.
		*cmd = append(*cmd, "--dangerously-bypass-approvals-and-sandbox")
	case ports.PermissionModeAcceptEdits:
		appendInteractiveWorkspaceFlags(cmd, false)
	case ports.PermissionModeAuto:
		appendInteractiveWorkspaceFlags(cmd, true)
	case ports.PermissionModeBypassPermissions:
		*cmd = append(*cmd, "--dangerously-bypass-approvals-and-sandbox")
	default:
		return fmt.Errorf("unsupported Codex permission mode %q", permissions)
	}
	return nil
}

// appendInteractiveWorkspaceFlags uses config and sandbox surfaces shared by
// both the root and exec parsers. The installed Codex exposes
// --ask-for-approval only at the root parser, so emitting it after exec fails
// before a model request.
func appendInteractiveWorkspaceFlags(cmd *[]string, autoReview bool) {
	*cmd = append(*cmd, "-c", `approval_policy="on-request"`)
	if autoReview {
		*cmd = append(*cmd, "-c", `approvals_reviewer="auto_review"`)
	}
	*cmd = append(*cmd, "--sandbox", "workspace-write")
}

// appendWorkspaceGitMetadataFlags grants the workspace-write sandbox only the
// two additional roots Git needs for a linked worktree: that worktree's
// private metadata directory and its repository's common .git directory.
//
// The paths are derived from and cross-checked against the concrete workspace
// instead of accepting caller-supplied add-dir values. Any missing, ordinary,
// ambiguous, or inconsistent Git layout fails closed before Codex starts.
func appendWorkspaceGitMetadataFlags(ctx context.Context, cmd *[]string, permissions ports.PermissionMode, workspacePath string) error {
	switch permissions {
	case ports.PermissionModeAcceptEdits, ports.PermissionModeAuto:
		gitDir, commonDir, err := workspaceGitMetadataRoots(ctx, workspacePath)
		if err != nil {
			return err
		}
		*cmd = append(*cmd, "--add-dir", gitDir, "--add-dir", commonDir)
	}
	return nil
}

const dcpReviewLabOrigin = "https://github.com/orenvlad-ai/dcp-review-lab.git"

// appendDCPReviewLabNetworkFlag opens Codex's workspace-write network only for
// the one PR-capable synthetic DCP contour. Every identity and path component
// is derived from the daemon data directory and the native session id; an
// exact-looking session with a foreign worktree, branch, Git common directory,
// or fetch/push remote fails before a model process starts. All ordinary DCP
// workers and every reviewer retain the stock network-disabled sandbox.
func appendDCPReviewLabNetworkFlag(ctx context.Context, cmd *[]string, profileEnabled bool, dataDir, sessionID string, kind domain.SessionKind, permissions ports.PermissionMode, workspacePath string) error {
	if !strings.HasPrefix(sessionID, "dcp-review-lab-") {
		return nil
	}
	if !isPositiveSessionSuffix(sessionID, "dcp-review-lab-") {
		return fmt.Errorf("codex DCP review-lab network: invalid session profile")
	}
	// Cards 1-5 are immutable pre-profile evidence and card 6 is the preserved
	// network-denied qualification attempt. Never retroactively grant any of
	// them network on restore/resume.
	if !dcpReviewLabNetworkSession(sessionID) {
		return nil
	}
	if !profileEnabled || kind != domain.KindWorker || permissions != ports.PermissionModeAcceptEdits {
		return fmt.Errorf("codex DCP review-lab network: invalid session profile")
	}
	data, err := canonicalExistingDir(strings.TrimSpace(dataDir))
	if err != nil {
		return fmt.Errorf("codex DCP review-lab network: data dir: %w", err)
	}
	workspace, err := canonicalExistingDir(strings.TrimSpace(workspacePath))
	if err != nil {
		return fmt.Errorf("codex DCP review-lab network: workspace: %w", err)
	}
	expectedWorkspace := filepath.Join(data, "worktrees", "dcp-review-lab", sessionID)
	if workspace != expectedWorkspace {
		return fmt.Errorf("codex DCP review-lab network: workspace %q does not match %q", workspace, expectedWorkspace)
	}
	gitDir, commonDir, err := workspaceGitMetadataRoots(ctx, workspace)
	if err != nil {
		return fmt.Errorf("codex DCP review-lab network: Git metadata: %w", err)
	}
	expectedCommon, err := canonicalExistingDir(filepath.Join(filepath.Dir(data), "targets", "dcp-review-lab", ".git"))
	if err != nil {
		return fmt.Errorf("codex DCP review-lab network: target Git dir: %w", err)
	}
	if commonDir != expectedCommon || gitDir != filepath.Join(expectedCommon, "worktrees", sessionID) {
		return fmt.Errorf("codex DCP review-lab network: linked-worktree identity mismatch")
	}
	checks := []struct {
		name string
		args []string
		want string
	}{
		{name: "fetch remote", args: []string{"remote", "get-url", "--all", "origin"}, want: dcpReviewLabOrigin},
		{name: "push remote", args: []string{"remote", "get-url", "--push", "--all", "origin"}, want: dcpReviewLabOrigin},
		{name: "branch", args: []string{"branch", "--show-current"}, want: "ao/" + sessionID + "/root"},
	}
	for _, check := range checks {
		got, err := gitSingleLine(ctx, workspace, check.args...)
		if err != nil {
			return fmt.Errorf("codex DCP review-lab network: %s: %w", check.name, err)
		}
		if got != check.want {
			return fmt.Errorf("codex DCP review-lab network: %s %q does not match %q", check.name, got, check.want)
		}
	}
	*cmd = append(*cmd, "-c", "sandbox_workspace_write.network_access=true")
	return nil
}

func isPositiveSessionSuffix(value, prefix string) bool {
	suffix := strings.TrimPrefix(value, prefix)
	if suffix == "" || suffix[0] == '0' {
		return false
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func dcpReviewLabNetworkSession(value string) bool {
	return value == "dcp-review-lab-7" || value == "dcp-review-lab-9" || value == "dcp-review-lab-10"
}

func workspaceGitMetadataRoots(ctx context.Context, workspacePath string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	workspace, err := canonicalExistingDir(strings.TrimSpace(workspacePath))
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: workspace: %w", err)
	}

	gitFile := filepath.Join(workspace, ".git")
	gitDirPath, err := readGitPathFile(gitFile, "gitdir: ")
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: .git pointer: %w", err)
	}
	gitDir, err := canonicalExistingDir(resolveGitPath(workspace, gitDirPath))
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: gitdir: %w", err)
	}

	commonPath, err := readGitPathFile(filepath.Join(gitDir, "commondir"), "")
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: commondir: %w", err)
	}
	commonDir, err := canonicalExistingDir(resolveGitPath(gitDir, commonPath))
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: common dir: %w", err)
	}
	if filepath.Base(commonDir) != ".git" {
		return "", "", fmt.Errorf("codex workspace metadata: common dir %q is not a .git directory", commonDir)
	}

	worktreesDir, err := canonicalExistingDir(filepath.Join(commonDir, "worktrees"))
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: common worktrees dir: %w", err)
	}
	rel, err := filepath.Rel(worktreesDir, gitDir)
	if err != nil || rel == "." || filepath.Dir(rel) != "." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("codex workspace metadata: gitdir %q is not one concrete child of %q", gitDir, worktreesDir)
	}

	backlinkPath, err := readGitPathFile(filepath.Join(gitDir, "gitdir"), "")
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: gitdir backlink: %w", err)
	}
	backlink, err := canonicalExistingFile(resolveGitPath(gitDir, backlinkPath))
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: gitdir backlink target: %w", err)
	}
	canonicalGitFile, err := canonicalExistingFile(gitFile)
	if err != nil {
		return "", "", fmt.Errorf("codex workspace metadata: .git file: %w", err)
	}
	if backlink != canonicalGitFile {
		return "", "", fmt.Errorf("codex workspace metadata: gitdir backlink %q does not target workspace .git %q", backlink, canonicalGitFile)
	}

	checks := []struct {
		name string
		arg  string
		want string
	}{
		{name: "top level", arg: "--show-toplevel", want: workspace},
		{name: "git dir", arg: "--absolute-git-dir", want: gitDir},
		{name: "common dir", arg: "--git-common-dir", want: commonDir},
	}
	for _, check := range checks {
		got, err := gitRevParsePath(ctx, workspace, check.arg)
		if err != nil {
			return "", "", fmt.Errorf("codex workspace metadata: verify %s: %w", check.name, err)
		}
		if got != check.want {
			return "", "", fmt.Errorf("codex workspace metadata: verified %s %q does not match %q", check.name, got, check.want)
		}
	}

	return gitDir, commonDir, nil
}

func readGitPathFile(path, prefix string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", path)
	}
	if info.Size() <= 0 || info.Size() > 4096 {
		return "", fmt.Errorf("%q has invalid size %d", path, info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if strings.ContainsAny(line, "\r\n\x00") || !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("%q is not a single valid Git path", path)
	}
	value := strings.TrimPrefix(line, prefix)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%q contains an empty Git path", path)
	}
	return value, nil
}

func resolveGitPath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func canonicalExistingDir(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q is not absolute", path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}
	return resolved, nil
}

func canonicalExistingFile(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("path %q is not absolute", path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", resolved)
	}
	return resolved, nil
}

func gitRevParsePath(ctx context.Context, workspace, arg string) (string, error) {
	command := aoprocess.CommandContext(ctx, "git", "-C", workspace, "rev-parse", "--path-format=absolute", arg)
	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w: %s", arg, err, strings.TrimSpace(string(out)))
	}
	line := strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	if strings.ContainsAny(line, "\r\n\x00") {
		return "", fmt.Errorf("git rev-parse %s returned an ambiguous path", arg)
	}
	return canonicalExistingDir(line)
}

func gitSingleLine(ctx context.Context, workspace string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", workspace}, args...)
	command := aoprocess.CommandContext(ctx, "git", commandArgs...)
	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	line := strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	if line == "" || strings.ContainsAny(line, "\r\n\x00") {
		return "", fmt.Errorf("git %s returned an empty or ambiguous value", strings.Join(args, " "))
	}
	return line, nil
}

// fileExists is a package var so tests can stub it to scope candidate probing.
var fileExists = func(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
