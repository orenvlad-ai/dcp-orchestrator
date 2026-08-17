package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	testHead  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBase  = "cccccccccccccccccccccccccccccccccccccccc"
	testMerge = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeStore struct {
	session           domain.SessionRecord
	project           domain.ProjectRecord
	pr                domain.PullRequest
	run               domain.ReviewRun
	claims            int
	admission         *domain.DCPReviewLabAdmission
	includeCohortPeer bool
	policyTask        *domain.DCPReviewLabPolicyTask
}

func (f *fakeStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return f.session, true, nil
}
func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return []domain.SessionRecord{f.session}, nil
}
func (f *fakeStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return f.project, true, nil
}
func (f *fakeStore) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	return []domain.PullRequest{f.pr}, nil
}
func (f *fakeStore) ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error) {
	return []domain.ReviewRun{f.run}, nil
}
func (f *fakeStore) EnqueueDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission != nil {
		return *f.admission, false, nil
	}
	admission.Sequence = 1
	f.admission = &admission
	return admission, true, nil
}
func (f *fakeStore) GetDCPReviewLabAdmissionByRun(_ context.Context, runID string) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.ReviewRunID != runID {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) GetClaimedDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.Status != domain.DCPAdmissionClaimed {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) GetNextWaitingDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.Status != domain.DCPAdmissionWaiting {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error) {
	var rows []domain.DCPReviewLabAdmission
	if f.includeCohortPeer {
		rows = append(rows, domain.DCPReviewLabAdmission{
			Sequence: 99, ID: "cohort-peer", ReviewRunID: "cohort-peer-run", ReviewID: "cohort-peer-review",
			SessionID: AdmissionSessionB, PRURL: "https://github.com/orenvlad-ai/dcp-review-lab/pull/99",
			PRNumber: 99, TargetSHA: testHead, ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase,
			Status: domain.DCPAdmissionSucceeded, LeaseID: "peer-lease", MergeCommitSHA: testMerge,
		})
	}
	if f.admission != nil {
		rows = append(rows, *f.admission)
	}
	return rows, nil
}
func (f *fakeStore) GetRefreshingDCPReviewLabAdmissionBySession(_ context.Context, sessionID domain.SessionID) (domain.DCPReviewLabAdmission, bool, error) {
	if f.admission == nil || f.admission.SessionID != sessionID || f.admission.Status != domain.DCPAdmissionRefreshing {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	return *f.admission, true, nil
}
func (f *fakeStore) RecoverDCPReviewLabCanonicalBaseIncident(_ context.Context, admission domain.DCPReviewLabAdmission, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionIncident ||
		f.admission.ErrorCode != "canonical_main_diverged" || f.admission.RefreshWakeCount != 0 || f.admission.RecoveredIncidentPacket != "" {
		return false, nil
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionWaiting, "", ""
	f.admission.ErrorCode, f.admission.RecoveredIncidentPacket, f.admission.IncidentPacket = "", f.admission.IncidentPacket, ""
	f.admission.UpdatedAt = now
	return true, nil
}
func (f *fakeStore) ResumeDCPReviewLabAdmissionAfterRefresh(_ context.Context, admission domain.DCPReviewLabAdmission, run domain.ReviewRun, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionRefreshing || f.admission.RefreshWakeCount != 1 || strings.EqualFold(f.admission.TargetSHA, run.TargetSHA) {
		return false, nil
	}
	f.admission.ReviewRunID, f.admission.ReviewID, f.admission.TargetSHA = run.ID, run.ReviewID, run.TargetSHA
	f.admission.ReviewBaseSHA, f.admission.Status, f.admission.LeaseID, f.admission.UpdatedAt = baseSHA, domain.DCPAdmissionWaiting, "", now
	return true, nil
}
func (f *fakeStore) ClaimDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionWaiting || f.run.TerminalMergeStatus != "" {
		return false, nil
	}
	f.claims++
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA, f.admission.UpdatedAt = domain.DCPAdmissionClaimed, leaseID, baseSHA, now
	f.run.TerminalMergeStatus = "running"
	return true, nil
}
func (f *fakeStore) ClaimDCPReleaseTrainAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission, task domain.DCPReviewLabPolicyTask, leaseID, baseSHA string, now time.Time) (bool, error) {
	if f.policyTask == nil || f.policyTask.TaskID != task.TaskID || f.policyTask.State != domain.DCPPolicyAdmissionWait {
		return false, nil
	}
	claimed, err := f.ClaimDCPReviewLabAdmission(ctx, admission, leaseID, baseSHA, now)
	if claimed {
		f.policyTask.State = domain.DCPPolicyReleaseWaiting
		f.policyTask.Revision++
	}
	return claimed, err
}
func (f *fakeStore) CompleteDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, sha string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionClaimed || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.admission.Status, f.admission.MergeCommitSHA, f.admission.UpdatedAt = domain.DCPAdmissionSucceeded, sha, now
	f.run.TerminalMergeStatus, f.run.TerminalMergeCommitSHA = "succeeded", sha
	return true, nil
}
func (f *fakeStore) FailDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, code string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionClaimed || f.run.TerminalMergeStatus != "running" {
		return false, nil
	}
	f.admission.Status, f.admission.ErrorCode, f.admission.UpdatedAt = domain.DCPAdmissionFailed, code, now
	f.run.TerminalMergeStatus, f.run.TerminalMergeError = "failed", code
	return true, nil
}
func (f *fakeStore) StartDCPReviewLabRefresh(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status != domain.DCPAdmissionWaiting || f.admission.RefreshWakeCount != 0 {
		return false, nil
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionRefreshing, leaseID, baseSHA
	f.admission.RefreshWakeCount, f.admission.UpdatedAt = 1, now
	return true, nil
}
func (f *fakeStore) RecordDCPReviewLabIncident(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA, code, packet string, now time.Time) (bool, error) {
	if f.admission == nil || f.admission.ID != admission.ID || f.admission.Status == domain.DCPAdmissionSucceeded || f.admission.Status == domain.DCPAdmissionIncident {
		return false, nil
	}
	if f.admission.Status == domain.DCPAdmissionClaimed {
		f.run.TerminalMergeStatus, f.run.TerminalMergeError = "failed", code
	}
	f.admission.Status, f.admission.LeaseID, f.admission.AdmittedBaseSHA = domain.DCPAdmissionIncident, leaseID, baseSHA
	f.admission.ErrorCode, f.admission.IncidentPacket, f.admission.UpdatedAt = code, packet, now
	return true, nil
}
func (f *fakeStore) GetDCPReviewLabPolicyTaskBySession(_ context.Context, id domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error) {
	if f.policyTask == nil || f.policyTask.SessionID != id {
		return domain.DCPReviewLabPolicyTask{}, false, nil
	}
	return *f.policyTask, true, nil
}
func (f *fakeStore) UpdateDCPReviewLabPolicyTaskCAS(_ context.Context, current, next domain.DCPReviewLabPolicyTask) (bool, error) {
	if f.policyTask == nil || f.policyTask.TaskID != current.TaskID || f.policyTask.Revision != current.Revision || f.policyTask.State != current.State {
		return false, nil
	}
	next.Revision++
	*f.policyTask = next
	return true, nil
}
func (f *fakeStore) EnqueueDCPReviewLabPolicyAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission, task domain.DCPReviewLabPolicyTask) (domain.DCPReviewLabAdmission, bool, error) {
	row, created, err := f.EnqueueDCPReviewLabAdmission(ctx, admission)
	if err != nil {
		return row, created, err
	}
	if f.policyTask == nil || f.policyTask.TaskID != task.TaskID || f.policyTask.State != domain.DCPPolicyAdmissionWait {
		return domain.DCPReviewLabAdmission{}, false, errors.New("policy task unavailable")
	}
	f.policyTask.AdmissionID = row.ID
	f.policyTask.Revision++
	return row, created, nil
}
func (f *fakeStore) CompleteDCPReviewLabPolicyAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission, task domain.DCPReviewLabPolicyTask, sha string, now time.Time) (bool, error) {
	completed, err := f.CompleteDCPReviewLabAdmission(ctx, admission, sha, now)
	if completed && f.policyTask != nil && f.policyTask.TaskID == task.TaskID {
		f.policyTask.State, f.policyTask.MergeCommitSHA = domain.DCPPolicyMerged, sha
		f.policyTask.Revision++
	}
	return completed, err
}
func (f *fakeStore) RecordDCPReviewLabPolicyIncident(ctx context.Context, admission domain.DCPReviewLabAdmission, task domain.DCPReviewLabPolicyTask, leaseID, baseSHA, code, packet string, now time.Time) (bool, error) {
	recorded, err := f.RecordDCPReviewLabIncident(ctx, admission, leaseID, baseSHA, code, packet, now)
	if recorded && f.policyTask != nil && f.policyTask.TaskID == task.TaskID {
		f.policyTask.State, f.policyTask.ErrorCode, f.policyTask.IncidentPacket = domain.DCPPolicyIncident, code, packet
		f.policyTask.Revision++
	}
	return recorded, err
}

