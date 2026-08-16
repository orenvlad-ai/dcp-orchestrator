package dcptask

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const testMarker = "DCP AO I3 disposable synthetic target\n"

func createDCPTestRepository(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "dcp-lab")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dcp-lab-target"), []byte(testMarker), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	runGitTest(t, dir, "init", "-b", "main")
	runGitTest(t, dir, "add", ".dcp-lab-target")
	runGitTest(t, dir, "-c", "user.email=dcp-test@example.invalid", "-c", "user.name=DCP Test", "commit", "-m", "synthetic baseline")
	return dir
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return string(out)
}

func testProject(path string) domain.ProjectRecord {
	return domain.ProjectRecord{
		ID:           TargetProjectID,
		Path:         path,
		DisplayName:  "DCP Lab",
		RegisteredAt: time.Unix(100, 0).UTC(),
		Kind:         domain.ProjectKindSingleRepo,
	}
}

func TestGitRepositoryValidatorAcceptsOnlyExactRemoteFreeLab(t *testing.T) {
	repo := createDCPTestRepository(t)
	validator := GitRepositoryValidator{
		TargetPath:          repo,
		AllowedWorktreeRoot: filepath.Join(filepath.Dir(repo), "worktrees"),
	}
	identity, err := validator.Validate(context.Background(), testProject(repo))
	if err != nil {
		t.Fatalf("validate exact dcp-lab: %v", err)
	}
	if identity.Repository != TargetRepository || identity.ProjectID != TargetProjectID || identity.MarkerDigest != markerDigest || len(identity.HeadSHA) != 40 || len(identity.IdentityDigest) != 64 {
		t.Fatalf("identity = %+v", identity)
	}

	runGitTest(t, repo, "remote", "add", "origin", "https://example.invalid/real.git")
	if _, err := validator.Validate(context.Background(), testProject(repo)); err == nil {
		t.Fatal("validator accepted a target with a remote")
	}
	runGitTest(t, repo, "remote", "remove", "origin")

	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("not clean\n"), 0o600); err != nil {
		t.Fatalf("dirty repo: %v", err)
	}
	if _, err := validator.Validate(context.Background(), testProject(repo)); err == nil {
		t.Fatal("validator accepted a dirty target")
	}
}

func TestGitRepositoryValidatorRejectsRegistryPathMismatch(t *testing.T) {
	repo := createDCPTestRepository(t)
	other := t.TempDir()
	validator := GitRepositoryValidator{TargetPath: repo, AllowedWorktreeRoot: t.TempDir()}
	if _, err := validator.Validate(context.Background(), testProject(other)); err == nil {
		t.Fatal("validator accepted a registered path mismatch")
	}
}

func TestReviewRepositoryValidatorAcceptsOnlyExactSyntheticOriginAndManagedWorktrees(t *testing.T) {
	repo := createDCPTestRepository(t)
	runGitTest(t, repo, "remote", "add", "origin", reviewLabOrigin)
	head := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "update-ref", "refs/remotes/origin/main", head)
	root := filepath.Join(filepath.Dir(repo), "worktrees")
	validator := ReviewRepositoryValidator{TargetPath: repo, AllowedWorktreeRoot: root, RunProvider: func(context.Context, string) (ReviewRepositoryProviderIdentity, error) {
		return ReviewRepositoryProviderIdentity{NameWithOwner: PolicyRepositoryName, DefaultBranch: "main", RepositoryID: 1329007118, OwnerID: 237411244}, nil
	}}
	project := domain.ProjectRecord{
		ID: PolicyTarget, Path: repo, Kind: domain.ProjectKindSingleRepo,
		RepoOriginURL: reviewLabOrigin, Config: reviewLabProjectConfig(), RegisteredAt: time.Unix(100, 0).UTC(),
	}
	identity, err := validator.Validate(context.Background(), project)
	if err != nil {
		t.Fatalf("validate exact review lab: %v", err)
	}
	if identity.ProjectID != PolicyTarget || identity.Repository != PolicyRepositoryName || identity.HeadSHA != head || len(identity.IdentityDigest) != 64 {
		t.Fatalf("review-lab identity = %+v", identity)
	}

	foreign := project
	foreign.RepoOriginURL = "https://github.com/orenvlad-ai/other.git"
	if _, err := validator.Validate(context.Background(), foreign); err == nil {
		t.Fatal("validator accepted foreign registered origin")
	}
	runGitTest(t, repo, "remote", "set-url", "--push", "origin", "https://example.invalid/foreign.git")
	if _, err := validator.Validate(context.Background(), project); err == nil {
		t.Fatal("validator accepted foreign push authority")
	}
	runGitTest(t, repo, "remote", "set-url", "--push", "origin", reviewLabOrigin)
	validator.RunProvider = func(context.Context, string) (ReviewRepositoryProviderIdentity, error) {
		return ReviewRepositoryProviderIdentity{NameWithOwner: PolicyRepositoryName, Private: true, DefaultBranch: "main", RepositoryID: 1329007118, OwnerID: 237411244}, nil
	}
	if _, err := validator.Validate(context.Background(), project); err == nil {
		t.Fatal("validator accepted private provider repository")
	}
}

