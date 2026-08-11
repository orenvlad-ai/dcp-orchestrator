package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type stage2Store struct {
	*fakeStore
	incident domain.DCPReleaseArbiterIncident
	peer     domain.DCPReviewLabAdmission
}

func (s *stage2Store) EnqueueDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error) {
	if s.admission != nil {
		return *s.admission, false, nil
	}
	admission.Sequence = 2
	s.admission = &admission
	return admission, true, nil
}

func (s *stage2Store) ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error) {
	rows := []domain.DCPReviewLabAdmission{s.peer}
	if s.admission != nil {
		rows = append(rows, *s.admission)
	}
	return rows, nil
}

func (s *stage2Store) GetDCPReviewLabAdmissionByID(_ context.Context, id string) (domain.DCPReviewLabAdmission, bool, error) {
	if s.admission != nil && s.admission.ID == id {
		return *s.admission, true, nil
	}
	if s.peer.ID == id {
		return s.peer, true, nil
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (s *stage2Store) OpenDCPReleaseArbiterIncident(_ context.Context, _ domain.DCPReviewLabAdmission, incident domain.DCPReleaseArbiterIncident) (domain.DCPReleaseArbiterIncident, bool, error) {
	if s.incident.IncidentID != "" {
		if !sameArbiterImmutable(s.incident, incident) {
			return domain.DCPReleaseArbiterIncident{}, false, errors.New("identity drift")
		}
		return s.incident, false, nil
	}
	s.incident = incident
	return incident, true, nil
}

func (s *stage2Store) GetDCPReleaseArbiterIncidentByID(_ context.Context, id string) (domain.DCPReleaseArbiterIncident, bool, error) {
	return s.arbiterBy(func() bool { return s.incident.IncidentID == id })
}
func (s *stage2Store) GetDCPReleaseArbiterIncidentByAdmission(_ context.Context, id string) (domain.DCPReleaseArbiterIncident, bool, error) {
	return s.arbiterBy(func() bool { return s.incident.AdmissionID == id })
}
func (s *stage2Store) GetDCPReleaseArbiterIncidentBySession(_ context.Context, id domain.SessionID) (domain.DCPReleaseArbiterIncident, bool, error) {
	return s.arbiterBy(func() bool { return s.incident.SessionID == id })
}
func (s *stage2Store) arbiterBy(match func() bool) (domain.DCPReleaseArbiterIncident, bool, error) {
	if s.incident.IncidentID == "" || !match() {
		return domain.DCPReleaseArbiterIncident{}, false, nil
	}
	return s.incident, true, nil
}
func (s *stage2Store) ListDCPReleaseArbiterIncidents(context.Context) ([]domain.DCPReleaseArbiterIncident, error) {
	if s.incident.IncidentID == "" {
		return nil, nil
	}
	return []domain.DCPReleaseArbiterIncident{s.incident}, nil
}
func (s *stage2Store) StartDCPReleaseArbiterCall(_ context.Context, incident domain.DCPReleaseArbiterIncident, now time.Time) (bool, error) {
	if s.incident.IncidentID != incident.IncidentID || s.incident.Status != domain.DCPArbiterRequested || s.incident.ModelCallCount != 0 {
		return false, nil
	}
	s.incident.Status, s.incident.ModelCallCount, s.incident.UpdatedAt = domain.DCPArbiterRunning, 1, now
	return true, nil
}
func (s *stage2Store) FailDCPReleaseArbiterPreflight(_ context.Context, id, code string, now time.Time) (bool, error) {
	if s.incident.IncidentID != id || s.incident.Status != domain.DCPArbiterRequested {
		return false, nil
	}
	s.incident.Status, s.incident.ErrorCode, s.incident.UpdatedAt = domain.DCPArbiterPreflightFailed, code, now
	return true, nil
}
func (s *stage2Store) RecordDCPReleaseArbiterDecision(_ context.Context, incident domain.DCPReleaseArbiterIncident, body, digest string, safe bool, code string, now time.Time) (bool, error) {
	if s.incident.IncidentID != incident.IncidentID || s.incident.Status != domain.DCPArbiterRunning || s.incident.DecisionJSON != "" {
		return false, nil
	}
	s.incident.DecisionJSON, s.incident.DecisionDigest, s.incident.ErrorCode, s.incident.UpdatedAt = body, digest, code, now
	s.incident.Status = domain.DCPArbiterDecided
	if safe {
		s.incident.Status = domain.DCPArbiterSafeStopped
	}
	return true, nil
}
func (s *stage2Store) ConsumeDCPReleaseArbiterRepair(_ context.Context, incident domain.DCPReleaseArbiterIncident, now time.Time) (bool, error) {
	if s.incident.IncidentID != incident.IncidentID || s.incident.Status != domain.DCPArbiterDecided || s.incident.RecoveryWakeCount != 0 {
		return false, nil
	}
	s.incident.Status, s.incident.RecoveryWakeCount = domain.DCPArbiterRepairing, 1
	s.incident.RecoveryOwnerSessionID, s.incident.RecoveryPath, s.incident.UpdatedAt = s.incident.SessionID, "same_worker_conflict_repair", now
	return true, nil
}
func (s *stage2Store) FailDCPReleaseArbiterCall(_ context.Context, id, code string, now time.Time) (bool, error) {
	if s.incident.IncidentID != id || s.incident.Status != domain.DCPArbiterRunning {
		return false, nil
	}
	s.incident.Status, s.incident.ErrorCode, s.incident.UpdatedAt = domain.DCPArbiterFailed, code, now
	return true, nil
}
func (s *stage2Store) FailDCPReleaseArbiterAfterDecision(_ context.Context, id, code string, now time.Time) (bool, error) {
	if s.incident.IncidentID != id || (s.incident.Status != domain.DCPArbiterDecided && s.incident.Status != domain.DCPArbiterRepairing) {
		return false, nil
	}
	s.incident.Status, s.incident.ErrorCode, s.incident.UpdatedAt = domain.DCPArbiterFailed, code, now
	return true, nil
}
func (s *stage2Store) RebindDCPAdmissionAfterArbiterRepair(context.Context, domain.DCPReviewLabAdmission, domain.DCPReleaseArbiterIncident, domain.ReviewRun, string, time.Time) (bool, error) {
	return false, nil
}

type stage2Launcher struct {
	root      string
	preflight int
	launches  int
	alive     bool
}

func (l *stage2Launcher) Preflight(context.Context, domain.DCPReleaseArbiterIncident) error {
	l.preflight++
	return nil
}
func (l *stage2Launcher) Launch(context.Context, domain.DCPReleaseArbiterIncident) error {
	l.launches++
	l.alive = true
	return nil
}
func (l *stage2Launcher) ProcessAlive(context.Context, domain.DCPReleaseArbiterIncident) (bool, error) {
	return l.alive, nil
}
func (l *stage2Launcher) ResultPath(incident domain.DCPReleaseArbiterIncident) (string, error) {
	return filepath.Join(l.root, incident.IncidentID, "result.json"), nil
}

func TestStage2ConflictLaunchDecisionAndRestartReplayAreSingleFlight(t *testing.T) {
	engine, base, scm := fixture(t)
	base.includeCohortPeer = false
	oldWorkspace := base.session.Metadata.WorkspacePath
	dataDir := engine.dataDir
	workspace := filepath.Join(dataDir, "worktrees", ProjectID, ArbiterSessionA)
	privateGit := filepath.Join(base.project.Path, ".git", "worktrees", ArbiterSessionA)
	for _, path := range []string{workspace, privateGit} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = oldWorkspace
	base.session.ID = ArbiterSessionA
	base.session.DisplayName = TaskDisplayPrefix + ArbiterTaskA
	base.session.Metadata.WorkspacePath = workspace
	base.session.Metadata.Branch = "ao/dcp-review-lab-11/root"
	base.session.Metadata.Prompt = TaskPromptPrefix + ArbiterTaskA + ": Create canary/i13-arbiter-conflict.txt with the exact line arbiter-a."
	base.pr.SessionID, base.pr.Number = ArbiterSessionA, 11
	base.pr.URL, base.pr.HTMLURL = "https://github.com/orenvlad-ai/dcp-review-lab/pull/11", "https://github.com/orenvlad-ai/dcp-review-lab/pull/11"
	base.pr.SourceBranch, base.pr.BaseSHA = base.session.Metadata.Branch, testBase
	base.run.ID, base.run.ReviewID, base.run.BatchID, base.run.SessionID = "run-11", "review-11", "batch-11", ArbiterSessionA
	base.run.PRURL, base.run.TargetSHA = base.pr.URL, testHead
	currentMain, treeSHA := strings.Repeat("b", 40), strings.Repeat("1", 40)
	scm.observation.PR.URL, scm.observation.PR.Number = base.pr.URL, 11
	scm.observation.PR.HTMLURL = base.pr.URL
	scm.observation.PR.SourceBranch, scm.observation.PR.HeadSHA = base.pr.SourceBranch, testHead
	scm.observation.PR.BaseSHA = currentMain
	scm.observation.PR.ProviderMergeable, scm.observation.PR.ProviderMergeStateStatus = "CONFLICTING", "DIRTY"
	scm.observation.CI.HeadSHA = testHead
	scm.observation.CI.Checks[0].ProviderID = "check-11"
	scm.observation.Mergeability.State, scm.observation.Mergeability.Mergeable = string(domain.MergeConflicting), false
	store := &stage2Store{fakeStore: base, peer: domain.DCPReviewLabAdmission{
		Sequence: 1, ID: "admission-12", ReviewRunID: "run-12", ReviewID: "review-12", SessionID: ArbiterSessionB,
		PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/12", PRNumber: 12, TargetSHA: strings.Repeat("2", 40),
		ReviewBaseSHA: testBase, Status: domain.DCPAdmissionSucceeded, LeaseID: "merge-12", MergeCommitSHA: currentMain,
	}}
	base.admission = &domain.DCPReviewLabAdmission{
		Sequence: 2, ID: "admission-11", ReviewRunID: base.run.ID, ReviewID: base.run.ReviewID,
		SessionID: ArbiterSessionA, PRURL: base.pr.URL, PRNumber: 11, TargetSHA: testHead,
		ReviewBaseSHA: testBase, Status: domain.DCPAdmissionWaiting, CreatedAt: time.Unix(10, 0).UTC(), UpdatedAt: time.Unix(10, 0).UTC(),
	}
	engine.store = store
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		switch cmd {
		case "status --porcelain":
			return "", nil
		case "remote":
			return "origin", nil
		case "remote get-url origin":
			return RepositoryURL, nil
		case "fetch --no-tags origin main":
			return "", nil
		case "merge-base --is-ancestor " + testBase + " " + currentMain:
			return "", nil
		case "rev-parse origin/main", "rev-parse HEAD":
			if path == base.project.Path {
				return currentMain, nil
			}
		case "rev-parse --show-toplevel":
			return path, nil
		case "branch --show-current":
			if path == base.project.Path {
				return TargetBranch, nil
			}
			return base.session.Metadata.Branch, nil
		case "log --reverse --format=%H%x00%T " + testBase + ".." + testHead:
			return testHead + "\x00" + treeSHA, nil
		case "diff --binary --full-index " + testBase + ".." + testHead:
			return "diff --git a/" + arbiterConflictPath + " b/" + arbiterConflictPath, nil
		case "diff --name-status " + testBase + ".." + testHead:
			return "A\t" + arbiterConflictPath, nil
		case "rev-parse " + testHead + "^{tree}":
			return treeSHA, nil
		case "merge-tree --write-tree " + currentMain + " " + testHead:
			return "CONFLICT (add/add): " + arbiterConflictPath, errors.New("conflict")
		case "show " + testHead + ":" + arbiterConflictPath:
			return "arbiter-a", nil
		case "show " + currentMain + ":" + arbiterConflictPath:
			return "arbiter-b", nil
		case "diff --name-only " + testBase + ".." + testHead, "diff --name-only " + testBase + ".." + currentMain:
			return arbiterConflictPath, nil
		case "rev-parse --path-format=absolute --git-common-dir":
			return filepath.Join(base.project.Path, ".git"), nil
		case "rev-parse --path-format=absolute --absolute-git-dir":
			return privateGit, nil
		}
		return "", errors.New("unexpected git: " + path + " :: " + cmd)
	}
	launcher := &stage2Launcher{root: t.TempDir()}
	engine.SetArbiterLauncher(launcher)
	wakes := 0
	engine.SetRefreshWaker(func(_ context.Context, id domain.SessionID, prompt string) error {
		wakes++
		if id != ArbiterSessionA || !strings.Contains(prompt, store.incident.ScopeDigest) || !strings.Contains(prompt, arbiterConflictPath) {
			t.Fatalf("wake id=%s prompt=%q", id, prompt)
		}
		return nil
	})
	if err := engine.Try(context.Background(), ArbiterSessionA); err != nil {
		t.Fatal(err)
	}
	if base.admission == nil || base.admission.Status != domain.DCPAdmissionIncident || store.incident.Status != domain.DCPArbiterRunning ||
		store.incident.ModelCallCount != 1 || launcher.preflight != 1 || launcher.launches != 1 || wakes != 0 {
		t.Fatalf("after conflict admission=%+v incident=%+v launcher=%+v wakes=%d", base.admission, store.incident, launcher, wakes)
	}
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if launcher.launches != 1 || store.incident.ModelCallCount != 1 || wakes != 0 {
		t.Fatalf("pre-verdict restart duplicated work: incident=%+v launcher=%+v wakes=%d", store.incident, launcher, wakes)
	}
	decision := validAssignDecision(store.incident)
	decision.EvidenceDigests = []string{store.incident.ScopeDigest, store.incident.MechanicalDigest}
	data, _ := json.Marshal(decision)
	resultPath, err := launcher.ResultPath(store.incident)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(resultPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	launcher.alive = false
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.incident.Status != domain.DCPArbiterRepairing || store.incident.RecoveryWakeCount != 1 || wakes != 1 {
		t.Fatalf("accepted decision = %+v wakes=%d", store.incident, wakes)
	}
	if err := engine.SubmitArbiterDecision(context.Background(), store.incident.IncidentID, data); err != nil {
		t.Fatalf("exact duplicate should be inert: %v", err)
	}
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if launcher.launches != 1 || store.incident.ModelCallCount != 1 || store.incident.RecoveryWakeCount != 1 || wakes != 1 {
		t.Fatalf("post-decision restart duplicated work: incident=%+v launcher=%+v wakes=%d", store.incident, launcher, wakes)
	}
}
