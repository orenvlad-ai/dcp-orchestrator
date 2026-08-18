package dcpterminalmerge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	wbcReadmissionProofMarker = "wb-core-dcp-release-readmission-required"
	wbcHandoffMarker          = "wb-core-dcp-release-handoff-proof"
	wbcHandoffV1              = "wb-core.dcp-release-handoff/v1"
	wbcHandoffV2              = "wb-core.dcp-release-handoff/v2"
)

type wbcReadmissionStore interface {
	GetDCPReviewLabAdmissionByID(context.Context, string) (domain.DCPReviewLabAdmission, bool, error)
	ListDCPReviewLabPolicyTasks(context.Context) ([]domain.DCPReviewLabPolicyTask, error)
	GetOpenDCPWBCReadmissionGenerationByTask(context.Context, string) (domain.DCPWBCReadmissionGeneration, bool, error)
	GetLatestDCPWBCReadmissionGenerationByTask(context.Context, string) (domain.DCPWBCReadmissionGeneration, bool, error)
	ListDCPWBCReadmissionGenerations(context.Context) ([]domain.DCPWBCReadmissionGeneration, error)
	ObserveDCPWBCReadmissionGeneration(context.Context, domain.DCPWBCReadmissionGeneration, domain.DCPReviewLabPolicyTask, domain.DCPReviewLabAdmission) (domain.DCPWBCReadmissionGeneration, bool, error)
	ClaimDCPWBCReadmissionGeneration(context.Context, domain.DCPWBCReadmissionGeneration, string, time.Time) (bool, error)
	PrepareDCPWBCReadmissionGeneration(context.Context, domain.DCPWBCReadmissionGeneration, string, string, string, time.Time) (bool, error)
	AdvanceDCPWBCReadmissionHead(context.Context, domain.DCPWBCReadmissionGeneration, domain.DCPReviewLabPolicyTask, time.Time) (bool, error)
	FailDCPWBCReadmissionGeneration(context.Context, domain.DCPWBCReadmissionGeneration, string, bool, time.Time) (bool, error)
}

type wbcReadmissionMarker struct {
	comment        ports.SCMReleaseComment
	bodyDigest     string
	version        string
	repository     string
	base           string
	task           string
	scope          string
	headRef        string
	session        int64
	pr             int64
	admittedHead   string
	observedHead   string
	main           string
	providerMain   string
	readyEvent     int64
	admissionCheck int64
	handoffProof   int64
	reason         string
}

func (e *Engine) reconcileAllWBCReadmissions(ctx context.Context) error {
	store, ok := e.store.(wbcReadmissionStore)
	if !ok {
		return nil
	}
	tasks, err := store.ListDCPReviewLabPolicyTasks(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Target != "wb-core" {
			continue
		}
		if _, err := e.reconcileWBCReadmission(ctx, task.SessionID); err != nil {
			return err
		}
	}
	return nil
}