func TestReviewRepositoryValidatorAllowsOnlyAncestralContinuationBehindMain(t *testing.T) {
	repo := createDCPTestRepository(t)
	runGitTest(t, repo, "remote", "add", "origin", reviewLabOrigin)
	base := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "checkout", "-b", "provider-main")
	if err := os.WriteFile(filepath.Join(repo, "provider.txt"), []byte("advanced\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "provider.txt")
	runGitTest(t, repo, "-c", "user.email=dcp-test@example.invalid", "-c", "user.name=DCP Test", "commit", "-m", "provider advance")
	advanced := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "update-ref", "refs/remotes/origin/main", advanced)
	runGitTest(t, repo, "checkout", "main")
	if got := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD")); got != base {
		t.Fatalf("local main = %s, want preserved base %s", got, base)
	}
	validator := ReviewRepositoryValidator{
		TargetPath: repo, AllowedWorktreeRoot: filepath.Join(filepath.Dir(repo), "worktrees"),
		RunProvider: func(context.Context, string) (ReviewRepositoryProviderIdentity, error) {
			return ReviewRepositoryProviderIdentity{NameWithOwner: PolicyRepositoryName, DefaultBranch: "main", RepositoryID: 1329007118, OwnerID: 237411244}, nil
		},
	}
	project := domain.ProjectRecord{ID: PolicyTarget, Path: repo, Kind: domain.ProjectKindSingleRepo, RepoOriginURL: reviewLabOrigin, Config: reviewLabProjectConfig()}
	if _, err := validator.Validate(context.Background(), project); err == nil {
		t.Fatal("new-submit validator accepted a local main behind origin/main")
	}
	identity, err := validator.ValidateContinuation(context.Background(), project)
	if err != nil {
		t.Fatalf("continuation validator rejected ancestral local main: %v", err)
	}
	if identity.HeadSHA != advanced {
		t.Fatalf("continuation identity head = %s, want origin/main %s", identity.HeadSHA, advanced)
	}
	runGitTest(t, repo, "checkout", "provider-main")
	runGitTest(t, repo, "checkout", "--orphan", "foreign-main")
	if err := os.WriteFile(filepath.Join(repo, "foreign.txt"), []byte("foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "foreign.txt")
	runGitTest(t, repo, "-c", "user.email=dcp-test@example.invalid", "-c", "user.name=DCP Test", "commit", "-m", "foreign main")
	foreign := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "update-ref", "refs/heads/main", foreign)
	runGitTest(t, repo, "checkout", "main")
	if _, err := validator.ValidateContinuation(context.Background(), project); err == nil {
		t.Fatal("continuation validator accepted a divergent local main")
	}
}

