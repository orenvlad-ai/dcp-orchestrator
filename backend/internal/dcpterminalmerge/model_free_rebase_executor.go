package dcpterminalmerge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	modelFreeGitPath      = "/usr/bin/git"
	modelFreeGHPath       = "/opt/homebrew/bin/gh"
	modelFreeGHDigest     = "f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e"
	modelFreeResolvedBlob = "80a658c4cfc3ffda5786da316bc0bd10ffb1834f"
	modelFreeStatusDigest = "0c7f653e181d09cdbbc96d3bcff1ca63851fcaf3a3db0236a0896d88f0f6be84"
)

type ModelFreeRebaseExecutor interface {
	Preflight(context.Context, domain.DCPCard12ModelFreeRebaseContinuation) error
	Execute(context.Context, domain.DCPCard12ModelFreeRebaseContinuation) (string, error)
	InspectCompleted(context.Context, domain.DCPCard12ModelFreeRebaseContinuation) (string, error)
}

type modelFreeRebaseExecutor struct {
	runtime arbiterRuntime
}

func NewModelFreeRebaseExecutor(runtime arbiterRuntime) ModelFreeRebaseExecutor {
	return &modelFreeRebaseExecutor{runtime: runtime}
}

func (x *modelFreeRebaseExecutor) Preflight(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	if x == nil || x.runtime == nil || !exactModelFreeRebaseContinuation(row) ||
		row.Status != domain.DCPModelFreeRebaseAuthorized || row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 {
		return errors.New("card-12 model-free rebase: executor identity is invalid")
	}
	if err := x.requireQuiescence(ctx); err != nil {
		return err
	}
	if info, err := os.Lstat(modelFreeGitPath); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, errors.New("card-12 model-free rebase: exact git binary is unavailable"))
	}
	if err := requireRegularDigest(modelFreeGHPath, modelFreeGHDigest); err != nil {
		return fmt.Errorf("card-12 model-free rebase: exact gh binary: %w", err)
	}
	return x.validatePreservedRebase(ctx, row)
}

func (x *modelFreeRebaseExecutor) Execute(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) (string, error) {
	if x == nil || x.runtime == nil || !exactModelFreeRebaseContinuation(row) ||
		row.Status != domain.DCPModelFreeRebaseRunning || row.ModelFreeActionCount != 1 || row.ReviewerModelCallCount != 0 {
		return "", errors.New("card-12 model-free rebase: fenced executor identity is invalid")
	}
	if err := x.requireQuiescence(ctx); err != nil {
		return "", err
	}
	if info, err := os.Lstat(modelFreeGitPath); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.Join(err, errors.New("card-12 model-free rebase: exact git binary is unavailable"))
	}
	if err := requireRegularDigest(modelFreeGHPath, modelFreeGHDigest); err != nil {
		return "", err
	}
	if err := x.validatePreservedRebase(ctx, row); err != nil {
		return "", err
	}
	repo := row.WorktreePath
	if _, err := x.git(ctx, repo, nil, "--literal-pathspecs", "add", "--", arbiterConflictPath); err != nil {
		return "", fmt.Errorf("card-12 model-free rebase: stage exact path: %w", err)
	}
	stage, err := x.git(ctx, repo, nil, "ls-files", "--stage", "--", arbiterConflictPath)
	if err != nil || strings.TrimSpace(string(stage)) != "100644 "+modelFreeResolvedBlob+" 0\t"+arbiterConflictPath {
		return "", errors.Join(err, errors.New("card-12 model-free rebase: staged blob is not exact"))
	}
	if err := x.requireOnlyIndexPath(ctx, row); err != nil {
		return "", err
	}
	env := []string{
		"GIT_EDITOR=:",
		"GIT_COMMITTER_NAME=Влад Сагитов",
		"GIT_COMMITTER_EMAIL=ovlmacbook@oVl-MacBook-Pro.local",
		"GIT_COMMITTER_DATE=2026-08-11T22:38:48+05:00",
	}
	if _, err := x.git(ctx, repo, env, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false", "rebase", "--continue"); err != nil {
		return "", fmt.Errorf("card-12 model-free rebase: one rebase continuation failed: %w", err)
	}
	newHead, err := x.validateDetachedCandidate(ctx, row)
	if err != nil {
		return newHead, err
	}
	if _, err := x.git(ctx, repo, nil, "update-ref", row.PushRef, newHead, row.OldHead); err != nil {
		return newHead, fmt.Errorf("card-12 model-free rebase: exact local ref update failed: %w", err)
	}
	if _, err := x.git(ctx, repo, nil, "symbolic-ref", "HEAD", row.PushRef); err != nil {
		return newHead, fmt.Errorf("card-12 model-free rebase: attach exact existing branch failed: %w", err)
	}
	if err := x.validateAttachedCandidate(ctx, row, newHead, false); err != nil {
		return newHead, err
	}
	if err := x.requireRemoteRefs(ctx, row, row.OldHead); err != nil {
		return newHead, err
	}
	lease := "--force-with-lease=" + row.PushRef + ":" + row.OldHead
	if _, err := x.git(ctx, repo, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential",
		"push", lease, "origin", "HEAD:"+row.PushRef); err != nil {
		return newHead, fmt.Errorf("card-12 model-free rebase: one guarded push failed: %w", err)
	}
	if err := x.requireRemoteRefs(ctx, row, newHead); err != nil {
		return newHead, err
	}
	if err := x.validateAttachedCandidate(ctx, row, newHead, true); err != nil {
		return newHead, err
	}
	return newHead, nil
}

