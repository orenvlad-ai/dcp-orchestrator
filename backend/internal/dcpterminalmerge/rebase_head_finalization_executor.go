package dcpterminalmerge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type rebaseHeadFinalizationExecutor struct {
	runtime arbiterRuntime
}

func NewRebaseHeadFinalizationExecutor(runtime arbiterRuntime) RebaseHeadFinalizationExecutor {
	return &rebaseHeadFinalizationExecutor{runtime: runtime}
}

func (x *rebaseHeadFinalizationExecutor) Preflight(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) error {
	if x == nil || x.runtime == nil || !exactRebaseHeadFinalization(row) || row.Status != domain.DCPRebaseHeadFinalizationAuthorized ||
		row.Revision != rebaseHeadAuthorizedRevision || row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 || row.ProviderNewHead != "" {
		return errors.New("card-12 REBASE_HEAD finalization: pristine preflight identity is invalid")
	}
	return x.validate(ctx, row, row.OldHead)
}

func (x *rebaseHeadFinalizationExecutor) Execute(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) error {
	if x == nil || x.runtime == nil || !exactRebaseHeadFinalization(row) || row.Status != domain.DCPRebaseHeadFinalizationRunning ||
		row.ModelFreeActionCount != 1 || row.ReviewerModelCallCount != 0 || row.ProviderNewHead != "" {
		return errors.New("card-12 REBASE_HEAD finalization: fenced identity is invalid")
	}
	if err := x.validate(ctx, row, row.OldHead); err != nil {
		return err
	}
	lease := "--force-with-lease=" + row.PushRef + ":" + row.OldHead
	if _, err := x.git(ctx, row.WorktreePath, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential",
		"push", lease, "origin", "HEAD:"+row.PushRef); err != nil {
		return fmt.Errorf("card-12 REBASE_HEAD finalization: one guarded push failed: %w", err)
	}
	return x.validate(ctx, row, row.CandidateHead)
}

func (x *rebaseHeadFinalizationExecutor) InspectCompleted(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) error {
	if x == nil || x.runtime == nil || !exactRebaseHeadFinalization(row) || row.Status != domain.DCPRebaseHeadFinalizationRunning ||
		row.ModelFreeActionCount != 1 || row.ReviewerModelCallCount != 0 || row.ProviderNewHead != "" {
		return errors.New("card-12 REBASE_HEAD finalization: running identity is invalid")
	}
	return x.validate(ctx, row, row.CandidateHead)
}

func (x *rebaseHeadFinalizationExecutor) validate(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, remoteHead string) error {
	cold := &coldStartRecoveryExecutor{runtime: x.runtime}
	if err := cold.requireQuiescence(ctx); err != nil {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: runtime is not quiescent"))
	}
	if err := cold.requireTools(); err != nil {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: trusted tools unavailable"))
	}
	if err := x.validateBackup(row); err != nil {
		return err
	}
	gitDir, commonDir, err := x.validateLocalCandidate(ctx, row)
	if err != nil {
		return err
	}
	if err := x.validatePseudorefs(row, gitDir, commonDir); err != nil {
		return err
	}
	return x.requireRemoteRefs(ctx, row, remoteHead)
}

func (x *rebaseHeadFinalizationExecutor) validateBackup(row domain.DCPCard12RebaseHeadFinalization) error {
	manifest, err := readRegular(filepath.Join(row.BackupPath, "manifest.txt"))
	if err != nil || digestBytes(manifest) != row.BackupDigest {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: sealed backup digest drifted"))
	}
	if err := verifySealedBackup(row.BackupPath, manifest); err != nil {
		return errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: sealed backup drifted"))
	}
	return nil
}