type fakeSCM struct {
	observation        ports.SCMObservation
	review             ports.SCMReviewObservation
	mergeErr           error
	mergeCalls         int
	expectedHead       string
	expectedRepo       string
	mergeSHA           string
	releaseReadyCalls  int
	releaseObservation ports.SCMReleaseObservation
}

type queueSelectionStore struct {
	*fakeStore
	rows  []domain.DCPReviewLabAdmission
	tasks map[domain.SessionID]domain.DCPReviewLabPolicyTask
}

type tripleQueueStore struct {
	*fakeStore
	sessions   map[domain.SessionID]domain.SessionRecord
	prs        map[domain.SessionID]domain.PullRequest
	runs       map[domain.SessionID]domain.ReviewRun
	tasks      map[domain.SessionID]domain.DCPReviewLabPolicyTask
	rows       []domain.DCPReviewLabAdmission
	claims     int
	mergeOrder []int64
}

func (s *tripleQueueStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	session, ok := s.sessions[id]
	return session, ok, nil
}

func (s *tripleQueueStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	result := make([]domain.SessionRecord, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result, nil
}

func (s *tripleQueueStore) ListPRsBySession(_ context.Context, id domain.SessionID) ([]domain.PullRequest, error) {
	pr, ok := s.prs[id]
	if !ok {
		return nil, nil
	}
	return []domain.PullRequest{pr}, nil
}

func (s *tripleQueueStore) ListReviewRunsBySession(_ context.Context, id domain.SessionID) ([]domain.ReviewRun, error) {
	run, ok := s.runs[id]
	if !ok {
		return nil, nil
	}
	return []domain.ReviewRun{run}, nil
}

func (s *tripleQueueStore) GetDCPReviewLabAdmissionByRun(_ context.Context, runID string) (domain.DCPReviewLabAdmission, bool, error) {
	for _, row := range s.rows {
		if row.ReviewRunID == runID {
			return row, true, nil
		}
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (s *tripleQueueStore) GetClaimedDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	for _, row := range s.rows {
		if row.Status == domain.DCPAdmissionClaimed {
			return row, true, nil
		}
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (s *tripleQueueStore) ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error) {
	return append([]domain.DCPReviewLabAdmission(nil), s.rows...), nil
}

func (s *tripleQueueStore) ClaimDCPReviewLabAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	for _, row := range s.rows {
		if row.Status == domain.DCPAdmissionClaimed {
			return false, nil
		}
	}
	for i := range s.rows {
		if s.rows[i].ID != admission.ID || s.rows[i].Status != domain.DCPAdmissionWaiting {
			continue
		}
		run := s.runs[admission.SessionID]
		if run.TerminalMergeStatus != "" {
			return false, nil
		}
		s.rows[i].Status, s.rows[i].LeaseID, s.rows[i].AdmittedBaseSHA, s.rows[i].UpdatedAt = domain.DCPAdmissionClaimed, leaseID, baseSHA, now
		run.TerminalMergeStatus = "running"
		s.runs[admission.SessionID] = run
		s.claims++
		return true, nil
	}
	return false, nil
}

func (s *tripleQueueStore) GetDCPReviewLabPolicyTaskBySession(_ context.Context, id domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error) {
	task, ok := s.tasks[id]
	return task, ok, nil
}

func (s *tripleQueueStore) CompleteDCPReviewLabPolicyAdmission(_ context.Context, admission domain.DCPReviewLabAdmission, task domain.DCPReviewLabPolicyTask, sha string, now time.Time) (bool, error) {
	for i := range s.rows {
		if s.rows[i].ID != admission.ID || s.rows[i].Status != domain.DCPAdmissionClaimed || s.rows[i].LeaseID != admission.LeaseID {
			continue
		}
		current := s.tasks[admission.SessionID]
		run := s.runs[admission.SessionID]
		if current.TaskID != task.TaskID || current.State != domain.DCPPolicyAdmissionWait || run.TerminalMergeStatus != "running" {
			return false, nil
		}
		s.rows[i].Status, s.rows[i].MergeCommitSHA, s.rows[i].UpdatedAt = domain.DCPAdmissionSucceeded, sha, now
		current.State, current.MergeCommitSHA, current.Revision = domain.DCPPolicyMerged, sha, current.Revision+1
		run.TerminalMergeStatus, run.TerminalMergeCommitSHA = "succeeded", sha
		s.tasks[admission.SessionID], s.runs[admission.SessionID] = current, run
		s.mergeOrder = append(s.mergeOrder, admission.Sequence)
		return true, nil
	}
	return false, nil
}

type tripleQueueSCM struct {
	store       *tripleQueueStore
	currentBase string
	mergeSHAs   map[int]string
	fetchedBase []string
	mergePRs    []int
	files       map[string]string
}

func (s *tripleQueueSCM) FetchPullRequests(_ context.Context, refs []ports.SCMPRRef) ([]ports.SCMObservation, error) {
	if len(refs) != 1 {
		return nil, errors.New("expected one exact PR refresh")
	}
	var pr domain.PullRequest
	for _, candidate := range s.store.prs {
		if candidate.Number == refs[0].Number && candidate.URL == refs[0].URL {
			pr = candidate
			break
		}
	}
	if pr.URL == "" {
		return nil, errors.New("unknown PR refresh")
	}
	s.fetchedBase = append(s.fetchedBase, s.currentBase)
	return []ports.SCMObservation{{
		Fetched: true, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
		PR: ports.SCMPRObservation{
			URL: pr.URL, Number: pr.Number, HeadRepo: RepositoryFullName, SourceBranch: pr.SourceBranch,
			TargetBranch: TargetBranch, HeadSHA: pr.HeadSHA, BaseSHA: s.currentBase,
			State: string(domain.PRStateOpen), ProviderState: "OPEN", Author: "orenvlad-ai", HTMLURL: pr.URL,
			ProviderMergeable: "MERGEABLE", ProviderMergeStateStatus: "CLEAN",
		},
		CI: ports.SCMCIObservation{Summary: string(domain.CIPassing), HeadSHA: pr.HeadSHA, Checks: []ports.SCMCheckObservation{{
			Name: RequiredCheckName, Status: string(domain.PRCheckPassed), Conclusion: "success",
			URL: fmt.Sprintf("https://github.com/orenvlad-ai/dcp-review-lab/actions/runs/%d/job/%d", pr.Number, pr.Number),
		}}},
		Mergeability: ports.SCMMergeabilityObservation{State: string(domain.MergeMergeable), Mergeable: true},
	}}, nil
}

func (s *tripleQueueSCM) FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error) {
	return ports.SCMReviewObservation{Decision: string(domain.ReviewNone)}, nil
}

func (s *tripleQueueSCM) MergePullRequest(_ context.Context, request ports.SCMMergeRequest) (ports.SCMMergeResult, error) {
	var pr domain.PullRequest
	for _, candidate := range s.store.prs {
		if candidate.Number == request.PR.Number {
			pr = candidate
			break
		}
	}
	if pr.URL == "" || request.ExpectedHeadSHA != pr.HeadSHA || request.Method != ports.SCMMergeSquash {
		return ports.SCMMergeResult{}, errors.New("unexpected triple merge request")
	}
	sha := s.mergeSHAs[pr.Number]
	if !validSHA(sha) {
		return ports.SCMMergeResult{}, errors.New("missing triple merge SHA")
	}
	s.currentBase = sha
	s.mergePRs = append(s.mergePRs, pr.Number)
	s.files[fmt.Sprintf("qualification/manual-triple-20260815-%c.txt", 'a'+rune(pr.Number-25))] = fmt.Sprintf("manual-triple-%c=ok", 'a'+rune(pr.Number-25))
	return ports.SCMMergeResult{MergeCommitSHA: sha}, nil
}

func (s *queueSelectionStore) ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error) {
	return append([]domain.DCPReviewLabAdmission(nil), s.rows...), nil
}

