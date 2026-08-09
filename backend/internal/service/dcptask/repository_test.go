package dcptask

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
