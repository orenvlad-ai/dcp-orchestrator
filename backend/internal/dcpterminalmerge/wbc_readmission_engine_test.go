package dcpterminalmerge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	readmissionAdmittedHead = "1111111111111111111111111111111111111111"
	readmissionAdmittedBase = "2222222222222222222222222222222222222222"
	readmissionCurrentMain  = "3333333333333333333333333333333333333333"
	readmissionTree         = "4444444444444444444444444444444444444444"
	readmissionNewHead      = "5555555555555555555555555555555555555555"
)

func TestPrepareWBCReadmissionUsesAdmittedBaseForMainAncestry(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(t.TempDir(), "wb-core")
	engine := &Engine{}
	var commands []string
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		commands = append(commands, path+" :: "+command)
		switch {
		case path == project && command == "rev-parse HEAD":
			return readmissionCurrentMain, nil
		case path == project && command == "status --porcelain":
			return "", nil
		case path == workspace && command == "rev-parse HEAD":
			return readmissionAdmittedHead, nil
		case path == workspace && command == "status --porcelain":
			return "", nil
		case path == workspace && command == "branch --show-current":
			return "ao/wb-core-1/root", nil
		case path == workspace && command == "merge-base --is-ancestor "+readmissionCurrentMain+" "+readmissionCurrentMain:
			return "", nil
		case path == workspace && command == "merge-base --is-ancestor "+readmissionAdmittedBase+" "+readmissionCurrentMain:
			return "", nil
		case path == workspace && command == "merge-tree --write-tree "+readmissionAdmittedHead+" "+readmissionCurrentMain:
			return readmissionTree, nil
		case path == workspace && strings.HasPrefix(command, "-c user.name=DCP Readmission -c user.email=dcp-readmission@users.noreply.github.com commit-tree "):
			return readmissionNewHead, nil
		default:
			return "", errors.New("unexpected git command: " + command)
		}
	}
	candidate := mergeCandidate{
		session: domain.SessionRecord{Metadata: domain.SessionMetadata{WorkspacePath: workspace}},
		project: domain.ProjectRecord{Path: project},
	}
	generation := domain.DCPWBCReadmissionGeneration{
		Sequence: 1, HeadRef: "ao/wb-core-1/root", AdmittedHeadSHA: readmissionAdmittedHead,
		AdmittedBaseSHA: readmissionAdmittedBase, MarkerMainSHA: readmissionCurrentMain, CurrentMainSHA: readmissionCurrentMain,
	}
	tree, head, conflict, err := engine.prepareWBCReadmissionCommit(context.Background(), candidate, generation)
	if err != nil || conflict || tree != readmissionTree || head != readmissionNewHead {
		t.Fatalf("prepared readmission: tree=%s head=%s conflict=%v err=%v", tree, head, conflict, err)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "merge-base --is-ancestor "+readmissionAdmittedBase+" "+readmissionCurrentMain) {
		t.Fatalf("admitted-base ancestry was not checked:\n%s", joined)
	}
	if strings.Contains(joined, "merge-base --is-ancestor "+readmissionAdmittedHead+" "+readmissionCurrentMain) {
		t.Fatalf("feature head was incorrectly required to be an ancestor of main:\n%s", joined)
	}
}

func TestPrepareWBCReadmissionFailsClosedOnCrossedAdmittedBase(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(t.TempDir(), "wb-core")
	engine := &Engine{}
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		switch {
		case path == project && command == "rev-parse HEAD":
			return readmissionCurrentMain, nil
		case command == "status --porcelain":
			return "", nil
		case path == workspace && command == "rev-parse HEAD":
			return readmissionAdmittedHead, nil
		case path == workspace && command == "branch --show-current":
			return "ao/wb-core-1/root", nil
		case path == workspace && command == "merge-base --is-ancestor "+readmissionCurrentMain+" "+readmissionCurrentMain:
			return "", nil
		case strings.HasPrefix(command, "merge-base --is-ancestor "):
			return "", errors.New("not an ancestor")
		default:
			return "", errors.New("unexpected git command: " + command)
		}
	}
	candidate := mergeCandidate{
		session: domain.SessionRecord{Metadata: domain.SessionMetadata{WorkspacePath: workspace}},
		project: domain.ProjectRecord{Path: project},
	}
	generation := domain.DCPWBCReadmissionGeneration{
		HeadRef: "ao/wb-core-1/root", AdmittedHeadSHA: readmissionAdmittedHead,
		AdmittedBaseSHA: readmissionAdmittedBase, MarkerMainSHA: readmissionCurrentMain, CurrentMainSHA: readmissionCurrentMain,
	}
	_, _, conflict, err := engine.prepareWBCReadmissionCommit(context.Background(), candidate, generation)
	if err == nil || !conflict || !strings.Contains(err.Error(), "admitted base is not an ancestor") {
		t.Fatalf("crossed base: conflict=%v err=%v", conflict, err)
	}
}

