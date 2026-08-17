package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// canonicalTempDir returns a t.TempDir() with symlinks resolved so the
// workspace trust flag collapses to a single predictable entry (macOS TempDir
// lives under a /var -> /private/var symlink).
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	raw := t.TempDir()
	dir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		if os.IsPermission(err) {
			return raw
		}
		t.Fatal(err)
	}
	return dir
}

func linkedWorktree(t *testing.T) (workspace, gitDir, commonDir string) {
	t.Helper()
	return linkedWorktreeAt(t, canonicalTempDir(t))
}

func linkedWorktreeAt(t *testing.T, root string) (workspace, gitDir, commonDir string) {
	t.Helper()
	repo := filepath.Join(root, "repo")
	workspace = filepath.Join(root, "worker")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "git", "-C", repo, "init", "--quiet")
	runTestCommand(t, "git", "-C", repo, "config", "user.name", "DCP Test")
	runTestCommand(t, "git", "-C", repo, "config", "user.email", "dcp-test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("linked worktree test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "git", "-C", repo, "add", "README.md")
	runTestCommand(t, "git", "-C", repo, "commit", "--quiet", "-m", "seed")
	runTestCommand(t, "git", "-C", repo, "worktree", "add", "--quiet", "-b", "worker", workspace)

	var err error
	gitDir, commonDir, err = workspaceGitMetadataRoots(context.Background(), workspace)
	if err != nil {
		t.Fatalf("workspaceGitMetadataRoots: %v", err)
	}
	return workspace, gitDir, commonDir
}

func dcpReviewLabWorktree(t *testing.T, sessionID string) (dataDir, workspace string) {
	return dcpPolicyWorktree(t, "dcp-review-lab", dcpReviewLabOrigin, sessionID)
}

func dcpPolicyWorktree(t *testing.T, target, origin, sessionID string) (dataDir, workspace string) {
	t.Helper()
	root := canonicalTempDir(t)
	dataDir = filepath.Join(root, "data")
	repo := filepath.Join(root, "targets", target)
	workspace = filepath.Join(dataDir, "worktrees", target, sessionID)
	if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(workspace), 0o755); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "git", "init", "--quiet", "--initial-branch=main", repo)
	runTestCommand(t, "git", "-C", repo, "config", "user.name", "DCP Test")
	runTestCommand(t, "git", "-C", repo, "config", "user.email", "dcp-test@example.invalid")
	runTestCommand(t, "git", "-C", repo, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("DCP review lab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "git", "-C", repo, "add", "README.md")
	runTestCommand(t, "git", "-C", repo, "commit", "--quiet", "-m", "seed")
	runTestCommand(t, "git", "-C", repo, "worktree", "add", "--quiet", "-b", "ao/"+sessionID+"/root", workspace)
	return dataDir, workspace
}

func TestGetLaunchCommandEnablesNetworkOnlyForExactRepoOnlyTarget(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	const sessionID = "wb-browser-extension-1"
	dataDir, workspace := dcpPolicyWorktree(t, "wb-browser-extension", dcpRepoOnlyOrigin, sessionID)
	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace, DCPReviewLabPolicyAuthorized: true,
	})
	if err != nil || !containsSubsequence(cmd, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
		t.Fatalf("exact repo-only command=%#v err=%v", cmd, err)
	}
	withoutAuthority, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace,
	})
	if err != nil || contains(withoutAuthority, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("unbound repo-only command=%#v err=%v", withoutAuthority, err)
	}
	repo := filepath.Join(filepath.Dir(dataDir), "targets", "wb-browser-extension")
	runTestCommand(t, "git", "-C", repo, "remote", "set-url", "--push", "origin", dcpReviewLabOrigin)
	if foreign, foreignErr := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace, DCPReviewLabPolicyAuthorized: true,
	}); foreignErr == nil {
		t.Fatalf("foreign repo-only push remote produced command %#v", foreign)
	}
}

