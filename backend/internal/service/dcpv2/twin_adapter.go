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
	"path/filepath"
	"strconv"
	"strings"

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

// TwinGitHubAdapter implements the exact Stage 5 repository, Release Train
// and deployment-observation seams. It can dispatch one immutable manifest;
// it deliberately has no merge, artifact-build, remote-host, install, start or probe
// method.
type TwinGitHubAdapter struct {
	gh  ghRunner
	git gitRunner
}

func NewTwinGitHubAdapter() *TwinGitHubAdapter {
	return &TwinGitHubAdapter{gh: runGH, git: runTwinGit}
}

func newTwinGitHubAdapterForTest(run ghRunner) *TwinGitHubAdapter {
	return &TwinGitHubAdapter{gh: run, git: runTwinGit}
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
	PRNumber     int64
	BaseSHA      string
	MainSHA      string
	HeadSHA      string
	HeadRef      string
	CheckRunID   int64
	CheckURL     string
	CheckPassed  bool
	EvidenceHash string
}

type TwinReviewFacts struct {
	ReviewID     int64
	ReviewDigest string
	Body         string
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
		!validV2SHA(revision.BaseSHA) || !validV2SHA(revision.HeadSHA) || !validV2SHA(currentMain) ||
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
	message := "DCP v2 readmission " + task.TaskID + " generation " + strconv.FormatInt(task.ReadmissionCount+1, 10)
	newHead, err := a.git(ctx, worktree, "-c", "user.name=DCP Readmission", "-c", "user.email=dcp-readmission@users.noreply.github.com",
		"commit-tree", tree, "-p", revision.HeadSHA, "-p", currentMain, "-m", message)
	if err != nil || !validV2SHA(newHead) || strings.EqualFold(newHead, revision.HeadSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission successor commit is invalid")
	}
	effect := ports.DCPV2RepositoryEffect{ExternalID: "readmission:" + strings.ToLower(newHead),
		OldHeadSHA: strings.ToLower(revision.HeadSHA), NewHeadSHA: strings.ToLower(newHead), BaseSHA: strings.ToLower(currentMain)}
	effect.EvidenceDigest = digestCanonical(effect)
	return effect, nil
}