func (s *queueSelectionStore) GetDCPReviewLabAdmissionByRun(_ context.Context, runID string) (domain.DCPReviewLabAdmission, bool, error) {
	for _, row := range s.rows {
		if row.ReviewRunID == runID {
			return row, true, nil
		}
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (s *queueSelectionStore) GetDCPReviewLabPolicyTaskBySession(_ context.Context, id domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error) {
	task, ok := s.tasks[id]
	return task, ok, nil
}

func TestNextPendingSkipsTerminalHumanGateThenSelectsTwentyThroughTwentyTwoFIFO(t *testing.T) {
	const packet = `{"reason":"merge_conflict_or_ambiguity"}`
	rows := []domain.DCPReviewLabAdmission{
		{Sequence: 19, ID: "admission-19", ReviewRunID: "run-19", SessionID: "dcp-review-lab-27", TargetSHA: strings.Repeat("1", 40), Status: domain.DCPAdmissionIncident, ErrorCode: "merge_conflict_or_ambiguity", IncidentPacket: packet},
		{Sequence: 20, ID: "admission-20", ReviewRunID: "run-20", SessionID: "dcp-review-lab-28", TargetSHA: strings.Repeat("2", 40), Status: domain.DCPAdmissionWaiting},
		{Sequence: 21, ID: "admission-21", ReviewRunID: "run-21", SessionID: "dcp-review-lab-29", TargetSHA: strings.Repeat("3", 40), Status: domain.DCPAdmissionWaiting},
		{Sequence: 22, ID: "admission-22", ReviewRunID: "run-22", SessionID: "dcp-review-lab-30", TargetSHA: strings.Repeat("4", 40), Status: domain.DCPAdmissionWaiting},
	}
	store := &queueSelectionStore{
		fakeStore: &fakeStore{},
		rows:      rows,
		tasks: map[domain.SessionID]domain.DCPReviewLabPolicyTask{
			"dcp-review-lab-27": {SessionID: "dcp-review-lab-27", State: domain.DCPPolicyIncident, AdmissionID: "admission-19", ReviewRunID: "run-19", CurrentHeadSHA: strings.Repeat("1", 40), ErrorCode: "merge_conflict_or_ambiguity", IncidentPacket: packet},
		},
	}
	engine := New(store, &fakeSCM{}, t.TempDir())
	for want := int64(20); want <= 22; want++ {
		got, ok, err := engine.nextPending(context.Background())
		if err != nil || !ok || got.Sequence != want {
			t.Fatalf("next after terminal Human Gate = %+v ok=%v err=%v, want sequence %d", got, ok, err, want)
		}
		store.rows[want-19].Status = domain.DCPAdmissionSucceeded
	}
	if got, ok, err := engine.nextPending(context.Background()); err != nil || ok {
		t.Fatalf("queue after sequence 22 = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestDrainThreeCleanOldBasePolicyPRsRefreshesProviderMainAndPreservesAllChanges(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	projectPath := filepath.Join(root, "targets", ProjectID)
	oldBase := strings.Repeat("d", 40)
	mergeSHAs := map[int]string{25: strings.Repeat("e", 40), 26: strings.Repeat("f", 40), 27: strings.Repeat("1", 40)}
	store := &tripleQueueStore{
		fakeStore: &fakeStore{project: domain.ProjectRecord{
			ID: ProjectID, Path: projectPath, RepoOriginURL: RepositoryURL, Kind: domain.ProjectKindSingleRepo,
			Config: domain.ProjectConfig{
				DefaultBranch: TargetBranch, SessionPrefix: SessionPrefix, AgentRules: ProfileAgentRules,
				Worker:    domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}},
				Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}},
			},
		}},
		sessions: map[domain.SessionID]domain.SessionRecord{}, prs: map[domain.SessionID]domain.PullRequest{},
		runs: map[domain.SessionID]domain.ReviewRun{}, tasks: map[domain.SessionID]domain.DCPReviewLabPolicyTask{},
	}
	for offset, taskID := range []string{"manual-triple-a", "manual-triple-b", "manual-triple-c"} {
		card := 28 + offset
		prNumber := 25 + offset
		id := domain.SessionID(fmt.Sprintf("dcp-review-lab-%d", card))
		head := strings.Repeat(string(rune('a'+offset)), 40)
		branch := "ao/" + string(id) + "/root"
		workspace := filepath.Join(dataDir, "worktrees", ProjectID, string(id))
		privateGitDir := filepath.Join(projectPath, ".git", "worktrees", string(id))
		for _, path := range []string{workspace, privateGitDir} {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		prompt := fmt.Sprintf("Add qualification/manual-triple-20260815-%c.txt.", 'a'+rune(offset))
		prURL := fmt.Sprintf("https://github.com/orenvlad-ai/dcp-review-lab/pull/%d", prNumber)
		runID := fmt.Sprintf("run-%d", prNumber)
		store.sessions[id] = domain.SessionRecord{
			ID: id, ProjectID: ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: TaskDisplayPrefix + taskID, Activity: domain.Activity{State: domain.ActivityIdle},
			Metadata: domain.SessionMetadata{WorkspacePath: workspace, Branch: branch, DiffBaseSHA: oldBase, DiffBaseRef: "origin/main", Prompt: TaskPromptPrefix + taskID + ": " + prompt},
		}
		store.prs[id] = domain.PullRequest{
			URL: prURL, SessionID: id, Number: prNumber, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
			SourceBranch: branch, TargetBranch: TargetBranch, HeadSHA: head, BaseSHA: oldBase,
			Author: "orenvlad-ai", ProviderState: "OPEN", HTMLURL: prURL,
		}
		store.runs[id] = domain.ReviewRun{
			ID: runID, ReviewID: "review-" + runID, BatchID: "batch-" + runID, SessionID: id,
			Harness: domain.ReviewerCodex, PRURL: prURL, TargetSHA: head, Body: "No blocking findings.",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, ResultChannel: structuredChannel,
		}
		store.tasks[id] = domain.DCPReviewLabPolicyTask{
			TaskID: taskID, Target: ProjectID, Profile: "synthetic-pr", Repository: RepositoryFullName,
			PolicyVersion: domain.DCPReviewLabPolicyVersion, SessionID: id, CardNumber: int64(card),
			WorktreePath: workspace, SourceBranch: branch, Prompt: prompt, State: domain.DCPPolicyAdmissionWait,
			Revision: 9, PRURL: prURL, PRNumber: int64(prNumber), CurrentHeadSHA: head, ReviewRunID: runID,
			AdmissionID: "admission-" + runID,
		}
		store.rows = append(store.rows, domain.DCPReviewLabAdmission{
			Sequence: int64(20 + offset), ID: "admission-" + runID, ReviewRunID: runID, ReviewID: "review-" + runID,
			SessionID: id, PRURL: prURL, PRNumber: int64(prNumber), TargetSHA: head, ReviewBaseSHA: oldBase,
			Status: domain.DCPAdmissionWaiting,
		})
	}
	scm := &tripleQueueSCM{store: store, currentBase: oldBase, mergeSHAs: mergeSHAs, files: map[string]string{}}
	engine := New(store, scm, dataDir)
	engine.providerRepository = func(context.Context) (string, error) {
		return RepositoryFullName + "|false|main|1329007118|237411244", nil
	}
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
		}
		if path == projectPath {
			switch cmd {
			case "rev-parse --show-toplevel":
				return projectPath, nil
			case "branch --show-current":
				return TargetBranch, nil
			case "rev-parse origin/main", "rev-parse HEAD":
				return scm.currentBase, nil
			}
		}
		for id, session := range store.sessions {
			if path != session.Metadata.WorkspacePath {
				continue
			}
			head := store.prs[id].HeadSHA
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return session.Metadata.Branch, nil
			case "rev-parse HEAD":
				return head, nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(projectPath, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return filepath.Join(projectPath, ".git", "worktrees", string(id)), nil
			case "merge-base --is-ancestor " + oldBase + " " + head:
				return "", nil
			case "rev-list --count " + scm.currentBase + ".." + head:
				return "1", nil
			case "rev-list --merges " + scm.currentBase + ".." + head:
				return "", nil
			}
		}
		return "", fmt.Errorf("unexpected triple git command in %s: %s", path, cmd)
	}

	if err := engine.Try(context.Background(), "dcp-review-lab-28"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(store.mergeOrder, []int64{20, 21, 22}) || !slices.Equal(scm.mergePRs, []int{25, 26, 27}) || store.claims != 3 {
		t.Fatalf("triple order=%v PRs=%v claims=%d", store.mergeOrder, scm.mergePRs, store.claims)
	}
	wantBases := []string{oldBase, mergeSHAs[25], mergeSHAs[26]}
	if !slices.Equal(scm.fetchedBase, wantBases) {
		t.Fatalf("fresh provider bases=%v, want %v", scm.fetchedBase, wantBases)
	}
	for offset, letter := range []rune{'a', 'b', 'c'} {
		row := store.rows[offset]
		if row.Status != domain.DCPAdmissionSucceeded || row.AdmittedBaseSHA != wantBases[offset] || row.MergeCommitSHA != mergeSHAs[25+offset] {
			t.Fatalf("terminal row %d = %+v", offset, row)
		}
		path := fmt.Sprintf("qualification/manual-triple-20260815-%c.txt", letter)
		if scm.files[path] != fmt.Sprintf("manual-triple-%c=ok", letter) {
			t.Fatalf("preserved files=%v", scm.files)
		}
	}
	if err := engine.Try(context.Background(), "dcp-review-lab-30"); err != nil {
		t.Fatal(err)
	}
	if store.claims != 3 || len(scm.mergePRs) != 3 || len(scm.fetchedBase) != 3 {
		t.Fatalf("terminal replay duplicated work: claims=%d PRs=%v fetches=%v", store.claims, scm.mergePRs, scm.fetchedBase)
	}
}

