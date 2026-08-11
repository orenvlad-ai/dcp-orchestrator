// Package dcpterminalmerge owns the one bounded terminal merge authorized for
// the synthetic DCP review lab. It is deliberately not a general auto-merge
// policy: every project, repository, session, worktree, branch, PR, head,
// structured verdict and provider readiness fact is exact and fail-closed.
package dcpterminalmerge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ProjectID          = "dcp-review-lab"
	SessionPrefix      = "dcp-pr-lab"
	ProfileAgentRules  = "DCP synthetic PR profile v1. Work only in this exact synthetic repository and the current AO branch. Do not create subagents, extra branches, worktrees, remotes, pull requests, or network services. Implement only the direct task, create one commit, push the current branch, open one ready pull request targeting main, and then stop. Do not merge; only the trusted DCP daemon may perform the terminal merge after exact-head review and checks."
	TaskDisplayPrefix  = "DCP Synthetic PR: "
	TaskPromptPrefix   = "DCP synthetic task "
	RepositoryFullName = "orenvlad-ai/dcp-review-lab"
	RepositoryURL      = "https://github.com/orenvlad-ai/dcp-review-lab.git"
	TargetBranch       = "main"
	RequiredCheckName  = "dcp-review-lab"
	structuredChannel  = "structured_dcp_v1"
)

type Store interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListAllSessions(context.Context) ([]domain.SessionRecord, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error)
	ClaimDCPReviewLabTerminalMerge(context.Context, domain.ReviewRun) (bool, error)
	CompleteDCPReviewLabTerminalMerge(context.Context, string, string) (bool, error)
	FailDCPReviewLabTerminalMerge(context.Context, string, string) (bool, error)
}

type SCM interface {
	FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error)
	FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error)
	MergePullRequest(context.Context, ports.SCMMergeRequest) (ports.SCMMergeResult, error)
}

type Engine struct {
	store   Store
	scm     SCM
	dataDir string
	mu      sync.Mutex
	locks   map[domain.SessionID]*sync.Mutex
	git     func(context.Context, string, ...string) (string, error)
}

func New(store Store, scm SCM, dataDir string) *Engine {
	return &Engine{
		store:   store,
		scm:     scm,
		dataDir: filepath.Clean(dataDir),
		locks:   map[domain.SessionID]*sync.Mutex{},
		git:     gitOutput,
	}
}

// ReconcileStartup closes an uncertain already-claimed action from fresh SCM
// facts and considers each still-unclaimed exact-profile session once. It never
// retries a failed or uncertain provider mutation.
func (e *Engine) ReconcileStartup(ctx context.Context) error {
	if e == nil || e.store == nil {
		return errors.New("dcp terminal merge: store is not configured")
	}
	sessions, err := e.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if eligibleSessionID(session.ID) {
			if err := e.Try(ctx, session.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Try performs at most one provider mutation for the exact approved head. A
// not-yet-ready session is a successful no-op and will be reconsidered only on
// a later lifecycle/SCM event or one startup reconciliation.
func (e *Engine) Try(ctx context.Context, sessionID domain.SessionID) error {
	if e == nil || e.store == nil || e.scm == nil || strings.TrimSpace(e.dataDir) == "" {
		return errors.New("dcp terminal merge: dependencies are not configured")
	}
	if !eligibleSessionID(sessionID) {
		return nil
	}
	unlock := e.lock(sessionID)
	defer unlock()

	candidate, ok, err := e.candidate(ctx, sessionID)
	if err != nil || !ok {
		return err
	}
	if candidate.run.TerminalMergeStatus == "succeeded" || candidate.run.TerminalMergeStatus == "failed" {
		return nil
	}

	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return err
	}
	if candidate.run.TerminalMergeStatus == "running" {
		if observation.PR.Merged && strings.EqualFold(observation.PR.HeadSHA, candidate.run.TargetSHA) && validSHA(observation.PR.MergeCommitSHA) {
			updated, updateErr := e.store.CompleteDCPReviewLabTerminalMerge(ctx, candidate.run.ID, strings.ToLower(observation.PR.MergeCommitSHA))
			if updateErr != nil {
				return updateErr
			}
			if !updated {
				return errors.New("dcp terminal merge: running action could not be reconciled")
			}
			return nil
		}
		_, failErr := e.store.FailDCPReviewLabTerminalMerge(ctx, candidate.run.ID, "uncertain_restart")
		return failErr
	}
	if !ready(candidate, observation, review) {
		return nil
	}
	if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA); err != nil {
		return err
	}
	claimed, err := e.store.ClaimDCPReviewLabTerminalMerge(ctx, candidate.run)
	if err != nil || !claimed {
		return err
	}
	result, mergeErr := e.scm.MergePullRequest(ctx, ports.SCMMergeRequest{
		PR: ports.SCMPRRef{
			Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName},
			Number: candidate.pr.Number,
			URL:    candidate.pr.URL,
		},
		ExpectedHeadSHA: candidate.run.TargetSHA,
		Method:          ports.SCMMergeSquash,
	})
	if mergeErr != nil {
		_, failErr := e.store.FailDCPReviewLabTerminalMerge(ctx, candidate.run.ID, mergeErrorCode(mergeErr))
		if failErr != nil {
			return errors.Join(mergeErr, failErr)
		}
		return mergeErr
	}
	if !validSHA(result.MergeCommitSHA) {
		_, failErr := e.store.FailDCPReviewLabTerminalMerge(ctx, candidate.run.ID, "invalid_merge_result")
		if failErr != nil {
			return failErr
		}
		return errors.New("dcp terminal merge: provider returned an invalid merge commit")
	}
	updated, err := e.store.CompleteDCPReviewLabTerminalMerge(ctx, candidate.run.ID, strings.ToLower(result.MergeCommitSHA))
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("dcp terminal merge: completed provider mutation could not be recorded")
	}
	return nil
}

