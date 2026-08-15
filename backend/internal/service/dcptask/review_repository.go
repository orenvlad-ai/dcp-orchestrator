package dcptask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const reviewLabOrigin = "https://github.com/orenvlad-ai/dcp-review-lab.git"

type ReviewRepositoryProviderIdentity struct {
	NameWithOwner string
	Private       bool
	DefaultBranch string
	RepositoryID  int64
	OwnerID       int64
}

// ReviewRepositoryValidator proves one compile-time-allowlisted public
// PR-capable target without
// fetching or mutating it. The canonical submit adapter performs its own
// locked refresh first; the daemon independently verifies the resulting facts.
type ReviewRepositoryValidator struct {
	TargetPath          string
	TargetRoot          string
	AllowedWorktreeRoot string
	Run                 func(context.Context, string, ...string) (string, error)
	RunProvider         func(context.Context, string) (ReviewRepositoryProviderIdentity, error)
}

func (v ReviewRepositoryValidator) Validate(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	return v.validate(ctx, project, false)
}

func (v ReviewRepositoryValidator) ValidateContinuation(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	return v.validate(ctx, project, true)
}

func (v ReviewRepositoryValidator) validate(ctx context.Context, project domain.ProjectRecord, allowBehind bool) (domain.DCPRepositoryIdentity, error) {
	var spec domain.DCPPolicyTargetSpec
	exact := false
	for _, identity := range [][2]string{{PolicyTarget, PolicyProfile}, {RepoOnlyTarget, RepoOnlyProfile}} {
		candidate, _ := domain.DCPPolicyTarget(identity[0], identity[1])
		if string(project.ID) == candidate.Target {
			spec, exact = candidate, true
			break
		}
	}
	if !exact {
		return domain.DCPRepositoryIdentity{}, invalidTarget("policy target is not allowlisted")
	}
	targetPath := v.TargetPath
	if v.TargetRoot != "" {
		targetPath = filepath.Join(v.TargetRoot, spec.Target)
	}
	target, err := physicalPolicyPath(targetPath)
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("policy target path is invalid")
	}
	projectPath, err := physicalPolicyPath(project.Path)
	if err != nil || projectPath != target || string(project.ID) != spec.Target || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || project.RepoOriginURL != spec.OriginURL ||
		project.Config.DefaultBranch != spec.DefaultBranch || project.Config.SessionPrefix != spec.SessionPrefix || project.Config.AgentRules != spec.AgentRules {
		return domain.DCPRepositoryIdentity{}, invalidTarget("registered policy project identity is out of scope")
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
		{[]string{"remote", "get-url", "origin"}, spec.OriginURL},
		{[]string{"remote", "get-url", "--push", "origin"}, spec.OriginURL},
		{[]string{"branch", "--show-current"}, spec.DefaultBranch},
		{[]string{"status", "--porcelain"}, ""},
	}
	for _, check := range checks {
		got, err := run(ctx, target, check.args...)
		if err != nil || got != check.want {
			return domain.DCPRepositoryIdentity{}, invalidTarget("policy repository facts are not exact")
		}
	}
	head, err := run(ctx, target, "rev-parse", "HEAD")
	if err != nil || !validPolicySHA(head) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab HEAD is invalid")
	}
	originMain, err := run(ctx, target, "rev-parse", "refs/remotes/origin/"+spec.DefaultBranch)
	if err != nil || !validPolicySHA(originMain) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab origin/main is invalid")
	}
	if originMain != head && !allowBehind {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab main is not the exact refreshed origin/main")
	}
	if originMain != head {
		if _, err := run(ctx, target, "merge-base", "--is-ancestor", head, originMain); err != nil {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-review-lab main is not an ancestor of refreshed origin/main")
		}
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
	providerIdentity, err := provider(ctx, spec.Repository)
	wantProvider := ReviewRepositoryProviderIdentity{
		NameWithOwner: spec.Repository, Private: false, DefaultBranch: spec.DefaultBranch,
		RepositoryID: spec.ProviderRepositoryID, OwnerID: spec.ProviderOwnerID,
	}
	if err != nil || providerIdentity != wantProvider {
		return domain.DCPRepositoryIdentity{}, invalidTarget("policy provider identity is not exact and public")
	}
	providerDigest := fmt.Sprintf("%s|%t|%s|%d|%d", wantProvider.NameWithOwner, wantProvider.Private, wantProvider.DefaultBranch, wantProvider.RepositoryID, wantProvider.OwnerID)
	digestInput := strings.Join([]string{spec.Target, spec.Repository, target, strings.ToLower(originMain), providerDigest}, "\x00")
	sum := sha256.Sum256([]byte(digestInput))
	return domain.DCPRepositoryIdentity{
		SchemaVersion: RepositorySchema, ProjectID: spec.Target, Repository: spec.Repository,
		Path: target, HeadSHA: strings.ToLower(originMain), IdentityDigest: hex.EncodeToString(sum[:]),
	}, nil
}

func readPublicReviewRepository(ctx context.Context, repository string) (ReviewRepositoryProviderIdentity, error) {
	out, err := exec.CommandContext(ctx, "gh", "api", "--method", "GET", "repos/"+repository).Output()
	if err != nil {
		return ReviewRepositoryProviderIdentity{}, err
	}
	var response struct {
		Repository    *string `json:"full_name"`
		Private       *bool   `json:"private"`
		DefaultBranch *string `json:"default_branch"`
		RepositoryID  *int64  `json:"id"`
		Owner         *struct {
			ID *int64 `json:"id"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return ReviewRepositoryProviderIdentity{}, err
	}
	if response.Repository == nil || response.Private == nil || response.DefaultBranch == nil ||
		response.RepositoryID == nil || response.Owner == nil || response.Owner.ID == nil {
		return ReviewRepositoryProviderIdentity{}, fmt.Errorf("provider repository identity is incomplete")
	}
	return ReviewRepositoryProviderIdentity{
		NameWithOwner: *response.Repository, Private: *response.Private, DefaultBranch: *response.DefaultBranch,
		RepositoryID: *response.RepositoryID, OwnerID: *response.Owner.ID,
	}, nil
}

func physicalPolicyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil || filepath.Clean(abs) != abs || !filepath.IsAbs(abs) {
		return "", fmt.Errorf("invalid absolute path")
	}
	return filepath.EvalSymlinks(abs)
}