func (f *fakeSCM) FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error) {
	return []ports.SCMObservation{f.observation}, nil
}
func (f *fakeSCM) FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error) {
	return f.review, nil
}
func (f *fakeSCM) MergePullRequest(_ context.Context, request ports.SCMMergeRequest) (ports.SCMMergeResult, error) {
	f.mergeCalls++
	expectedRepo := f.expectedRepo
	if expectedRepo == "" {
		expectedRepo = RepositoryFullName
	}
	if request.ExpectedHeadSHA != f.expectedHead || request.Method != ports.SCMMergeSquash || request.PR.Repo.Repo != expectedRepo {
		return ports.SCMMergeResult{}, errors.New("unexpected merge request")
	}
	if f.mergeErr != nil {
		return ports.SCMMergeResult{}, f.mergeErr
	}
	return ports.SCMMergeResult{MergeCommitSHA: f.mergeSHA}, nil
}
func (f *fakeSCM) ApplyReleaseReady(_ context.Context, request ports.SCMReleaseReadyRequest) error {
	f.releaseReadyCalls++
	if request.PR.Repo.Repo != "orenvlad-ai/wb-core" || request.ExpectedHeadSHA != f.expectedHead ||
		request.ExpectedBaseBranch != "main" || request.RequiredTaskLabel != "task:standard" || request.RequiredScopeLabel != "scope:repo-only" {
		return errors.New("unexpected release-ready request")
	}
	if !hasExactLabel(f.releaseObservation.Labels, "release:ready") {
		f.releaseObservation.Labels = append(f.releaseObservation.Labels, "release:ready")
	}
	return nil
}
func (f *fakeSCM) ObserveRelease(context.Context, ports.SCMPRRef) (ports.SCMReleaseObservation, error) {
	return f.releaseObservation, nil
}

func fixture(t *testing.T) (*Engine, *fakeStore, *fakeSCM) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	id := domain.SessionID(AdmissionSessionA)
	workspace := filepath.Join(dataDir, "worktrees", ProjectID, string(id))
	projectPath := filepath.Join(root, "targets", ProjectID)
	privateGitDir := filepath.Join(projectPath, ".git", "worktrees", string(id))
	for _, path := range []string{workspace, privateGitDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	branch := "ao/" + string(id) + "/root"
	prURL := "https://github.com/orenvlad-ai/dcp-review-lab/pull/4"
	taskID := "i7-terminal"
	if len(TaskDisplayPrefix+taskID) > 20 {
		t.Fatal("exact task identity must fit the stock spawn display-name limit")
	}
	if !strings.Contains(ProfileAgentRules, "additional pull requests") || !strings.Contains(ProfileAgentRules, "open one ready pull request") {
		t.Fatal("exact profile must allow one ready PR while rejecting extras")
	}
	store := &fakeStore{
		includeCohortPeer: true,
		session: domain.SessionRecord{
			ID: id, ProjectID: ProjectID, Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: TaskDisplayPrefix + taskID,
			Activity:    domain.Activity{State: domain.ActivityIdle},
			Metadata: domain.SessionMetadata{
				WorkspacePath: workspace, Branch: branch, DiffBaseSHA: testBase, DiffBaseRef: "origin/main",
				Prompt: TaskPromptPrefix + taskID + ": Add the exact synthetic canary workflow.",
			},
		},
		project: domain.ProjectRecord{
			ID: ProjectID, Path: projectPath, RepoOriginURL: RepositoryURL, Kind: domain.ProjectKindSingleRepo,
			Config: domain.ProjectConfig{
				DefaultBranch: TargetBranch, SessionPrefix: SessionPrefix, AgentRules: ProfileAgentRules,
				Worker:    domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}},
				Reviewers: []domain.ReviewerConfig{{Harness: domain.ReviewerCodex}},
			},
		},
		pr: domain.PullRequest{
			URL: prURL, SessionID: id, Number: 4, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
			SourceBranch: branch, TargetBranch: TargetBranch, HeadSHA: testHead, BaseSHA: testBase,
			Author: "orenvlad-ai", ProviderState: "OPEN", HTMLURL: prURL,
		},
		run: domain.ReviewRun{
			ID: "run-7", ReviewID: "review-record-7", BatchID: "batch-7", SessionID: id, Harness: domain.ReviewerCodex,
			PRURL: prURL, TargetSHA: testHead, Body: "No blocking findings.",
			Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, ResultChannel: structuredChannel,
		},
	}
	scm := &fakeSCM{expectedHead: testHead, mergeSHA: testMerge, review: ports.SCMReviewObservation{Decision: string(domain.ReviewNone)}, observation: ports.SCMObservation{
		Fetched: true, Provider: "github", Host: "github.com", Repo: RepositoryFullName,
		PR: ports.SCMPRObservation{
			URL: prURL, Number: 4, HeadRepo: RepositoryFullName, SourceBranch: branch, TargetBranch: TargetBranch,
			HeadSHA: testHead, BaseSHA: testBase, State: string(domain.PRStateOpen), ProviderState: "OPEN",
			Author: "orenvlad-ai", HTMLURL: prURL, ProviderMergeable: "MERGEABLE", ProviderMergeStateStatus: "CLEAN",
		},
		CI: ports.SCMCIObservation{Summary: string(domain.CIPassing), HeadSHA: testHead, Checks: []ports.SCMCheckObservation{{
			Name: RequiredCheckName, Status: string(domain.PRCheckPassed), Conclusion: "success",
		}}},
		Mergeability: ports.SCMMergeabilityObservation{State: string(domain.MergeMergeable), Mergeable: true},
	}}
	engine := New(store, scm, dataDir)
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		if cmd == "status --porcelain" {
			return "", nil
		}
		if cmd == "remote" {
			return "origin", nil
		}
		if cmd == "remote get-url origin" {
			return RepositoryURL, nil
		}
		if cmd == "fetch --no-tags origin main" {
			return "", nil
		}
		if path == store.project.Path {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return TargetBranch, nil
			case "rev-parse origin/main", "rev-parse HEAD":
				return testBase, nil
			}
		}
		if path == workspace {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return branch, nil
			case "rev-parse HEAD":
				return strings.ToLower(store.pr.HeadSHA), nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(store.project.Path, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return privateGitDir, nil
			}
		}
		return "", errors.New("unexpected git command")
	}
	return engine, store, scm
}

func futurePolicyFixture(t *testing.T) (*Engine, *fakeStore, *fakeSCM) {
	spec, _ := domain.DCPPolicyTarget("dcp-review-lab", "synthetic-pr")
	return policyTargetFixture(t, spec, 13)
}