func TestGetLaunchCommandEnablesNetworkOnlyForExactWBCRepoOnlyTarget(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	const sessionID = "wb-core-1"
	dataDir, workspace := dcpPolicyWorktree(t, "wb-core", dcpWBCRepoOnlyOrigin, sessionID)
	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace, DCPReviewLabPolicyAuthorized: true,
	})
	if err != nil || !containsSubsequence(cmd, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
		t.Fatalf("exact WBC repo-only command=%#v err=%v", cmd, err)
	}
	withoutAuthority, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace,
	})
	if err != nil || contains(withoutAuthority, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("unbound WBC repo-only command=%#v err=%v", withoutAuthority, err)
	}
	repo := filepath.Join(filepath.Dir(dataDir), "targets", "wb-core")
	runTestCommand(t, "git", "-C", repo, "remote", "set-url", "--push", "origin", dcpRepoOnlyOrigin)
	if foreign, foreignErr := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: dataDir,
		SessionID: sessionID, Kind: domain.KindWorker, Permissions: ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace, DCPReviewLabPolicyAuthorized: true,
	}); foreignErr == nil {
		t.Fatalf("foreign WBC repo-only push remote produced command %#v", foreign)
	}
}

func TestGetLaunchCommandRejectsLegacyRepoOnlyTargetEvenWithPolicyFlag(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	if cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: t.TempDir(),
		SessionID: "wb-price-extension-1", Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: t.TempDir(),
		DCPReviewLabPolicyAuthorized: true,
	}); err == nil || contains(cmd, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("legacy target crossed worker network gate: command=%#v err=%v", cmd, err)
	}
}

func runTestCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %#v: %v\n%s", name, args, err, out)
	}
}

func TestExitDetectionUsesAOProcessSupervisor(t *testing.T) {
	plugin := &Plugin{}
	if got := plugin.ExitDetectionMode(); got != ports.AgentExitDetectionSupervisorIdleOnSuccess {
		t.Fatalf("exit detection mode = %q, want %q", got, ports.AgentExitDetectionSupervisorIdleOnSuccess)
	}
}

func TestGetLaunchCommandBuildsCrossPlatformArgv(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace := canonicalTempDir(t)

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:      ports.PermissionModeBypassPermissions,
		Prompt:           "-fix this",
		SystemPromptFile: filepath.Join("tmp", "prompt with spaces.md"),
		SystemPrompt:     "inline wins",
		WorkspacePath:    workspace,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"codex",
		"exec", "--ignore-user-config", "--ephemeral", "--strict-config",
		"--disable", "hooks",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "multi_agent",
		"-c", "check_for_update_on_startup=false",
		"-c", "notice.hide_rate_limit_model_nudge=true",
		"--dangerously-bypass-approvals-and-sandbox",
	}
	want = append(want,
		"-c", `projects={`+codexTOMLConfigString(workspace)+`={trust_level="trusted"}}`,
		"-c", "developer_instructions="+codexTOMLConfigString("inline wins"),
		"--", "-fix this",
	)
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetLaunchCommandWithoutWorkspaceOmitsTrustFlag(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range cmd {
		if strings.HasPrefix(arg, "projects=") {
			t.Fatalf("command %#v contains a projects trust flag without a workspace", cmd)
		}
	}
	if !containsSubsequence(cmd, []string{"exec", "--ignore-user-config", "--ephemeral", "--strict-config", "--disable", "hooks"}) {
		t.Fatalf("command %#v missing worker isolation flags", cmd)
	}
	for _, arg := range cmd {
		if strings.Contains(arg, "hooks.") || arg == "--dangerously-bypass-hook-trust" {
			t.Fatalf("command %#v contains a hook launch path", cmd)
		}
	}
}

func TestGetLaunchCommandAppendsConfiguredModel(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Model: "  gpt-5.4-mini  "},
		Prompt: "fix this",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--model", "gpt-5.4-mini"}) {
		t.Fatalf("command %#v missing trimmed --model flag", cmd)
	}
	if containsSubsequence(cmd, []string{"--model", "  gpt-5.4-mini  "}) {
		t.Fatalf("command %#v used untrimmed model", cmd)
	}
}

func TestGetLaunchCommandOmitsBlankConfiguredModel(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Model: " \t "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(cmd, "--model") {
		t.Fatalf("command %#v contains --model for blank model", cmd)
	}
}

func TestResolveCodexBinaryFindsNVMInstallWhenPathIsSparse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NVM install discovery is Unix-specific")
	}
	home := t.TempDir()
	binDir := filepath.Join(home, ".nvm", "versions", "node", "v20.19.4", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(binDir, "codex")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	origFileExists := fileExists
	fileExists = func(path string) bool {
		return strings.HasPrefix(path, home+string(os.PathSeparator)) && origFileExists(path)
	}
	t.Cleanup(func() {
		fileExists = origFileExists
	})

	got, err := ResolveCodexBinary(context.Background())
	if err != nil {
		t.Fatalf("ResolveCodexBinary: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveCodexBinary = %q, want %q", got, want)
	}
}