func TestReviewRepositoryValidatorAcceptsOnlyExactRepoOnlyProviderIdentity(t *testing.T) {
	repo := createDCPTestRepository(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/orenvlad-ai/wb-browser-extension.git")
	head := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	runGitTest(t, repo, "update-ref", "refs/remotes/origin/main", head)
	validator := ReviewRepositoryValidator{
		TargetPath: repo, AllowedWorktreeRoot: filepath.Join(filepath.Dir(repo), "worktrees"),
		RunProvider: func(_ context.Context, repository string) (ReviewRepositoryProviderIdentity, error) {
			if repository != RepoOnlyRepositoryName {
				t.Fatalf("provider lookup repository = %q", repository)
			}
			return ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}, nil
		},
	}
	project := domain.ProjectRecord{ID: RepoOnlyTarget, Path: repo, Kind: domain.ProjectKindSingleRepo,
		RepoOriginURL: "https://github.com/orenvlad-ai/wb-browser-extension.git",
		Config:        domain.ProjectConfig{DefaultBranch: "main", SessionPrefix: RepoOnlyTarget, AgentRules: domain.DCPRepoOnlyPolicyAgentRules}}
	identity, err := validator.Validate(context.Background(), project)
	if err != nil || identity.ProjectID != RepoOnlyTarget || identity.Repository != RepoOnlyRepositoryName || identity.HeadSHA != head {
		t.Fatalf("repo-only identity=%+v err=%v", identity, err)
	}
	exact := ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}
	tests := []struct {
		name     string
		identity ReviewRepositoryProviderIdentity
		err      error
	}{
		{name: "private", identity: ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, Private: true, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}},
		{name: "wrong repository", identity: ReviewRepositoryProviderIdentity{NameWithOwner: "orenvlad-ai/foreign", DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}},
		{name: "wrong default branch", identity: ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "master", RepositoryID: 1335072844, OwnerID: 237411244}},
		{name: "wrong repository id", identity: ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1329007118, OwnerID: 237411244}},
		{name: "wrong owner id", identity: ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 1}},
		{name: "provider error", identity: exact, err: errors.New("provider unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator.RunProvider = func(context.Context, string) (ReviewRepositoryProviderIdentity, error) {
				return test.identity, test.err
			}
			if _, err := validator.Validate(context.Background(), project); err == nil {
				t.Fatal("repo-only validator accepted inexact provider identity")
			}
		})
	}
}

func TestReadPublicReviewRepositoryUsesSupportedGHAPI(t *testing.T) {
	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	script := `#!/bin/sh
set -eu
test "$#" -eq 4
test "$1" = api
test "$2" = --method
test "$3" = GET
test "$4" = repos/orenvlad-ai/wb-browser-extension
printf '%s\n' '{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}'
`
	if err := os.WriteFile(gh, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	identity, err := readPublicReviewRepository(context.Background(), RepoOnlyRepositoryName)
	want := ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}
	if err != nil || identity != want {
		t.Fatalf("provider identity=%+v err=%v", identity, err)
	}
}

func TestReadPublicReviewRepositoryRejectsMalformedOrIncompleteJSON(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "malformed", json: `{`},
		{name: "missing repository", json: `{"private":false,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}`},
		{name: "null repository id", json: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":null,"owner":{"id":237411244}}`},
		{name: "null owner", json: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844,"owner":null}`},
		{name: "missing owner id", json: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844,"owner":{}}`},
		{name: "wrong id type", json: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":"1335072844","owner":{"id":237411244}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			gh := filepath.Join(bin, "gh")
			script := "#!/bin/sh\nprintf '%s\\n' '" + test.json + "'\n"
			if err := os.WriteFile(gh, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			if _, err := readPublicReviewRepository(context.Background(), RepoOnlyRepositoryName); err == nil {
				t.Fatal("provider lookup accepted malformed or incomplete JSON")
			}
		})
	}
}

func TestReadPublicReviewRepositoryRejectsCommandFailure(t *testing.T) {
	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := readPublicReviewRepository(context.Background(), RepoOnlyRepositoryName); err == nil {
		t.Fatal("provider lookup accepted command failure")
	}
}

func TestReadPublicReviewRepositoryLiveExactProvider(t *testing.T) {
	if os.Getenv("DCP_PROVIDER_IDENTITY_LIVE_TEST") != "1" {
		t.Skip("set DCP_PROVIDER_IDENTITY_LIVE_TEST=1 for the model-free exact-provider harness")
	}
	identity, err := readPublicReviewRepository(context.Background(), RepoOnlyRepositoryName)
	want := ReviewRepositoryProviderIdentity{NameWithOwner: RepoOnlyRepositoryName, DefaultBranch: "main", RepositoryID: 1335072844, OwnerID: 237411244}
	if err != nil || identity != want {
		t.Fatalf("live provider identity=%+v err=%v", identity, err)
	}
}

func reviewLabProjectConfig() domain.ProjectConfig {
	return domain.ProjectConfig{DefaultBranch: "main", SessionPrefix: PolicyTarget, AgentRules: domain.DCPReviewLabPolicyAgentRules}
}