// reconcileWBCReadmission advances at most one durable fence. Replays are
// idempotent and never create a task, session, PR, branch or initial worker.
func (e *Engine) reconcileWBCReadmission(ctx context.Context, sessionID domain.SessionID) (bool, error) {
	store, ok := e.store.(wbcReadmissionStore)
	policy, policyOK := e.store.(policyStore)
	release, releaseOK := e.scm.(ports.SCMReleaseTrain)
	if !ok || !policyOK || !releaseOK {
		return false, nil
	}
	task, found, err := policy.GetDCPReviewLabPolicyTaskBySession(ctx, sessionID)
	if err != nil || !found || task.Target != "wb-core" {
		return false, err
	}
	spec, exact := domain.DCPPolicyTargetForTask(task)
	if !exact || !spec.UsesWBCReleaseTrain() {
		return false, nil
	}
	if generation, open, getErr := store.GetOpenDCPWBCReadmissionGenerationByTask(ctx, task.TaskID); getErr != nil {
		return false, getErr
	} else if open {
		return e.advanceWBCReadmissionGeneration(ctx, store, task, generation, release)
	}
	if task.State != domain.DCPPolicyIncident || task.ErrorCode != "release_state_drift" || task.AdmissionID == "" || task.ReviewRunID == "" {
		return false, nil
	}
	admission, found, err := store.GetDCPReviewLabAdmissionByID(ctx, task.AdmissionID)
	if err != nil || !found {
		return false, err
	}
	if admission.Status != domain.DCPAdmissionIncident || admission.ErrorCode != task.ErrorCode ||
		admission.IncidentPacket != task.IncidentPacket || admission.SessionID != task.SessionID ||
		admission.ReviewRunID != task.ReviewRunID || !strings.EqualFold(admission.TargetSHA, task.CurrentHeadSHA) {
		return false, errors.New("dcp WBC readmission: incident identity drifted")
	}
	ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "wb-core", Repo: spec.Repository}, Number: int(task.PRNumber), URL: task.PRURL}
	observed, err := release.ObserveRelease(ctx, ref)
	if err != nil {
		return false, err
	}
	marker, found, err := exactWBCReadmissionMarker(observed, task, admission)
	if err != nil || !found {
		return false, err
	}
	row := domain.DCPWBCReadmissionGeneration{
		GenerationID: "dcp-wbc-readmission-" + task.TaskID + "-" + strconv.FormatInt(marker.comment.ID, 10),
		MarkerDigest: marker.bodyDigest, MarkerVersion: marker.version, MarkerCommentID: marker.comment.ID,
		MarkerAuthor: marker.comment.Author, MarkerCreatedAt: marker.comment.CreatedAt, MarkerUpdatedAt: marker.comment.UpdatedAt,
		MarkerMainSHA: marker.main,
		TaskID:        task.TaskID, SessionID: task.SessionID, OldAdmissionID: admission.ID, PRURL: task.PRURL, PRNumber: task.PRNumber,
		Repository: spec.Repository, BaseBranch: spec.DefaultBranch, Scope: task.Profile, HeadRef: task.SourceBranch,
		SessionNumber: task.CardNumber, AdmittedHeadSHA: marker.admittedHead, ObservedHeadSHA: marker.observedHead,
		AdmittedBaseSHA: admission.AdmittedBaseSHA,
		CurrentMainSHA:  marker.providerMain, ReadyEventID: marker.readyEvent, AdmissionCheckID: marker.admissionCheck,
		HandoffProofID: marker.handoffProof, Reason: marker.reason, Status: domain.DCPWBCReadmissionObserved,
		CreatedAt: e.clock(), UpdatedAt: e.clock(),
	}
	generation, _, err := store.ObserveDCPWBCReadmissionGeneration(ctx, row, task, admission)
	if err != nil {
		return false, err
	}
	return e.advanceWBCReadmissionGeneration(ctx, store, task, generation, release)
}

