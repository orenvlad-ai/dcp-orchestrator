package dcpv2

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

const (
	TwinTarget               = "dcp-wbc-integration-lab"
	TwinRepository           = "orenvlad-ai/dcp-wbc-integration-lab"
	TwinRepositoryID   int64 = 1340359100
	TwinOwnerID        int64 = 237411244
	TwinBase                 = "main"
	TwinProfile              = "live-runtime"
	TwinTargetSpec           = "dcp-wbc-integration-lab/v2"
	TwinRequiredCheck        = "baseline"
	TwinEnvironment          = "dcp-wbc-integration-lab-selectel"
	TwinServiceName          = "dcp-wbc-integration-lab"
	TwinAdapterVersion       = "selectel-systemd/v1"
	TwinIssuerKind           = "dcp/v2"
	TwinIssuerActor          = "orenvlad-ai"
	TwinIssuerEvent          = "repository_dispatch"
	TwinDispatchEvent        = "dcp-admission-v2"
	TwinWorkflowID     int64 = 338377713
)

var errTwinTerminalProofUnavailable = errors.New("DCP v2 terminal proof artifact is unavailable")

type ghRunner func(context.Context, []byte, ...string) ([]byte, error)
type gitRunner func(context.Context, string, ...string) (string, error)
type gitEnvRunner func(context.Context, string, map[string]string, ...string) (string, error)

// TwinGitHubAdapter implements the exact Stage 5 repository, Release Train
// and deployment-observation seams. It can dispatch one immutable manifest;
// it deliberately has no merge, artifact-build, remote-host, install, start or probe
// method.
type TwinGitHubAdapter struct {
	gh     ghRunner
	git    gitRunner
	gitEnv gitEnvRunner
}

func NewTwinGitHubAdapter() *TwinGitHubAdapter {
	return &TwinGitHubAdapter{gh: runGH, git: runTwinGit, gitEnv: runTwinGitEnv}
}

func newTwinGitHubAdapterForTest(run ghRunner) *TwinGitHubAdapter {
	return &TwinGitHubAdapter{gh: run, git: runTwinGit, gitEnv: runTwinGitEnv}
}

func runGH(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := aoprocess.CommandContext(ctx, "gh", append([]string{"api"}, args...)...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("DCP v2 GitHub adapter: %w", err)
	}
	return out, nil
}

func runTwinGit(ctx context.Context, worktree string, args ...string) (string, error) {
	cmd := aoprocess.CommandContext(ctx, "git", append([]string{"-C", worktree}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("DCP v2 readmission Git: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runTwinGitEnv(ctx context.Context, worktree string, env map[string]string, args ...string) (string, error) {
	cmd := aoprocess.CommandContext(ctx, "git", append([]string{"-C", worktree}, args...)...)
	cmd.Env = os.Environ()
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+env[key])
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("DCP v2 readmission Git: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *TwinGitHubAdapter) gitWithEnv(ctx context.Context, worktree string, env map[string]string, args ...string) (string, error) {
	if a.gitEnv != nil {
		return a.gitEnv(ctx, worktree, env, args...)
	}
	return a.git(ctx, worktree, args...)
}

type twinPR struct {
	Number         int64  `json:"number"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	Merged         bool   `json:"merged"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	Base           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

type TwinRepositoryFacts struct {
	PRNumber              int64
	BaseSHA               string
	MainSHA               string
	HeadSHA               string
	HeadRef               string
	CheckRunID            int64
	CheckURL              string
	CheckPassed           bool
	ReviewThreadsObserved bool
	EvidenceHash          string
}

type TwinReviewFacts struct {
	ReviewID     int64
	ReviewDigest string
	Body         string
}

func (a *TwinGitHubAdapter) InspectStage6WorkerOutput(ctx context.Context, worktree, branch string) (Stage6WorkerOutput, error) {
	if a == nil || a.git == nil || a.gh == nil || !filepath.IsAbs(worktree) || filepath.Clean(worktree) != worktree || branch != stage6CanaryBranch {
		return Stage6WorkerOutput{}, errors.New("DCP v2 Stage 6 adoption worktree identity is incomplete")
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain"}, ""},
		{[]string{"branch", "--show-current"}, branch},
		{[]string{"rev-parse", "HEAD"}, stage6CanaryCommit},
		{[]string{"rev-parse", "HEAD^{tree}"}, stage6CanaryTree},
		{[]string{"diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"}, "docs/STAGE6_CANARY.md"},
		{[]string{"show", "HEAD:docs/STAGE6_CANARY.md"}, "Stage 6 DCP v2 canary."},
	}
	for _, check := range checks {
		got, err := a.git(ctx, worktree, check.args...)
		if err != nil || got != check.want {
			return Stage6WorkerOutput{}, errors.Join(err, errors.New("DCP v2 Stage 6 Worker output drifted"))
		}
	}
	if _, err := a.git(ctx, worktree, "merge-base", "--is-ancestor", stage6RecoveryBaseSHA, stage6CanaryCommit); err != nil {
		return Stage6WorkerOutput{}, errors.New("DCP v2 Stage 6 Worker base ancestry drifted")
	}
	remote, err := a.git(ctx, worktree, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil || remote != "" {
		return Stage6WorkerOutput{}, errors.Join(err, errors.New("DCP v2 Stage 6 Worker branch already has a remote effect"))
	}
	query := url.Values{"state": {"open"}, "base": {TwinBase}, "head": {"orenvlad-ai:" + branch}, "per_page": {"100"}}
	var pulls []twinPR
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/pulls?"+query.Encode(), &pulls); err != nil || len(pulls) != 0 {
		return Stage6WorkerOutput{}, errors.Join(err, errors.New("DCP v2 Stage 6 Worker output already has a PR effect"))
	}
	worktreeDigest := digestCanonical(map[string]string{"branch": branch, "worktree": filepath.Clean(worktree)})
	outputDigest := digestCanonical(map[string]string{"commit": stage6CanaryCommit, "tree": stage6CanaryTree,
		"path": "docs/STAGE6_CANARY.md", "content": "Stage 6 DCP v2 canary."})
	return Stage6WorkerOutput{CommitSHA: stage6CanaryCommit, TreeSHA: stage6CanaryTree, Branch: branch,
		WorktreePath: filepath.Clean(worktree), WorktreeDigest: worktreeDigest, OutputDigest: outputDigest,
		RemoteBranchAbsent: true, OpenPRCount: 0}, nil
}

func (a *TwinGitHubAdapter) ObserveMain(ctx context.Context) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/git/ref/heads/"+TwinBase, &ref); err != nil {
		return "", err
	}
	if !validV2SHA(ref.Object.SHA) {
		return "", errors.New("DCP v2 twin main identity is malformed")
	}
	return strings.ToLower(ref.Object.SHA), nil
}

func (a *TwinGitHubAdapter) ObserveBranch(ctx context.Context, branch string) (TwinRepositoryFacts, error) {
	if a == nil || a.gh == nil || branch == "" || branch == TwinBase {
		return TwinRepositoryFacts{}, errors.New("DCP v2 twin branch identity is incomplete")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/git/ref/heads/"+TwinBase, &ref); err != nil {
		return TwinRepositoryFacts{}, err
	}
	query := url.Values{"state": {"open"}, "base": {TwinBase}, "head": {"orenvlad-ai:" + branch}, "per_page": {"100"}}
	var pulls []twinPR
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/pulls?"+query.Encode(), &pulls); err != nil {
		return TwinRepositoryFacts{}, err
	}
	if len(pulls) != 1 {
		return TwinRepositoryFacts{}, fmt.Errorf("DCP v2 twin expected one open PR, got %d", len(pulls))
	}
	pr := pulls[0]
	if pr.Number < 1 || pr.State != "open" || pr.Draft || pr.Merged || pr.Base.Ref != TwinBase ||
		pr.Head.Ref != branch || pr.Head.Repo.FullName != TwinRepository || !validV2SHA(pr.Base.SHA) ||
		!validV2SHA(pr.Head.SHA) || !validV2SHA(ref.Object.SHA) {
		return TwinRepositoryFacts{}, errors.New("DCP v2 twin PR/provider identity drifted")
	}
	facts := TwinRepositoryFacts{PRNumber: pr.Number, BaseSHA: strings.ToLower(pr.Base.SHA),
		MainSHA: strings.ToLower(ref.Object.SHA), HeadSHA: strings.ToLower(pr.Head.SHA), HeadRef: branch}
	facts.EvidenceHash = digestCanonical(facts)
	return facts, nil
}

func (a *TwinGitHubAdapter) ObserveChecks(ctx context.Context, branch string) (TwinRepositoryFacts, error) {
	facts, err := a.ObserveBranch(ctx, branch)
	if err != nil {
		return TwinRepositoryFacts{}, err
	}
	var exactPR twinPR
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/pulls/"+strconv.FormatInt(facts.PRNumber, 10), &exactPR); err != nil {
		return TwinRepositoryFacts{}, err
	}
	if exactPR.Number != facts.PRNumber || exactPR.Mergeable == nil || !*exactPR.Mergeable || exactPR.MergeableState != "clean" ||
		exactPR.State != "open" || exactPR.Draft || exactPR.Merged || exactPR.Base.Ref != TwinBase ||
		!strings.EqualFold(exactPR.Base.SHA, facts.BaseSHA) || !strings.EqualFold(exactPR.Head.SHA, facts.HeadSHA) ||
		exactPR.Head.Ref != branch || exactPR.Head.Repo.FullName != TwinRepository {
		return TwinRepositoryFacts{}, errors.New("DCP v2 twin exact-head PR is not cleanly mergeable")
	}
	var response struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			DetailsURL string `json:"details_url"`
			HeadSHA    string `json:"head_sha"`
		} `json:"check_runs"`
	}
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/commits/"+facts.HeadSHA+"/check-runs", &response); err != nil {
		return TwinRepositoryFacts{}, err
	}
	var exact []struct {
		ID         int64
		Status     string
		Conclusion string
		DetailsURL string
		HeadSHA    string
	}
	for _, check := range response.CheckRuns {
		if check.Name == TwinRequiredCheck && strings.EqualFold(check.HeadSHA, facts.HeadSHA) {
			exact = append(exact, struct {
				ID         int64
				Status     string
				Conclusion string
				DetailsURL string
				HeadSHA    string
			}{check.ID, check.Status, check.Conclusion, check.DetailsURL, check.HeadSHA})
		}
	}
	if len(exact) != 1 || exact[0].ID < 1 || exact[0].Status != "completed" || exact[0].Conclusion != "success" ||
		!strings.HasPrefix(exact[0].DetailsURL, "https://github.com/"+TwinRepository+"/actions/runs/") {
		return TwinRepositoryFacts{}, errors.New("DCP v2 exact baseline check is not successful")
	}
	facts.CheckRunID, facts.CheckURL, facts.CheckPassed = exact[0].ID, exact[0].DetailsURL, true
	facts.EvidenceHash = digestCanonical(facts)
	return facts, nil
}

