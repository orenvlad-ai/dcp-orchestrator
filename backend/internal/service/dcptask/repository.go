package dcptask

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const markerDigest = "63af912083e6fc32693b315457555805855fe3db87bc6ab730946a061a2219f1"

// TargetValidationError is safe to expose as a rejected lab-target reason.
type TargetValidationError struct{ reason string }

func (e *TargetValidationError) Error() string { return e.reason }

type GitRepositoryValidator struct {
	TargetPath          string
	AllowedWorktreeRoot string
	Run                 func(ctx context.Context, dir string, args ...string) (string, error)
}

func (v GitRepositoryValidator) Validate(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error) {
	target, err := filepath.Abs(v.TargetPath)
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab target path is invalid")
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab target path cannot be resolved")
	}
	projectPath, err := filepath.EvalSymlinks(project.Path)
	if err != nil || filepath.Clean(projectPath) != filepath.Clean(target) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("registered dcp-lab project points at another path")
	}
	if project.ID != TargetProjectID || project.RepoOriginURL != "" || project.Kind.WithDefault() != domain.ProjectKindSingleRepo {
		return domain.DCPRepositoryIdentity{}, invalidTarget("registered dcp-lab project identity is out of scope")
	}

	run := v.Run
	if run == nil {
		run = runReadOnlyGit
	}
	repoRoot, err := run(ctx, target, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(repoRoot) != filepath.Clean(target) {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab repository root mismatch")
	}
	remotes, err := run(ctx, target, "remote")
	if err != nil || strings.TrimSpace(remotes) != "" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab target must have no remotes")
	}
	tracked, err := run(ctx, target, "ls-files")
	if err != nil || tracked != ".dcp-lab-target" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab baseline may track only its identity marker")
	}
	branch, err := run(ctx, target, "branch", "--show-current")
	if err != nil || branch != "main" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab baseline branch must be main")
	}
	count, err := run(ctx, target, "rev-list", "--count", "HEAD")
	if err != nil || count != "1" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab baseline must contain exactly one commit")
	}
	status, err := run(ctx, target, "status", "--porcelain")
	if err != nil || status != "" {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab target must be clean")
	}
	head, err := run(ctx, target, "rev-parse", "HEAD")
	if err != nil || len(head) != 40 {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab HEAD identity is invalid")
	}
	marker, err := os.ReadFile(filepath.Join(target, ".dcp-lab-target"))
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab identity marker is unreadable")
	}
	markerSum := sha256.Sum256(marker)
	markerSHA := hex.EncodeToString(markerSum[:])
	if markerSHA != markerDigest {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab identity marker content mismatch")
	}
	worktrees, err := run(ctx, target, "worktree", "list", "--porcelain")
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab linked worktrees cannot be inspected")
	}
	allowedRoot, err := filepath.Abs(v.AllowedWorktreeRoot)
	if err != nil {
		return domain.DCPRepositoryIdentity{}, invalidTarget("DCP worktree root is invalid")
	}
	for _, worktree := range worktreePaths(worktrees) {
		resolved, resolveErr := filepath.EvalSymlinks(worktree)
		if resolveErr != nil {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab has an unresolved linked worktree")
		}
		if filepath.Clean(resolved) == filepath.Clean(target) {
			continue
		}
		contained, containErr := filepath.Rel(allowedRoot, resolved)
		if containErr != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
			return domain.DCPRepositoryIdentity{}, invalidTarget("dcp-lab has a foreign linked worktree")
		}
	}

	identity := domain.DCPRepositoryIdentity{
		SchemaVersion: RepositorySchema,
		ProjectID:     TargetProjectID,
		Repository:    TargetRepository,
		Path:          target,
		HeadSHA:       head,
		MarkerDigest:  markerSHA,
	}
	identity.IdentityDigest, err = digestJSON(struct {
		SchemaVersion string `json:"schemaVersion"`
		ProjectID     string `json:"projectId"`
		Repository    string `json:"repository"`
		Path          string `json:"path"`
		HeadSHA       string `json:"headSha"`
		MarkerDigest  string `json:"markerDigest"`
	}{identity.SchemaVersion, identity.ProjectID, identity.Repository, identity.Path, identity.HeadSHA, identity.MarkerDigest})
	if err != nil {
		return domain.DCPRepositoryIdentity{}, fmt.Errorf("digest repository identity: %w", err)
	}
	return identity, nil
}

func runReadOnlyGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_OPTIONAL_LOCKS=0",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func worktreePaths(output string) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") {
			out = append(out, strings.TrimPrefix(line, "worktree "))
		}
	}
	return out
}

func invalidTarget(reason string) error { return &TargetValidationError{reason: reason} }