func (x *modelFreeRebaseExecutor) InspectCompleted(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) (string, error) {
	if x == nil || !exactModelFreeRebaseContinuation(row) || row.Status != domain.DCPModelFreeRebaseRunning ||
		row.ModelFreeActionCount != 1 || row.ReviewerModelCallCount != 0 {
		return "", errors.New("card-12 model-free rebase: running reconciliation identity is invalid")
	}
	local, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", row.PushRef)
	if err != nil || !validSHA(local) || strings.EqualFold(local, row.OldHead) {
		return "", errors.Join(err, errors.New("card-12 model-free rebase: no exact completed local ref exists"))
	}
	if err := x.requireRemoteRefs(ctx, row, local); err != nil {
		return "", err
	}
	if err := x.validateAttachedCandidate(ctx, row, local, true); err != nil {
		return "", err
	}
	return strings.ToLower(local), nil
}

func (x *modelFreeRebaseExecutor) requireQuiescence(ctx context.Context) error {
	inspector, ok := x.runtime.(ports.RuntimeQuiescenceInspector)
	if !ok {
		return errors.New("card-12 model-free rebase: runtime quiescence inspection is unavailable")
	}
	for _, handle := range []string{
		"dcp-review-lab-7", "dcp-review-lab-9", "dcp-review-lab-10", "dcp-review-lab-11", "dcp-review-lab-12",
		"review-dcp-review-lab-7", "review-dcp-review-lab-9", "review-dcp-review-lab-10", "review-dcp-review-lab-11", "review-dcp-review-lab-12",
		ArbiterRuntimeHandle, ArbiterSuccessorRuntimeHandle, "dcp-card12-fresh-worker-recovery",
	} {
		quiescent, err := inspector.IsRuntimeQuiescent(ctx, ports.RuntimeHandle{ID: handle})
		if err != nil || !quiescent {
			return errors.Join(err, fmt.Errorf("card-12 model-free rebase: runtime %s is not provably quiescent", handle))
		}
	}
	return nil
}