func policyTargetFixture(t *testing.T, spec domain.DCPPolicyTargetSpec, card int64) (*Engine, *fakeStore, *fakeSCM) {
	t.Helper()
	engine, store, scm := fixture(t)
	id := domain.SessionID(spec.SessionPrefix + "-" + strconv.FormatInt(card, 10))
	store.project.ID = spec.Target
	store.project.Path = filepath.Join(filepath.Dir(engine.dataDir), "targets", spec.Target)
	store.project.RepoOriginURL = spec.OriginURL
	store.project.Config.DefaultBranch, store.project.Config.SessionPrefix = spec.DefaultBranch, spec.SessionPrefix
	store.project.Config.AgentRules = spec.AgentRules
	workspace := filepath.Join(engine.dataDir, "worktrees", spec.Target, string(id))
	privateGitDir := filepath.Join(store.project.Path, ".git", "worktrees", string(id))
	for _, path := range []string{workspace, privateGitDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	branch := "ao/" + string(id) + "/root"
	store.session.ID, store.session.DisplayName = id, "DCP:future-1"
	store.session.ProjectID = domain.ProjectID(spec.Target)
	store.session.Metadata.WorkspacePath, store.session.Metadata.Branch = workspace, branch
	promptPrefix := "DCP synthetic task "
	if spec.Profile == "repo-only" {
		promptPrefix = "DCP repo-only task "
	}
	store.session.Metadata.Prompt = promptPrefix + "future-1: Add one future policy fixture."
	prURL := "https://github.com/" + spec.Repository + "/pull/4"
	store.pr.SessionID, store.pr.SourceBranch, store.pr.Repo = id, branch, spec.Repository
	store.pr.URL, store.pr.HTMLURL, store.pr.TargetBranch = prURL, prURL, spec.DefaultBranch
	store.run.SessionID, store.run.PRURL = id, prURL
	store.policyTask = &domain.DCPReviewLabPolicyTask{
		TaskID: "future-1", Target: spec.Target, Profile: spec.Profile, Repository: spec.Repository,
		PolicyVersion: spec.PolicyVersion, SessionID: id, CardNumber: card,
		WorktreePath: workspace, SourceBranch: branch, Prompt: "Add one future policy fixture.",
		State: domain.DCPPolicyAdmissionWait, Revision: 8, CurrentHeadSHA: testHead, ReviewRunID: store.run.ID,
	}
	scm.observation.Repo, scm.observation.PR.HeadRepo = spec.Repository, spec.Repository
	scm.expectedRepo = spec.Repository
	scm.observation.PR.URL, scm.observation.PR.HTMLURL = prURL, prURL
	scm.observation.PR.SourceBranch, scm.observation.PR.TargetBranch = branch, spec.DefaultBranch
	scm.observation.CI.Checks[0].Name = spec.RequiredCheck
	scm.observation.CI.Checks[0].URL = "https://github.com/" + spec.Repository + "/actions/runs/123/job/456"
	engine.providerRepository = func(context.Context) (string, error) {
		return policyProviderIdentity(spec), nil
	}
	engine.providerRepositoryFor = func(_ context.Context, repository string) (string, error) {
		if repository != spec.Repository {
			return "", errors.New("foreign provider lookup")
		}
		return policyProviderIdentity(spec), nil
	}
	if spec.UsesWBCReleaseTrain() {
		scm.releaseObservation = ports.SCMReleaseObservation{
			Number: 4, URL: prURL, State: "open", HeadRepository: spec.Repository, HeadBranch: branch,
			HeadSHA: testHead, BaseBranch: spec.DefaultBranch, Author: "orenvlad-ai",
			Labels: []string{"scope:repo-only", "task:standard"},
		}
	}
	engine.git = func(_ context.Context, path string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		if cmd == "status --porcelain" {
			return "", nil
		}
		if cmd == "remote" {
			return "origin", nil
		}
		if cmd == "remote get-url origin" {
			return spec.OriginURL, nil
		}
		if cmd == "fetch --no-tags origin "+spec.DefaultBranch {
			return "", nil
		}
		if path == store.project.Path {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return spec.DefaultBranch, nil
			case "rev-parse origin/" + spec.DefaultBranch, "rev-parse HEAD":
				return strings.ToLower(store.pr.BaseSHA), nil
			case "merge-tree --write-tree " + strings.ToLower(store.pr.BaseSHA) + " " + testHead:
				return testMerge, nil
			}
		}
		if path == workspace {
			canonicalDelta := strings.ToLower(store.pr.BaseSHA) + ".." + testHead
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return branch, nil
			case "rev-parse HEAD":
				return testHead, nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(store.project.Path, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return privateGitDir, nil
			case "merge-base --is-ancestor " + testBase + " " + testHead:
				return "", nil
			case "rev-list --count " + canonicalDelta:
				return "1", nil
			case "rev-list --merges " + canonicalDelta:
				return "", nil
			}
		}
		return "", errors.New("unexpected future-policy git command: " + cmd)
	}
	return engine, store, scm
}

func TestFuturePolicyCleanMainAdvanceMergesAndProjectsTerminalOnce(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	advancedMain := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	store.pr.BaseSHA, scm.observation.PR.BaseSHA = advancedMain, advancedMain
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.policyTask.State != domain.DCPPolicyMerged || store.policyTask.MergeCommitSHA != testMerge || store.admission == nil || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("future terminal projection: merges=%d task=%+v admission=%+v", scm.mergeCalls, store.policyTask, store.admission)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 {
		t.Fatalf("future terminal merge duplicated: %d", scm.mergeCalls)
	}
}

func TestRepoOnlyPolicyUsesExactProviderCheckAndTrustedMerge(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-browser-extension", "repo-only")
	engine, store, scm := policyTargetFixture(t, spec, 1)
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.policyTask.State != domain.DCPPolicyMerged || store.policyTask.MergeCommitSHA != testMerge ||
		store.admission == nil || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("repo-only terminal projection: merges=%d task=%+v admission=%+v", scm.mergeCalls, store.policyTask, store.admission)
	}

	foreignEngine, foreignStore, foreignSCM := policyTargetFixture(t, spec, 1)
	foreignEngine.providerRepositoryFor = func(context.Context, string) (string, error) {
		reviewSpec, _ := domain.DCPPolicyTarget("dcp-review-lab", "synthetic-pr")
		return policyProviderIdentity(reviewSpec), nil
	}
	if err := foreignEngine.Try(context.Background(), foreignStore.session.ID); err != nil {
		t.Fatal(err)
	}
	if foreignSCM.mergeCalls != 0 || foreignStore.policyTask.State != domain.DCPPolicyIncident {
		t.Fatalf("foreign provider identity was not failed closed: merges=%d task=%+v", foreignSCM.mergeCalls, foreignStore.policyTask)
	}
}

func TestWBCPolicyHandsOffOnlyReleaseReadyAndObservesExactTerminalProof(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	engine, store, scm := policyTargetFixture(t, spec, 1)
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || scm.releaseReadyCalls != 1 || store.policyTask.State != domain.DCPPolicyReleaseWaiting ||
		store.admission == nil || store.admission.Status != domain.DCPAdmissionClaimed {
		t.Fatalf("WBC handoff: merges=%d ready=%d task=%+v admission=%+v", scm.mergeCalls, scm.releaseReadyCalls, store.policyTask, store.admission)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || scm.releaseReadyCalls != 1 || store.policyTask.State != domain.DCPPolicyReleaseWaiting {
		t.Fatalf("WBC passive wait mutated: merges=%d ready=%d task=%+v", scm.mergeCalls, scm.releaseReadyCalls, store.policyTask)
	}
	scm.releaseObservation.State = "closed"
	scm.releaseObservation.Merged = true
	scm.releaseObservation.MergeCommitSHA = testMerge
	scm.releaseObservation.Labels = []string{"release:done", "scope:repo-only", "task:standard"}
	scm.releaseObservation.Body = "<!-- wb-core-release-completion-proof contour=repo-only merge=" + testMerge + " pr=4 -->"
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || store.policyTask.State != domain.DCPPolicyMerged || store.policyTask.MergeCommitSHA != testMerge ||
		store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("WBC terminal observation: merges=%d task=%+v admission=%+v", scm.mergeCalls, store.policyTask, store.admission)
	}
}