func (e *Engine) advanceWBCReadmissionGeneration(ctx context.Context, store wbcReadmissionStore, task domain.DCPReviewLabPolicyTask, generation domain.DCPWBCReadmissionGeneration, release ports.SCMReleaseTrain) (bool, error) {
	switch generation.Status {
	case domain.DCPWBCReadmissionObserved:
		leaseID := "dcp-wbc-readmission-lease-" + strconv.FormatInt(generation.Sequence, 10)
		changed, err := store.ClaimDCPWBCReadmissionGeneration(ctx, generation, leaseID, e.clock())
		if err != nil || !changed {
			return false, err
		}
		generation.Status, generation.LeaseID = domain.DCPWBCReadmissionClaimed, leaseID
		return e.advanceWBCReadmissionGeneration(ctx, store, task, generation, release)
	case domain.DCPWBCReadmissionClaimed:
		oldAdmission, found, getErr := store.GetDCPReviewLabAdmissionByID(ctx, generation.OldAdmissionID)
		if getErr != nil || !found {
			return false, errors.Join(getErr, errors.New("dcp WBC readmission: old admission is unavailable"))
		}
		candidate, ok, err := e.candidateForFutureArbiterAdmission(ctx, oldAdmission)
		if err != nil || !ok {
			return false, errors.Join(err, errors.New("dcp WBC readmission: exact incident candidate is unavailable"))
		}
		canonical, err := e.syncCanonicalMain(ctx, candidate, generation.CurrentMainSHA)
		if err != nil {
			return false, errors.Join(err, errors.New("dcp WBC readmission: provider main identity drifted"))
		}
		preparedGeneration := generation
		preparedGeneration.CurrentMainSHA = canonical
		tree, head, conflict, err := e.prepareWBCReadmissionCommit(ctx, candidate, preparedGeneration)
		if err != nil {
			_, _ = store.FailDCPWBCReadmissionGeneration(ctx, generation, "readmission_git_invalid", conflict, e.clock())
			return false, err
		}
		changed, err := store.PrepareDCPWBCReadmissionGeneration(ctx, generation, tree, head, canonical, e.clock())
		if err != nil || !changed {
			return false, errors.Join(err, errors.New("dcp WBC readmission: prepared head could not be persisted"))
		}
		generation.Status, generation.MergeTreeSHA, generation.NewHeadSHA, generation.CurrentMainSHA = domain.DCPWBCReadmissionPrepared, tree, head, canonical
		return e.advanceWBCReadmissionGeneration(ctx, store, task, generation, release)
	case domain.DCPWBCReadmissionPrepared:
		if err := e.pushWBCReadmissionHead(ctx, task, generation); err != nil {
			return false, err
		}
		ref := ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "wb-core", Repo: generation.Repository}, Number: int(generation.PRNumber), URL: generation.PRURL}
		observed, err := release.ObserveRelease(ctx, ref)
		if err != nil {
			return false, err
		}
		if observed.State != "open" || observed.Merged || observed.Draft || observed.Number != int(generation.PRNumber) ||
			observed.URL != generation.PRURL || observed.HeadRepository != generation.Repository || observed.HeadBranch != generation.HeadRef ||
			observed.BaseBranch != generation.BaseBranch || observed.Author != "orenvlad-ai" || !strings.EqualFold(observed.HeadSHA, generation.NewHeadSHA) {
			return false, errors.New("dcp WBC readmission: provider did not confirm the exact new head")
		}
		generation.Status = domain.DCPWBCReadmissionHeadPushed
		changed, err := store.AdvanceDCPWBCReadmissionHead(ctx, generation, task, e.clock())
		if err != nil || !changed {
			return false, errors.Join(err, errors.New("dcp WBC readmission: task could not enter fresh CI wait"))
		}
		if e.modelFreeReviewTrigger != nil {
			if err := e.modelFreeReviewTrigger(ctx, task.SessionID); err != nil {
				return true, err
			}
		}
		return true, nil
	case domain.DCPWBCReadmissionHeadPushed:
		if e.modelFreeReviewTrigger == nil {
			return false, nil
		}
		if err := e.modelFreeReviewTrigger(ctx, task.SessionID); err != nil {
			return false, err
		}
		return true, nil
	case domain.DCPWBCReadmissionReviewQueue, domain.DCPWBCReadmissionReviewed,
		domain.DCPWBCReadmissionAdmitted, domain.DCPWBCReadmissionReleaseWait:
		return false, nil
	default:
		return false, nil
	}
}