func canonicalReadmissionComment(t *testing.T, author string) (ports.SCMReleaseComment, wbcReadmissionMarker) {
	t.Helper()
	return canonicalReadmissionCommentFor(t, author, 903, 902, 901, 900, readmissionAdmittedHead, readmissionCurrentMain)
}

func canonicalReadmissionCommentFor(t *testing.T, author string, commentID, handoffID, admissionCheck, readyEvent int64, admittedHead, currentMain string) (ports.SCMReleaseComment, wbcReadmissionMarker) {
	t.Helper()
	values := map[string]any{
		"admission_check": admissionCheck, "admitted_head": admittedHead, "base": "main",
		"handoff_proof": handoffID, "head_ref": "ao/wb-core-1/root", "main": currentMain,
		"observed_head": admittedHead, "pr": 987, "ready_event": readyEvent,
		"reason": "base-behind-after-admission", "repo": "orenvlad-ai/wb-core", "scope": "scope:repo-only",
		"session": 1, "task": "task:standard", "version": wbcHandoffV2,
	}
	values["digest"] = proofValuesDigest(values)
	body := fmt.Sprintf("Release Train removed DCP release eligibility without updating or merging the admitted head `%s`. A fresh exact-head baseline, DCP review and FIFO admission are required.\n\n%s", admittedHead, proofMarker(wbcReadmissionProofMarker, values))
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	comment := ports.SCMReleaseComment{ID: commentID, Author: author, Body: body, CreatedAt: now, UpdatedAt: now}
	marker, err := parseWBCReadmissionComment(comment)
	if err != nil {
		t.Fatal(err)
	}
	return comment, marker
}

func canonicalHandoffComment(marker wbcReadmissionMarker, author string) ports.SCMReleaseComment {
	values := map[string]any{
		"admission_check": marker.admissionCheck, "base": marker.base, "head": marker.admittedHead,
		"head_ref": marker.headRef, "main": marker.main, "pr": marker.pr, "ready_event": marker.readyEvent,
		"release_check": 904, "repo": marker.repository, "scope": "scope:" + marker.scope,
		"session": marker.session, "task": "task:" + marker.task, "version": wbcHandoffV2,
	}
	values["digest"] = proofValuesDigest(values)
	now := marker.comment.CreatedAt
	return ports.SCMReleaseComment{
		ID: marker.handoffProof, Author: author, CreatedAt: now, UpdatedAt: now,
		Body: "Release Train accepted the DCP exact-head handoff without synchronizing or replacing the admitted branch.\n\n" + proofMarker(wbcHandoffMarker, values),
	}
}

func canonicalProductionComment(marker wbcReadmissionMarker, merge, author string) ports.SCMReleaseComment {
	values := map[string]any{
		"base": marker.base, "deploy_evidence": "sha256:" + strings.Repeat("a", 64), "deployed": merge,
		"handoff_proof": marker.handoffProof, "head": marker.admittedHead, "head_ref": marker.headRef,
		"merge": merge, "pr": marker.pr, "repo": marker.repository,
		"runtime_evidence": "sha256:" + strings.Repeat("b", 64), "scope": "scope:" + marker.scope,
		"service": "wb-core-registry-http.service", "session": marker.session,
		"target": "wb_core_eu_hosted_runtime_active", "task": "task:" + marker.task, "version": wbcHandoffV2,
	}
	values["digest"] = proofValuesDigest(values)
	now := marker.comment.CreatedAt
	return ports.SCMReleaseComment{
		ID: 905, Author: author, CreatedAt: now, UpdatedAt: now,
		Body: "Release Train deployed and independently read back the exact DCP live-runtime merge on the canonical production target.\n\n" + proofMarker("wb-core-dcp-release-production-proof", values),
	}
}