func TestResolveCodexBinaryPrefersNPMOverWindowsAppsExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows resolver only")
	}
	root := t.TempDir()
	appData := filepath.Join(root, "Roaming")
	npmDir := filepath.Join(appData, "npm")
	want := filepath.Join(npmDir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("native codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(npmDir, "codex.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	windowsApps := filepath.Join(root, "WindowsApps", "OpenAI.Codex_1.0.0.0_x64__test", "app", "resources")
	if err := os.MkdirAll(windowsApps, 0o755); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(windowsApps, "codex.exe")
	if err := os.WriteFile(blocked, []byte("blocked codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	t.Setenv("PATH", windowsApps)

	got, err := ResolveCodexBinary(context.Background())
	if err != nil {
		t.Fatalf("ResolveCodexBinary: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveCodexBinary = %q, want %q", got, want)
	}
}

func TestGetLaunchCommandMapsApprovalModes(t *testing.T) {
	tests := []struct {
		name        string
		permission  ports.PermissionMode
		want        []string
		notExpected string
		wantErr     bool
	}{
		{
			name:       "default",
			permission: ports.PermissionModeDefault,
			want:       []string{"--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:        "accept-edits",
			permission:  ports.PermissionModeAcceptEdits,
			want:        []string{"-c", `approval_policy="on-request"`, "--sandbox", "workspace-write"},
			notExpected: "--dangerously-bypass-approvals-and-sandbox",
		},
		{
			name:        "auto",
			permission:  ports.PermissionModeAuto,
			want:        []string{"-c", `approval_policy="on-request"`, "-c", `approvals_reviewer="auto_review"`, "--sandbox", "workspace-write"},
			notExpected: "--dangerously-bypass-approvals-and-sandbox",
		},
		{
			name:       "bypass-permissions",
			permission: ports.PermissionModeBypassPermissions,
			want:       []string{"--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:       "empty",
			permission: "",
			want:       []string{"--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:       "unknown-fails-closed",
			permission: "future-mode",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{resolvedBinary: "codex"}
			workspace := ""
			var gitDir, commonDir string
			if tt.permission == ports.PermissionModeAcceptEdits || tt.permission == ports.PermissionModeAuto {
				workspace, gitDir, commonDir = linkedWorktree(t)
			}
			cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
				Permissions:   tt.permission,
				WorkspacePath: workspace,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetLaunchCommand(%q) succeeded with %#v, want error", tt.permission, cmd)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if contains(cmd, "--ask-for-approval") {
				t.Fatalf("command %#v contains unsupported exec-level --ask-for-approval", cmd)
			}
			if len(tt.want) > 0 && !containsSubsequence(cmd, tt.want) {
				t.Fatalf("command %#v does not contain %#v", cmd, tt.want)
			}
			if tt.notExpected != "" && contains(cmd, tt.notExpected) {
				t.Fatalf("command %#v contains %q", cmd, tt.notExpected)
			}
			if gitDir != "" && !containsSubsequence(cmd, []string{"--add-dir", gitDir, "--add-dir", commonDir}) {
				t.Fatalf("command %#v lacks exact linked-worktree metadata roots", cmd)
			}
		})
	}
}

func TestGetLaunchCommandEnablesNetworkOnlyForExactDCPReviewLabWorker(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	const sessionID = "dcp-review-lab-7"
	dataDir, workspace := dcpReviewLabWorktree(t, sessionID)

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config:        ports.AgentConfig{DCPReviewLabNetwork: true},
		DataDir:       dataDir,
		SessionID:     sessionID,
		Kind:          domain.KindWorker,
		Permissions:   ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
		t.Fatalf("exact DCP review-lab worker command lacks scoped network flag: %#v", cmd)
	}
	for _, stageID := range []string{"dcp-review-lab-9", "dcp-review-lab-10", "dcp-review-lab-11", "dcp-review-lab-12"} {
		stageDataDir, stageWorkspace := dcpReviewLabWorktree(t, stageID)
		stage, stageErr := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
			Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: stageDataDir,
			SessionID: stageID, Kind: domain.KindWorker,
			Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: stageWorkspace,
		})
		if stageErr != nil || !containsSubsequence(stage, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
			t.Fatalf("I13 session %s network command=%#v err=%v", stageID, stage, stageErr)
		}
	}
	if cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		DataDir: dataDir, SessionID: sessionID, Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: workspace,
	}); err == nil {
		t.Fatalf("missing profile marker produced command %#v, want rejection", cmd)
	}

	oldDataDir, oldWorkspace := dcpReviewLabWorktree(t, "dcp-review-lab-6")
	old, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config:  ports.AgentConfig{DCPReviewLabNetwork: true},
		DataDir: oldDataDir, SessionID: "dcp-review-lab-6", Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: oldWorkspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(old, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("preserved card 6 unexpectedly received network: %#v", old)
	}

	retiredDataDir, retiredWorkspace := dcpReviewLabWorktree(t, "dcp-review-lab-8")
	retired, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: retiredDataDir,
		SessionID: "dcp-review-lab-8", Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: retiredWorkspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(retired, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("pre-stage card 8 unexpectedly received network: %#v", retired)
	}

	futureDataDir, futureWorkspace := dcpReviewLabWorktree(t, "dcp-review-lab-13")
	future, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: futureDataDir,
		SessionID: "dcp-review-lab-13", Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: futureWorkspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(future, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("future card 13 unexpectedly received network: %#v", future)
	}
	policyFuture, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{DCPReviewLabNetwork: true}, DataDir: futureDataDir,
		SessionID: "dcp-review-lab-13", Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: futureWorkspace,
		DCPReviewLabPolicyAuthorized: true,
	})
	if err != nil || !containsSubsequence(policyFuture, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
		t.Fatalf("policy-authorized future card lacks scoped network: %#v err=%v", policyFuture, err)
	}

	ordinaryWorkspace, _, _ := linkedWorktree(t)
	ordinary, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SessionID:     "ordinary-1",
		Kind:          domain.KindWorker,
		Permissions:   ports.PermissionModeAcceptEdits,
		WorkspacePath: ordinaryWorkspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(ordinary, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("ordinary worker unexpectedly received network: %#v", ordinary)
	}
}