func (e *Engine) prepareWBCReadmissionCommit(ctx context.Context, candidate mergeCandidate, generation domain.DCPWBCReadmissionGeneration) (tree, head string, conflict bool, err error) {
	workspace := candidate.session.Metadata.WorkspacePath
	project := candidate.project.Path
	checks := []struct {
		path string
		args []string
		want string
	}{
		{project, []string{"rev-parse", "HEAD"}, strings.ToLower(generation.CurrentMainSHA)},
		{project, []string{"status", "--porcelain"}, ""},
		{workspace, []string{"rev-parse", "HEAD"}, strings.ToLower(generation.AdmittedHeadSHA)},
		{workspace, []string{"status", "--porcelain"}, ""},
		{workspace, []string{"branch", "--show-current"}, generation.HeadRef},
	}
	for _, check := range checks {
		got, gitErr := e.git(ctx, check.path, check.args...)
		if gitErr != nil || got != check.want {
			return "", "", false, errors.New("dcp WBC readmission: local repository identity is not exact and clean")
		}
	}
	if !validSHA(generation.AdmittedBaseSHA) {
		return "", "", false, errors.New("dcp WBC readmission: admitted base identity is invalid")
	}
	if !validSHA(generation.MarkerMainSHA) || !validSHA(generation.CurrentMainSHA) {
		return "", "", false, errors.New("dcp WBC readmission: marker/provider main identity is invalid")
	}
	if _, gitErr := e.git(ctx, workspace, "merge-base", "--is-ancestor", generation.MarkerMainSHA, generation.CurrentMainSHA); gitErr != nil {
		return "", "", true, errors.New("dcp WBC readmission: provider main does not descend from the marker event")
	}
	if _, gitErr := e.git(ctx, workspace, "merge-base", "--is-ancestor", generation.AdmittedBaseSHA, generation.CurrentMainSHA); gitErr != nil {
		return "", "", true, errors.New("dcp WBC readmission: admitted base is not an ancestor of current main")
	}
	mergedTree, gitErr := e.git(ctx, workspace, "merge-tree", "--write-tree", generation.AdmittedHeadSHA, generation.CurrentMainSHA)
	if gitErr != nil || !validSHA(mergedTree) {
		return "", "", true, errors.New("dcp WBC readmission: exact mechanical merge conflicts")
	}
	message := "DCP readmission generation " + strconv.FormatInt(generation.Sequence, 10)
	commit, gitErr := e.git(ctx, workspace, "-c", "user.name=DCP Readmission", "-c", "user.email=dcp-readmission@users.noreply.github.com",
		"commit-tree", mergedTree, "-p", generation.AdmittedHeadSHA, "-p", generation.CurrentMainSHA, "-m", message)
	if gitErr != nil || !validSHA(commit) {
		return "", "", false, errors.New("dcp WBC readmission: integration commit could not be created")
	}
	return strings.ToLower(mergedTree), strings.ToLower(commit), false, nil
}