func TestWBCReadmissionProofAcceptsExactActionsIdentityAndRejectsEditedHandoff(t *testing.T) {
	for _, author := range []string{"github-actions", "github-actions[bot]"} {
		t.Run(author, func(t *testing.T) {
			_, marker := canonicalReadmissionComment(t, author)
			handoff := canonicalHandoffComment(marker, author)
			if !validWBCReferencedHandoff([]ports.SCMReleaseComment{handoff}, marker) {
				t.Fatal("exact referenced handoff was rejected")
			}
			handoff.Body = strings.Replace(handoff.Body, "release_check=904", "release_check=905", 1)
			if validWBCReferencedHandoff([]ports.SCMReleaseComment{handoff}, marker) {
				t.Fatal("edited referenced handoff bypassed its digest")
			}
		})
	}
	comment, _ := canonicalReadmissionComment(t, "github-actions[bot]")
	comment.Author = "orenvlad-ai"
	if _, err := parseWBCReadmissionComment(comment); err == nil {
		t.Fatal("non-Actions readmission marker was accepted")
	}
}

func TestWBCReadmissionLegacyV1IsAcceptedOnlyForRepoOnly(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "wbc-canary-v1", Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
		SessionID: "wb-core-1", CardNumber: 1, SourceBranch: "ao/wb-core-1/root",
		PRURL: "https://github.com/orenvlad-ai/wb-core/pull/987", PRNumber: 987, CurrentHeadSHA: readmissionAdmittedHead,
	}
	admission := domain.DCPReviewLabAdmission{
		ID: "admission-v1", SessionID: task.SessionID, PRURL: task.PRURL, PRNumber: task.PRNumber,
		TargetSHA: readmissionAdmittedHead, AdmittedBaseSHA: readmissionAdmittedBase, Status: domain.DCPAdmissionIncident,
	}
	body := fmt.Sprintf("Release Train removed DCP release eligibility without updating or merging the admitted head `%s`. A fresh exact-head baseline, DCP review and FIFO admission are required.\n\n<!-- %s base=main head=%s pr=987 reason=base-behind-after-admission version=%s -->", readmissionAdmittedHead, wbcReadmissionProofMarker, readmissionAdmittedHead, wbcHandoffV1)
	observation := ports.SCMReleaseObservation{
		Number: 987, URL: task.PRURL, State: "open", HeadRepository: task.Repository,
		HeadBranch: task.SourceBranch, HeadSHA: readmissionAdmittedHead, BaseBranch: "main", BaseSHA: readmissionAdmittedBase,
		ProviderMainSHA: readmissionCurrentMain, Author: "orenvlad-ai", Comments: []ports.SCMReleaseComment{{ID: 801, Author: "github-actions[bot]", Body: body, CreatedAt: now, UpdatedAt: now}},
	}
	marker, found, err := exactWBCReadmissionMarker(observation, task, admission)
	if err != nil || !found || marker.version != wbcHandoffV1 || marker.main != readmissionAdmittedBase || marker.providerMain != readmissionCurrentMain {
		t.Fatalf("legacy repo-only marker=%+v found=%v err=%v", marker, found, err)
	}

	task.Profile = "live-runtime"
	if _, _, err := exactWBCReadmissionMarker(observation, task, admission); err == nil {
		t.Fatal("legacy v1 marker authorized a live-runtime readmission")
	}
}

func TestWBCReadmissionMarkerRejectsCrossedOrDuplicateEvidence(t *testing.T) {
	comment, _ := canonicalReadmissionComment(t, "github-actions[bot]")
	comment.Body += "\n" + proofMarker(wbcReadmissionProofMarker, map[string]any{"pr": strconv.Itoa(987)})
	if _, err := parseWBCReadmissionComment(comment); err == nil {
		t.Fatal("duplicated marker was accepted")
	}
}