func (a *TwinGitHubAdapter) ObserveZeroUnresolvedReviewThreads(ctx context.Context, prNumber int64) error {
	if a == nil || a.gh == nil || prNumber < 1 {
		return errors.New("DCP v2 review-thread observation identity is incomplete")
	}
	query := `query($owner:String!,$repo:String!,$number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$number){reviewThreads(first:100){totalCount nodes{isResolved} pageInfo{hasNextPage}}}}}`
	payload, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{
		"owner": "orenvlad-ai", "repo": "dcp-wbc-integration-lab", "number": prNumber,
	}})
	if err != nil {
		return err
	}
	var response struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Repository *struct {
				PullRequest *struct {
					ReviewThreads struct {
						TotalCount int64 `json:"totalCount"`
						Nodes      []struct {
							IsResolved bool `json:"isResolved"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	body, err := a.gh(ctx, payload, "--method", "POST", "graphql", "--input", "-")
	if err != nil || json.Unmarshal(body, &response) != nil || len(response.Errors) != 0 || response.Data.Repository == nil ||
		response.Data.Repository.PullRequest == nil {
		return errors.Join(err, errors.New("DCP v2 review-thread observation is unavailable"))
	}
	threads := response.Data.Repository.PullRequest.ReviewThreads
	if threads.PageInfo.HasNextPage || threads.TotalCount != int64(len(threads.Nodes)) {
		return errors.New("DCP v2 review-thread observation is incomplete")
	}
	for _, thread := range threads.Nodes {
		if !thread.IsResolved {
			return errors.New("DCP v2 exact-head PR has an unresolved review thread")
		}
	}
	return nil
}

func (a *TwinGitHubAdapter) PublishExactReview(ctx context.Context, facts TwinRepositoryFacts, run domain.ReviewRun) (TwinReviewFacts, error) {
	if run.ID == "" || run.TargetSHA != facts.HeadSHA || run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved {
		return TwinReviewFacts{}, errors.New("DCP v2 local review is not an approved exact-head result")
	}
	body := "DCP v2 context-free semantic/security review " + run.ID + " for exact head " + facts.HeadSHA + ": no findings."
	var reviews []struct {
		ID       int64  `json:"id"`
		Body     string `json:"body"`
		CommitID string `json:"commit_id"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	path := "repos/" + TwinRepository + "/pulls/" + strconv.FormatInt(facts.PRNumber, 10) + "/reviews"
	if err := a.getJSON(ctx, path+"?per_page=100", &reviews); err != nil {
		return TwinReviewFacts{}, err
	}
	var matched []int64
	for _, review := range reviews {
		if review.Body == body && strings.EqualFold(review.CommitID, facts.HeadSHA) && review.User.Login == TwinIssuerActor {
			matched = append(matched, review.ID)
		}
	}
	if len(matched) > 1 {
		return TwinReviewFacts{}, errors.New("DCP v2 exact review cardinality drifted")
	}
	if len(matched) == 0 {
		payload, _ := json.Marshal(map[string]string{"body": body, "commit_id": facts.HeadSHA, "event": "COMMENT"})
		var created struct {
			ID int64 `json:"id"`
		}
		if err := a.mutateJSON(ctx, payload, path, &created); err != nil {
			return TwinReviewFacts{}, err
		}
		if created.ID < 1 {
			return TwinReviewFacts{}, errors.New("DCP v2 GitHub review lacks an identity")
		}
		matched = []int64{created.ID}
	}
	sum := sha256.Sum256([]byte(body))
	return TwinReviewFacts{ReviewID: matched[0], ReviewDigest: hex.EncodeToString(sum[:]), Body: body}, nil
}

func (a *TwinGitHubAdapter) PublishExactDirectReview(ctx context.Context, facts TwinRepositoryFacts, receipt domain.DCPV2ModelTerminalReceipt) (TwinReviewFacts, error) {
	body, _, err := directReviewRequest(facts, receipt)
	if err != nil {
		return TwinReviewFacts{}, err
	}
	return a.publishReviewBody(ctx, facts, body)
}

func directReviewRequest(facts TwinRepositoryFacts, receipt domain.DCPV2ModelTerminalReceipt) (string, string, error) {
	var output directReviewResult
	factsForDigest := facts
	factsForDigest.EvidenceHash = ""
	if decodeExactDirectJSON([]byte(receipt.OutputJSON), &output) != nil || output.Verdict != "approved" ||
		!strings.EqualFold(output.HeadSHA, facts.HeadSHA) || !validDirectReviewResult(output) ||
		receipt.Status != domain.DCPV2ModelTerminalSucceeded || !validV2Digest(receipt.OutputDigest) ||
		receipt.ReceiptID == "" || facts.PRNumber < 1 || facts.CheckRunID < 1 || !facts.CheckPassed ||
		!facts.ReviewThreadsObserved ||
		!validV2SHA(facts.BaseSHA) || !validV2SHA(facts.MainSHA) || !strings.EqualFold(facts.BaseSHA, facts.MainSHA) ||
		!validV2SHA(facts.HeadSHA) || facts.HeadRef == "" || !validV2Digest(facts.EvidenceHash) ||
		facts.EvidenceHash != digestCanonical(factsForDigest) {
		return "", "", errors.New("DCP v2 direct review receipt is not an approved exact-head result")
	}
	body := "DCP v2 context-free semantic/security review " + receipt.ReceiptID + " for exact head " + facts.HeadSHA + ": no findings."
	fence := "review:" + digestCanonical(map[string]any{"body": body, "checkRunId": facts.CheckRunID,
		"evidence": facts.EvidenceHash, "head": strings.ToLower(facts.HeadSHA), "output": receipt.OutputDigest,
		"pr": facts.PRNumber, "receipt": receipt.ReceiptID})
	return body, fence, nil
}

func (a *TwinGitHubAdapter) ExpectedDirectReviewFence(facts TwinRepositoryFacts, receipt domain.DCPV2ModelTerminalReceipt) (string, error) {
	_, fence, err := directReviewRequest(facts, receipt)
	return fence, err
}

func (a *TwinGitHubAdapter) publishReviewBody(ctx context.Context, facts TwinRepositoryFacts, body string) (TwinReviewFacts, error) {
	var reviews []struct {
		ID       int64  `json:"id"`
		Body     string `json:"body"`
		CommitID string `json:"commit_id"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	path := "repos/" + TwinRepository + "/pulls/" + strconv.FormatInt(facts.PRNumber, 10) + "/reviews"
	if err := a.getJSON(ctx, path+"?per_page=100", &reviews); err != nil {
		return TwinReviewFacts{}, err
	}
	var matched []int64
	for _, review := range reviews {
		if review.Body == body && strings.EqualFold(review.CommitID, facts.HeadSHA) && review.User.Login == TwinIssuerActor {
			matched = append(matched, review.ID)
		}
	}
	if len(matched) > 1 {
		return TwinReviewFacts{}, errors.New("DCP v2 exact review cardinality drifted")
	}
	if len(matched) == 0 {
		payload, _ := json.Marshal(map[string]string{"body": body, "commit_id": facts.HeadSHA, "event": "COMMENT"})
		var created struct {
			ID int64 `json:"id"`
		}
		if err := a.mutateJSON(ctx, payload, path, &created); err != nil {
			return TwinReviewFacts{}, err
		}
		if created.ID < 1 {
			return TwinReviewFacts{}, errors.New("DCP v2 GitHub review lacks an identity")
		}
		matched = []int64{created.ID}
	}
	sum := sha256.Sum256([]byte(body))
	return TwinReviewFacts{ReviewID: matched[0], ReviewDigest: hex.EncodeToString(sum[:]), Body: body}, nil
}

func (a *TwinGitHubAdapter) Publish(ctx context.Context, request ports.DCPV2PublicationRequest) (ports.DCPV2PublicationReceipt, error) {
	if err := a.validatePublicationWorktree(ctx, request); err != nil {
		return ports.DCPV2PublicationReceipt{}, err
	}
	remote, err := a.git(ctx, request.Worktree, "ls-remote", "--heads", "origin", "refs/heads/"+request.Branch)
	if err != nil {
		return ports.DCPV2PublicationReceipt{}, err
	}
	remoteHead := ""
	if remote != "" {
		fields := strings.Fields(remote)
		if len(fields) != 2 || fields[1] != "refs/heads/"+request.Branch || !validV2SHA(fields[0]) {
			return ports.DCPV2PublicationReceipt{}, errors.New("DCP v2 publication remote ref is malformed")
		}
		remoteHead = strings.ToLower(fields[0])
	}
	switch {
	case remoteHead == strings.ToLower(request.CommitSHA):
	case remoteHead == strings.ToLower(request.ExpectedOldHead):
		lease := "--force-with-lease=refs/heads/" + request.Branch + ":" + remoteHead
		if _, err := a.git(ctx, request.Worktree, "push", lease, "origin", request.CommitSHA+":refs/heads/"+request.Branch); err != nil {
			return ports.DCPV2PublicationReceipt{}, errors.New("DCP v2 expected-old-head publication failed")
		}
	default:
		return ports.DCPV2PublicationReceipt{}, errors.New("DCP v2 publication remote head crossed its fence")
	}
	return a.ensurePublicationPR(ctx, request)
}

func (a *TwinGitHubAdapter) ReconcilePublication(ctx context.Context, request ports.DCPV2PublicationRequest) (ports.DCPV2PublicationReceipt, bool, error) {
	if err := a.validatePublicationWorktree(ctx, request); err != nil {
		return ports.DCPV2PublicationReceipt{}, false, err
	}
	remote, err := a.git(ctx, request.Worktree, "ls-remote", "--heads", "origin", "refs/heads/"+request.Branch)
	if err != nil || !strings.HasPrefix(strings.ToLower(remote), strings.ToLower(request.CommitSHA)+"\t") {
		return ports.DCPV2PublicationReceipt{}, false, err
	}
	receipt, err := a.ensurePublicationPR(ctx, request)
	return receipt, err == nil, err
}

func (a *TwinGitHubAdapter) validatePublicationWorktree(ctx context.Context, request ports.DCPV2PublicationRequest) error {
	if a == nil || a.git == nil || a.gh == nil || request.Repository != TwinRepository || request.BaseRef != TwinBase ||
		!validV2SHA(request.BaseSHA) || !validV2SHA(request.CommitSHA) || !validV2SHA(request.TreeSHA) ||
		request.Branch == "" || request.Branch == TwinBase || !filepath.IsAbs(request.Worktree) || len(request.WorktreeDigest) != 64 ||
		(request.ExpectedOldHead != "" && !validV2SHA(request.ExpectedOldHead)) {
		return errors.New("DCP v2 publication identity is incomplete")
	}
	wantFence := "publication:" + digestCanonical(map[string]any{"baseSha": request.BaseSHA, "branch": request.Branch,
		"commitSha": request.CommitSHA, "expectedOldHead": request.ExpectedOldHead, "treeSha": request.TreeSHA,
		"worktree": request.Worktree, "worktreeDigest": request.WorktreeDigest})
	if request.EffectFence != wantFence {
		return errors.New("DCP v2 publication effect fence drifted")
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain"}, ""},
		{[]string{"branch", "--show-current"}, request.Branch},
		{[]string{"rev-parse", "HEAD"}, strings.ToLower(request.CommitSHA)},
		{[]string{"rev-parse", request.CommitSHA + "^{tree}"}, strings.ToLower(request.TreeSHA)},
	}
	for _, check := range checks {
		got, err := a.git(ctx, request.Worktree, check.args...)
		if err != nil || got != check.want {
			return errors.Join(err, errors.New("DCP v2 publication worktree identity drifted"))
		}
	}
	if _, err := a.git(ctx, request.Worktree, "merge-base", "--is-ancestor", request.BaseSHA, request.CommitSHA); err != nil {
		return errors.New("DCP v2 publication base ancestry drifted")
	}
	return nil
}

func (a *TwinGitHubAdapter) ensurePublicationPR(ctx context.Context, request ports.DCPV2PublicationRequest) (ports.DCPV2PublicationReceipt, error) {
	query := url.Values{"state": {"open"}, "base": {TwinBase}, "head": {"orenvlad-ai:" + request.Branch}, "per_page": {"100"}}
	var pulls []twinPR
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/pulls?"+query.Encode(), &pulls); err != nil {
		return ports.DCPV2PublicationReceipt{}, err
	}
	if len(pulls) == 0 {
		payload, _ := json.Marshal(map[string]any{"title": "DCP Stage 6 canary", "head": request.Branch, "base": TwinBase,
			"body": "DCP v2 exact same-identity Worker publication. Model-free publication command; no duplicate model call."})
		var created twinPR
		if err := a.mutateJSON(ctx, payload, "repos/"+TwinRepository+"/pulls", &created); err != nil {
			return ports.DCPV2PublicationReceipt{}, err
		}
		pulls = []twinPR{created}
	}
	if len(pulls) != 1 || pulls[0].Number < 1 || pulls[0].State != "open" || pulls[0].Draft || pulls[0].Merged ||
		pulls[0].Base.Ref != TwinBase || pulls[0].Head.Ref != request.Branch || !strings.EqualFold(pulls[0].Head.SHA, request.CommitSHA) {
		return ports.DCPV2PublicationReceipt{}, errors.New("DCP v2 publication PR identity drifted")
	}
	evidence := digestCanonical(map[string]any{"branch": request.Branch, "commit": request.CommitSHA, "tree": request.TreeSHA, "pr": pulls[0].Number})
	return ports.DCPV2PublicationReceipt{ExternalID: "github-pr:" + strconv.FormatInt(pulls[0].Number, 10), Branch: request.Branch,
		CommitSHA: strings.ToLower(request.CommitSHA), TreeSHA: strings.ToLower(request.TreeSHA), PRNumber: pulls[0].Number, EvidenceDigest: evidence}, nil
}

func (a *TwinGitHubAdapter) ObserveRevision(ctx context.Context, _ domain.DCPV2Task, revision domain.DCPV2Revision) (ports.DCPV2RepositoryObservation, error) {
	facts, err := a.ObserveChecks(ctx, revision.HeadRef)
	if err != nil {
		return ports.DCPV2RepositoryObservation{}, err
	}
	return ports.DCPV2RepositoryObservation{Repository: TwinRepository, BaseRef: TwinBase, BaseSHA: facts.BaseSHA,
		PRNumber: facts.PRNumber, HeadSHA: facts.HeadSHA, RequiredCheckID: strconv.FormatInt(facts.CheckRunID, 10),
		RequiredCheckOK: facts.CheckPassed, EvidenceDigest: facts.EvidenceHash}, nil
}

// MaterializeReadmission prepares the deterministic two-parent successor in
// the existing native worktree but does not update a provider ref. The caller
// must durably fence the returned new head before PublishReadmission.
func (a *TwinGitHubAdapter) MaterializeReadmission(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, currentMain, worktree string) (ports.DCPV2RepositoryEffect, error) {
	if a == nil || a.git == nil || task.TaskID == "" || revision.TaskID != task.TaskID || revision.RevisionID != task.CurrentRevisionID ||
		revision.Repository != TwinRepository || revision.BaseRef != TwinBase || revision.HeadRef == "" || revision.HeadRef == TwinBase ||
		!validV2SHA(revision.BaseSHA) || !validV2SHA(revision.HeadSHA) || !validV2SHA(revision.TreeSHA) || !validV2SHA(currentMain) ||
		strings.EqualFold(currentMain, revision.BaseSHA) || !filepath.IsAbs(worktree) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission identity is incomplete")
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain"}, ""},
		{[]string{"branch", "--show-current"}, revision.HeadRef},
		{[]string{"rev-parse", "HEAD"}, strings.ToLower(revision.HeadSHA)},
		{[]string{"rev-parse", "HEAD^{tree}"}, strings.ToLower(revision.TreeSHA)},
	}
	for _, check := range checks {
		got, err := a.git(ctx, worktree, check.args...)
		if err != nil || got != check.want {
			return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission worktree identity drifted"))
		}
	}
	if _, err := a.git(ctx, worktree, "fetch", "--no-tags", "origin", TwinBase); err != nil {
		return ports.DCPV2RepositoryEffect{}, err
	}
	fetched, err := a.git(ctx, worktree, "rev-parse", "refs/remotes/origin/"+TwinBase)
	if err != nil || !strings.EqualFold(fetched, currentMain) {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission current main drifted"))
	}
	if _, err := a.git(ctx, worktree, "merge-base", "--is-ancestor", revision.BaseSHA, currentMain); err != nil {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission base ancestry conflicts")
	}
	tree, err := a.git(ctx, worktree, "merge-tree", "--write-tree", revision.HeadSHA, currentMain)
	if err != nil || !validV2SHA(tree) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission mechanical merge conflicts")
	}
	generation := task.ReadmissionCount + 1
	message := "DCP v2 readmission " + task.TaskID + " generation " + strconv.FormatInt(generation, 10)
	commitTime := revision.CreatedAt.UTC().Add(time.Duration(generation) * time.Second).Format(time.RFC3339Nano)
	env := map[string]string{
		"GIT_AUTHOR_NAME": "DCP Readmission", "GIT_AUTHOR_EMAIL": "dcp-readmission@users.noreply.github.com",
		"GIT_COMMITTER_NAME": "DCP Readmission", "GIT_COMMITTER_EMAIL": "dcp-readmission@users.noreply.github.com",
		"GIT_AUTHOR_DATE": commitTime, "GIT_COMMITTER_DATE": commitTime,
	}
	newHead, err := a.gitWithEnv(ctx, worktree, env,
		"commit-tree", tree, "-p", revision.HeadSHA, "-p", currentMain, "-m", message)
	if err != nil || !validV2SHA(newHead) || strings.EqualFold(newHead, revision.HeadSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission successor commit is invalid")
	}
	effect := ports.DCPV2RepositoryEffect{ExternalID: "readmission:" + strings.ToLower(newHead),
		OldHeadSHA: strings.ToLower(revision.HeadSHA), NewHeadSHA: strings.ToLower(newHead), TreeSHA: strings.ToLower(tree),
		BaseSHA: strings.ToLower(currentMain)}
	effect.EvidenceDigest = digestCanonical(effect)
	return effect, nil
}

// PublishReadmission performs one normal expected-old-head fast-forward push.
// A restart adopts the exact already-published head and never pushes it twice.
func (a *TwinGitHubAdapter) PublishReadmission(ctx context.Context, revision domain.DCPV2Revision, effect ports.DCPV2RepositoryEffect, worktree string) (ports.DCPV2RepositoryEffect, error) {
	if a == nil || a.git == nil || effect.ExternalID != "readmission:"+effect.NewHeadSHA ||
		effect.OldHeadSHA != strings.ToLower(revision.HeadSHA) || !validV2SHA(effect.NewHeadSHA) ||
		!validV2SHA(effect.TreeSHA) || !validV2SHA(effect.BaseSHA) ||
		effect.EvidenceDigest != digestCanonical(ports.DCPV2RepositoryEffect{ExternalID: effect.ExternalID, OldHeadSHA: effect.OldHeadSHA,
			NewHeadSHA: effect.NewHeadSHA, TreeSHA: effect.TreeSHA, BaseSHA: effect.BaseSHA}) || !filepath.IsAbs(worktree) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission effect fence drifted")
	}
	if _, err := a.git(ctx, worktree, "cat-file", "-e", effect.NewHeadSHA+"^{commit}"); err != nil {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission fenced commit disappeared")
	}
	if tree, err := a.git(ctx, worktree, "rev-parse", effect.NewHeadSHA+"^{tree}"); err != nil || tree != effect.TreeSHA {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission fenced tree drifted"))
	}
	facts, err := a.ObserveBranch(ctx, revision.HeadRef)
	if err != nil || !strings.EqualFold(facts.MainSHA, effect.BaseSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission provider main drifted"))
	}
	switch strings.ToLower(facts.HeadSHA) {
	case effect.NewHeadSHA:
	case effect.OldHeadSHA:
		lease := "--force-with-lease=refs/heads/" + revision.HeadRef + ":" + effect.OldHeadSHA
		if _, err := a.git(ctx, worktree, "push", lease, "origin", effect.NewHeadSHA+":refs/heads/"+revision.HeadRef); err != nil {
			return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission expected-old-head push failed")
		}
	default:
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission branch crossed its effect fence")
	}
	facts, err = a.ObserveBranch(ctx, revision.HeadRef)
	if err != nil || !strings.EqualFold(facts.HeadSHA, effect.NewHeadSHA) || !strings.EqualFold(facts.MainSHA, effect.BaseSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission provider did not confirm the exact successor"))
	}
	if _, err := a.git(ctx, worktree, "reset", "--hard", effect.NewHeadSHA); err != nil {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission worktree could not adopt the exact successor")
	}
	checks := []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain"}, ""},
		{[]string{"branch", "--show-current"}, revision.HeadRef},
		{[]string{"rev-parse", "HEAD"}, effect.NewHeadSHA},
		{[]string{"rev-parse", "HEAD^{tree}"}, effect.TreeSHA},
	}
	for _, check := range checks {
		got, checkErr := a.git(ctx, worktree, check.args...)
		if checkErr != nil || got != check.want {
			return ports.DCPV2RepositoryEffect{}, errors.Join(checkErr, errors.New("DCP v2 readmission worktree successor drifted"))
		}
	}
	return effect, nil
}

type TwinManifest struct {
	Protocol          string            `json:"protocol"`
	TargetSpec        string            `json:"target_spec"`
	Repository        string            `json:"repository"`
	RepositoryID      int64             `json:"repository_id"`
	Base              string            `json:"base"`
	RequiredCheck     string            `json:"required_check"`
	Profile           string            `json:"profile"`
	Environment       string            `json:"environment"`
	Service           string            `json:"service"`
	Adapter           string            `json:"adapter"`
	Issuer            map[string]string `json:"issuer"`
	QualificationCase string            `json:"qualification_case"`
	TaskID            string            `json:"task_id"`
	RevisionID        string            `json:"revision_id"`
	AdmissionID       string            `json:"admission_id"`
	AdmissionSequence int64             `json:"admission_sequence"`
	PRNumber          int64             `json:"pr_number"`
	HeadRepository    string            `json:"head_repository"`
	HeadBranch        string            `json:"head_branch"`
	AdmittedHead      string            `json:"admitted_head"`
	AdmittedBase      string            `json:"admitted_base"`
	MainSnapshot      string            `json:"main_snapshot"`
	CheckRunID        int64             `json:"check_run_id"`
	ReviewID          int64             `json:"review_id"`
	ReviewDigest      string            `json:"review_digest"`
	IssuedAt          string            `json:"issued_at"`
	ManifestDigest    string            `json:"manifest_digest"`
}

func BuildTwinManifest(task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission) (TwinManifest, error) {
	checkID, err := strconv.ParseInt(admission.RequiredCheckID, 10, 64)
	if err != nil || checkID < 1 {
		return TwinManifest{}, errors.New("DCP v2 admission check identity is malformed")
	}
	reviewParts := strings.SplitN(admission.ReviewID, ":", 2)
	if len(reviewParts) != 2 {
		return TwinManifest{}, errors.New("DCP v2 admission review identity is malformed")
	}
	reviewID, err := strconv.ParseInt(reviewParts[0], 10, 64)
	if err != nil || reviewID < 1 || !validV2Digest(reviewParts[1]) {
		return TwinManifest{}, errors.New("DCP v2 admission review identity is malformed")
	}
	if task.TaskID == "" || task.CurrentRevisionID != revision.RevisionID || revision.TaskID != task.TaskID ||
		(revision.Kind != domain.DCPV2RevisionProvider && revision.Kind != domain.DCPV2RevisionReadmission) ||
		revision.Repository != TwinRepository || revision.BaseRef != TwinBase || revision.PRNumber < 1 ||
		admission.AdmissionID == "" || admission.Sequence < 1 || admission.LineKey != TwinRepository+":"+TwinBase ||
		admission.TaskID != task.TaskID || admission.RevisionID != revision.RevisionID ||
		admission.PRNumber != revision.PRNumber || !strings.EqualFold(admission.HeadSHA, revision.HeadSHA) ||
		!strings.EqualFold(admission.BaseSHA, revision.BaseSHA) || !strings.EqualFold(admission.MainSHA, revision.BaseSHA) ||
		!validV2SHA(revision.HeadSHA) || !validV2SHA(revision.BaseSHA) || revision.HeadRef == "" || revision.HeadRef == TwinBase ||
		revision.CreatedAt.IsZero() {
		return TwinManifest{}, errors.New("DCP v2 Task/Revision/Admission manifest binding drifted")
	}
	manifest := TwinManifest{
		Protocol: "dcp-release-manifest/v1", TargetSpec: TwinTargetSpec, Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, Base: TwinBase, RequiredCheck: TwinRequiredCheck,
		Profile: "persistent-lab", Environment: TwinEnvironment, Service: TwinServiceName, Adapter: TwinAdapterVersion,
		Issuer:            map[string]string{"actor": TwinIssuerActor, "event": TwinIssuerEvent, "kind": TwinIssuerKind},
		QualificationCase: "dcp_canary", TaskID: task.TaskID, RevisionID: revision.RevisionID,
		AdmissionID: admission.AdmissionID, AdmissionSequence: admission.Sequence,
		PRNumber: admission.PRNumber, HeadRepository: TwinRepository, HeadBranch: revision.HeadRef,
		AdmittedHead: admission.HeadSHA, AdmittedBase: admission.BaseSHA,
		MainSnapshot: admission.MainSHA, CheckRunID: checkID, ReviewID: reviewID, ReviewDigest: reviewParts[1],
		IssuedAt: revision.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
	manifest.ManifestDigest = digestWithoutField(manifest, "manifest_digest")
	return manifest, nil
}

func (a *TwinGitHubAdapter) DispatchAdmission(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, fence string) (ports.DCPV2ReleaseReceipt, error) {
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil || manifest.ManifestDigest != admission.ManifestDigest || fence != admission.ManifestDigest {
		return ports.DCPV2ReleaseReceipt{}, errors.Join(err, errors.New("DCP v2 manifest/admission fence drifted"))
	}
	manifestJSON, _ := json.Marshal(manifest)
	payload, _ := json.Marshal(map[string]any{"event_type": TwinDispatchEvent, "client_payload": map[string]string{
		"manifest_b64": base64.StdEncoding.EncodeToString(manifestJSON), "manifest_digest": admission.ManifestDigest,
		"task_id": task.TaskID, "revision_id": revision.RevisionID, "admission_id": admission.AdmissionID,
	}})
	if _, err := a.gh(ctx, payload, "--method", "POST", "repos/"+TwinRepository+"/dispatches", "--input", "-"); err != nil {
		return ports.DCPV2ReleaseReceipt{}, err
	}
	evidence := digestCanonical(map[string]string{"manifest": admission.ManifestDigest, "event": TwinDispatchEvent})
	return ports.DCPV2ReleaseReceipt{ExternalID: "repository_dispatch:" + admission.ManifestDigest, Provider: "github",
		Actor: TwinIssuerActor, AdmissionID: admission.AdmissionID, ManifestDigest: admission.ManifestDigest, EvidenceDigest: evidence}, nil
}

type twinProof struct {
	Protocol          string `json:"protocol"`
	TargetSpec        string `json:"target_spec"`
	QualificationCase string `json:"qualification_case"`
	TaskID            string `json:"task_id"`
	RevisionID        string `json:"revision_id"`
	AdmissionID       string `json:"admission_id"`
	AdmissionSequence int64  `json:"admission_sequence"`
	AdmissionDigest   string `json:"admission_digest"`
	Repository        string `json:"repository"`
	RepositoryID      int64  `json:"repository_id"`
	Base              string `json:"base"`
	PRNumber          int64  `json:"pr_number"`
	AdmittedHead      string `json:"admitted_head"`
	CheckRunID        int64  `json:"check_run_id"`
	ReviewID          int64  `json:"review_id"`
	ReviewDigest      string `json:"review_digest"`
	MergeSHA          string `json:"merge_sha"`
	MergeActor        string `json:"merge_actor"`
	ArtifactID        string `json:"artifact_id"`
	ArtifactMediaType string `json:"artifact_media_type"`
	ArtifactSourceSHA string `json:"artifact_source_sha"`
	ArtifactDigest    string `json:"artifact_digest"`
	DeployedSHA       string `json:"deployed_sha"`
	Environment       string `json:"environment"`
	Service           string `json:"service"`
	Effects           struct {
		RefUpdates       int64 `json:"ref_updates"`
		Merges           int64 `json:"merges"`
		ReleaseArtifacts int64 `json:"release_artifacts"`
		Deploys          int64 `json:"deploys"`
		TerminalProofs   int64 `json:"terminal_proofs"`
	} `json:"effects"`
	Workflow      string `json:"workflow"`
	RunID         string `json:"run_id"`
	RunAttempt    string `json:"run_attempt"`
	Job           string `json:"job"`
	DispatchActor string `json:"dispatch_actor"`
	Timestamps    struct {
		Validated string `json:"validated"`
		Merged    string `json:"merged"`
		Built     string `json:"built"`
		Installed string `json:"installed"`
		Probed    string `json:"probed"`
		Published string `json:"published"`
	} `json:"timestamps"`
	Probes []struct {
		Name           string `json:"name"`
		Target         string `json:"target"`
		Result         string `json:"result"`
		EvidenceDigest string `json:"evidence_digest"`
	} `json:"probes"`
	ProofDigest string `json:"proof_digest"`
}

func (a *TwinGitHubAdapter) observeProof(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, expectedRunID int64) (twinProof, error) {
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil || manifest.ManifestDigest != admission.ManifestDigest {
		return twinProof{}, errors.Join(err, errors.New("DCP v2 expected manifest drifted"))
	}
	name := "deploy-proof-" + admission.ManifestDigest
	var artifacts struct {
		Artifacts []struct {
			ID      int64 `json:"id"`
			Expired bool  `json:"expired"`
			Run     struct {
				ID int64 `json:"id"`
			} `json:"workflow_run"`
		} `json:"artifacts"`
	}
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/actions/artifacts?name="+name+"&per_page=100", &artifacts); err != nil {
		return twinProof{}, err
	}
	active := artifacts.Artifacts[:0]
	for _, artifact := range artifacts.Artifacts {
		if !artifact.Expired {
			active = append(active, artifact)
		}
	}
	if len(active) != 1 || active[0].ID < 1 || active[0].Run.ID < 1 || (expectedRunID > 0 && active[0].Run.ID != expectedRunID) {
		return twinProof{}, errTwinTerminalProofUnavailable
	}
	archive, err := a.gh(ctx, nil, "repos/"+TwinRepository+"/actions/artifacts/"+strconv.FormatInt(active[0].ID, 10)+"/zip")
	if err != nil {
		return twinProof{}, err
	}
	files, err := readProofZip(archive)
	if err != nil {
		return twinProof{}, err
	}
	var gotManifest TwinManifest
	var proof twinProof
	if err := decodeExactDirectJSON(files["manifest.json"], &gotManifest); err != nil {
		return twinProof{}, err
	}
	if err := decodeExactDirectJSON(files["deploy-proof.json"], &proof); err != nil {
		return twinProof{}, err
	}
	wantManifest, _ := json.Marshal(manifest)
	gotManifestJSON, _ := json.Marshal(gotManifest)
	if !bytes.Equal(wantManifest, gotManifestJSON) || proof.ProofDigest != digestWithoutField(proof, "proof_digest") ||
		proof.Protocol != "dcp-deployment-proof/v1" || proof.TargetSpec != TwinTargetSpec || proof.QualificationCase != "dcp_canary" ||
		proof.TaskID != task.TaskID ||
		proof.RevisionID != revision.RevisionID || proof.AdmissionID != admission.AdmissionID ||
		proof.AdmissionSequence != admission.Sequence || proof.AdmissionDigest != admission.ManifestDigest ||
		proof.Repository != TwinRepository || proof.RepositoryID != TwinRepositoryID || proof.Base != TwinBase ||
		proof.PRNumber != admission.PRNumber || proof.AdmittedHead != admission.HeadSHA ||
		proof.CheckRunID != manifest.CheckRunID || proof.ReviewID != manifest.ReviewID || proof.ReviewDigest != manifest.ReviewDigest ||
		!validV2SHA(proof.MergeSHA) || proof.ArtifactSourceSHA != proof.MergeSHA || proof.DeployedSHA != proof.MergeSHA ||
		proof.ArtifactID != "dcp-wbc-integration-lab-"+proof.MergeSHA || proof.ArtifactMediaType != "application/gzip" ||
		!validV2Digest(proof.ArtifactDigest) ||
		proof.Environment != TwinEnvironment || proof.Service != TwinServiceName || proof.DispatchActor != TwinIssuerActor ||
		proof.MergeActor != "github-actions[bot]" || proof.Workflow != "release-train.yml" || proof.Job != "release" ||
		proof.RunID != strconv.FormatInt(active[0].Run.ID, 10) {
		return twinProof{}, errors.New("DCP v2 deployment proof identity drifted")
	}
	if attempt, attemptErr := strconv.ParseInt(proof.RunAttempt, 10, 64); attemptErr != nil || attempt < 1 ||
		proof.Effects.RefUpdates != 1 || proof.Effects.Merges != 1 || proof.Effects.ReleaseArtifacts != 1 ||
		proof.Effects.Deploys != 1 || proof.Effects.TerminalProofs != 1 {
		return twinProof{}, errors.New("DCP v2 deployment proof run/effect identity drifted")
	}
	orderedTimestamps := []string{proof.Timestamps.Validated, proof.Timestamps.Merged, proof.Timestamps.Built,
		proof.Timestamps.Installed, proof.Timestamps.Probed, proof.Timestamps.Published}
	var prior time.Time
	for _, value := range orderedTimestamps {
		parsed, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil || (!prior.IsZero() && parsed.Before(prior)) {
			return twinProof{}, errors.New("DCP v2 deployment proof timestamp sequence drifted")
		}
		prior = parsed
	}
	wantProbes := [][2]string{{"healthz", "loopback"}, {"provenance", "loopback"}, {"post_job_readback", "forced-ssh-probe"}}
	if len(proof.Probes) != len(wantProbes) {
		return twinProof{}, errors.New("DCP v2 deployment proof probe cardinality drifted")
	}
	for i, probe := range proof.Probes {
		if probe.Name != wantProbes[i][0] || probe.Target != wantProbes[i][1] || probe.Result != "success" ||
			!validV2Digest(probe.EvidenceDigest) || (i > 0 && probe.EvidenceDigest != proof.Probes[0].EvidenceDigest) {
			return twinProof{}, errors.New("DCP v2 deployment proof contains an unsuccessful probe")
		}
	}
	return proof, nil
}

type twinReleaseEvidence struct {
	Protocol          string            `json:"protocol"`
	TargetSpec        string            `json:"target_spec"`
	QualificationCase string            `json:"qualification_case"`
	Kind              string            `json:"kind"`
	Phase             string            `json:"phase"`
	Reason            string            `json:"reason"`
	Repository        string            `json:"repository"`
	RepositoryID      int64             `json:"repository_id"`
	ManifestDigest    string            `json:"manifest_digest"`
	RunID             string            `json:"run_id"`
	RunAttempt        string            `json:"run_attempt"`
	Observed          map[string]string `json:"observed"`
	Effects           map[string]int64  `json:"effects"`
	ObservedAt        string            `json:"observed_at"`
	EvidenceDigest    string            `json:"evidence_digest"`
}

func (a *TwinGitHubAdapter) observeReadmissionProof(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, runID int64) (ports.DCPV2ReleaseObservation, error) {
	if runID < 1 {
		return ports.DCPV2ReleaseObservation{}, errors.New("DCP v2 readmission run identity is missing")
	}
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil || manifest.ManifestDigest != admission.ManifestDigest {
		return ports.DCPV2ReleaseObservation{}, errors.Join(err, errors.New("DCP v2 expected readmission manifest drifted"))
	}
	name := "qualification-evidence-" + strconv.FormatInt(runID, 10)
	var artifacts struct {
		Artifacts []struct {
			ID      int64 `json:"id"`
			Expired bool  `json:"expired"`
			Run     struct {
				ID int64 `json:"id"`
			} `json:"workflow_run"`
		} `json:"artifacts"`
	}
	if err := a.getJSON(ctx, "repos/"+TwinRepository+"/actions/artifacts?name="+name+"&per_page=100", &artifacts); err != nil {
		return ports.DCPV2ReleaseObservation{}, err
	}
	var active []int64
	for _, artifact := range artifacts.Artifacts {
		if !artifact.Expired && artifact.ID > 0 && artifact.Run.ID == runID {
			active = append(active, artifact.ID)
		}
	}
	if len(active) != 1 {
		return ports.DCPV2ReleaseObservation{}, errors.New("DCP v2 readmission proof artifact is not unique")
	}
	archive, err := a.gh(ctx, nil, "repos/"+TwinRepository+"/actions/artifacts/"+strconv.FormatInt(active[0], 10)+"/zip")
	if err != nil {
		return ports.DCPV2ReleaseObservation{}, err
	}
	files, err := readEvidenceZip(archive, "manifest.json", "release-evidence.json")
	if err != nil {
		return ports.DCPV2ReleaseObservation{}, err
	}
	var gotManifest TwinManifest
	var evidence twinReleaseEvidence
	if decodeExactDirectJSON(files["manifest.json"], &gotManifest) != nil ||
		decodeExactDirectJSON(files["release-evidence.json"], &evidence) != nil {
		return ports.DCPV2ReleaseObservation{}, errors.New("DCP v2 readmission proof cannot be decoded")
	}
	wantManifest, _ := json.Marshal(manifest)
	gotManifestJSON, _ := json.Marshal(gotManifest)
	zeroEffects := len(evidence.Effects) == 5 && evidence.Effects["ref_updates"] == 0 && evidence.Effects["merges"] == 0 &&
		evidence.Effects["release_artifacts"] == 0 && evidence.Effects["deploys"] == 0 && evidence.Effects["terminal_proofs"] == 0
	currentMain, currentHead := strings.ToLower(evidence.Observed["current_main"]), strings.ToLower(evidence.Observed["current_head"])
	exactReason := len(evidence.Observed) == 3 &&
		((evidence.Reason == "main_drift" && strings.EqualFold(evidence.Observed["expected_main"], admission.MainSHA) &&
			strings.EqualFold(currentHead, admission.HeadSHA)) ||
			(evidence.Reason == "head_drift" && strings.EqualFold(evidence.Observed["expected_head"], admission.HeadSHA)))
	observedAt, observedAtErr := time.Parse(time.RFC3339Nano, evidence.ObservedAt)
	runAttempt, runAttemptErr := strconv.ParseInt(evidence.RunAttempt, 10, 64)
	if !bytes.Equal(wantManifest, gotManifestJSON) || evidence.EvidenceDigest != digestWithoutField(evidence, "evidence_digest") ||
		evidence.Protocol != "dcp-release-evidence/v1" || evidence.TargetSpec != TwinTargetSpec || evidence.QualificationCase != "dcp_canary" ||
		evidence.Kind != "readmission_required" || evidence.Phase != "validation" || !exactReason || evidence.Repository != TwinRepository ||
		evidence.RepositoryID != TwinRepositoryID || evidence.ManifestDigest != admission.ManifestDigest ||
		evidence.RunID != strconv.FormatInt(runID, 10) || runAttemptErr != nil || runAttempt < 1 || observedAtErr != nil || observedAt.IsZero() ||
		!validV2SHA(currentMain) || !validV2SHA(currentHead) || !zeroEffects {
		return ports.DCPV2ReleaseObservation{}, errors.New("DCP v2 readmission proof identity drifted")
	}
	return ports.DCPV2ReleaseObservation{ProofID: evidence.EvidenceDigest, Provider: "github", RunID: evidence.RunID,
		Actor: "github-actions[bot]", AdmissionID: admission.AdmissionID, ManifestDigest: admission.ManifestDigest,
		Readmission: true, CurrentMainSHA: currentMain, EvidenceDigest: evidence.EvidenceDigest}, nil
}

func (a *TwinGitHubAdapter) ObserveRelease(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, runID int64) (ports.DCPV2ReleaseObservation, error) {
	proof, err := a.observeProof(ctx, task, revision, admission, runID)
	if err != nil {
		if !errors.Is(err, errTwinTerminalProofUnavailable) {
			return ports.DCPV2ReleaseObservation{}, err
		}
		readmission, readmissionErr := a.observeReadmissionProof(ctx, task, revision, admission, runID)
		if readmissionErr != nil {
			return ports.DCPV2ReleaseObservation{}, errors.Join(err, readmissionErr)
		}
		return readmission, nil
	}
	return ports.DCPV2ReleaseObservation{ProofID: proof.ProofDigest, Provider: "github", RunID: proof.RunID,
		Actor: proof.DispatchActor, AdmissionID: admission.AdmissionID, ManifestDigest: admission.ManifestDigest,
		MergeSHA: proof.MergeSHA, ArtifactSourceSHA: proof.ArtifactSourceSHA,
		ArtifactDigest: proof.ArtifactDigest, EvidenceDigest: proof.ProofDigest}, nil
}

func (a *TwinGitHubAdapter) ObserveDeployment(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, mergeSHA string) (ports.DCPV2DeploymentObservation, error) {
	proof, err := a.observeProof(ctx, task, revision, admission, 0)
	if err != nil {
		return ports.DCPV2DeploymentObservation{}, err
	}
	if proof.MergeSHA != mergeSHA {
		return ports.DCPV2DeploymentObservation{}, errors.New("DCP v2 release/deployment merge identity drifted")
	}
	probeDigest := proof.Probes[0].EvidenceDigest
	for _, probe := range proof.Probes[1:] {
		if probe.EvidenceDigest != probeDigest {
			return ports.DCPV2DeploymentObservation{}, errors.New("DCP v2 deployment probe evidence drifted")
		}
	}
	return ports.DCPV2DeploymentObservation{ProofID: proof.ProofDigest, Provider: "github", RunID: proof.RunID,
		Actor: proof.DispatchActor, AdmissionID: admission.AdmissionID, ManifestDigest: admission.ManifestDigest,
		MergeSHA: proof.MergeSHA, ArtifactSourceSHA: proof.ArtifactSourceSHA,
		ArtifactDigest: proof.ArtifactDigest, DeployedSHA: proof.DeployedSHA,
		Environment: proof.Environment, Service: proof.Service, ProbeDigest: probeDigest,
		EvidenceDigest: proof.ProofDigest, Succeeded: true}, nil
}

func (a *TwinGitHubAdapter) getJSON(ctx context.Context, path string, out any) error {
	body, err := a.gh(ctx, nil, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("DCP v2 GitHub adapter decode %s: %w", path, err)
	}
	return nil
}

func (a *TwinGitHubAdapter) mutateJSON(ctx context.Context, payload []byte, path string, out any) error {
	body, err := a.gh(ctx, payload, "--method", "POST", path, "--input", "-")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("DCP v2 GitHub adapter decode mutation %s: %w", path, err)
	}
	return nil
}

func readProofZip(data []byte) (map[string][]byte, error) {
	return readEvidenceZip(data, "manifest.json", "deploy-proof.json")
}

func readEvidenceZip(data []byte, required ...string) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	wanted := map[string]bool{}
	for _, name := range required {
		wanted[name] = true
	}
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := file.Name
		base := name[strings.LastIndex(name, "/")+1:]
		if !wanted[base] || file.Mode()&os.ModeSymlink != 0 || base == "" {
			return nil, errors.New("DCP v2 proof archive contains a foreign evidence file")
		}
		if _, exists := out[base]; exists {
			return nil, errors.New("DCP v2 proof archive contains duplicate evidence files")
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		b, readErr := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		_ = r.Close()
		if readErr != nil || len(b) > 1<<20 {
			return nil, errors.Join(readErr, errors.New("DCP v2 proof evidence file exceeds its bound"))
		}
		out[base] = b
	}
	for _, name := range required {
		if len(out[name]) == 0 {
			return nil, errors.New("DCP v2 proof archive is incomplete")
		}
	}
	return out, nil
}

func digestCanonical(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func digestWithoutField(value any, field string) string {
	b, _ := json.Marshal(value)
	var object map[string]any
	_ = json.Unmarshal(b, &object)
	delete(object, field)
	return digestCanonical(object)
}

func validV2SHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validV2Digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
