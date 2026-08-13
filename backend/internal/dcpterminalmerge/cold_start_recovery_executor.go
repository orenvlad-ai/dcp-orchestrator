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

type coldStartRecoveryExecutor struct {
	runtime arbiterRuntime
	dataDir string
}

func NewColdStartRecoveryExecutor(runtime arbiterRuntime, dataDir string) ColdStartRecoveryExecutor {
	return &coldStartRecoveryExecutor{runtime: runtime, dataDir: filepath.Clean(dataDir)}
}

func (x *coldStartRecoveryExecutor) PrepareBackup(ctx context.Context, row domain.DCPCard12ColdStartRecovery) (string, string, error) {
	if x == nil || x.runtime == nil || !exactColdStartRecovery(row) || row.Status != domain.DCPColdStartRecoveryAuthorized ||
		(row.Revision != 0 && row.Revision != 2) || row.ModelFreeActionCount != 0 || row.ReviewerModelCallCount != 0 || row.BackupPath != "" || row.BackupDigest != "" {
		return "", "", errors.New("card-12 cold-start recovery: backup identity is invalid")
	}
	if err := x.requireQuiescence(ctx); err != nil {
		return "", "", err
	}
	if err := x.requireTools(); err != nil {
		return "", "", err
	}
	gitDir, commonDir, err := x.validateAttachedConflict(ctx, row)
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(filepath.Dir(x.dataDir), "evidence", "dcp-card12-cold-start-recovery", row.RecoveryID)
	if filepath.Clean(root) != root || !strings.HasPrefix(root, filepath.Join(filepath.Dir(x.dataDir), "evidence")+string(os.PathSeparator)) {
		return "", "", errors.New("card-12 cold-start recovery: backup root is foreign")
	}
	files, err := x.captureBackupFiles(ctx, row, gitDir, commonDir)
	if err != nil {
		return "", "", err
	}
	manifest := backupManifest(files)
	digest := digestBytes(manifest)
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("card-12 cold-start recovery: existing backup root is foreign")
		}
		if err := verifyBackup(root, files, manifest); err != nil {
			return "", "", err
		}
		return root, digest, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", "", err
	}
	temp, err := os.MkdirTemp(parent, ".cold-start-backup-")
	if err != nil {
		return "", "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(temp)
		}
	}()
	for relative, data := range files {
		target := filepath.Join(temp, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return "", "", err
		}
	}
	if err := os.WriteFile(filepath.Join(temp, "manifest.txt"), manifest, 0o600); err != nil {
		return "", "", err
	}
	if err := verifyBackup(temp, files, manifest); err != nil {
		return "", "", err
	}
	if err := os.Rename(temp, root); err != nil {
		return "", "", err
	}
	keep = true
	return root, digest, nil
}