func TestGetLaunchCommandDCPReviewLabNetworkFailsClosed(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	const sessionID = "dcp-review-lab-9"
	dataDir, workspace := dcpReviewLabWorktree(t, sessionID)
	tests := []struct {
		name       string
		dataDir    string
		sessionID  string
		kind       domain.SessionKind
		permission ports.PermissionMode
		workspace  string
	}{
		{name: "wrong data", dataDir: filepath.Join(dataDir, "foreign"), sessionID: sessionID, kind: domain.KindWorker, permission: ports.PermissionModeAcceptEdits, workspace: workspace},
		{name: "wrong session suffix", dataDir: dataDir, sessionID: "dcp-review-lab-08", kind: domain.KindWorker, permission: ports.PermissionModeAcceptEdits, workspace: workspace},
		{name: "orchestrator kind", dataDir: dataDir, sessionID: sessionID, kind: domain.KindOrchestrator, permission: ports.PermissionModeAcceptEdits, workspace: workspace},
		{name: "auto permission", dataDir: dataDir, sessionID: sessionID, kind: domain.KindWorker, permission: ports.PermissionModeAuto, workspace: workspace},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
				Config:  ports.AgentConfig{DCPReviewLabNetwork: true},
				DataDir: test.dataDir, SessionID: test.sessionID, Kind: test.kind,
				Permissions: test.permission, WorkspacePath: test.workspace,
			})
			if err == nil {
				t.Fatalf("command = %#v, want exact profile rejection", cmd)
			}
		})
	}
	repo := filepath.Join(filepath.Dir(dataDir), "targets", "dcp-review-lab")
	runTestCommand(t, "git", "-C", repo, "remote", "set-url", "--push", "origin", "https://github.com/orenvlad-ai/foreign.git")
	if cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config:  ports.AgentConfig{DCPReviewLabNetwork: true},
		DataDir: dataDir, SessionID: sessionID, Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: workspace,
	}); err == nil {
		t.Fatalf("foreign push remote produced command %#v, want rejection", cmd)
	}
}

func TestGetLaunchCommandGitMetadataFailsClosed(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	for _, tc := range []struct {
		name      string
		workspace func(*testing.T) string
	}{
		{name: "blank", workspace: func(*testing.T) string { return "" }},
		{name: "non-git", workspace: func(t *testing.T) string { return canonicalTempDir(t) }},
		{name: "ordinary checkout", workspace: func(t *testing.T) string {
			repo := canonicalTempDir(t)
			runTestCommand(t, "git", "-C", repo, "init", "--quiet")
			return repo
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
				Permissions:   ports.PermissionModeAcceptEdits,
				WorkspacePath: tc.workspace(t),
			})
			if err == nil || cmd != nil {
				t.Fatalf("GetLaunchCommand = (%#v, %v), want fail-closed metadata error", cmd, err)
			}
		})
	}
}