func (x *modelFreeRebaseExecutor) validatePreservedRebase(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	repo := row.WorktreePath
	if !filepath.IsAbs(repo) || filepath.Clean(repo) != repo {
		return errors.New("card-12 model-free rebase: worktree path is not exact")
	}
	labRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(repo))))
	expectedCommon := filepath.Join(labRoot, "targets", ProjectID, ".git")
	expectedGitDir := filepath.Join(expectedCommon, "worktrees", "dcp-review-lab-12")
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, repo},
		{[]string{"rev-parse", "--absolute-git-dir"}, expectedGitDir},
		{[]string{"rev-parse", "--path-format=absolute", "--git-common-dir"}, expectedCommon},
		{[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, RepositoryURL},
		{[]string{"remote", "get-url", "--push", "origin"}, RepositoryURL},
		{[]string{"rev-parse", "HEAD"}, row.CurrentMain},
		{[]string{"rev-parse", row.PushRef}, row.OldHead},
		{[]string{"rev-parse", "ORIG_HEAD"}, row.OldHead},
		{[]string{"rev-parse", "REBASE_HEAD"}, row.OldHead},
	} {
		got, err := x.gitText(ctx, repo, nil, check.args...)
		if err != nil || got != check.want {
			return errors.Join(err, errors.New("card-12 model-free rebase: preserved Git identity drifted"))
		}
	}
	if out, err := x.git(ctx, repo, nil, "symbolic-ref", "-q", "HEAD"); err == nil || len(out) != 0 {
		return errors.New("card-12 model-free rebase: HEAD is not exactly detached")
	}
	worktrees, err := x.gitText(ctx, repo, nil, "worktree", "list", "--porcelain")
	if err != nil || strings.Count(worktrees, "worktree "+repo+"\n") != 1 ||
		!strings.Contains(worktrees, "worktree "+repo+"\nHEAD "+row.CurrentMain+"\ndetached") ||
		strings.Contains(worktrees, "branch "+row.PushRef) {
		return errors.Join(err, errors.New("card-12 model-free rebase: worktree/branch ownership drifted"))
	}
	status, err := x.git(ctx, repo, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || digestBytes(status) != modelFreeStatusDigest || !bytes.Equal(status, []byte("AA "+arbiterConflictPath+"\x00")) {
		return errors.Join(err, errors.New("card-12 model-free rebase: exact one-path status drifted"))
	}
	if err := requireRegularFile(filepath.Join(repo, filepath.FromSlash(arbiterConflictPath)), []byte(freshWorkerExpectedBytes), 0o644); err != nil {
		return err
	}
	unmerged, err := x.gitText(ctx, repo, nil, "ls-files", "--unmerged", "--", arbiterConflictPath)
	if err != nil || unmerged != "100644 ed237ce2dd2684371797e22634480ffb28dc9e77 2\t"+arbiterConflictPath+"\n100644 a4c945ba7328504f2efea44f076a1407c6aa7b47 3\t"+arbiterConflictPath {
		return errors.Join(err, errors.New("card-12 model-free rebase: exact AA index stages drifted"))
	}
	if err := validateExactRebaseMetadata(expectedGitDir, row); err != nil {
		return err
	}
	for _, residue := range []string{"MERGE_HEAD", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "sequencer", "index.lock", "packed-refs.lock", "shallow.lock"} {
		if _, err := os.Lstat(filepath.Join(expectedGitDir, residue)); err == nil {
			return fmt.Errorf("card-12 model-free rebase: foreign Git residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := x.requireRemoteRefs(ctx, row, row.OldHead); err != nil {
		return err
	}
	parent, err := x.gitText(ctx, repo, nil, "show", "-s", "--format=%P", row.OldHead)
	if err != nil || parent != "dbaf01b05e85ffffa4c843a905e2fe5229eaf0da" {
		return errors.Join(err, errors.New("card-12 model-free rebase: stopped commit parent drifted"))
	}
	base, err := x.gitText(ctx, repo, nil, "merge-base", row.OldHead, row.CurrentMain)
	if err != nil || base != parent {
		return errors.Join(err, errors.New("card-12 model-free rebase: exact merge base drifted"))
	}
	diff, err := x.git(ctx, repo, nil, "diff", "--binary", "--full-index", row.CurrentMain+".."+row.OldHead, "--", arbiterConflictPath)
	if err != nil || digestBytes(diff) != "9a752434961d4ef2dc8c6478582ab497ee4c19436b28ee0112c0fb5600b81a18" {
		return errors.Join(err, errors.New("card-12 model-free rebase: old candidate diff drifted"))
	}
	return nil
}

func (x *modelFreeRebaseExecutor) requireOnlyIndexPath(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	status, err := x.gitText(ctx, row.WorktreePath, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "M  "+arbiterConflictPath {
		return errors.Join(err, errors.New("card-12 model-free rebase: stage produced foreign status"))
	}
	return nil
}

func (x *modelFreeRebaseExecutor) validateDetachedCandidate(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation) (string, error) {
	if out, err := x.git(ctx, row.WorktreePath, nil, "symbolic-ref", "-q", "HEAD"); err == nil || len(out) != 0 {
		return "", errors.New("card-12 model-free rebase: continued head is not detached")
	}
	head, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if err := x.validateCandidateCommit(ctx, row, head); err != nil {
		return head, err
	}
	return strings.ToLower(head), nil
}

func (x *modelFreeRebaseExecutor) validateAttachedCandidate(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, head string, remote bool) error {
	branch, err := x.gitText(ctx, row.WorktreePath, nil, "branch", "--show-current")
	if err != nil || branch != row.SourceBranch {
		return errors.Join(err, errors.New("card-12 model-free rebase: exact branch is not attached"))
	}
	local, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", row.PushRef)
	if err != nil || local != head {
		return errors.Join(err, errors.New("card-12 model-free rebase: local ref is not exact"))
	}
	if remote {
		if err := x.requireRemoteRefs(ctx, row, head); err != nil {
			return err
		}
	}
	return x.validateCandidateCommit(ctx, row, head)
}

func (x *modelFreeRebaseExecutor) validateCandidateCommit(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, head string) error {
	if !validSHA(head) || strings.EqualFold(head, row.OldHead) {
		return errors.New("card-12 model-free rebase: new head is invalid")
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"show", "-s", "--format=%P", head}, row.CurrentMain},
		{[]string{"show", "-s", "--format=%s", head}, "chore: add i13 arbiter intent B canary"},
		{[]string{"show", "-s", "--format=%an%x00%ae%x00%aI", head}, "Влад Сагитов\x00ovlmacbook@oVl-MacBook-Pro.local\x002026-08-11T22:38:48+05:00"},
		{[]string{"diff", "--name-status", row.CurrentMain + ".." + head}, "M\t" + arbiterConflictPath},
		{[]string{"show", head + ":" + arbiterConflictPath}, strings.TrimSuffix(freshWorkerExpectedBytes, "\n")},
		{[]string{"status", "--porcelain=v1", "--untracked-files=all"}, ""},
	} {
		got, err := x.gitText(ctx, row.WorktreePath, nil, check.args...)
		if err != nil || got != check.want {
			return errors.Join(err, errors.New("card-12 model-free rebase: exact candidate postcondition drifted"))
		}
	}
	gitDir, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	for _, residue := range []string{"AUTO_MERGE", "MERGE_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "sequencer", "index.lock"} {
		if _, err := os.Lstat(filepath.Join(gitDir, residue)); err == nil {
			return fmt.Errorf("card-12 model-free rebase: postcondition residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (x *modelFreeRebaseExecutor) requireRemoteRefs(ctx context.Context, row domain.DCPCard12ModelFreeRebaseContinuation, wantBranch string) error {
	out, err := x.gitText(ctx, row.WorktreePath, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential",
		"ls-remote", "--heads", "origin", "refs/heads/main", row.PushRef)
	if err != nil {
		return fmt.Errorf("card-12 model-free rebase: authenticated remote read failed: %w", err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !validSHA(fields[0]) {
			return errors.New("card-12 model-free rebase: remote ref response is malformed")
		}
		refs[fields[1]] = strings.ToLower(fields[0])
	}
	if len(refs) != 2 || refs["refs/heads/main"] != row.CurrentMain || refs[row.PushRef] != strings.ToLower(wantBranch) {
		return errors.New("card-12 model-free rebase: exact remote refs drifted")
	}
	return nil
}

func (x *modelFreeRebaseExecutor) git(ctx context.Context, repo string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, modelFreeGitPath, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (x *modelFreeRebaseExecutor) gitText(ctx context.Context, repo string, env []string, args ...string) (string, error) {
	out, err := x.git(ctx, repo, env, args...)
	return strings.TrimSpace(string(out)), err
}

func validateExactRebaseMetadata(gitDir string, row domain.DCPCard12ModelFreeRebaseContinuation) error {
	paths := []string{"AUTO_MERGE", "MERGE_MSG", "REBASE_HEAD"}
	for _, name := range []string{"author-script", "done", "drop_redundant_commits", "end", "git-rebase-todo", "git-rebase-todo.backup", "head-name", "interactive", "message", "msgnum", "no-reschedule-failed-exec", "onto", "orig-head", "patch", "stopped-sha"} {
		paths = append(paths, filepath.Join("rebase-merge", name))
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(gitDir, relative))
		if err != nil {
			return err
		}
		fileDigest := sha256.Sum256(data)
		for _, field := range []string{filepath.ToSlash(relative), strconv.Itoa(len(data)), hex.EncodeToString(fileDigest[:])} {
			_, _ = h.Write([]byte(field))
			_, _ = h.Write([]byte{0})
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != row.RebaseMetadataDigest {
		return errors.New("card-12 model-free rebase: metadata aggregate drifted")
	}
	checks := map[string]string{
		"rebase-merge/onto": row.CurrentMain + "\n", "rebase-merge/orig-head": row.OldHead + "\n",
		"rebase-merge/stopped-sha": row.OldHead + "\n", "rebase-merge/head-name": "detached HEAD\n",
		"rebase-merge/msgnum": "1\n", "rebase-merge/end": "1\n", "rebase-merge/git-rebase-todo": "",
		"rebase-merge/message": "chore: add i13 arbiter intent B canary\n\n# Conflicts:\n#\tcanary/i13-arbiter-conflict.txt\n",
	}
	for relative, want := range checks {
		data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(relative)))
		if err != nil || string(data) != want {
			return errors.Join(err, fmt.Errorf("card-12 model-free rebase: metadata %s drifted", relative))
		}
	}
	entries, err := os.ReadDir(filepath.Join(gitDir, "rebase-merge"))
	if err != nil || len(entries) != 15 {
		return errors.Join(err, errors.New("card-12 model-free rebase: rebase metadata set drifted"))
	}
	return nil
}

func requireRegularDigest(path, want string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, errors.New("artifact is not a regular file"))
	}
	data, err := os.ReadFile(path)
	if err != nil || digestBytes(data) != want {
		return errors.Join(err, errors.New("artifact digest drifted"))
	}
	return nil
}

func requireRegularFile(path string, want []byte, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode {
		return errors.Join(err, errors.New("card-12 model-free rebase: resolved file identity drifted"))
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, want) {
		return errors.Join(err, errors.New("card-12 model-free rebase: resolved bytes drifted"))
	}
	return nil
}