type mergeCandidate struct {
	session domain.SessionRecord
	project domain.ProjectRecord
	pr      domain.PullRequest
	run     domain.ReviewRun
}

func (e *Engine) candidate(ctx context.Context, id domain.SessionID) (mergeCandidate, bool, error) {
	session, ok, err := e.store.GetSession(ctx, id)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	if session.ProjectID != domain.ProjectID(ProjectID) || session.Kind != domain.KindWorker || session.Harness != domain.HarnessCodex ||
		session.ReviewerHarness != "" || session.IssueID != "" || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.TerminateOnPRMerge || session.Metadata.RuntimeLaunchID != "" || session.Metadata.DiffBaseRef != "origin/main" ||
		!validSHA(session.Metadata.DiffBaseSHA) || !validTaskIdentity(session) {
		return mergeCandidate{}, false, nil
	}
	expectedWorkspace := filepath.Join(e.dataDir, "worktrees", ProjectID, string(id))
	expectedBranch := "ao/" + string(id) + "/root"
	if !sameExactPath(session.Metadata.WorkspacePath, expectedWorkspace) || session.Metadata.Branch != expectedBranch {
		return mergeCandidate{}, false, nil
	}
	project, ok, err := e.store.GetProject(ctx, ProjectID)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	expectedProjectPath := filepath.Join(filepath.Dir(e.dataDir), "targets", ProjectID)
	if !sameExactPath(project.Path, expectedProjectPath) || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || project.RepoOriginURL != RepositoryURL ||
		project.Config.DefaultBranch != TargetBranch || project.Config.SessionPrefix != SessionPrefix ||
		project.Config.AgentRules != ProfileAgentRules || project.Config.AgentRulesFile != "" || project.Config.OrchestratorRules != "" ||
		!project.Config.AgentConfig.IsZero() || project.Config.Worker != (domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits}}) ||
		project.Config.Orchestrator != (domain.RoleOverride{}) || project.Config.TrackerIntake != (domain.TrackerIntakeConfig{}) ||
		project.Config.ContainerReap != (domain.ContainerReapConfig{}) ||
		len(project.Config.Reviewers) != 1 || project.Config.Reviewers[0].Harness != domain.ReviewerCodex ||
		len(project.Config.Env) != 0 || len(project.Config.Symlinks) != 0 || len(project.Config.PostCreate) != 0 {
		return mergeCandidate{}, false, nil
	}
	prs, err := e.store.ListPRsBySession(ctx, id)
	if err != nil || len(prs) != 1 {
		return mergeCandidate{}, false, err
	}
	pr := prs[0]
	if pr.Provider != "github" || pr.Host != "github.com" || pr.Repo != RepositoryFullName || pr.TargetBranch != TargetBranch ||
		pr.SourceBranch != expectedBranch || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		!validPRURL(pr.URL, pr.Number) || !validSHA(pr.HeadSHA) ||
		!strings.EqualFold(pr.BaseSHA, session.Metadata.DiffBaseSHA) {
		return mergeCandidate{}, false, nil
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, id)
	if err != nil {
		return mergeCandidate{}, false, err
	}
	var exact []domain.ReviewRun
	for _, run := range runs {
		if run.PRURL == pr.URL && strings.EqualFold(run.TargetSHA, pr.HeadSHA) {
			exact = append(exact, run)
		}
	}
	if len(exact) != 1 {
		return mergeCandidate{}, false, nil
	}
	run := exact[0]
	if run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved || run.ResultChannel != structuredChannel ||
		run.Harness != domain.ReviewerCodex || run.ID == "" || run.ReviewID == "" || run.BatchID == "" || run.Body == "" || run.GithubReviewID != "" {
		return mergeCandidate{}, false, nil
	}
	switch run.TerminalMergeStatus {
	case "":
		if pr.Draft || pr.Merged || pr.Closed || pr.ProviderState != "OPEN" {
			return mergeCandidate{}, false, nil
		}
	case "running":
		if pr.ProviderState != "OPEN" && pr.ProviderState != "MERGED" && pr.ProviderState != "CLOSED" {
			return mergeCandidate{}, false, nil
		}
	case "succeeded", "failed":
	default:
		return mergeCandidate{}, false, nil
	}
	return mergeCandidate{session: session, project: project, pr: pr, run: run}, true, nil
}