func TestWBCReadmissionSelectsCurrentGenerationWhilePreservingExactHistory(t *testing.T) {
	historicalComment, historicalMarker := canonicalReadmissionComment(t, "github-actions[bot]")
	historicalHandoff := canonicalHandoffComment(historicalMarker, "github-actions[bot]")
	currentMain := strings.Repeat("6", 40)
	currentComment, currentMarker := canonicalReadmissionCommentFor(
		t, "github-actions[bot]", 907, 906, 908, 909, readmissionNewHead, currentMain,
	)
	currentHandoff := canonicalHandoffComment(currentMarker, "github-actions[bot]")
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "wbc-canary-v1", Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
		SessionID: "wb-core-1", CardNumber: 1, SourceBranch: "ao/wb-core-1/root",
		PRURL: "https://github.com/orenvlad-ai/wb-core/pull/987", PRNumber: 987, CurrentHeadSHA: readmissionNewHead,
	}
	admission := domain.DCPReviewLabAdmission{
		ID: "admission-v2", SessionID: task.SessionID, PRURL: task.PRURL, PRNumber: task.PRNumber,
		TargetSHA: readmissionNewHead, AdmittedBaseSHA: readmissionCurrentMain, Status: domain.DCPAdmissionIncident,
	}
	observation := ports.SCMReleaseObservation{
		Number: 987, URL: task.PRURL, State: "open", HeadRepository: task.Repository,
		HeadBranch: task.SourceBranch, HeadSHA: readmissionNewHead, BaseBranch: "main", BaseSHA: currentMain,
		ProviderMainSHA: currentMain, Author: "orenvlad-ai", Comments: []ports.SCMReleaseComment{
			historicalHandoff, historicalComment, currentHandoff, currentComment,
		},
	}

	marker, found, err := exactWBCReadmissionMarker(observation, task, admission)
	if err != nil || !found || marker.comment.ID != currentComment.ID || marker.main != currentMain {
		t.Fatalf("current marker=%+v found=%v err=%v", marker, found, err)
	}

	observation.Comments = append(observation.Comments, currentComment)
	if _, _, err := exactWBCReadmissionMarker(observation, task, admission); err == nil {
		t.Fatal("duplicate current-generation marker was accepted")
	}
}

func TestWBCReadmissionUsesFreshProviderMainBeyondStalePRBase(t *testing.T) {
	comment, marker := canonicalReadmissionComment(t, "github-actions[bot]")
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "wbc-canary-v1", Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
		SessionID: "wb-core-1", CardNumber: 1, SourceBranch: "ao/wb-core-1/root",
		PRURL: "https://github.com/orenvlad-ai/wb-core/pull/987", PRNumber: 987, CurrentHeadSHA: readmissionAdmittedHead,
	}
	admission := domain.DCPReviewLabAdmission{
		ID: "admission-v1", SessionID: task.SessionID, PRURL: task.PRURL, PRNumber: task.PRNumber,
		TargetSHA: readmissionAdmittedHead, AdmittedBaseSHA: readmissionAdmittedBase, Status: domain.DCPAdmissionIncident,
	}
	freshMain := strings.Repeat("6", 40)
	observation := ports.SCMReleaseObservation{
		Number: 987, URL: task.PRURL, State: "open", HeadRepository: task.Repository,
		HeadBranch: task.SourceBranch, HeadSHA: readmissionAdmittedHead, BaseBranch: "main", BaseSHA: readmissionAdmittedBase,
		ProviderMainSHA: freshMain, Author: "orenvlad-ai",
		Comments: []ports.SCMReleaseComment{canonicalHandoffComment(marker, "github-actions[bot]"), comment},
	}
	got, found, err := exactWBCReadmissionMarker(observation, task, admission)
	if err != nil || !found || got.main != readmissionCurrentMain || got.providerMain != freshMain {
		t.Fatalf("marker=%+v found=%v err=%v", got, found, err)
	}
}