func (x *rebaseHeadFinalizationExecutor) validateLocalCandidate(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization) (string, string, error) {
	repo := row.WorktreePath
	if !filepath.IsAbs(repo) || filepath.Clean(repo) != repo {
		return "", "", errors.New("card-12 REBASE_HEAD finalization: worktree path invalid")
	}
	labRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(repo))))
	commonDir := filepath.Join(labRoot, "targets", ProjectID, ".git")
	gitDir := filepath.Join(commonDir, "worktrees", "dcp-review-lab-12")
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, repo},
		{[]string{"rev-parse", "--absolute-git-dir"}, gitDir},
		{[]string{"rev-parse", "--path-format=absolute", "--git-common-dir"}, commonDir},
		{[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, RepositoryURL},
		{[]string{"remote", "get-url", "--push", "origin"}, RepositoryURL},
		{[]string{"config", "--get", "branch." + row.SourceBranch + ".remote"}, "origin"},
		{[]string{"config", "--get", "branch." + row.SourceBranch + ".merge"}, row.PushRef},
		{[]string{"branch", "--show-current"}, row.SourceBranch},
		{[]string{"symbolic-ref", "HEAD"}, row.PushRef},
		{[]string{"rev-parse", "HEAD"}, row.CandidateHead},
		{[]string{"rev-parse", row.PushRef}, row.CandidateHead},
		{[]string{"show", "-s", "--format=%P", row.CandidateHead}, row.CurrentMain},
		{[]string{"show", "-s", "--format=%P", row.OldHead}, row.ProviderBase},
		{[]string{"merge-base", row.OldHead, row.CurrentMain}, row.ProviderBase},
		{[]string{"show", "-s", "--format=%s", row.CandidateHead}, "chore: add i13 arbiter intent B canary"},
		{[]string{"show", "-s", "--format=%an%x00%ae%x00%aI", row.CandidateHead}, "Влад Сагитов\x00ovlmacbook@oVl-MacBook-Pro.local\x002026-08-11T22:38:48+05:00"},
		{[]string{"diff", "--name-status", row.CurrentMain + ".." + row.CandidateHead}, "M\t" + row.ConflictPath},
		{[]string{"rev-parse", row.CandidateHead + ":" + row.ConflictPath}, row.ResolvedBlob},
	}
	for _, check := range checks {
		got, err := x.gitText(ctx, repo, nil, check.args...)
		if err != nil || got != check.want {
			return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: retained candidate identity drifted"))
		}
	}
	worktrees, err := x.gitText(ctx, repo, nil, "worktree", "list", "--porcelain")
	wantWorktree := "worktree " + repo + "\nHEAD " + row.CandidateHead + "\nbranch " + row.PushRef
	if err != nil || strings.Count(worktrees, "branch "+row.PushRef) != 1 || !strings.Contains(worktrees, wantWorktree) {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: worktree/branch ownership drifted"))
	}
	status, err := x.git(ctx, repo, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || digestBytes(status) != row.CleanStatusDigest || len(status) != 0 {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: index/worktree is not exact clean state"))
	}
	if _, err := x.git(ctx, repo, nil, "diff-files", "--quiet"); err != nil {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: worktree differs from index"))
	}
	if _, err := x.git(ctx, repo, nil, "diff-index", "--quiet", "HEAD", "--"); err != nil {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: index differs from HEAD"))
	}
	file := filepath.Join(repo, filepath.FromSlash(row.ConflictPath))
	data, err := readRegular(file)
	if err != nil || !bytes.Equal(data, []byte(freshWorkerExpectedBytes)) || digestBytes(data) != row.ResolvedBytesDigest {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: retained bytes drifted"))
	}
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: retained file mode drifted"))
	}
	diff, err := x.git(ctx, repo, nil, "diff", "--binary", "--full-index", row.CurrentMain+".."+row.CandidateHead, "--")
	if err != nil || digestBytes(diff) != row.CandidateDiffDigest {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: retained diff drifted"))
	}
	if _, err := x.git(ctx, repo, nil, "merge-base", "--is-ancestor", row.ProviderBase, row.CurrentMain); err != nil {
		return "", "", errors.Join(err, errors.New("card-12 REBASE_HEAD finalization: provider-base ancestry drifted"))
	}
	return gitDir, commonDir, nil
}

func (x *rebaseHeadFinalizationExecutor) validatePseudorefs(row domain.DCPCard12RebaseHeadFinalization, gitDir, commonDir string) error {
	want := []byte(row.OldHead + "\n")
	for name, digest := range map[string]string{"REBASE_HEAD": row.RebaseHeadDigest, "ORIG_HEAD": row.OrigHeadDigest} {
		path := filepath.Join(gitDir, name)
		data, err := readRegular(path)
		if err != nil || !bytes.Equal(data, want) || digestBytes(data) != digest {
			return errors.Join(err, fmt.Errorf("card-12 REBASE_HEAD finalization: exact %s bytes drifted", name))
		}
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 || info.Size() != 41 {
			return errors.Join(err, fmt.Errorf("card-12 REBASE_HEAD finalization: exact %s mode drifted", name))
		}
	}
	for _, residue := range []string{"AUTO_MERGE", "MERGE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "BISECT_START", "sequencer", "index.lock", "HEAD.lock", "ORIG_HEAD.lock", "REBASE_HEAD.lock"} {
		if _, err := os.Lstat(filepath.Join(gitDir, residue)); err == nil {
			return fmt.Errorf("card-12 REBASE_HEAD finalization: foreign Git residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, residue := range []string{"packed-refs.lock", "shallow.lock", filepath.Join("refs", "heads", "ao", "dcp-review-lab-12", "root.lock")} {
		if _, err := os.Lstat(filepath.Join(commonDir, residue)); err == nil {
			return fmt.Errorf("card-12 REBASE_HEAD finalization: foreign common Git residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (x *rebaseHeadFinalizationExecutor) requireRemoteRefs(ctx context.Context, row domain.DCPCard12RebaseHeadFinalization, branchHead string) error {
	out, err := x.gitText(ctx, row.WorktreePath, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential", "ls-remote", "--heads", "origin", "refs/heads/main", row.PushRef)
	if err != nil {
		return fmt.Errorf("card-12 REBASE_HEAD finalization: authenticated remote read failed: %w", err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !validSHA(fields[0]) {
			return errors.New("card-12 REBASE_HEAD finalization: malformed remote refs")
		}
		refs[fields[1]] = strings.ToLower(fields[0])
	}
	if len(refs) != 2 || refs["refs/heads/main"] != row.CurrentMain || refs[row.PushRef] != strings.ToLower(branchHead) {
		return errors.New("card-12 REBASE_HEAD finalization: remote refs drifted")
	}
	return nil
}

func (x *rebaseHeadFinalizationExecutor) git(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	cold := &coldStartRecoveryExecutor{}
	return cold.git(ctx, repo, env, args...)
}

func (x *rebaseHeadFinalizationExecutor) gitText(ctx context.Context, repo string, env []string, args ...string) (string, error) {
	out, err := x.git(ctx, repo, env, args...)
	return strings.TrimSpace(string(out)), err
}