func (e *Engine) fresh(ctx context.Context, pr domain.PullRequest) (ports.SCMObservation, ports.SCMReviewObservation, error) {
	ref := ports.SCMPRRef{
		Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "dcp-review-lab", Repo: RepositoryFullName},
		Number: pr.Number,
		URL:    pr.URL,
	}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	if len(observations) != 1 || !observations[0].Fetched {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, errors.New("dcp terminal merge: exact PR could not be refreshed")
	}
	review, err := e.scm.FetchReviewThreads(ctx, ref)
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	return observations[0], review, nil
}

func ready(candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation) bool {
	pr := observation.PR
	if observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != RepositoryFullName ||
		pr.Number != candidate.pr.Number || pr.URL != candidate.pr.URL || pr.HeadRepo != RepositoryFullName ||
		pr.SourceBranch != candidate.pr.SourceBranch || pr.TargetBranch != TargetBranch ||
		!strings.EqualFold(pr.HeadSHA, candidate.run.TargetSHA) || !strings.EqualFold(pr.BaseSHA, candidate.session.Metadata.DiffBaseSHA) ||
		pr.State != string(domain.PRStateOpen) || pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		pr.Draft || pr.Merged || pr.Closed ||
		pr.ProviderMergeable != "MERGEABLE" || pr.ProviderMergeStateStatus != "CLEAN" ||
		observation.Mergeability.State != string(domain.MergeMergeable) || !observation.Mergeability.Mergeable || len(observation.Mergeability.Blockers) != 0 || review.Partial {
		return false
	}
	if (review.Decision != "" && review.Decision != string(domain.ReviewApproved)) || hasBlockingReview(review) {
		return false
	}
	if len(observation.CI.Checks) == 0 || observation.CI.Summary != string(domain.CIPassing) || !strings.EqualFold(observation.CI.HeadSHA, candidate.run.TargetSHA) {
		return false
	}
	required := 0
	for _, check := range observation.CI.Checks {
		if check.Status != string(domain.PRCheckPassed) && check.Status != string(domain.PRCheckSkipped) {
			return false
		}
		if check.Name == RequiredCheckName {
			if check.Status != string(domain.PRCheckPassed) || check.Conclusion != "success" {
				return false
			}
			required++
		}
	}
	return required == 1
}