func (e *Engine) pushWBCReadmissionHead(ctx context.Context, task domain.DCPReviewLabPolicyTask, generation domain.DCPWBCReadmissionGeneration) error {
	workspace := task.WorktreePath
	local, err := e.git(ctx, workspace, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if status, statusErr := e.git(ctx, workspace, "status", "--porcelain"); statusErr != nil || status != "" {
		return errors.Join(statusErr, errors.New("dcp WBC readmission: integration worktree is not clean"))
	}
	switch strings.ToLower(local) {
	case strings.ToLower(generation.AdmittedHeadSHA):
		if _, err := e.git(ctx, workspace, "-c", "core.hooksPath=/dev/null", "merge", "--ff-only", generation.NewHeadSHA); err != nil {
			return errors.New("dcp WBC readmission: local exact-head fast-forward failed")
		}
	case strings.ToLower(generation.NewHeadSHA):
	default:
		return errors.New("dcp WBC readmission: local branch moved outside the exact lease")
	}
	if status, err := e.git(ctx, workspace, "status", "--porcelain"); err != nil || status != "" {
		return errors.Join(err, errors.New("dcp WBC readmission: integration worktree is not clean"))
	}
	_, err = e.git(ctx, workspace, "-c", "core.hooksPath=/dev/null", "push", "origin", generation.NewHeadSHA+":refs/heads/"+task.SourceBranch)
	if err != nil {
		return errors.New("dcp WBC readmission: normal fast-forward push failed")
	}
	return nil
}

func exactWBCReadmissionMarker(observation ports.SCMReleaseObservation, task domain.DCPReviewLabPolicyTask, admission domain.DCPReviewLabAdmission) (wbcReadmissionMarker, bool, error) {
	if observation.Number != int(task.PRNumber) || observation.URL != task.PRURL || observation.HeadRepository != task.Repository ||
		observation.HeadBranch != task.SourceBranch || observation.BaseBranch != "main" || observation.Author != "orenvlad-ai" ||
		observation.Merged || observation.State != "open" || observation.Draft || !strings.EqualFold(observation.HeadSHA, admission.TargetSHA) ||
		!validSHA(observation.ProviderMainSHA) || strings.EqualFold(observation.ProviderMainSHA, admission.TargetSHA) {
		return wbcReadmissionMarker{}, false, errors.New("dcp WBC readmission: provider identity is stale or crossed")
	}
	var history []wbcReadmissionMarker
	for _, comment := range observation.Comments {
		if !strings.Contains(comment.Body, wbcReadmissionProofMarker) {
			continue
		}
		marker, err := parseWBCReadmissionComment(comment)
		if err != nil {
			return wbcReadmissionMarker{}, false, err
		}
		history = append(history, marker)
	}
	if len(history) == 0 {
		return wbcReadmissionMarker{}, false, nil
	}
	wantScope := task.Profile
	seenHeads := make(map[string]struct{}, len(history))
	matches := make([]wbcReadmissionMarker, 0, 1)
	for _, marker := range history {
		if marker.version == wbcHandoffV1 {
			if task.Profile != "repo-only" || marker.pr != task.PRNumber || marker.base != "main" || marker.reason != "base-behind-after-admission" {
				return wbcReadmissionMarker{}, false, errors.New("dcp WBC readmission: v1 marker identity is invalid")
			}
			marker.repository, marker.task, marker.scope, marker.headRef, marker.session = task.Repository, "standard", wantScope, task.SourceBranch, task.CardNumber
			marker.observedHead = marker.admittedHead
		} else if marker.pr != task.PRNumber || marker.repository != task.Repository || marker.base != "main" || marker.task != "standard" || marker.scope != wantScope ||
			marker.headRef != task.SourceBranch || marker.session != task.CardNumber || marker.readyEvent <= 0 || marker.admissionCheck <= 0 || marker.handoffProof <= 0 ||
			marker.reason != "base-behind-after-admission" || !validWBCReferencedHandoff(observation.Comments, marker) {
			return wbcReadmissionMarker{}, false, errors.New("dcp WBC readmission: historical proof identity is stale or crossed")
		}
		marker.providerMain = strings.ToLower(observation.ProviderMainSHA)
		generationKey := marker.version + ":" + marker.admittedHead
		if _, duplicate := seenHeads[generationKey]; duplicate {
			return wbcReadmissionMarker{}, false, errors.New("dcp WBC readmission: marker generation is duplicated")
		}
		seenHeads[generationKey] = struct{}{}
		if !strings.EqualFold(marker.admittedHead, admission.TargetSHA) || !strings.EqualFold(marker.observedHead, admission.TargetSHA) {
			continue
		}
		if marker.version == wbcHandoffV1 {
			marker.main = observation.BaseSHA
		}
		if !validSHA(marker.main) || strings.EqualFold(marker.main, admission.TargetSHA) ||
			marker.pr != task.PRNumber || marker.reason != "base-behind-after-admission" {
			continue
		}
		matches = append(matches, marker)
	}
	if len(matches) == 0 {
		return wbcReadmissionMarker{}, false, nil
	}
	if len(matches) != 1 {
		return wbcReadmissionMarker{}, false, errors.New("dcp WBC readmission: current marker cardinality is not exact")
	}
	return matches[0], true, nil
}

func parseWBCReadmissionComment(comment ports.SCMReleaseComment) (wbcReadmissionMarker, error) {
	if comment.ID <= 0 || !isWBCActionsActor(comment.Author) || comment.CreatedAt.IsZero() || !comment.CreatedAt.Equal(comment.UpdatedAt) {
		return wbcReadmissionMarker{}, errors.New("dcp WBC readmission: marker author or timestamps are not immutable")
	}
	fields, err := exactMarkerFields(comment.Body, wbcReadmissionProofMarker)
	if err != nil {
		return wbcReadmissionMarker{}, err
	}
	version := fields["version"]
	marker := wbcReadmissionMarker{comment: comment, bodyDigest: sha256Hex(comment.Body), version: version, base: fields["base"], reason: fields["reason"]}
	if version == wbcHandoffV1 {
		if !exactFieldSet(fields, "base", "head", "pr", "reason", "version") {
			return marker, errors.New("dcp WBC readmission: v1 fields are incomplete")
		}
		marker.pr, err = positiveInt(fields["pr"])
		marker.admittedHead, marker.observedHead = strings.ToLower(fields["head"]), strings.ToLower(fields["head"])
		want := fmt.Sprintf("Release Train removed DCP release eligibility without updating or merging the admitted head `%s`. A fresh exact-head baseline, DCP review and FIFO admission are required.\n\n<!-- %s base=main head=%s pr=%d reason=base-behind-after-admission version=%s -->", marker.admittedHead, wbcReadmissionProofMarker, marker.admittedHead, marker.pr, wbcHandoffV1)
		if err != nil || !validSHA(marker.admittedHead) || comment.Body != want {
			return marker, errors.New("dcp WBC readmission: v1 body is non-canonical")
		}
		return marker, nil
	}
	if version != wbcHandoffV2 || !exactFieldSet(fields, "admission_check", "admitted_head", "base", "digest", "handoff_proof", "head_ref", "main", "observed_head", "pr", "ready_event", "reason", "repo", "scope", "session", "task", "version") {
		return marker, errors.New("dcp WBC readmission: v2 fields are incomplete")
	}
	marker.repository, marker.task, marker.scope, marker.headRef = fields["repo"], strings.TrimPrefix(fields["task"], "task:"), strings.TrimPrefix(fields["scope"], "scope:"), fields["head_ref"]
	marker.admittedHead, marker.observedHead, marker.main = strings.ToLower(fields["admitted_head"]), strings.ToLower(fields["observed_head"]), strings.ToLower(fields["main"])
	marker.pr, err = positiveInt(fields["pr"])
	if err == nil {
		marker.session, err = positiveInt(fields["session"])
	}
	if err == nil {
		marker.readyEvent, err = positiveInt(fields["ready_event"])
	}
	if err == nil {
		marker.admissionCheck, err = positiveInt(fields["admission_check"])
	}
	if err == nil {
		marker.handoffProof, err = positiveInt(fields["handoff_proof"])
	}
	if err != nil || !validSHA(marker.admittedHead) || !validSHA(marker.observedHead) || !validSHA(marker.main) {
		return marker, errors.New("dcp WBC readmission: v2 typed fields are invalid")
	}
	values := map[string]any{"admission_check": marker.admissionCheck, "admitted_head": marker.admittedHead, "base": marker.base, "handoff_proof": marker.handoffProof, "head_ref": marker.headRef, "main": marker.main, "observed_head": marker.observedHead, "pr": marker.pr, "ready_event": marker.readyEvent, "reason": marker.reason, "repo": marker.repository, "scope": "scope:" + marker.scope, "session": marker.session, "task": "task:" + marker.task, "version": marker.version}
	if fields["digest"] != proofValuesDigest(values) {
		return marker, errors.New("dcp WBC readmission: v2 digest is invalid")
	}
	values["digest"] = fields["digest"]
	markerLine := proofMarker(wbcReadmissionProofMarker, values)
	want := fmt.Sprintf("Release Train removed DCP release eligibility without updating or merging the admitted head `%s`. A fresh exact-head baseline, DCP review and FIFO admission are required.\n\n%s", marker.admittedHead, markerLine)
	if comment.Body != want {
		return marker, errors.New("dcp WBC readmission: v2 body is non-canonical")
	}
	return marker, nil
}

func validWBCReferencedHandoff(comments []ports.SCMReleaseComment, marker wbcReadmissionMarker) bool {
	for _, comment := range comments {
		if comment.ID != marker.handoffProof || !isWBCActionsActor(comment.Author) || comment.CreatedAt.IsZero() || !comment.CreatedAt.Equal(comment.UpdatedAt) {
			continue
		}
		fields, err := exactMarkerFields(comment.Body, wbcHandoffMarker)
		if err != nil || fields["version"] != wbcHandoffV2 || !exactFieldSet(fields, "admission_check", "base", "digest", "head", "head_ref", "main", "pr", "ready_event", "release_check", "repo", "scope", "session", "task", "version") {
			continue
		}
		admissionCheck, err1 := positiveInt(fields["admission_check"])
		readyEvent, err2 := positiveInt(fields["ready_event"])
		releaseCheck, err3 := positiveInt(fields["release_check"])
		proofPR, err4 := positiveInt(fields["pr"])
		proofSession, err5 := positiveInt(fields["session"])
		main := strings.ToLower(fields["main"])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil ||
			fields["repo"] != marker.repository || fields["base"] != marker.base || fields["head_ref"] != marker.headRef ||
			fields["scope"] != "scope:"+marker.scope || fields["task"] != "task:"+marker.task ||
			strings.ToLower(fields["head"]) != marker.admittedHead || !validSHA(main) || proofPR != marker.pr ||
			proofSession != marker.session || readyEvent != marker.readyEvent || admissionCheck != marker.admissionCheck {
			continue
		}
		values := map[string]any{
			"admission_check": admissionCheck, "base": marker.base, "head": marker.admittedHead,
			"head_ref": marker.headRef, "main": main, "pr": marker.pr, "ready_event": readyEvent,
			"release_check": releaseCheck, "repo": marker.repository, "scope": "scope:" + marker.scope,
			"session": marker.session, "task": "task:" + marker.task, "version": wbcHandoffV2,
		}
		if fields["digest"] != proofValuesDigest(values) {
			continue
		}
		values["digest"] = fields["digest"]
		want := "Release Train accepted the DCP exact-head handoff without synchronizing or replacing the admitted branch.\n\n" + proofMarker(wbcHandoffMarker, values)
		return comment.Body == want
	}
	return false
}

func validWBCProductionProof(observation ports.SCMReleaseObservation, task domain.DCPReviewLabPolicyTask, mergeSHA string) bool {
	var proofs []ports.SCMReleaseComment
	for _, comment := range observation.Comments {
		if strings.Contains(comment.Body, "wb-core-dcp-release-production-proof") {
			proofs = append(proofs, comment)
		}
	}
	if len(proofs) != 1 {
		return false
	}
	comment := proofs[0]
	if comment.ID <= 0 || !isWBCActionsActor(comment.Author) || comment.CreatedAt.IsZero() || !comment.CreatedAt.Equal(comment.UpdatedAt) {
		return false
	}
	fields, err := exactMarkerFields(comment.Body, "wb-core-dcp-release-production-proof")
	if err != nil || !exactFieldSet(fields, "base", "deploy_evidence", "deployed", "digest", "handoff_proof", "head", "head_ref", "merge", "pr", "repo", "runtime_evidence", "scope", "service", "session", "target", "task", "version") {
		return false
	}
	pr, err := positiveInt(fields["pr"])
	if err != nil {
		return false
	}
	session, err := positiveInt(fields["session"])
	if err != nil {
		return false
	}
	handoff, err := positiveInt(fields["handoff_proof"])
	if err != nil {
		return false
	}
	head, deployed, merge := strings.ToLower(fields["head"]), strings.ToLower(fields["deployed"]), strings.ToLower(fields["merge"])
	if !validSHA(head) || !validSHA(deployed) || !validSHA(merge) || merge != strings.ToLower(mergeSHA) || deployed != merge || head != strings.ToLower(task.CurrentHeadSHA) ||
		fields["base"] != "main" || fields["repo"] != task.Repository || fields["scope"] != "scope:live-runtime" || fields["task"] != "task:standard" ||
		fields["head_ref"] != task.SourceBranch || pr != task.PRNumber || session != task.CardNumber || fields["version"] != wbcHandoffV2 ||
		fields["target"] != "wb_core_eu_hosted_runtime_active" || fields["service"] != "wb-core-registry-http.service" ||
		!validSHA256Fingerprint(fields["deploy_evidence"]) || !validSHA256Fingerprint(fields["runtime_evidence"]) {
		return false
	}
	values := map[string]any{
		"base": fields["base"], "deploy_evidence": fields["deploy_evidence"], "deployed": deployed,
		"handoff_proof": handoff, "head": head, "head_ref": fields["head_ref"], "merge": merge,
		"pr": pr, "repo": fields["repo"], "runtime_evidence": fields["runtime_evidence"], "scope": fields["scope"],
		"service": fields["service"], "session": session, "target": fields["target"], "task": fields["task"], "version": fields["version"],
	}
	if fields["digest"] != proofValuesDigest(values) {
		return false
	}
	values["digest"] = fields["digest"]
	want := "Release Train deployed and independently read back the exact DCP live-runtime merge on the canonical production target.\n\n" + proofMarker("wb-core-dcp-release-production-proof", values)
	if comment.Body != want {
		return false
	}
	return validWBCProductionHandoff(observation.Comments, handoff, task, head, pr, session)
}

func validWBCProductionHandoff(comments []ports.SCMReleaseComment, commentID int64, task domain.DCPReviewLabPolicyTask, head string, pr, session int64) bool {
	for _, comment := range comments {
		if comment.ID != commentID || !isWBCActionsActor(comment.Author) || comment.CreatedAt.IsZero() || !comment.CreatedAt.Equal(comment.UpdatedAt) {
			continue
		}
		fields, err := exactMarkerFields(comment.Body, wbcHandoffMarker)
		if err != nil || !exactFieldSet(fields, "admission_check", "base", "digest", "head", "head_ref", "main", "pr", "ready_event", "release_check", "repo", "scope", "session", "task", "version") {
			continue
		}
		admissionCheck, err1 := positiveInt(fields["admission_check"])
		readyEvent, err2 := positiveInt(fields["ready_event"])
		releaseCheck, err3 := positiveInt(fields["release_check"])
		proofPR, err4 := positiveInt(fields["pr"])
		proofSession, err5 := positiveInt(fields["session"])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil ||
			fields["version"] != wbcHandoffV2 || fields["base"] != "main" || fields["repo"] != task.Repository ||
			fields["scope"] != "scope:live-runtime" || fields["task"] != "task:standard" || fields["head_ref"] != task.SourceBranch ||
			strings.ToLower(fields["head"]) != head || !validSHA(fields["main"]) || proofPR != pr || proofSession != session {
			continue
		}
		values := map[string]any{"admission_check": admissionCheck, "base": "main", "head": head, "head_ref": task.SourceBranch, "main": strings.ToLower(fields["main"]), "pr": pr, "ready_event": readyEvent, "release_check": releaseCheck, "repo": task.Repository, "scope": "scope:live-runtime", "session": session, "task": "task:standard", "version": wbcHandoffV2}
		if fields["digest"] != proofValuesDigest(values) {
			continue
		}
		values["digest"] = fields["digest"]
		want := "Release Train accepted the DCP exact-head handoff without synchronizing or replacing the admitted branch.\n\n" + proofMarker(wbcHandoffMarker, values)
		if comment.Body == want {
			return true
		}
	}
	return false
}

func validSHA256Fingerprint(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != 71 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func isWBCActionsActor(actor string) bool {
	return actor == "github-actions" || actor == "github-actions[bot]"
}

func exactMarkerFields(body, marker string) (map[string]string, error) {
	prefix := "<!-- " + marker + " "
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || strings.Count(body, marker) != 1 || !strings.HasSuffix(lines[0], " -->") {
		return nil, errors.New("dcp WBC proof marker is malformed or duplicated")
	}
	fields := map[string]string{}
	for _, token := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(lines[0], prefix), " -->")) {
		key, value, ok := strings.Cut(token, "=")
		if !ok || key == "" || value == "" || fields[key] != "" {
			return nil, errors.New("dcp WBC proof marker has malformed fields")
		}
		fields[key] = value
	}
	return fields, nil
}

func exactFieldSet(fields map[string]string, keys ...string) bool {
	if len(fields) != len(keys) {
		return false
	}
	for _, key := range keys {
		if fields[key] == "" {
			return false
		}
	}
	return true
}

func positiveInt(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("positive integer required")
	}
	return parsed, nil
}

func proofValuesDigest(values map[string]any) string {
	data, _ := json.Marshal(values)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func proofMarker(marker string, values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, values[key]))
	}
	return "<!-- " + marker + " " + strings.Join(parts, " ") + " -->"
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