func TestWBCPolicyAdmissionUsesOnlyExactConfiguredBaseline(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	for _, tc := range []struct {
		name   string
		mutate func(*fakeSCM)
		ready  bool
	}{
		{
			name: "current complex Release Train with unrelated skipped jobs",
			mutate: func(scm *fakeSCM) {
				scm.observation.CI.Checks = append(scm.observation.CI.Checks,
					ports.SCMCheckObservation{Name: "Select queued PR", Status: string(domain.PRCheckSkipped), Conclusion: "skipped"},
					ports.SCMCheckObservation{Name: "Merge repo-only PR", Status: string(domain.PRCheckSkipped), Conclusion: "skipped"},
				)
			},
			ready: true,
		},
		{name: "future simplified Release Train", mutate: func(*fakeSCM) {}, ready: true},
		{name: "pending baseline", mutate: func(scm *fakeSCM) {
			scm.observation.CI.Checks[0].Status, scm.observation.CI.Checks[0].Conclusion = string(domain.PRCheckInProgress), ""
		}},
		{name: "failed baseline", mutate: func(scm *fakeSCM) {
			scm.observation.CI.Checks[0].Status, scm.observation.CI.Checks[0].Conclusion = string(domain.PRCheckFailed), "failure"
		}},
		{name: "skipped baseline", mutate: func(scm *fakeSCM) {
			scm.observation.CI.Checks[0].Status, scm.observation.CI.Checks[0].Conclusion = string(domain.PRCheckSkipped), "skipped"
		}},
		{name: "duplicate baseline", mutate: func(scm *fakeSCM) {
			scm.observation.CI.Checks = append(scm.observation.CI.Checks, scm.observation.CI.Checks[0])
		}},
		{name: "wrong check head", mutate: func(scm *fakeSCM) { scm.observation.CI.HeadSHA = strings.Repeat("f", 40) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, store, scm := policyTargetFixture(t, spec, 1)
			tc.mutate(scm)
			if err := engine.Try(context.Background(), store.session.ID); err != nil {
				t.Fatal(err)
			}
			if got := scm.releaseReadyCalls == 1; got != tc.ready {
				t.Fatalf("releaseReadyCalls=%d task=%+v", scm.releaseReadyCalls, store.policyTask)
			}
			if scm.mergeCalls != 0 {
				t.Fatalf("wb-core direct merge calls=%d", scm.mergeCalls)
			}
		})
	}
}

func TestWBCPolicyHeadDriftFailsClosedWithoutDirectMerge(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	engine, store, scm := policyTargetFixture(t, spec, 1)
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	scm.releaseObservation.HeadSHA = strings.Repeat("f", 40)
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || store.policyTask.State != domain.DCPPolicyIncident || store.policyTask.ErrorCode != "release_head_drift" {
		t.Fatalf("WBC head drift: merges=%d task=%+v", scm.mergeCalls, store.policyTask)
	}
}

func TestWBCPolicyReleaseWaitSurvivesRestartWithoutDuplicateHandoff(t *testing.T) {
	spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
	engine, store, scm := policyTargetFixture(t, spec, 1)
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	restarted := New(store, scm, engine.dataDir)
	restarted.git = engine.git
	restarted.providerRepository = engine.providerRepository
	restarted.providerRepositoryFor = engine.providerRepositoryFor
	if err := restarted.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || scm.releaseReadyCalls != 1 || store.policyTask.State != domain.DCPPolicyReleaseWaiting ||
		store.admission == nil || store.admission.Status != domain.DCPAdmissionClaimed {
		t.Fatalf("WBC restart replay: merges=%d ready=%d task=%+v admission=%+v", scm.mergeCalls, scm.releaseReadyCalls, store.policyTask, store.admission)
	}
}

func TestWBCPolicyReleaseIdentityAndTerminalDriftFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		wantCode string
		mutate   func(*ports.SCMReleaseObservation)
	}{
		{name: "crossed repository", wantCode: "release_identity_drift", mutate: func(o *ports.SCMReleaseObservation) {
			o.HeadRepository = "orenvlad-ai/wb-browser-extension"
		}},
		{name: "crossed base", wantCode: "release_identity_drift", mutate: func(o *ports.SCMReleaseObservation) {
			o.BaseBranch = "release"
		}},
		{name: "crossed scope", wantCode: "release_label_drift", mutate: func(o *ports.SCMReleaseObservation) {
			o.Labels = []string{"release:ready", "scope:live-runtime", "task:standard"}
		}},
		{name: "merge without completion proof", wantCode: "release_terminal_proof_invalid", mutate: func(o *ports.SCMReleaseObservation) {
			o.State, o.Merged, o.MergeCommitSHA = "closed", true, testMerge
			o.Labels = []string{"release:done", "scope:repo-only", "task:standard"}
			o.Body = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, _ := domain.DCPPolicyTarget("wb-core", "repo-only")
			engine, store, scm := policyTargetFixture(t, spec, 1)
			if err := engine.Try(context.Background(), store.session.ID); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&scm.releaseObservation)
			if err := engine.Try(context.Background(), store.session.ID); err != nil {
				t.Fatal(err)
			}
			if scm.mergeCalls != 0 || store.policyTask.State != domain.DCPPolicyIncident || store.policyTask.ErrorCode != tc.wantCode {
				t.Fatalf("WBC drift: merges=%d task=%+v", scm.mergeCalls, store.policyTask)
			}
		})
	}
}

func TestFuturePolicyAdmissionCommitSignalsExactIdentityBeforeClaimAndReplaysOnce(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	var signals []AdmissionCommitSignal
	var nestedErr error
	engine.SetAdmissionCommittedHandler(func(ctx context.Context, signal AdmissionCommitSignal) {
		signals = append(signals, signal)
		if store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting ||
			store.policyTask.AdmissionID != signal.AdmissionID || store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatalf("post-commit signal ran before exact durable waiting identity: signal=%+v task=%+v admission=%+v claims=%d merges=%d", signal, store.policyTask, store.admission, store.claims, scm.mergeCalls)
		}
		// The callback is synchronous on purpose: Try must have released its
		// process mutex before the existing terminal-merger entry is signalled.
		nestedErr = engine.Try(ctx, signal.SessionID)
	})

	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if nestedErr != nil {
		t.Fatal(nestedErr)
	}
	if len(signals) != 1 || signals[0].AdmissionID != "dcp-admission-"+store.run.ID ||
		signals[0].ReviewRunID != store.run.ID || signals[0].SessionID != store.session.ID ||
		signals[0].TargetSHA != testHead {
		t.Fatalf("commit signals=%+v", signals)
	}
	if store.claims != 1 || scm.mergeCalls != 1 || store.policyTask.State != domain.DCPPolicyMerged ||
		store.admission == nil || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("trusted drain did not merge once: claims=%d merges=%d task=%+v admission=%+v", store.claims, scm.mergeCalls, store.policyTask, store.admission)
	}

	// Lifecycle/SCM replay and startup reconciliation all reach the same
	// admission/lease owner and cannot recreate the post-commit signal or merge.
	for range 3 {
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
	}
	restarted := New(store, scm, engine.dataDir)
	restarted.git = engine.git
	restarted.providerRepository = engine.providerRepository
	restarted.SetAdmissionCommittedHandler(func(_ context.Context, signal AdmissionCommitSignal) {
		signals = append(signals, signal)
	})
	if err := restarted.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || store.claims != 1 || scm.mergeCalls != 1 {
		t.Fatalf("replay duplicated identity: signals=%d claims=%d merges=%d", len(signals), store.claims, scm.mergeCalls)
	}
}

func TestFuturePolicyAdmissionCommitSignalFailsClosedForStaleIdentity(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	scm.observation.PR.HeadSHA = strings.Repeat("f", 40)
	signals := 0
	engine.SetAdmissionCommittedHandler(func(context.Context, AdmissionCommitSignal) { signals++ })
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if signals != 0 || store.admission != nil || store.policyTask.State != domain.DCPPolicyIncident {
		t.Fatalf("stale identity reached signal/admission: signals=%d admission=%+v task=%+v", signals, store.admission, store.policyTask)
	}
}

func TestFuturePolicySuccessorRepairCountsOnlyExactCanonicalDelta(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	advancedMain := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	store.pr.BaseSHA, scm.observation.PR.BaseSHA = advancedMain, advancedMain
	store.policyTask.RepairCount = 1

	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.claims != 1 || store.policyTask.State != domain.DCPPolicyMerged {
		t.Fatalf("successor repair did not merge from exact advanced base: merges=%d claims=%d task=%+v", scm.mergeCalls, store.claims, store.policyTask)
	}
}