func TestWBCProductionProofFailsClosedOnMissingStaleOrDuplicateEvidence(t *testing.T) {
	task := domain.DCPReviewLabPolicyTask{
		TaskID: "wbc-live-v1", Target: "wb-core", Profile: "live-runtime", Repository: "orenvlad-ai/wb-core",
		PolicyVersion: domain.DCPWBCLiveRuntimePolicyVersion, SessionID: "wb-core-2", CardNumber: 2,
		SourceBranch: "ao/wb-core-2/root", PRNumber: 991, CurrentHeadSHA: readmissionAdmittedHead,
	}
	marker := wbcReadmissionMarker{
		repository: task.Repository, base: "main", task: "standard", scope: task.Profile,
		headRef: task.SourceBranch, session: task.CardNumber, pr: task.PRNumber,
		admittedHead: task.CurrentHeadSHA, main: readmissionCurrentMain,
		readyEvent: 910, admissionCheck: 911, handoffProof: 912,
		comment: ports.SCMReleaseComment{CreatedAt: time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC)},
	}
	handoff := canonicalHandoffComment(marker, "github-actions[bot]")
	production := canonicalProductionComment(marker, readmissionNewHead, "github-actions[bot]")
	observation := ports.SCMReleaseObservation{Comments: []ports.SCMReleaseComment{handoff, production}}
	if !validWBCProductionProof(observation, task, readmissionNewHead) {
		t.Fatal("exact production proof was rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ports.SCMReleaseObservation)
	}{
		{name: "missing", mutate: func(o *ports.SCMReleaseObservation) { o.Comments = o.Comments[:1] }},
		{name: "duplicate", mutate: func(o *ports.SCMReleaseObservation) { o.Comments = append(o.Comments, o.Comments[1]) }},
		{name: "foreign actor", mutate: func(o *ports.SCMReleaseObservation) { o.Comments[1].Author = "orenvlad-ai" }},
		{name: "edited evidence", mutate: func(o *ports.SCMReleaseObservation) {
			o.Comments[1].Body = strings.Replace(o.Comments[1].Body, "deployed="+readmissionNewHead, "deployed="+readmissionCurrentMain, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyObservation := observation
			copyObservation.Comments = append([]ports.SCMReleaseComment(nil), observation.Comments...)
			tc.mutate(&copyObservation)
			if validWBCProductionProof(copyObservation, task, readmissionNewHead) {
				t.Fatal("invalid production proof was accepted")
			}
		})
	}
}

func TestWBCPreparedReadmissionPushAndRestartReplayAreIdempotent(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	engine, store, scm := policyTargetFixture(t, spec, 1)
	store.policyTask.State, store.policyTask.ErrorCode = domain.DCPPolicyIncident, "release_state_drift"
	store.policyTask.AdmissionID, store.policyTask.ReviewRunID = "admission-old", "review-old"
	store.policyTask.CurrentHeadSHA = readmissionAdmittedHead
	store.admission = &domain.DCPReviewLabAdmission{
		ID: "admission-old", SessionID: store.session.ID, ReviewRunID: "review-old", PRURL: store.policyTask.PRURL,
		PRNumber: store.policyTask.PRNumber, TargetSHA: readmissionAdmittedHead, Status: domain.DCPAdmissionIncident,
	}
	store.readmission = &domain.DCPWBCReadmissionGeneration{
		Sequence: 1, GenerationID: "generation-1", TaskID: store.policyTask.TaskID, SessionID: store.session.ID,
		OldAdmissionID: store.admission.ID, PRURL: store.policyTask.PRURL, PRNumber: store.policyTask.PRNumber,
		Repository: spec.Repository, BaseBranch: spec.DefaultBranch, Scope: spec.Profile, HeadRef: store.policyTask.SourceBranch,
		AdmittedHeadSHA: readmissionAdmittedHead, CurrentMainSHA: readmissionCurrentMain,
		MergeTreeSHA: readmissionTree, NewHeadSHA: readmissionNewHead, Status: domain.DCPWBCReadmissionPrepared,
		LeaseID: "lease-1",
	}
	scm.releaseObservation = ports.SCMReleaseObservation{
		Number: int(store.policyTask.PRNumber), URL: store.policyTask.PRURL, State: "open", HeadRepository: spec.Repository,
		HeadBranch: store.policyTask.SourceBranch, HeadSHA: readmissionNewHead, BaseBranch: spec.DefaultBranch, Author: "orenvlad-ai",
	}
	localHead, pushes := readmissionAdmittedHead, 0
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		if path != store.policyTask.WorktreePath {
			return "", errors.New("foreign path")
		}
		switch command {
		case "rev-parse HEAD":
			return localHead, nil
		case "-c core.hooksPath=/dev/null merge --ff-only " + readmissionNewHead:
			localHead = readmissionNewHead
			return "", nil
		case "status --porcelain":
			return "", nil
		case "-c core.hooksPath=/dev/null push origin " + readmissionNewHead + ":refs/heads/" + store.policyTask.SourceBranch:
			pushes++
			return "", nil
		default:
			return "", errors.New("unexpected git command: " + command)
		}
	}
	handled, err := engine.reconcileWBCReadmission(context.Background(), store.session.ID)
	if err != nil || !handled || pushes != 1 || store.readmission.Status != domain.DCPWBCReadmissionHeadPushed ||
		store.policyTask.State != domain.DCPPolicyCIWaiting || store.policyTask.CurrentHeadSHA != readmissionNewHead {
		t.Fatalf("first push handled=%v pushes=%d task=%+v generation=%+v err=%v", handled, pushes, store.policyTask, store.readmission, err)
	}
	restarted := New(store, scm, engine.dataDir)
	restarted.git = engine.git
	handled, err = restarted.reconcileWBCReadmission(context.Background(), store.session.ID)
	if err != nil || handled || pushes != 1 {
		t.Fatalf("restart replay handled=%v pushes=%d err=%v", handled, pushes, err)
	}
}