func TestGetLaunchCommandRejectsMismatchedWorktreeBacklink(t *testing.T) {
	workspace, gitDir, _ := linkedWorktree(t)
	foreign := filepath.Join(canonicalTempDir(t), ".git")
	if err := os.WriteFile(foreign, []byte("foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "gitdir"), []byte(foreign+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := (&Plugin{resolvedBinary: "codex"}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:   ports.PermissionModeAuto,
		WorkspacePath: workspace,
	})
	if err == nil || cmd != nil || !strings.Contains(err.Error(), "backlink") {
		t.Fatalf("GetLaunchCommand = (%#v, %v), want backlink rejection", cmd, err)
	}
}

func TestBypassDoesNotResolveOrGrantGitMetadata(t *testing.T) {
	cmd, err := (&Plugin{resolvedBinary: "codex"}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:   ports.PermissionModeBypassPermissions,
		WorkspacePath: filepath.Join(t.TempDir(), "missing"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if contains(cmd, "--add-dir") {
		t.Fatalf("bypass command unexpectedly contains Git metadata roots: %#v", cmd)
	}
}

func TestAppendWorkspaceTrustFlagCoversLiteralAndResolvedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs extra privileges on Windows")
	}
	base := canonicalTempDir(t)
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	var cmd []string
	appendWorkspaceTrustFlag(&cmd, link)
	want := []string{
		"-c",
		`projects={'` + link + `'={trust_level="trusted"},'` + target + `'={trust_level="trusted"}}`,
	}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("trust flag\nwant: %#v\n got: %#v", want, cmd)
	}

	cmd = nil
	appendWorkspaceTrustFlag(&cmd, target)
	want = []string{"-c", `projects={'` + target + `'={trust_level="trusted"}}`}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("canonical-path trust flag\nwant: %#v\n got: %#v", want, cmd)
	}

	cmd = nil
	appendWorkspaceTrustFlag(&cmd, "   ")
	if cmd != nil {
		t.Fatalf("blank workspace produced %#v, want no flag", cmd)
	}
}