func TestFuturePolicyWaitingAdmissionDrainsOnLaterCleanEventOnceAcrossRestart(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	scm.observation.PR.ProviderMergeable = "UNKNOWN"
	scm.observation.PR.ProviderMergeStateStatus = "UNKNOWN"
	scm.observation.Mergeability = ports.SCMMergeabilityObservation{}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || store.claims != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting {
		t.Fatalf("pending event must remain passive: merges=%d claims=%d admission=%+v", scm.mergeCalls, store.claims, store.admission)
	}

	scm.observation.PR.ProviderMergeable = "MERGEABLE"
	scm.observation.PR.ProviderMergeStateStatus = "CLEAN"
	scm.observation.Mergeability = ports.SCMMergeabilityObservation{State: string(domain.MergeMergeable), Mergeable: true}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.claims != 1 || store.policyTask.State != domain.DCPPolicyMerged || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("clean catch-up did not merge once: merges=%d claims=%d task=%+v admission=%+v", scm.mergeCalls, store.claims, store.policyTask, store.admission)
	}

	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	restarted := New(store, scm, engine.dataDir)
	restarted.git = engine.git
	restarted.providerRepository = engine.providerRepository
	if err := restarted.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.claims != 1 {
		t.Fatalf("terminal replay/restart duplicated merge: merges=%d claims=%d", scm.mergeCalls, store.claims)
	}
}

func TestFuturePolicyNonCleanOrForeignNamedCIFailsClosedWithoutWake(t *testing.T) {
	for name, mutate := range map[string]func(*fakeSCM){
		"behind":           func(scm *fakeSCM) { scm.observation.PR.ProviderMergeStateStatus = "BEHIND" },
		"foreign named CI": func(scm *fakeSCM) { scm.observation.CI.Checks[0].URL = "https://example.invalid/actions/runs/123" },
	} {
		t.Run(name, func(t *testing.T) {
			engine, store, scm := futurePolicyFixture(t)
			mutate(scm)
			wakes := 0
			engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
			if err := engine.Try(context.Background(), store.session.ID); err != nil {
				t.Fatal(err)
			}
			if wakes != 0 || scm.mergeCalls != 0 || store.policyTask.State != domain.DCPPolicyIncident || (store.admission != nil && store.admission.Status != domain.DCPAdmissionIncident) {
				t.Fatalf("future fail-closed projection: wakes=%d merges=%d task=%+v admission=%+v", wakes, scm.mergeCalls, store.policyTask, store.admission)
			}
		})
	}
}

func TestFutureArbiterCandidateAcceptsOnlyExactIncidentState(t *testing.T) {
	engine, store, scm := futurePolicyFixture(t)
	scm.observation.PR.ProviderMergeable = "CONFLICTING"
	scm.observation.PR.ProviderMergeStateStatus = "DIRTY"
	scm.observation.Mergeability.State = string(domain.MergeConflicting)
	scm.observation.Mergeability.Mergeable = false
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.policyTask.State != domain.DCPPolicyIncident || store.admission == nil || store.admission.Status != domain.DCPAdmissionIncident {
		t.Fatalf("incident was not persisted: task=%+v admission=%+v", store.policyTask, store.admission)
	}
	if _, ok, err := engine.candidateForAdmission(context.Background(), *store.admission); err != nil || ok {
		t.Fatalf("ordinary admission candidate weakened for incident: ok=%v err=%v", ok, err)
	}
	candidate, ok, err := engine.candidateForFutureArbiterAdmission(context.Background(), *store.admission)
	if err != nil || !ok || candidate.policyTask.State != domain.DCPPolicyIncident || candidate.policyTask.AdmissionID != store.admission.ID {
		t.Fatalf("future arbiter candidate rejected exact incident: ok=%v err=%v candidate=%+v", ok, err, candidate)
	}
}

func TestTryMergesExactCleanApprovedHeadOnce(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.review.Decision = string(domain.ReviewApproved)
	candidate, ok, err := engine.candidate(context.Background(), store.session.ID)
	if err != nil || !ok {
		t.Fatalf("candidate ok=%v err=%v", ok, err)
	}
	observation, review, err := engine.fresh(context.Background(), candidate.pr)
	if err != nil || !ready(candidate, observation, review) {
		t.Fatalf("ready=false err=%v observation=%+v review=%+v", err, observation, review)
	}
	if err := engine.validateGit(context.Background(), candidate, observation.PR.HeadSHA, observation.PR.BaseSHA); err != nil {
		t.Fatal(err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 1 || scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "succeeded" || store.run.TerminalMergeCommitSHA != testMerge {
		t.Fatalf("claims=%d merges=%d run=%+v", store.claims, scm.mergeCalls, store.run)
	}
}

func TestTryMergesWithStockNativeMissingDiffBaseMetadata(t *testing.T) {
	engine, store, scm := fixture(t)
	store.session.Metadata.DiffBaseSHA = ""
	store.session.Metadata.DiffBaseRef = ""
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 1 || scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "succeeded" {
		t.Fatalf("claims=%d merges=%d run=%+v", store.claims, scm.mergeCalls, store.run)
	}
}

func TestTryRejectsOldSessionAndNonCleanProviderFacts(t *testing.T) {
	for _, rejectedID := range []domain.SessionID{"dcp-review-lab-6", "dcp-review-lab-8", "dcp-review-lab-13"} {
		engine, store, scm := fixture(t)
		store.session.ID = rejectedID
		if err := engine.Try(context.Background(), rejectedID); err != nil {
			t.Fatal(err)
		}
		if scm.mergeCalls != 0 || store.admission != nil {
			t.Fatalf("rejected session %s reached admission or merge", rejectedID)
		}
	}
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeStateStatus = "BLOCKED"
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("non-clean PR reached merge")
	}
}

func TestTryRequiresEveryVisibleCheckToPass(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.CI = ports.SCMCIObservation{
		Summary: string(domain.CIPending), HeadSHA: testHead,
		Checks: []ports.SCMCheckObservation{{Name: RequiredCheckName, Status: string(domain.PRCheckInProgress)}},
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("pending check reached merge")
	}
}

func TestTryRequiresOneSuccessfulNamedCheck(t *testing.T) {
	for _, mutate := range []func(*fakeSCM){
		func(scm *fakeSCM) { scm.observation.CI.Checks = nil },
		func(scm *fakeSCM) { scm.observation.CI.Checks[0].Name = "foreign" },
		func(scm *fakeSCM) { scm.observation.CI.Checks[0].Status = string(domain.PRCheckSkipped) },
	} {
		engine, store, scm := fixture(t)
		mutate(scm)
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatal("missing, foreign, or skipped required check reached merge")
		}
	}
}

func TestTryRejectsForeignTaskBaseAndProfile(t *testing.T) {
	for _, mutate := range []func(*fakeStore, *fakeSCM){
		func(store *fakeStore, _ *fakeSCM) { store.session.DisplayName = TaskDisplayPrefix + "FOREIGN" },
		func(store *fakeStore, _ *fakeSCM) { store.session.Metadata.Prompt = "unbound prompt" },
		func(store *fakeStore, _ *fakeSCM) { store.session.Metadata.DiffBaseRef = "" },
		func(store *fakeStore, _ *fakeSCM) { store.pr.BaseSHA = "" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRules += " malicious override" },
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.AgentRulesFile = "AGENTS.md" },
		func(store *fakeStore, _ *fakeSCM) {
			store.project.Config.Worker.AgentConfig.DCPReviewLabNetwork = false
		},
		func(store *fakeStore, _ *fakeSCM) { store.project.Config.TrackerIntake.Enabled = true },
		func(_ *fakeStore, scm *fakeSCM) { scm.observation.PR.BaseSHA = testHead },
	} {
		engine, store, scm := fixture(t)
		mutate(store, scm)
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatal("foreign task, base, or profile reached merge")
		}
	}
}

func TestTryRejectsAnyUnresolvedReviewThread(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.review.Threads = []ports.SCMReviewThreadObservation{{ID: "bot-thread", IsBot: true}}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if store.claims != 0 || scm.mergeCalls != 0 {
		t.Fatal("unresolved review thread reached merge")
	}
}

func TestTryRejectsUnknownOrBlockingProviderReviewDecision(t *testing.T) {
	for _, decision := range []string{"", string(domain.ReviewRequired), string(domain.ReviewChangesRequest), "foreign"} {
		engine, store, scm := fixture(t)
		scm.review.Decision = decision
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
		if store.claims != 0 || scm.mergeCalls != 0 {
			t.Fatalf("decision %q reached merge", decision)
		}
	}
}

