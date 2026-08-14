package dcptask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const reviewLabOrigin = "https://github.com/orenvlad-ai/dcp-review-lab.git"

// ReviewRepositoryValidator proves the sole public PR-capable target without
// fetching or mutating it. The canonical submit adapter performs its own
// locked refresh first; the daemon independently verifies the resulting facts.
type ReviewRepositoryValidator struct {
	TargetPath          string
	AllowedWorktreeRoot string
	Run                 func(context.Context, string, ...string) (string, error)
	RunProvider         func(context.Context, string) (string, error)
}

func (v ReviewRepositoryValidator) Validate(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	target, err := physicalPolicyPath(v.TargetPath)
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab target path is invalid")
	}
	projectPath, err := physicalPolicyPath(project.Path)
	if err != nil || projectPath != target || project.ID != PolicyTarget || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || project.RepoOriginURL != reviewLabOrigin {
		return domain.DCPRepositoryIdentity{}, invalidTarget("registered dcp-review-lab project identity is out of scope")
	}
	run := v.Run
	if run == nil {
		run = runReadOnlyGit
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, target},
		{[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, reviewLabOrigin},
		{[]string{"remote", "get-url", "--push", "origin"}, reviewLabOrigin},
		{[]string{"branch", "--show-current"}, "main"},
		{[]string{"status", "--porcelain"}, ""},
	}
	for _, check := range checks {
		got, err := run(ctx, target, check.args...)
		if err != nil || got != check.want {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab repository facts are not exact")
		}
	}
	head, err := run(ctx, target, "rev-parse", "HEAD")
	if err != nil || !validPolicySHA(head) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab HEAD is invalid")
	}
	originMain, err := run(ctx, target, "rev-parse", "refs/remotes/origin/main")
	if err != nil || originMain != head {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab main is not the exact refreshed origin/main")
	}
	worktrees, err := run(ctx, target, "worktree", "list", "--porcelain")
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab worktrees are unreadable")
	}
	allowed, err := filepath.Abs(v.AllowedWorktreeRoot)
	if err != nil || !filepath.IsAbs(allowed) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("DCP worktree root is invalid")
	}
	for _, path := range worktreePaths(worktrees) {
		resolved, err := physicalPolicyPath(path)
		if err != nil {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab has an unresolved worktree")
		}
		if resolved == target {
			continue
		}
		rel, err := filepath.Rel(allowed, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab has a foreign linked worktree")
		}
	}
	provider := v.RunProvider
	if provider == nil {
		provider = readPublicReviewRepository
	}
	providerIdentity, err := provider(ctx, PolicyRepositoryName)
	if err != nil || providerIdentity != PolicyRepositoryName+"|false|main" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab provider visibility is not exact and public")
	}
	digestInput := strings.Join([]string{PolicyTarget, PolicyRepositoryName, target, strings.ToLower(head)}, "\x00")
	sum := sha256.Sum256([]byte(digestInput))
	return domain.DCPRepositoryIdentity{
		SchemaVersion: RepositorySchema, ProjectID: PolicyTarget, Repository: PolicyRepositoryName,
		Path: target, HeadSHA: strings.ToLower(head), IdentityDigest: hex.EncodeToString(sum[:]),
	}, nil
}

func readPublicReviewRepository(ctx context.Context, repository string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "repo", "view", repository, "--json", "nameWithOwner,isPrivate,defaultBranchRef", "--jq", `.nameWithOwner + "|" + (.isPrivate|tostring) + "|" + .defaultBranchRef.name`).Output()
	return strings.TrimSpace(string(out)), err
}

func physicalPolicyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil || filepath.Clean(abs) != abs || !filepath.IsAbs(abs) {
		return "", fmt.Errorf("invalid absolute path")
	}
	return filepath.EvalSymlinks(abs)
}