func (x *coldStartRecoveryExecutor) Execute(ctx context.Context, row domain.DCPCard12ColdStartRecovery) (string, error) {
	if x == nil || x.runtime == nil || !exactColdStartRecovery(row) || row.Status != domain.DCPColdStartRecoveryRunning ||
		row.ModelFreeActionCount != 1 || row.ReviewerModelCallCount != 0 || row.BackupPath == "" || len(row.BackupDigest) != 64 {
		return "", errors.New("card-12 cold-start recovery: fenced executor identity is invalid")
	}
	if err := x.requireQuiescence(ctx); err != nil {
		return "", err
	}
	if err := x.requireTools(); err != nil {
		return "", err
	}
	gitDir, commonDir, err := x.validateAttachedConflict(ctx, row)
	if err != nil {
		return "", err
	}
	files, err := x.captureBackupFiles(ctx, row, gitDir, commonDir)
	if err != nil {
		return "", err
	}
	manifest := backupManifest(files)
	if digestBytes(manifest) != row.BackupDigest {
		return "", errors.New("card-12 cold-start recovery: immutable backup digest drifted")
	}
	if err := verifySealedBackup(row.BackupPath, manifest); err != nil {
		return "", err
	}
	if err := verifyBackup(row.BackupPath, files, manifest); err != nil {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: immutable backup digest drifted"))
	}
	repo := row.WorktreePath
	if _, err := x.git(ctx, repo, nil, "reset", "--hard", row.OldHead); err != nil {
		return "", fmt.Errorf("card-12 cold-start recovery: restore clean old-head basis: %w", err)
	}
	if err := x.validateCleanOldHead(ctx, row); err != nil {
		return "", err
	}
	if out, err := x.git(ctx, repo, nil, "-c", "core.hooksPath=/dev/null", "rebase", row.CurrentMain); err == nil {
		return "", errors.New("card-12 cold-start recovery: exact one-commit rebase unexpectedly avoided its conflict")
	} else if !bytes.Contains(out, []byte("CONFLICT")) {
		return "", errors.New("card-12 cold-start recovery: rebase failed outside the expected conflict")
	}
	if err := x.validateExpectedRebaseConflict(ctx, row); err != nil {
		return "", err
	}
	path := filepath.Join(repo, filepath.FromSlash(row.ConflictPath))
	if err := os.WriteFile(path, []byte(freshWorkerExpectedBytes), 0o644); err != nil {
		return "", err
	}
	if err := requireRegularFile(path, []byte(freshWorkerExpectedBytes), 0o644); err != nil {
		return "", err
	}
	if _, err := x.git(ctx, repo, nil, "--literal-pathspecs", "add", "--", row.ConflictPath); err != nil {
		return "", err
	}
	stage, err := x.gitText(ctx, repo, nil, "ls-files", "--stage", "--", row.ConflictPath)
	if err != nil || stage != "100644 "+row.ResolvedBlob+" 0\t"+row.ConflictPath {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: staged blob is not exact"))
	}
	status, err := x.gitText(ctx, repo, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "M  "+row.ConflictPath {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: staged scope drifted"))
	}
	env := []string{"GIT_EDITOR=:", "GIT_COMMITTER_NAME=Влад Сагитов", "GIT_COMMITTER_EMAIL=ovlmacbook@oVl-MacBook-Pro.local", "GIT_COMMITTER_DATE=2026-08-11T22:38:48+05:00"}
	if _, err := x.git(ctx, repo, env, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false", "rebase", "--continue"); err != nil {
		return "", fmt.Errorf("card-12 cold-start recovery: one rebase continuation failed: %w", err)
	}
	newHead, err := x.gitText(ctx, repo, nil, "rev-parse", "HEAD")
	if err != nil || !validSHA(newHead) || strings.EqualFold(newHead, row.OldHead) {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: new head is invalid"))
	}
	if err := x.validateCandidate(ctx, row, newHead); err != nil {
		return newHead, err
	}
	if err := x.requireRemoteRefs(ctx, row, row.OldHead); err != nil {
		return newHead, err
	}
	lease := "--force-with-lease=" + row.PushRef + ":" + row.OldHead
	if _, err := x.git(ctx, repo, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential",
		"push", lease, "origin", "HEAD:"+row.PushRef); err != nil {
		return newHead, fmt.Errorf("card-12 cold-start recovery: one guarded push failed: %w", err)
	}
	if err := x.requireRemoteRefs(ctx, row, newHead); err != nil {
		return newHead, err
	}
	return strings.ToLower(newHead), x.validateCandidate(ctx, row, newHead)
}

func (x *coldStartRecoveryExecutor) captureBackupFiles(ctx context.Context, row domain.DCPCard12ColdStartRecovery, gitDir, commonDir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for relative, source := range map[string]string{
		"worktree/conflict.txt": filepath.Join(row.WorktreePath, filepath.FromSlash(row.ConflictPath)),
		"git/HEAD":              filepath.Join(gitDir, "HEAD"),
		"git/index":             filepath.Join(gitDir, "index"),
		"git/branch-ref":        filepath.Join(commonDir, filepath.FromSlash(row.PushRef)),
	} {
		data, err := readRegular(source)
		if err != nil {
			return nil, fmt.Errorf("card-12 cold-start recovery: backup source %s: %w", relative, err)
		}
		files[relative] = data
	}
	status, err := x.git(ctx, row.WorktreePath, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	unmerged, err := x.git(ctx, row.WorktreePath, nil, "ls-files", "--unmerged", "--", row.ConflictPath)
	if err != nil {
		return nil, err
	}
	worktrees, err := x.git(ctx, row.WorktreePath, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	files["audit/status.z"] = status
	files["audit/unmerged.txt"] = unmerged
	files["audit/worktrees.txt"] = worktrees
	return files, nil
}

func (x *coldStartRecoveryExecutor) InspectCompleted(ctx context.Context, row domain.DCPCard12ColdStartRecovery) (string, error) {
	if x == nil || !exactColdStartRecovery(row) || row.Status != domain.DCPColdStartRecoveryRunning || row.ModelFreeActionCount != 1 ||
		row.ReviewerModelCallCount != 0 || row.BackupPath == "" || len(row.BackupDigest) != 64 {
		return "", errors.New("card-12 cold-start recovery: running inspection identity invalid")
	}
	manifest, err := readRegular(filepath.Join(row.BackupPath, "manifest.txt"))
	if err != nil || digestBytes(manifest) != row.BackupDigest {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: backup drifted"))
	}
	if err := verifySealedBackup(row.BackupPath, manifest); err != nil {
		return "", err
	}
	head, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", row.PushRef)
	if err != nil || !validSHA(head) || strings.EqualFold(head, row.OldHead) {
		return "", errors.Join(err, errors.New("card-12 cold-start recovery: completed ref unavailable"))
	}
	if err := x.requireRemoteRefs(ctx, row, head); err != nil {
		return "", err
	}
	return strings.ToLower(head), x.validateCandidate(ctx, row, head)
}

func (x *coldStartRecoveryExecutor) requireQuiescence(ctx context.Context) error {
	inspector, ok := x.runtime.(ports.RuntimeQuiescenceInspector)
	if !ok {
		return errors.New("card-12 cold-start recovery: runtime quiescence inspection unavailable")
	}
	for _, id := range []string{"dcp-review-lab-7", "dcp-review-lab-9", "dcp-review-lab-10", "dcp-review-lab-11", "dcp-review-lab-12", "review-dcp-review-lab-7", "review-dcp-review-lab-9", "review-dcp-review-lab-10", "review-dcp-review-lab-11", "review-dcp-review-lab-12", ArbiterRuntimeHandle, ArbiterSuccessorRuntimeHandle, "dcp-card12-fresh-worker-recovery"} {
		quiet, err := inspector.IsRuntimeQuiescent(ctx, ports.RuntimeHandle{ID: id})
		if err != nil || !quiet {
			return errors.Join(err, fmt.Errorf("card-12 cold-start recovery: runtime %s is not quiescent", id))
		}
	}
	return nil
}

func (x *coldStartRecoveryExecutor) requireTools() error {
	if info, err := os.Lstat(modelFreeGitPath); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, errors.New("card-12 cold-start recovery: exact git unavailable"))
	}
	if err := requireRegularDigest(modelFreeGHPath, modelFreeGHDigest); err != nil {
		return fmt.Errorf("card-12 cold-start recovery: exact gh: %w", err)
	}
	return nil
}

func (x *coldStartRecoveryExecutor) validateAttachedConflict(ctx context.Context, row domain.DCPCard12ColdStartRecovery) (string, string, error) {
	repo := row.WorktreePath
	if !filepath.IsAbs(repo) || filepath.Clean(repo) != repo {
		return "", "", errors.New("card-12 cold-start recovery: worktree path invalid")
	}
	labRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(repo))))
	common := filepath.Join(labRoot, "targets", ProjectID, ".git")
	gitDir := filepath.Join(common, "worktrees", "dcp-review-lab-12")
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, repo}, {[]string{"rev-parse", "--absolute-git-dir"}, gitDir},
		{[]string{"rev-parse", "--path-format=absolute", "--git-common-dir"}, common}, {[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, RepositoryURL}, {[]string{"remote", "get-url", "--push", "origin"}, RepositoryURL},
		{[]string{"branch", "--show-current"}, row.SourceBranch}, {[]string{"symbolic-ref", "HEAD"}, row.PushRef},
		{[]string{"rev-parse", "HEAD"}, row.OldHead}, {[]string{"rev-parse", row.PushRef}, row.OldHead},
	}
	for _, check := range checks {
		got, err := x.gitText(ctx, repo, nil, check.args...)
		if err != nil || got != check.want {
			return "", "", errors.Join(err, errors.New("card-12 cold-start recovery: attached Git identity drifted"))
		}
	}
	status, err := x.git(ctx, repo, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || digestBytes(status) != row.StatusDigest || !bytes.Equal(status, []byte("UU "+row.ConflictPath+"\x00")) {
		return "", "", errors.Join(err, errors.New("card-12 cold-start recovery: exact UU status drifted"))
	}
	file := filepath.Join(repo, filepath.FromSlash(row.ConflictPath))
	data, err := readRegular(file)
	if err != nil || digestBytes(data) != row.MarkerDigest {
		return "", "", errors.Join(err, errors.New("card-12 cold-start recovery: marker bytes drifted"))
	}
	if info, err := os.Lstat(file); err != nil || info.Mode().Perm() != 0o644 {
		return "", "", errors.Join(err, errors.New("card-12 cold-start recovery: marker mode drifted"))
	}
	unmerged, err := x.gitText(ctx, repo, nil, "ls-files", "--unmerged", "--", row.ConflictPath)
	want := "100644 " + row.Stage1Blob + " 1\t" + row.ConflictPath + "\n100644 " + row.Stage2Blob + " 2\t" + row.ConflictPath + "\n100644 " + row.Stage3Blob + " 3\t" + row.ConflictPath
	if err != nil || unmerged != want {
		return "", "", errors.Join(err, errors.New("card-12 cold-start recovery: exact UU stages drifted"))
	}
	if err := x.validateCommitTopology(ctx, row); err != nil {
		return "", "", err
	}
	for _, residue := range []string{"AUTO_MERGE", "MERGE_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "sequencer", "index.lock", "packed-refs.lock", "shallow.lock"} {
		if _, err := os.Lstat(filepath.Join(gitDir, residue)); err == nil {
			return "", "", fmt.Errorf("card-12 cold-start recovery: foreign Git residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	if err := x.requireRemoteRefs(ctx, row, row.OldHead); err != nil {
		return "", "", err
	}
	return gitDir, common, nil
}

func (x *coldStartRecoveryExecutor) validateCommitTopology(ctx context.Context, row domain.DCPCard12ColdStartRecovery) error {
	parent, err := x.gitText(ctx, row.WorktreePath, nil, "show", "-s", "--format=%P", row.OldHead)
	if err != nil || parent != row.ProviderBase {
		return errors.Join(err, errors.New("card-12 cold-start recovery: candidate parent drifted"))
	}
	if _, err := x.git(ctx, row.WorktreePath, nil, "merge-base", "--is-ancestor", row.ProviderBase, row.CurrentMain); err != nil {
		return errors.Join(err, errors.New("card-12 cold-start recovery: provider-base ancestry drifted"))
	}
	base, err := x.gitText(ctx, row.WorktreePath, nil, "merge-base", row.OldHead, row.CurrentMain)
	if err != nil || base != row.ProviderBase {
		return errors.Join(err, errors.New("card-12 cold-start recovery: merge base drifted"))
	}
	return nil
}

func (x *coldStartRecoveryExecutor) validateCleanOldHead(ctx context.Context, row domain.DCPCard12ColdStartRecovery) error {
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"branch", "--show-current"}, row.SourceBranch}, {[]string{"rev-parse", "HEAD"}, row.OldHead},
		{[]string{"status", "--porcelain=v1", "--untracked-files=all"}, ""},
		{[]string{"show", row.OldHead + ":" + row.ConflictPath}, "arbiter intent B"},
	}
	for _, check := range checks {
		got, err := x.gitText(ctx, row.WorktreePath, nil, check.args...)
		if err != nil || got != check.want {
			return errors.Join(err, errors.New("card-12 cold-start recovery: clean old-head basis drifted"))
		}
	}
	return x.validateCommitTopology(ctx, row)
}