func TestCodexTOMLBasicStringEscapes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "\"plain\""},
		{"C:\\Users\\dev", "\"C:\\\\Users\\\\dev\""},
		{"with \"quotes\"", "\"with \\\"quotes\\\"\""},
		{"tab\there", "\"tab\\u0009here\""},
	}
	for _, tt := range tests {
		if got := codexTOMLBasicString(tt.in); got != tt.want {
			t.Fatalf("codexTOMLBasicString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestGetPromptDeliveryStrategyIsInCommand(t *testing.T) {
	plugin := &Plugin{}

	got, err := plugin.GetPromptDeliveryStrategy(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got != ports.PromptDeliveryInCommand {
		t.Fatalf("unexpected strategy: %q", got)
	}
}

func TestGetConfigSpecReportsModelField(t *testing.T) {
	plugin := &Plugin{}

	spec, err := plugin.GetConfigSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.ConfigField{
		{
			Key:         "model",
			Type:        ports.ConfigFieldString,
			Description: "Model override passed to `codex --model`.",
		},
	}
	if !reflect.DeepEqual(spec.Fields, want) {
		t.Fatalf("config fields\nwant: %#v\n got: %#v", want, spec.Fields)
	}
}

// legacyHooksJSON builds a hooks.json in the shape older AO versions wrote:
// AO-managed entries plus one user-defined Stop hook.
func legacyHooksJSON() string {
	return `{
  "hooks": {
    "Stop": [
      {"matcher": null, "hooks": [
        {"type": "command", "command": "custom stop hook", "timeout": 3},
        {"type": "command", "command": "ao hooks codex stop", "timeout": 30}
      ]}
    ],
    "UserPromptSubmit": [
      {"matcher": null, "hooks": [
        {"type": "command", "command": "ao hooks codex user-prompt-submit", "timeout": 30}
      ]}
    ]
  },
  "unmanagedKey": {"keep": true}
}`
}

func TestGetAgentHooksWritesNothingIntoFreshWorkspace(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace := t.TempDir()

	cfg := ports.WorkspaceHookConfig{
		DataDir:       t.TempDir(),
		SessionID:     "sess-1",
		WorkspacePath: workspace,
	}
	if err := plugin.GetAgentHooks(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(workspace, codexHooksDirName)); !os.IsNotExist(err) {
		t.Fatalf(".codex dir state = %v, want not-exist: hooks ride the launch command", err)
	}
}

func TestGetAgentHooksRequiresWorkspacePath(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	err := plugin.GetAgentHooks(context.Background(), ports.WorkspaceHookConfig{WorkspacePath: "  "})
	if err == nil {
		t.Fatal("expected error for blank WorkspacePath")
	}
}

func TestGetAgentHooksStripsLegacyAOEntries(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, codexHooksDirName, codexHooksFileName)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(legacyHooksJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ports.WorkspaceHookConfig{
		DataDir:       t.TempDir(),
		SessionID:     "sess-1",
		WorkspacePath: workspace,
	}
	if err := plugin.GetAgentHooks(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var config codexHookFile
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, spec := range codexManagedHooks {
		if got := countCodexHookCommand(config.Hooks[spec.Event], spec.Command); got != 0 {
			t.Fatalf("%s command %q count = %d after cleanup, want 0", spec.Event, spec.Command, got)
		}
	}
	if countCodexHookCommand(config.Hooks["Stop"], "custom stop hook") != 1 {
		t.Fatalf("user Stop hook not preserved: %#v", config.Hooks["Stop"])
	}
	if _, ok := config.Hooks["UserPromptSubmit"]; ok {
		t.Fatalf("UserPromptSubmit left behind after its only entry was AO's: %#v", config.Hooks)
	}
	if !strings.Contains(string(data), "unmanagedKey") {
		t.Fatalf("top-level keys AO doesn't manage were dropped: %s", data)
	}
}

func TestGetAgentHooksLeavesFilesWithoutAOEntriesUntouched(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, codexHooksDirName, codexHooksFileName)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"hooks":{"Stop":[{"matcher":null,"hooks":[{"type":"command","command":"custom stop hook","timeout":3}]}]}}`
	if err := os.WriteFile(hooksPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ports.WorkspaceHookConfig{
		DataDir:       t.TempDir(),
		SessionID:     "sess-1",
		WorkspacePath: workspace,
	}
	if err := plugin.GetAgentHooks(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != seed {
		t.Fatalf("user-only hooks.json was rewritten\n--- before ---\n%s\n--- after ---\n%s", seed, data)
	}
}

func TestUninstallHooksRemovesLegacyCodexHooks(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, codexHooksDirName, codexHooksFileName)

	ctx := context.Background()

	// Missing file is a no-op.
	if err := plugin.UninstallHooks(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(legacyHooksJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	if installed, err := plugin.AreHooksInstalled(ctx, workspace); err != nil || !installed {
		t.Fatalf("AreHooksInstalled with legacy entries = (%v, %v), want (true, nil)", installed, err)
	}

	if err := plugin.UninstallHooks(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if installed, err := plugin.AreHooksInstalled(ctx, workspace); err != nil || installed {
		t.Fatalf("AreHooksInstalled after uninstall = (%v, %v), want (false, nil)", installed, err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var config codexHookFile
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, spec := range codexManagedHooks {
		if got := countCodexHookCommand(config.Hooks[spec.Event], spec.Command); got != 0 {
			t.Fatalf("%s command %q count = %d after uninstall, want 0", spec.Event, spec.Command, got)
		}
	}
	if countCodexHookCommand(config.Hooks["Stop"], "custom stop hook") != 1 {
		t.Fatalf("user Stop hook not preserved: %#v", config.Hooks["Stop"])
	}
}

func TestGetRestoreCommandReadsAgentSessionID(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	workspace, gitDir, commonDir := linkedWorktree(t)

	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions:      ports.PermissionModeAuto,
		SystemPrompt:     "restore inline wins",
		SystemPromptFile: filepath.Join("tmp", "restore-system.md"),
		Session: ports.SessionRef{
			Metadata:      map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"},
			WorkspacePath: workspace,
		},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := []string{
		"codex",
		"exec", "--ignore-user-config", "--ephemeral", "--strict-config",
		"--disable", "hooks",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "multi_agent",
		"-c", "check_for_update_on_startup=false",
		"-c", "notice.hide_rate_limit_model_nudge=true",
		"-c", `approval_policy="on-request"`,
		"-c", `approvals_reviewer="auto_review"`,
		"--sandbox", "workspace-write",
		"--add-dir", gitDir,
		"--add-dir", commonDir,
	}
	want = append(want,
		"-c", `projects={`+codexTOMLConfigString(workspace)+`={trust_level="trusted"}}`,
		"-c", "developer_instructions="+codexTOMLConfigString("restore inline wins"),
		"resume", "thread-123",
	)
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandAppendsConfiguredModel(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Config: ports.AgentConfig{Model: "  gpt-5.4-mini  "},
		Session: ports.SessionRef{
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"},
		},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if !containsSubsequence(cmd, []string{"--model", "gpt-5.4-mini"}) {
		t.Fatalf("restore command %#v missing trimmed --model flag", cmd)
	}
}

func TestGetRestoreCommandAppendsNativeContinuationPrompt(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	prompt := "bounded refresh"
	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Prompt:  prompt,
		Session: ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"}},
	})
	if err != nil || !ok {
		t.Fatalf("GetRestoreCommand = (%#v, %v, %v)", cmd, ok, err)
	}
	if got := cmd[len(cmd)-3:]; !reflect.DeepEqual(got, []string{"resume", "thread-123", prompt}) {
		t.Fatalf("restore suffix = %#v", got)
	}
}

func TestGetRestoreCommandRejectsUnknownPermissionMode(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}
	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions: "future-mode",
		Session: ports.SessionRef{
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"},
		},
	})
	if err == nil || ok || cmd != nil {
		t.Fatalf("GetRestoreCommand = (%#v, %v, %v), want fail-closed error", cmd, ok, err)
	}
}

func TestGetRestoreCommandFalseWithoutAgentSessionID(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	cases := []struct {
		name string
		ref  ports.SessionRef
	}{
		{"empty session ref", ports.SessionRef{}},
		{"empty metadata", ports.SessionRef{Metadata: map[string]string{}}},
		{"blank agent session metadata", ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "   "}}},
		{"workspace path only", ports.SessionRef{WorkspacePath: "/some/path"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
				Permissions: ports.PermissionModeAuto,
				Session:     tc.ref,
			})
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if ok {
				t.Fatalf("ok = true, want false")
			}
			if cmd != nil {
				t.Fatalf("cmd = %#v, want nil", cmd)
			}
		})
	}
}

func TestSessionInfoReadsHookMetadata(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	info, ok, err := plugin.SessionInfo(context.Background(), ports.SessionRef{
		WorkspacePath: "/some/path",
		Metadata: map[string]string{
			ports.MetadataKeyAgentSessionID: "thread-123",
			ports.MetadataKeyTitle:          "Fix login redirect",
			ports.MetadataKeySummary:        "Updated the auth callback and tests.",
			"ignored":                       "not returned",
		},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if info.AgentSessionID != "thread-123" {
		t.Fatalf("AgentSessionID = %q, want native id", info.AgentSessionID)
	}
	if info.Title != "Fix login redirect" {
		t.Fatalf("Title = %q, want hook title", info.Title)
	}
	if info.Summary != "Updated the auth callback and tests." {
		t.Fatalf("Summary = %q, want hook summary", info.Summary)
	}
	if info.Metadata != nil {
		t.Fatalf("Metadata = %#v, want nil for Codex", info.Metadata)
	}
}

func TestSessionInfoFalseWhenNoHookMetadata(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	info, ok, err := plugin.SessionInfo(context.Background(), ports.SessionRef{
		WorkspacePath: "/some/path",
		Metadata:      map[string]string{},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ok {
		t.Fatalf("ok = true, want false")
	}
	if !reflect.DeepEqual(info, ports.SessionInfo{}) {
		t.Fatalf("info = %#v, want zero value", info)
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func containsSubsequence(values []string, needle []string) bool {
	if len(needle) == 0 {
		return true
	}

	for start := range values {
		if start+len(needle) > len(values) {
			return false
		}
		ok := true
		for offset, want := range needle {
			if values[start+offset] != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}

	return false
}

func countCodexHookCommand(entries []codexMatcherGroup, command string) int {
	count := 0
	for _, entry := range entries {
		for _, hook := range entry.Hooks {
			if hook.Command == command {
				count++
			}
		}
	}
	return count
}

func TestDoctorLaunchProbesMirrorLaunchFlags(t *testing.T) {
	probes := DoctorLaunchProbes()
	if len(probes) != 2 {
		t.Fatalf("probes = %d, want 2", len(probes))
	}
	if len(probes[0]) < 4 || !reflect.DeepEqual(probes[0][:3], []string{"exec", "--ignore-user-config", "--ephemeral"}) {
		t.Fatalf("exec probe = %#v", probes[0])
	}
	override := probes[1]
	if len(override) < 2 || override[len(override)-2] != "features" || override[len(override)-1] != "list" {
		t.Fatalf("override probe must ride `features list`, got %#v", override)
	}
	joined := strings.Join(probes[0], " ") + " " + strings.Join(override, " ")
	for _, want := range []string{
		"--disable hooks", "--disable apps", "--disable plugins", "--disable multi_agent",
		"notice.hide_rate_limit_model_nudge=true",
		`approval_policy="on-request"`,
		"--sandbox workspace-write",
		`projects={`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("override probe missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--ask-for-approval") {
		t.Fatalf("model-free probes contain unsupported exec-level approval flag: %s", joined)
	}
}

func TestInstalledCodexParsesGeneratedWorkerCommandWithoutModelRequest(t *testing.T) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed")
	}
	plugin := &Plugin{resolvedBinary: binary}
	workspace, _, _ := linkedWorktree(t)
	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:   ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd = append(cmd, "--help")
	t.Setenv("CODEX_HOME", t.TempDir())
	probe := exec.Command(cmd[0], cmd[1:]...)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("generated worker argv failed parser-only smoke: %v\n%s", err, out)
	}
	for _, args := range DoctorLaunchProbes() {
		probe := exec.Command(binary, args...)
		if out, err := probe.CombinedOutput(); err != nil {
			t.Fatalf("offline worker capability probe %#v failed: %v\n%s", args, err, out)
		}
	}
}

func TestInstalledCodexParsesExactDCPReviewLabNetworkFlagWithoutModelRequest(t *testing.T) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed")
	}
	const sessionID = "dcp-review-lab-9"
	dataDir, workspace := dcpReviewLabWorktree(t, sessionID)
	cmd, err := (&Plugin{resolvedBinary: binary}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config:  ports.AgentConfig{DCPReviewLabNetwork: true},
		DataDir: dataDir, SessionID: sessionID, Kind: domain.KindWorker,
		Permissions: ports.PermissionModeAcceptEdits, WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"-c", "sandbox_workspace_write.network_access=true"}) {
		t.Fatalf("generated command lacks exact network config: %#v", cmd)
	}
	cmd = append(cmd, "--help")
	t.Setenv("CODEX_HOME", t.TempDir())
	probe := exec.Command(cmd[0], cmd[1:]...)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("exact DCP review-lab argv failed parser-only smoke: %v\n%s", err, out)
	}
}

func TestGeneratedGitMetadataRootsPermitGitAddInInstalledCodexSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("installed Codex sandbox regression is macOS-specific")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex is not installed")
	}
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(packageDir, ".codex-git-metadata-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	workspace, gitDir, commonDir := linkedWorktreeAt(t, root)
	marker := filepath.Join(workspace, "MARKER.md")
	if err := os.WriteFile(marker, []byte("model-free sandbox proof\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := (&Plugin{resolvedBinary: binary}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:   ports.PermissionModeAcceptEdits,
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--add-dir", gitDir, "--add-dir", commonDir}) {
		t.Fatalf("generated command lacks verified metadata roots: %#v", cmd)
	}

	t.Setenv("CODEX_HOME", t.TempDir())
	baseline := exec.Command(binary,
		"sandbox",
		"-c", `permissions={}`,
		"-c", `default_permissions=":workspace"`,
		"-P", ":workspace",
		"-C", workspace,
		"--", "git", "add", "MARKER.md",
	)
	if out, err := baseline.CombinedOutput(); err == nil {
		t.Fatalf("baseline workspace sandbox unexpectedly permitted linked-worktree git add\n%s", out)
	}

	profile := `permissions={dcp_test={extends=":workspace",workspace_roots={` +
		codexTOMLConfigString(gitDir) + `=true,` + codexTOMLConfigString(commonDir) + `=true}}}`
	probe := exec.Command(binary,
		"sandbox",
		"-c", profile,
		"-c", `default_permissions="dcp_test"`,
		"-P", "dcp_test",
		"-C", workspace,
		"--", "git", "add", "MARKER.md",
	)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("verified generated metadata roots did not permit model-free git add: %v\n%s", err, out)
	}
	staged := exec.Command("git", "-C", workspace, "diff", "--cached", "--name-only")
	out, err := staged.CombinedOutput()
	if err != nil {
		t.Fatalf("git diff --cached: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "MARKER.md" {
		t.Fatalf("staged files = %q, want MARKER.md", out)
	}
}