// PublishReadmission performs one normal expected-old-head fast-forward push.
// A restart adopts the exact already-published head and never pushes it twice.
func (a *TwinGitHubAdapter) PublishReadmission(ctx context.Context, revision domain.DCPV2Revision, effect ports.DCPV2RepositoryEffect, worktree string) (ports.DCPV2RepositoryEffect, error) {
	if a == nil || a.git == nil || effect.ExternalID != "readmission:"+effect.NewHeadSHA ||
		effect.OldHeadSHA != strings.ToLower(revision.HeadSHA) || !validV2SHA(effect.NewHeadSHA) || !validV2SHA(effect.BaseSHA) ||
		effect.EvidenceDigest != digestCanonical(ports.DCPV2RepositoryEffect{ExternalID: effect.ExternalID, OldHeadSHA: effect.OldHeadSHA,
			NewHeadSHA: effect.NewHeadSHA, BaseSHA: effect.BaseSHA}) || !filepath.IsAbs(worktree) {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission effect fence drifted")
	}
	if _, err := a.git(ctx, worktree, "cat-file", "-e", effect.NewHeadSHA+"^{commit}"); err != nil {
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission fenced commit disappeared")
	}
	facts, err := a.ObserveBranch(ctx, revision.HeadRef)
	if err != nil || !strings.EqualFold(facts.MainSHA, effect.BaseSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission provider main drifted"))
	}
	switch strings.ToLower(facts.HeadSHA) {
	case effect.NewHeadSHA:
		return effect, nil
	case effect.OldHeadSHA:
		if _, err := a.git(ctx, worktree, "push", "origin", effect.NewHeadSHA+":refs/heads/"+revision.HeadRef); err != nil {
			return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission expected-old-head push failed")
		}
	default:
		return ports.DCPV2RepositoryEffect{}, errors.New("DCP v2 readmission branch crossed its effect fence")
	}
	facts, err = a.ObserveBranch(ctx, revision.HeadRef)
	if err != nil || !strings.EqualFold(facts.HeadSHA, effect.NewHeadSHA) || !strings.EqualFold(facts.MainSHA, effect.BaseSHA) {
		return ports.DCPV2RepositoryEffect{}, errors.Join(err, errors.New("DCP v2 readmission provider did not confirm the exact successor"))
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
	if err != nil {
		return TwinManifest{}, errors.New("DCP v2 admission check identity is malformed")
	}
	reviewParts := strings.Split(admission.ReviewID, ":")
	if len(reviewParts) != 2 {
		return TwinManifest{}, errors.New("DCP v2 admission review identity is malformed")
	}
	reviewID, err := strconv.ParseInt(reviewParts[0], 10, 64)
	if err != nil || len(reviewParts[1]) != 64 {
		return TwinManifest{}, errors.New("DCP v2 admission review identity is malformed")
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
	ArtifactDigest    string `json:"artifact_digest"`
	DeployedSHA       string `json:"deployed_sha"`
	Environment       string `json:"environment"`
	Service           string `json:"service"`
	RunID             string `json:"run_id"`
	DispatchActor     string `json:"dispatch_actor"`
	Probes            []struct {
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
	if err := json.Unmarshal(files["manifest.json"], &gotManifest); err != nil {
		return twinProof{}, err
	}
	if err := json.Unmarshal(files["deploy-proof.json"], &proof); err != nil {
		return twinProof{}, err
	}
	wantManifest, _ := json.Marshal(manifest)
	gotManifestJSON, _ := json.Marshal(gotManifest)
	if !bytes.Equal(wantManifest, gotManifestJSON) || proof.ProofDigest != digestWithoutField(proof, "proof_digest") ||
		proof.Protocol != "dcp-deployment-proof/v1" || proof.TargetSpec != TwinTargetSpec || proof.TaskID != task.TaskID ||
		proof.RevisionID != revision.RevisionID || proof.AdmissionID != admission.AdmissionID ||
		proof.AdmissionSequence != admission.Sequence || proof.AdmissionDigest != admission.ManifestDigest ||
		proof.Repository != TwinRepository || proof.RepositoryID != TwinRepositoryID || proof.Base != TwinBase ||
		proof.PRNumber != admission.PRNumber || proof.AdmittedHead != admission.HeadSHA ||
		proof.CheckRunID != manifest.CheckRunID || proof.ReviewID != manifest.ReviewID || proof.ReviewDigest != manifest.ReviewDigest ||
		!validV2SHA(proof.MergeSHA) || proof.DeployedSHA != proof.MergeSHA || !validV2Digest(proof.ArtifactDigest) ||
		proof.Environment != TwinEnvironment || proof.Service != TwinServiceName || proof.DispatchActor != TwinIssuerActor ||
		proof.MergeActor != "github-actions[bot]" || proof.RunID != strconv.FormatInt(active[0].Run.ID, 10) {
		return twinProof{}, errors.New("DCP v2 deployment proof identity drifted")
	}
	wantProbes := [][2]string{{"healthz", "loopback"}, {"provenance", "loopback"}, {"post_job_readback", "forced-ssh-probe"}}
	if len(proof.Probes) != len(wantProbes) {
		return twinProof{}, errors.New("DCP v2 deployment proof probe cardinality drifted")
	}
	for i, probe := range proof.Probes {
		if probe.Name != wantProbes[i][0] || probe.Target != wantProbes[i][1] || probe.Result != "success" ||
			!validV2Digest(probe.EvidenceDigest) {
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
	if json.Unmarshal(files["manifest.json"], &gotManifest) != nil || json.Unmarshal(files["release-evidence.json"], &evidence) != nil {
		return ports.DCPV2ReleaseObservation{}, errors.New("DCP v2 readmission proof cannot be decoded")
	}
	wantManifest, _ := json.Marshal(manifest)
	gotManifestJSON, _ := json.Marshal(gotManifest)
	zeroEffects := evidence.Effects["ref_updates"] == 0 && evidence.Effects["merges"] == 0 &&
		evidence.Effects["release_artifacts"] == 0 && evidence.Effects["deploys"] == 0 && evidence.Effects["terminal_proofs"] == 0
	currentMain, currentHead := strings.ToLower(evidence.Observed["current_main"]), strings.ToLower(evidence.Observed["current_head"])
	exactReason := (evidence.Reason == "main_drift" && strings.EqualFold(evidence.Observed["expected_main"], admission.MainSHA) && strings.EqualFold(currentHead, admission.HeadSHA)) ||
		(evidence.Reason == "head_drift" && strings.EqualFold(evidence.Observed["expected_head"], admission.HeadSHA))
	if !bytes.Equal(wantManifest, gotManifestJSON) || evidence.EvidenceDigest != digestWithoutField(evidence, "evidence_digest") ||
		evidence.Protocol != "dcp-release-evidence/v1" || evidence.TargetSpec != TwinTargetSpec || evidence.QualificationCase != "dcp_canary" ||
		evidence.Kind != "readmission_required" || evidence.Phase != "validation" || !exactReason || evidence.Repository != TwinRepository ||
		evidence.RepositoryID != TwinRepositoryID || evidence.ManifestDigest != admission.ManifestDigest ||
		evidence.RunID != strconv.FormatInt(runID, 10) || !validV2SHA(currentMain) || !validV2SHA(currentHead) || !zeroEffects {
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
		MergeSHA: proof.MergeSHA, ArtifactDigest: proof.ArtifactDigest, EvidenceDigest: proof.ProofDigest}, nil
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
		MergeSHA: proof.MergeSHA, ArtifactDigest: proof.ArtifactDigest, DeployedSHA: proof.DeployedSHA,
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
		name := file.Name
		base := name[strings.LastIndex(name, "/")+1:]
		if wanted[base] {
			if _, exists := out[base]; exists {
				return nil, errors.New("DCP v2 proof archive contains duplicate evidence files")
			}
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			b, readErr := io.ReadAll(io.LimitReader(r, 1<<20))
			_ = r.Close()
			if readErr != nil {
				return nil, readErr
			}
			out[base] = b
		}
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