func (x *coldStartRecoveryExecutor) validateExpectedRebaseConflict(ctx context.Context, row domain.DCPCard12ColdStartRecovery) error {
	if branch, _ := x.gitText(ctx, row.WorktreePath, nil, "branch", "--show-current"); branch != "" {
		return errors.New("card-12 cold-start recovery: rebase conflict is not detached")
	}
	head, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", "HEAD")
	if err != nil || head != row.CurrentMain {
		return errors.Join(err, errors.New("card-12 cold-start recovery: rebase onto drifted"))
	}
	status, err := x.git(ctx, row.WorktreePath, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || !bytes.Equal(status, []byte("AA "+row.ConflictPath+"\x00")) {
		return errors.Join(err, errors.New("card-12 cold-start recovery: reconstructed conflict scope drifted"))
	}
	unmerged, err := x.gitText(ctx, row.WorktreePath, nil, "ls-files", "--unmerged", "--", row.ConflictPath)
	want := "100644 " + row.Stage1Blob + " 2\t" + row.ConflictPath + "\n100644 " + row.Stage2Blob + " 3\t" + row.ConflictPath
	if err != nil || unmerged != want {
		return errors.Join(err, errors.New("card-12 cold-start recovery: reconstructed AA stages drifted"))
	}
	return nil
}

func (x *coldStartRecoveryExecutor) validateCandidate(ctx context.Context, row domain.DCPCard12ColdStartRecovery, head string) error {
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"branch", "--show-current"}, row.SourceBranch}, {[]string{"rev-parse", row.PushRef}, head},
		{[]string{"show", "-s", "--format=%P", head}, row.CurrentMain},
		{[]string{"show", "-s", "--format=%s", head}, "chore: add i13 arbiter intent B canary"},
		{[]string{"show", "-s", "--format=%an%x00%ae%x00%aI", head}, "Влад Сагитов\x00ovlmacbook@oVl-MacBook-Pro.local\x002026-08-11T22:38:48+05:00"},
		{[]string{"diff", "--name-status", row.CurrentMain + ".." + head}, "M\t" + row.ConflictPath},
		{[]string{"show", head + ":" + row.ConflictPath}, strings.TrimSuffix(freshWorkerExpectedBytes, "\n")},
		{[]string{"status", "--porcelain=v1", "--untracked-files=all"}, ""},
	}
	for _, check := range checks {
		got, err := x.gitText(ctx, row.WorktreePath, nil, check.args...)
		if err != nil || got != check.want {
			return errors.Join(err, errors.New("card-12 cold-start recovery: candidate postcondition drifted"))
		}
	}
	gitDir, err := x.gitText(ctx, row.WorktreePath, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	for _, residue := range []string{"AUTO_MERGE", "MERGE_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "sequencer", "index.lock"} {
		if _, err := os.Lstat(filepath.Join(gitDir, residue)); err == nil {
			return fmt.Errorf("card-12 cold-start recovery: postcondition residue %s exists", residue)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (x *coldStartRecoveryExecutor) requireRemoteRefs(ctx context.Context, row domain.DCPCard12ColdStartRecovery, branchHead string) error {
	out, err := x.gitText(ctx, row.WorktreePath, []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never"},
		"-c", "credential.helper=!"+modelFreeGHPath+" auth git-credential", "ls-remote", "--heads", "origin", "refs/heads/main", row.PushRef)
	if err != nil {
		return fmt.Errorf("card-12 cold-start recovery: authenticated remote read failed: %w", err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !validSHA(fields[0]) {
			return errors.New("card-12 cold-start recovery: malformed remote refs")
		}
		refs[fields[1]] = strings.ToLower(fields[0])
	}
	if len(refs) != 2 || refs["refs/heads/main"] != row.CurrentMain || refs[row.PushRef] != strings.ToLower(branchHead) {
		return errors.New("card-12 cold-start recovery: remote refs drifted")
	}
	return nil
}

func (x *coldStartRecoveryExecutor) git(ctx context.Context, repo string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, modelFreeGitPath, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (x *coldStartRecoveryExecutor) gitText(ctx context.Context, repo string, env []string, args ...string) (string, error) {
	out, err := x.git(ctx, repo, env, args...)
	return strings.TrimSpace(string(out)), err
}

func backupManifest(files map[string][]byte) []byte {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, path := range paths {
		sum := sha256.Sum256(files[path])
		b.WriteString(path)
		b.WriteByte('\x00')
		b.WriteString("0600")
		b.WriteByte('\x00')
		b.WriteString(strconv.Itoa(len(files[path])))
		b.WriteByte('\x00')
		b.WriteString(hex.EncodeToString(sum[:]))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func verifyBackup(root string, files map[string][]byte, manifest []byte) error {
	expected := make(map[string]struct{}, len(files)+1)
	for relative := range files {
		expected[filepath.ToSlash(relative)] = struct{}{}
	}
	expected["manifest.txt"] = struct{}{}
	seen := make(map[string]struct{}, len(expected))
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, ok := expected[relative]; !ok || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("card-12 cold-start recovery: backup inventory drifted")
		}
		seen[relative] = struct{}{}
		return nil
	}); err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return errors.New("card-12 cold-start recovery: backup inventory incomplete")
	}
	for relative, want := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
			return errors.Join(err, errors.New("card-12 cold-start recovery: backup file identity drifted"))
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			return errors.Join(err, errors.New("card-12 cold-start recovery: backup bytes drifted"))
		}
	}
	gotManifest, err := readRegular(filepath.Join(root, "manifest.txt"))
	if err != nil || !bytes.Equal(gotManifest, manifest) {
		return errors.Join(err, errors.New("card-12 cold-start recovery: backup manifest drifted"))
	}
	return nil
}

func verifySealedBackup(root string, manifest []byte) error {
	expected := map[string]struct{}{"manifest.txt": {}}
	lines := bytes.Split(manifest, []byte{'\n'})
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		fields := bytes.Split(line, []byte{0})
		if len(fields) != 4 || string(fields[1]) != "0600" || len(fields[3]) != 64 {
			return errors.New("card-12 cold-start recovery: backup manifest is malformed")
		}
		relative := string(fields[0])
		if relative == "" || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(relative)) != relative || strings.HasPrefix(relative, "../") {
			return errors.New("card-12 cold-start recovery: backup manifest path is foreign")
		}
		size, err := strconv.ParseInt(string(fields[2]), 10, 64)
		if err != nil || size < 0 {
			return errors.New("card-12 cold-start recovery: backup manifest size is invalid")
		}
		data, err := readRegular(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || int64(len(data)) != size || digestBytes(data) != string(fields[3]) {
			return errors.Join(err, errors.New("card-12 cold-start recovery: sealed backup bytes drifted"))
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || info.Mode().Perm() != 0o600 {
			return errors.Join(err, errors.New("card-12 cold-start recovery: sealed backup mode drifted"))
		}
		if _, exists := expected[relative]; exists {
			return errors.New("card-12 cold-start recovery: backup manifest repeats a path")
		}
		expected[relative] = struct{}{}
	}
	seen := make(map[string]struct{}, len(expected))
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, ok := expected[relative]; !ok || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("card-12 cold-start recovery: sealed backup inventory drifted")
		}
		seen[relative] = struct{}{}
		return nil
	}); err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return errors.New("card-12 cold-start recovery: sealed backup inventory incomplete")
	}
	return nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.Join(err, errors.New("not a regular file"))
	}
	return os.ReadFile(path)
}