func hasBlockingReview(review ports.SCMReviewObservation) bool {
	for _, thread := range review.Threads {
		if !thread.Resolved {
			return true
		}
	}
	return false
}

func (e *Engine) validateGit(ctx context.Context, candidate mergeCandidate, head string) error {
	projectPath := candidate.project.Path
	workspacePath := candidate.session.Metadata.WorkspacePath
	base := strings.ToLower(candidate.session.Metadata.DiffBaseSHA)
	checks := []struct {
		path string
		args []string
		want string
	}{
		{projectPath, []string{"rev-parse", "--show-toplevel"}, projectPath},
		{projectPath, []string{"branch", "--show-current"}, TargetBranch},
		{projectPath, []string{"remote"}, "origin"},
		{projectPath, []string{"remote", "get-url", "origin"}, RepositoryURL},
		{projectPath, []string{"rev-parse", "origin/main"}, base},
		{projectPath, []string{"rev-parse", "HEAD"}, base},
		{projectPath, []string{"status", "--porcelain"}, ""},
		{workspacePath, []string{"rev-parse", "--show-toplevel"}, workspacePath},
		{workspacePath, []string{"branch", "--show-current"}, candidate.session.Metadata.Branch},
		{workspacePath, []string{"remote"}, "origin"},
		{workspacePath, []string{"remote", "get-url", "origin"}, RepositoryURL},
		{workspacePath, []string{"rev-parse", "HEAD"}, strings.ToLower(head)},
		{workspacePath, []string{"status", "--porcelain"}, ""},
	}
	for _, check := range checks {
		got, err := e.git(ctx, check.path, check.args...)
		if err != nil || got != check.want {
			return errors.New("dcp terminal merge: local repository identity is not exact and clean")
		}
	}
	common, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || !sameExactPath(common, filepath.Join(projectPath, ".git")) {
		return errors.New("dcp terminal merge: linked worktree common git directory is foreign")
	}
	private, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err != nil || !sameExactPath(private, filepath.Join(projectPath, ".git", "worktrees", string(candidate.session.ID))) {
		return errors.New("dcp terminal merge: linked worktree private git directory is foreign")
	}
	return nil
}

func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	argv := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", argv...).Output()
	return strings.TrimSpace(string(out)), err
}

func eligibleSessionID(id domain.SessionID) bool {
	value := string(id)
	prefix := SessionPrefix + "-"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	return err == nil && n > 0
}

func validPRURL(raw string, number int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/"+RepositoryFullName+"/pull/"+strconv.Itoa(number)
}

func validSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func sameExactPath(a, b string) bool {
	if a == "" || b == "" || !filepath.IsAbs(a) || !filepath.IsAbs(b) || filepath.Clean(a) != a || filepath.Clean(b) != b || a != b {
		return false
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func validTaskIdentity(session domain.SessionRecord) bool {
	if !strings.HasPrefix(session.DisplayName, TaskDisplayPrefix) {
		return false
	}
	taskID := strings.TrimPrefix(session.DisplayName, TaskDisplayPrefix)
	if !validTaskID(taskID) {
		return false
	}
	prefix := TaskPromptPrefix + taskID + ": "
	if !strings.HasPrefix(session.Metadata.Prompt, prefix) {
		return false
	}
	prompt := strings.TrimPrefix(session.Metadata.Prompt, prefix)
	if prompt == "" || len(prompt) > 512 || !utf8.ValidString(prompt) {
		return false
	}
	for _, r := range prompt {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validTaskID(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func mergeErrorCode(err error) string {
	switch {
	case errors.Is(err, ports.ErrSCMHeadChanged):
		return "head_changed"
	case errors.Is(err, ports.ErrSCMNotMergeable):
		return "not_mergeable"
	case errors.Is(err, ports.ErrSCMNotFound):
		return "not_found"
	default:
		return "provider_failed"
	}
}

func (e *Engine) lock(id domain.SessionID) func() {
	e.mu.Lock()
	mu := e.locks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		e.locks[id] = mu
	}
	e.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (e *Engine) String() string {
	return fmt.Sprintf("%s/%s", ProjectID, SessionPrefix)
}