func TestTryRecordsFailureWithoutRetry(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.mergeErr = ports.ErrSCMNotMergeable
	if err := engine.Try(context.Background(), store.session.ID); !errors.Is(err, ports.ErrSCMNotMergeable) {
		t.Fatalf("error=%v", err)
	}
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.run.TerminalMergeStatus != "failed" || store.run.TerminalMergeError != "not_mergeable" || store.admission.Status != domain.DCPAdmissionIncident {
		t.Fatalf("merges=%d run=%+v", scm.mergeCalls, store.run)
	}
}

func TestReconcileRunningUsesFreshMergedFactWithoutSecondMutation(t *testing.T) {
	engine, store, scm := fixture(t)
	store.run.TerminalMergeStatus = "running"
	store.admission = &domain.DCPReviewLabAdmission{
		Sequence: 1, ID: "dcp-admission-" + store.run.ID, ReviewRunID: store.run.ID, ReviewID: store.run.ReviewID,
		SessionID: store.session.ID, PRURL: store.pr.URL, PRNumber: int64(store.pr.Number), TargetSHA: testHead,
		ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase, Status: domain.DCPAdmissionClaimed, LeaseID: "dcp-merge-dcp-admission-" + store.run.ID,
	}
	store.pr.Merged = true
	scm.observation.PR.Merged = true
	scm.observation.PR.MergeCommitSHA = testMerge
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 0 || store.run.TerminalMergeStatus != "succeeded" || store.run.TerminalMergeCommitSHA != testMerge {
		t.Fatalf("merges=%d run=%+v", scm.mergeCalls, store.run)
	}
}

func TestBehindHeadGetsOneBoundedWakeThenReusesSameFIFOAdmission(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeStateStatus = "BEHIND"
	wakes := 0
	engine.SetRefreshWaker(func(_ context.Context, id domain.SessionID, prompt string) error {
		wakes++
		if id != store.session.ID || !strings.Contains(prompt, "force-with-lease") || !strings.Contains(prompt, testHead) {
			t.Fatalf("wake id=%s prompt=%q", id, prompt)
		}
		store.session.Metadata.RuntimeLaunchID = "refresh-launch"
		return nil
	})
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || store.admission == nil || store.admission.Status != domain.DCPAdmissionRefreshing || store.admission.RefreshWakeCount != 1 || scm.mergeCalls != 0 {
		t.Fatalf("wake=%d admission=%+v merges=%d", wakes, store.admission, scm.mergeCalls)
	}
	sequence := store.admission.Sequence
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Fatalf("active refresh duplicated wake: %d", wakes)
	}

	newHead := "dddddddddddddddddddddddddddddddddddddddd"
	store.session.Metadata.RuntimeLaunchID = ""
	store.pr.HeadSHA = newHead
	store.run.ID, store.run.ReviewID, store.run.BatchID, store.run.TargetSHA = "run-8-refresh", "review-record-8-refresh", "batch-8-refresh", newHead
	scm.expectedHead = newHead
	scm.observation.PR.HeadSHA = newHead
	scm.observation.CI.HeadSHA = newHead
	scm.observation.PR.ProviderMergeStateStatus = "CLEAN"
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || scm.mergeCalls != 1 || store.admission.Sequence != sequence || store.admission.ReviewRunID != store.run.ID ||
		store.admission.TargetSHA != newHead || store.admission.Status != domain.DCPAdmissionSucceeded || store.admission.RefreshWakeCount != 1 {
		t.Fatalf("wake=%d merges=%d admission=%+v run=%+v", wakes, scm.mergeCalls, store.admission, store.run)
	}
}

func TestConflictCreatesStructuredIncidentWithoutWakeOrMerge(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeable = "CONFLICTING"
	scm.observation.PR.ProviderMergeStateStatus = "DIRTY"
	scm.observation.Mergeability.State = string(domain.MergeConflicting)
	scm.observation.Mergeability.Mergeable = false
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionIncident || store.admission.ErrorCode != "merge_conflict_or_ambiguity" {
		t.Fatalf("wake=%d merges=%d admission=%+v", wakes, scm.mergeCalls, store.admission)
	}
	var packet incidentPacket
	if err := json.Unmarshal([]byte(store.admission.IncidentPacket), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.SchemaVersion != "dcp.review-lab.arbiter-needed/v1" || packet.SessionID != string(store.session.ID) || packet.TargetSHA != testHead || packet.EvidenceDigest == "" {
		t.Fatalf("packet=%+v", packet)
	}
}

func TestPassiveWaitingConsumesNoWakeOrMerge(t *testing.T) {
	engine, store, scm := fixture(t)
	scm.observation.PR.ProviderMergeable = "UNKNOWN"
	scm.observation.PR.ProviderMergeStateStatus = "UNKNOWN"
	scm.observation.Mergeability = ports.SCMMergeabilityObservation{}
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	for range 3 {
		if err := engine.Try(context.Background(), store.session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if wakes != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting {
		t.Fatalf("wake=%d merges=%d admission=%+v", wakes, scm.mergeCalls, store.admission)
	}
}

func TestCohortBarrierIsPassiveAndSurvivesStartupReconciliation(t *testing.T) {
	engine, store, scm := fixture(t)
	store.includeCohortPeer = false
	wakes := 0
	engine.SetRefreshWaker(func(context.Context, domain.SessionID, string) error { wakes++; return nil })
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || store.claims != 0 || scm.mergeCalls != 0 || store.admission == nil || store.admission.Status != domain.DCPAdmissionWaiting {
		t.Fatalf("before peer: wakes=%d claims=%d merges=%d admission=%+v", wakes, store.claims, scm.mergeCalls, store.admission)
	}
	store.includeCohortPeer = true
	if err := engine.Try(context.Background(), store.session.ID); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || store.claims != 1 || scm.mergeCalls != 1 || store.admission.Status != domain.DCPAdmissionSucceeded {
		t.Fatalf("after peer: wakes=%d claims=%d merges=%d admission=%+v", wakes, store.claims, scm.mergeCalls, store.admission)
	}
}

func TestStartupRecoversAuditedCanonicalAdvanceAndMergesCompatibleHead(t *testing.T) {
	engine, store, scm := fixture(t)
	newBase := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	packet, err := json.Marshal(incidentPacket{
		SchemaVersion: "dcp.review-lab.arbiter-needed/v1", Reason: "canonical_main_diverged",
		AdmissionID: "dcp-admission-" + store.run.ID, SessionID: string(store.session.ID),
		ReviewRunID: store.run.ID, TargetSHA: testHead,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.admission = &domain.DCPReviewLabAdmission{
		Sequence: 1, ID: "dcp-admission-" + store.run.ID, ReviewRunID: store.run.ID, ReviewID: store.run.ReviewID,
		SessionID: store.session.ID, PRURL: store.pr.URL, PRNumber: int64(store.pr.Number), TargetSHA: testHead,
		ReviewBaseSHA: testBase, AdmittedBaseSHA: testBase, Status: domain.DCPAdmissionIncident,
		LeaseID: "dcp-incident-dcp-admission-" + store.run.ID, ErrorCode: "canonical_main_diverged", IncidentPacket: string(packet),
	}
	canonicalHead := testBase
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
		}
		if path == store.project.Path {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return TargetBranch, nil
			case "rev-parse origin/main":
				return newBase, nil
			case "rev-parse HEAD":
				return canonicalHead, nil
			case "merge-base --is-ancestor " + testBase + " " + newBase:
				return "", nil
			case "merge --ff-only origin/main":
				canonicalHead = newBase
				return "", nil
			case "merge-tree --write-tree " + newBase + " " + testHead:
				return testMerge, nil
			}
		}
		if path == store.session.Metadata.WorkspacePath {
			switch cmd {
			case "rev-parse --show-toplevel":
				return path, nil
			case "branch --show-current":
				return store.session.Metadata.Branch, nil
			case "rev-parse HEAD":
				return testHead, nil
			case "rev-parse --path-format=absolute --git-common-dir":
				return filepath.Join(store.project.Path, ".git"), nil
			case "rev-parse --path-format=absolute --absolute-git-dir":
				return filepath.Join(store.project.Path, ".git", "worktrees", string(store.session.ID)), nil
			}
		}
		return "", errors.New("unexpected git command: " + cmd)
	}

	if err := engine.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scm.mergeCalls != 1 || store.admission.Status != domain.DCPAdmissionSucceeded ||
		store.admission.AdmittedBaseSHA != newBase || store.admission.RecoveredIncidentPacket != string(packet) ||
		store.admission.IncidentPacket != "" || store.admission.ErrorCode != "" {
		t.Fatalf("merges=%d admission=%+v", scm.mergeCalls, store.admission)
	}
}