func TestWBCPreparedReadmissionNeverAdvancesDirtyWorktree(t *testing.T) {
	engine := &Engine{}
	task := domain.DCPReviewLabPolicyTask{WorktreePath: "/tmp/wb-core-1", SourceBranch: "ao/wb-core-1/root"}
	generation := domain.DCPWBCReadmissionGeneration{
		AdmittedHeadSHA: readmissionAdmittedHead, NewHeadSHA: readmissionNewHead,
	}
	mutated := false
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		if path != task.WorktreePath {
			return "", errors.New("foreign path")
		}
		switch strings.Join(args, " ") {
		case "rev-parse HEAD":
			return readmissionAdmittedHead, nil
		case "status --porcelain":
			return " M owner-change.txt", nil
		default:
			mutated = true
			return "", nil
		}
	}
	if err := engine.pushWBCReadmissionHead(context.Background(), task, generation); err == nil {
		t.Fatal("dirty readmission worktree was advanced")
	}
	if mutated {
		t.Fatal("readmission mutated the worktree before proving it clean")
	}
}

func TestWBCHeadPushedRestartReplaysOnlyModelFreeReviewEligibility(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	engine, store, _ := policyTargetFixture(t, spec, 1)
	store.policyTask.State = domain.DCPPolicyCIWaiting
	store.policyTask.CurrentHeadSHA = readmissionNewHead
	store.readmission = &domain.DCPWBCReadmissionGeneration{
		Sequence: 1, GenerationID: "generation-1", TaskID: store.policyTask.TaskID, SessionID: store.session.ID,
		Status: domain.DCPWBCReadmissionHeadPushed, NewHeadSHA: readmissionNewHead,
	}
	triggers := 0
	engine.SetModelFreeReviewTrigger(func(_ context.Context, id domain.SessionID) error {
		if id != store.session.ID {
			t.Fatalf("review trigger session=%s", id)
		}
		triggers++
		return nil
	})
	handled, err := engine.reconcileWBCReadmission(context.Background(), store.session.ID)
	if err != nil || !handled || triggers != 1 {
		t.Fatalf("head-pushed restart handled=%v triggers=%d err=%v", handled, triggers, err)
	}
}
