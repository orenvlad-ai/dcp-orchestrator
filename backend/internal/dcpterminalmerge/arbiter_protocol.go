package dcpterminalmerge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ArbiterIncidentSchema = "dcp.review-lab.global-release-incident/v1"
	ArbiterInputSchema    = "dcp.review-lab.global-release-arbiter-input/v1"
	ArbiterDecisionSchema = "dcp.review-lab.global-release-arbiter-decision/v1"
	ArbiterModel          = "gpt-5.6-sol"
	ArbiterReasoning      = "xhigh"
	ArbiterTokenBudget    = int64(16384)
	ArbiterRuntimeHandle  = "dcp-global-release-arbiter-v1"
	ArbiterSessionA       = "dcp-review-lab-11"
	ArbiterSessionB       = "dcp-review-lab-12"
	ArbiterTaskA          = "i13-arbiter-a"
	ArbiterTaskB          = "i13-arbiter-b"
	arbiterConflictPath   = "canary/i13-arbiter-conflict.txt"
	arbiterMaxInputBytes  = 16384
	arbiterMaxResultBytes = 16384
)

type arbiterScope struct {
	Repository            string `json:"repository"`
	TaskID                string `json:"taskId"`
	TaskText              string `json:"taskText"`
	FixedSyntheticProfile string `json:"fixedSyntheticProfile"`
}

type arbiterIdentity struct {
	AdmissionID       string `json:"admissionId"`
	AdmissionSequence int64  `json:"admissionSequence"`
	IncidentLeaseID   string `json:"incidentLeaseId"`
	TaskID            string `json:"taskId"`
	SessionID         string `json:"sessionId"`
	WorktreePath      string `json:"worktreePath"`
	SourceBranch      string `json:"sourceBranch"`
	Repository        string `json:"repository"`
	PRURL             string `json:"prUrl"`
	PRNumber          int64  `json:"prNumber"`
	TargetSHA         string `json:"targetSha"`
	ReviewedBaseSHA   string `json:"reviewedBaseSha"`
	CurrentBaseSHA    string `json:"currentBaseSha"`
	ReviewID          string `json:"reviewId"`
	ReviewRunID       string `json:"reviewRunId"`
	BatchID           string `json:"batchId"`
}

type arbiterCandidateFacts struct {
	CommitSHA     string              `json:"commitSha"`
	TreeSHA       string              `json:"treeSha"`
	HistoryDigest string              `json:"historyDigest"`
	DiffDigest    string              `json:"diffDigest"`
	Files         []arbiterFileStatus `json:"files"`
}

type arbiterFileStatus struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type arbiterCheckFact struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	HeadSHA    string `json:"headSha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type arbiterReviewFact struct {
	ReviewID          string `json:"reviewId"`
	ReviewRunID       string `json:"reviewRunId"`
	BatchID           string `json:"batchId"`
	Harness           string `json:"harness"`
	Channel           string `json:"channel"`
	Verdict           string `json:"verdict"`
	BodyDigest        string `json:"bodyDigest"`
	ProviderSetDigest string `json:"providerSetDigest"`
}

type arbiterQueueFact struct {
	Sequence    int64  `json:"sequence"`
	AdmissionID string `json:"admissionId"`
	SessionID   string `json:"sessionId"`
	ReviewRunID string `json:"reviewRunId"`
	PRURL       string `json:"prUrl"`
	TargetSHA   string `json:"targetSha"`
	Status      string `json:"status"`
	LeaseID     string `json:"leaseId,omitempty"`
	MergeSHA    string `json:"mergeSha,omitempty"`
}

type arbiterMechanicalFacts struct {
	ProviderMergeable        string   `json:"providerMergeable"`
	ProviderMergeStateStatus string   `json:"providerMergeStateStatus"`
	NormalizedMergeability   string   `json:"normalizedMergeability"`
	ReviewedBaseAncestor     bool     `json:"reviewedBaseAncestor"`
	CanonicalMain            string   `json:"canonicalMain"`
	MergeTreeClean           bool     `json:"mergeTreeClean"`
	MergeTreeEvidenceDigest  string   `json:"mergeTreeEvidenceDigest"`
	CandidateContentDigest   string   `json:"candidateContentDigest"`
	CurrentContentDigest     string   `json:"currentContentDigest"`
	ConflictPaths            []string `json:"conflictPaths"`
	ExhaustedRecoveries      []string `json:"exhaustedRecoveries"`
}

type arbiterInput struct {
	SchemaVersion      string                 `json:"schemaVersion"`
	IncidentSchema     string                 `json:"incidentSchema"`
	IncidentID         string                 `json:"incidentId"`
	Generation         int64                  `json:"generation"`
	IdentityDigest     string                 `json:"identityDigest"`
	SourcePacketDigest string                 `json:"sourcePacketDigest"`
	Reason             string                 `json:"reason"`
	SourceRecordedAt   string                 `json:"sourceRecordedAt"`
	DerivedAt          string                 `json:"derivedAt"`
	Scope              arbiterScope           `json:"scope"`
	ScopeDigest        string                 `json:"scopeDigest"`
	Identity           arbiterIdentity        `json:"identity"`
	Candidate          arbiterCandidateFacts  `json:"candidate"`
	Checks             []arbiterCheckFact     `json:"checks"`
	CheckSetDigest     string                 `json:"checkSetDigest"`
	Review             arbiterReviewFact      `json:"review"`
	ReviewSetDigest    string                 `json:"reviewSetDigest"`
	FrozenQueue        []arbiterQueueFact     `json:"frozenQueue"`
	FrozenQueueDigest  string                 `json:"frozenQueueDigest"`
	Mechanical         arbiterMechanicalFacts `json:"mechanical"`
	MechanicalDigest   string                 `json:"mechanicalDigest"`
	AllowedPaths       []string               `json:"allowedPaths"`
	AllowedSafeStops   []string               `json:"allowedSafeStops"`
}

// ArbiterDecision is the only model-produced artifact. Validation below binds
// every identity field to one already-persisted immutable input.
type ArbiterDecision struct {
	SchemaVersion   string                `json:"schemaVersion"`
	IncidentID      string                `json:"incidentId"`
	Generation      int64                 `json:"generation"`
	IdentityDigest  string                `json:"identityDigest"`
	InputDigest     string                `json:"inputDigest"`
	AdmissionID     string                `json:"admissionId"`
	TaskID          string                `json:"taskId"`
	SessionID       string                `json:"sessionId"`
	Repository      string                `json:"repository"`
	PRURL           string                `json:"prUrl"`
	PRNumber        int64                 `json:"prNumber"`
	TargetSHA       string                `json:"targetSha"`
	CurrentBaseSHA  string                `json:"currentBaseSha"`
	Verdict         string                `json:"verdict"`
	RecoveryOwner   *ArbiterRecoveryOwner `json:"recoveryOwner,omitempty"`
	RecoveryPath    *ArbiterRecoveryPath  `json:"recoveryPath,omitempty"`
	SafeStopCode    string                `json:"safeStopCode,omitempty"`
	Summary         string                `json:"summary"`
	EvidenceDigests []string              `json:"evidenceDigests"`
}

type ArbiterRecoveryOwner struct {
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId"`
}

type ArbiterRecoveryPath struct {
	Kind            string `json:"kind"`
	MaxWorkerCalls  int64  `json:"maxWorkerCalls"`
	MaxFreshReviews int64  `json:"maxFreshReviews"`
}

func arbiterTask(session domain.SessionRecord) (string, string, bool) {
	if !validTaskIdentity(session) {
		return "", "", false
	}
	taskID := strings.TrimPrefix(session.DisplayName, TaskDisplayPrefix)
	if (session.ID == ArbiterSessionA && taskID != ArbiterTaskA) || (session.ID == ArbiterSessionB && taskID != ArbiterTaskB) {
		return "", "", false
	}
	prefix := TaskPromptPrefix + taskID + ": "
	return taskID, strings.TrimPrefix(session.Metadata.Prompt, prefix), true
}

func (e *Engine) deriveArbiterIncident(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation, derivedAt time.Time) (domain.DCPReleaseArbiterIncident, error) {
	if admission.Status != domain.DCPAdmissionIncident || admission.ErrorCode != "merge_conflict_or_ambiguity" ||
		(admission.SessionID != ArbiterSessionA && admission.SessionID != ArbiterSessionB) ||
		candidate.session.ID != admission.SessionID || candidate.run.ID != admission.ReviewRunID ||
		admission.IncidentPacket == "" || !admissionFacts(candidate, observation, review) {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: source incident identity is ineligible")
	}
	var source incidentPacket
	dec := json.NewDecoder(strings.NewReader(admission.IncidentPacket))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&source); err != nil || source.SchemaVersion != "dcp.review-lab.arbiter-needed/v1" || source.Reason != "merge_conflict_or_ambiguity" ||
		source.AdmissionID != admission.ID || source.Sequence != admission.Sequence || source.LeaseID != admission.LeaseID ||
		source.SessionID != string(admission.SessionID) || source.ReviewRunID != admission.ReviewRunID || source.TargetSHA != admission.TargetSHA {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: source packet is not exact")
	}
	taskID, taskText, ok := arbiterTask(candidate.session)
	if !ok {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: exact approved task is unavailable")
	}
	currentBase, err := e.syncCanonicalMain(ctx, candidate, strings.ToLower(observation.PR.BaseSHA))
	if err != nil || !validSHA(currentBase) {
		return domain.DCPReleaseArbiterIncident{}, fmt.Errorf("dcp arbiter: canonical main: %w", err)
	}
	mechanical, err := e.arbiterMechanicalEvidence(ctx, candidate, admission.ReviewBaseSHA, observation, currentBase)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	scope := arbiterScope{Repository: RepositoryFullName, TaskID: taskID, TaskText: taskText, FixedSyntheticProfile: ProfileAgentRules}
	scopeDigest, _, err := canonicalDigest(scope)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	history, err := e.git(ctx, candidate.project.Path, "log", "--reverse", "--format=%H%x00%T", strings.ToLower(admission.ReviewBaseSHA)+".."+strings.ToLower(admission.TargetSHA))
	if err != nil || history == "" {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: candidate history is unavailable")
	}
	historyDigest := digestString(history)
	diff, err := e.git(ctx, candidate.project.Path, "diff", "--binary", "--full-index", strings.ToLower(admission.ReviewBaseSHA)+".."+strings.ToLower(admission.TargetSHA))
	if err != nil || diff == "" {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: candidate diff is unavailable")
	}
	diffDigest := digestString(diff)
	files, err := e.arbiterFileStatuses(ctx, candidate, admission)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	treeSHA, err := e.git(ctx, candidate.project.Path, "rev-parse", strings.ToLower(admission.TargetSHA)+"^{tree}")
	if err != nil || !validSHA(treeSHA) {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: candidate tree identity is unavailable")
	}
	checks := arbiterChecks(observation)
	checkDigest, _, err := canonicalDigest(checks)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	providerReviewDigest, _, err := canonicalDigest(review)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	reviewFact := arbiterReviewFact{
		ReviewID: candidate.run.ReviewID, ReviewRunID: candidate.run.ID, BatchID: candidate.run.BatchID,
		Harness: string(candidate.run.Harness), Channel: candidate.run.ResultChannel,
		Verdict: string(candidate.run.Verdict), BodyDigest: digestString(candidate.run.Body), ProviderSetDigest: providerReviewDigest,
	}
	reviewDigest, _, err := canonicalDigest(reviewFact)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	queue, err := e.arbiterFrozenQueue(ctx)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	queueDigest, _, err := canonicalDigest(queue)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	mechanicalDigest, _, err := canonicalDigest(mechanical)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	sourceDigest := digestString(admission.IncidentPacket)
	identity := arbiterIdentity{
		AdmissionID: admission.ID, AdmissionSequence: admission.Sequence, IncidentLeaseID: admission.LeaseID,
		TaskID: taskID, SessionID: string(admission.SessionID), WorktreePath: candidate.session.Metadata.WorkspacePath,
		SourceBranch: candidate.pr.SourceBranch, Repository: RepositoryFullName, PRURL: admission.PRURL,
		PRNumber: admission.PRNumber, TargetSHA: strings.ToLower(admission.TargetSHA),
		ReviewedBaseSHA: strings.ToLower(admission.ReviewBaseSHA), CurrentBaseSHA: currentBase,
		ReviewID: admission.ReviewID, ReviewRunID: admission.ReviewRunID, BatchID: candidate.run.BatchID,
	}
	identityParts := []string{
		ArbiterIncidentSchema, "1", RepositoryFullName, admission.ID, strconv.FormatInt(admission.Sequence, 10),
		admission.LeaseID, sourceDigest, taskID, string(admission.SessionID), candidate.session.Metadata.WorkspacePath,
		candidate.pr.SourceBranch, admission.PRURL, strconv.FormatInt(admission.PRNumber, 10),
		strings.ToLower(admission.ReviewBaseSHA), currentBase, strings.ToLower(admission.TargetSHA), admission.ReviewID,
		admission.ReviewRunID, candidate.run.BatchID, scopeDigest, historyDigest, diffDigest, checkDigest,
		reviewDigest, queueDigest, mechanicalDigest,
	}
	identityDigest := digestString(strings.Join(identityParts, "\x00"))
	incidentID := "dcp-global-release-" + identityDigest
	input := arbiterInput{
		SchemaVersion: ArbiterInputSchema, IncidentSchema: ArbiterIncidentSchema, IncidentID: incidentID,
		Generation: 1, IdentityDigest: identityDigest, SourcePacketDigest: sourceDigest,
		Reason: "merge_conflict_or_ambiguity", SourceRecordedAt: source.RecordedAt,
		DerivedAt: derivedAt.UTC().Format(time.RFC3339Nano), Scope: scope, ScopeDigest: scopeDigest, Identity: identity,
		Candidate: arbiterCandidateFacts{CommitSHA: strings.ToLower(admission.TargetSHA), TreeSHA: strings.ToLower(treeSHA), HistoryDigest: historyDigest, DiffDigest: diffDigest, Files: files},
		Checks:    checks, CheckSetDigest: checkDigest, Review: reviewFact, ReviewSetDigest: reviewDigest,
		FrozenQueue: queue, FrozenQueueDigest: queueDigest, Mechanical: mechanical, MechanicalDigest: mechanicalDigest,
		AllowedPaths:     []string{"same_worker_conflict_repair"},
		AllowedSafeStops: []string{"scope_not_proven", "identity_ambiguous", "evidence_incomplete", "no_safe_bounded_path"},
	}
	inputDigest, inputJSON, err := canonicalDigest(input)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, err
	}
	if len(inputJSON) > arbiterMaxInputBytes {
		return domain.DCPReleaseArbiterIncident{}, errors.New("dcp arbiter: frozen input exceeds 16384 bytes")
	}
	return domain.DCPReleaseArbiterIncident{
		IncidentID: incidentID, Generation: 1, IdentityDigest: identityDigest,
		AdmissionID: admission.ID, IncidentLeaseID: admission.LeaseID,
		SourcePacketJSON: admission.IncidentPacket, SourcePacketDigest: sourceDigest,
		InputJSON: string(inputJSON), InputDigest: inputDigest, TaskID: taskID,
		SessionID: admission.SessionID, WorktreePath: candidate.session.Metadata.WorkspacePath,
		SourceBranch: candidate.pr.SourceBranch, PRURL: admission.PRURL, PRNumber: admission.PRNumber,
		TargetSHA: strings.ToLower(admission.TargetSHA), ReviewedBaseSHA: strings.ToLower(admission.ReviewBaseSHA),
		CurrentBaseSHA: currentBase, ReviewID: admission.ReviewID, ReviewRunID: admission.ReviewRunID,
		BatchID: candidate.run.BatchID, ScopeDigest: scopeDigest, HistoryDigest: historyDigest, DiffDigest: diffDigest,
		CheckSetDigest: checkDigest, ReviewSetDigest: reviewDigest, FrozenQueueDigest: queueDigest,
		MechanicalDigest: mechanicalDigest, Model: ArbiterModel, Reasoning: ArbiterReasoning,
		TokenBudget: ArbiterTokenBudget, RuntimeHandleID: ArbiterRuntimeHandle, LaunchID: incidentID,
		Status: domain.DCPArbiterRequested, CreatedAt: derivedAt.UTC(), UpdatedAt: derivedAt.UTC(),
	}, nil
}

func (e *Engine) arbiterMechanicalEvidence(ctx context.Context, candidate mergeCandidate, reviewedBase string, observation ports.SCMObservation, currentBase string) (arbiterMechanicalFacts, error) {
	if mergeDisposition(observation) != dispositionIncident || !validSHA(currentBase) {
		return arbiterMechanicalFacts{}, errors.New("dcp arbiter: provider did not prove a conflict incident")
	}
	if _, err := e.git(ctx, candidate.project.Path, "merge-base", "--is-ancestor", strings.ToLower(reviewedBase), currentBase); err != nil {
		return arbiterMechanicalFacts{}, errors.New("dcp arbiter: reviewed base is not an ancestor of current main")
	}
	mergeTree, mergeErr := e.git(ctx, candidate.project.Path, "merge-tree", "--write-tree", currentBase, strings.ToLower(candidate.run.TargetSHA))
	if mergeErr == nil {
		return arbiterMechanicalFacts{}, errors.New("dcp arbiter: merge tree is clean; no structural ambiguity exists")
	}
	conflicts, err := e.arbiterConflictPaths(ctx, candidate, reviewedBase, currentBase)
	if err != nil {
		return arbiterMechanicalFacts{}, err
	}
	if len(conflicts) != 1 || conflicts[0] != arbiterConflictPath {
		return arbiterMechanicalFacts{}, errors.New("dcp arbiter: conflict paths are outside the bounded canary")
	}
	candidateLine, currentLine, err := e.arbiterCanarySourceLines(ctx, candidate.project.Path, candidate.run.TargetSHA, currentBase)
	if err != nil {
		return arbiterMechanicalFacts{}, err
	}
	return arbiterMechanicalFacts{
		ProviderMergeable:        observation.PR.ProviderMergeable,
		ProviderMergeStateStatus: observation.PR.ProviderMergeStateStatus,
		NormalizedMergeability:   observation.Mergeability.State, ReviewedBaseAncestor: true,
		CanonicalMain: currentBase, MergeTreeClean: false, MergeTreeEvidenceDigest: digestString(mergeTree),
		CandidateContentDigest: digestString(candidateLine), CurrentContentDigest: digestString(currentLine),
		ConflictPaths:       conflicts,
		ExhaustedRecoveries: []string{"canonical_main_synced", "reviewed_base_ancestry_proven", "merge_tree_conflict_proven", "ordinary_fast_forward_refresh_inapplicable"},
	}, nil
}

func (e *Engine) arbiterCanarySourceLines(ctx context.Context, repo, candidateSHA, currentBaseSHA string) (string, string, error) {
	candidateLine, err := e.git(ctx, repo, "show", strings.ToLower(candidateSHA)+":"+arbiterConflictPath)
	if err != nil || !validArbiterCanaryLine(candidateLine) {
		return "", "", errors.New("dcp arbiter: candidate canary content is not one bounded line")
	}
	currentLine, err := e.git(ctx, repo, "show", strings.ToLower(currentBaseSHA)+":"+arbiterConflictPath)
	if err != nil || !validArbiterCanaryLine(currentLine) || candidateLine == currentLine {
		return "", "", errors.New("dcp arbiter: current-main canary content is not one distinct bounded line")
	}
	return candidateLine, currentLine, nil
}

func validArbiterCanaryLine(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (e *Engine) validateArbiterRecoveryCandidate(ctx context.Context, candidate mergeCandidate, incident domain.DCPReleaseArbiterIncident) error {
	newHead := strings.ToLower(candidate.run.TargetSHA)
	currentBase := strings.ToLower(incident.CurrentBaseSHA)
	if !validSHA(newHead) || !validSHA(currentBase) || strings.EqualFold(newHead, incident.TargetSHA) {
		return errors.New("dcp arbiter: recovery head identity is invalid")
	}
	if _, err := e.git(ctx, candidate.project.Path, "merge-base", "--is-ancestor", currentBase, newHead); err != nil {
		return errors.New("dcp arbiter: recovery head is not based on exact current main")
	}
	parents, err := e.git(ctx, candidate.project.Path, "show", "-s", "--format=%P", newHead)
	if err != nil || parents != currentBase {
		return errors.New("dcp arbiter: recovery is not one direct commit on exact current main")
	}
	status, err := e.git(ctx, candidate.project.Path, "diff", "--name-status", currentBase+".."+newHead)
	if err != nil || status != "M\t"+arbiterConflictPath {
		return errors.New("dcp arbiter: recovery diff changed scope or did not preserve the canary")
	}
	candidateLine, currentLine, err := e.arbiterCanarySourceLines(ctx, candidate.project.Path, incident.TargetSHA, currentBase)
	if err != nil {
		return err
	}
	recovered, err := e.git(ctx, candidate.project.Path, "show", newHead+":"+arbiterConflictPath)
	if err != nil {
		return errors.New("dcp arbiter: recovered canary content is unavailable")
	}
	lines := strings.Split(recovered, "\n")
	if len(lines) != 2 || !validArbiterCanaryLine(lines[0]) || !validArbiterCanaryLine(lines[1]) || lines[0] == lines[1] {
		return errors.New("dcp arbiter: recovered canary is not exactly two distinct bounded lines")
	}
	seen := map[string]bool{lines[0]: true, lines[1]: true}
	if !seen[candidateLine] || !seen[currentLine] {
		return errors.New("dcp arbiter: recovery did not preserve both exact canary intents")
	}
	return nil
}

func (e *Engine) arbiterConflictPaths(ctx context.Context, candidate mergeCandidate, reviewedBase, currentBase string) ([]string, error) {
	candidatePaths, err := e.git(ctx, candidate.project.Path, "diff", "--name-only", strings.ToLower(reviewedBase)+".."+strings.ToLower(candidate.run.TargetSHA))
	if err != nil {
		return nil, err
	}
	mainPaths, err := e.git(ctx, candidate.project.Path, "diff", "--name-only", strings.ToLower(reviewedBase)+".."+currentBase)
	if err != nil {
		return nil, err
	}
	mainSet := map[string]bool{}
	for _, path := range strings.Split(mainPaths, "\n") {
		if path != "" {
			mainSet[path] = true
		}
	}
	var result []string
	for _, path := range strings.Split(candidatePaths, "\n") {
		if path != "" && mainSet[path] {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (e *Engine) arbiterFileStatuses(ctx context.Context, candidate mergeCandidate, admission domain.DCPReviewLabAdmission) ([]arbiterFileStatus, error) {
	out, err := e.git(ctx, candidate.project.Path, "diff", "--name-status", strings.ToLower(admission.ReviewBaseSHA)+".."+strings.ToLower(admission.TargetSHA))
	if err != nil {
		return nil, err
	}
	var files []arbiterFileStatus
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(files) >= 8 {
			return nil, errors.New("dcp arbiter: candidate file status is unbounded or malformed")
		}
		files = append(files, arbiterFileStatus{Status: fields[0], Path: fields[1]})
	}
	if len(files) != 1 || files[0].Status != "A" || files[0].Path != arbiterConflictPath {
		return nil, errors.New("dcp arbiter: candidate diff is outside the exact canary task")
	}
	return files, nil
}

func arbiterChecks(observation ports.SCMObservation) []arbiterCheckFact {
	checks := make([]arbiterCheckFact, 0, len(observation.CI.Checks))
	for _, check := range observation.CI.Checks {
		checks = append(checks, arbiterCheckFact{ID: check.ProviderID, Name: check.Name, HeadSHA: strings.ToLower(observation.CI.HeadSHA), Status: check.Status, Conclusion: check.Conclusion})
	}
	sort.Slice(checks, func(i, j int) bool {
		if checks[i].Name == checks[j].Name {
			return checks[i].ID < checks[j].ID
		}
		return checks[i].Name < checks[j].Name
	})
	return checks
}

func (e *Engine) arbiterFrozenQueue(ctx context.Context) ([]arbiterQueueFact, error) {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return nil, err
	}
	var queue []arbiterQueueFact
	for _, row := range rows {
		stage2 := row.SessionID == ArbiterSessionA || row.SessionID == ArbiterSessionB
		nonterminal := row.Status == domain.DCPAdmissionWaiting || row.Status == domain.DCPAdmissionClaimed || row.Status == domain.DCPAdmissionRefreshing || row.Status == domain.DCPAdmissionIncident
		if !stage2 && !nonterminal {
			continue
		}
		queue = append(queue, arbiterQueueFact{Sequence: row.Sequence, AdmissionID: row.ID, SessionID: string(row.SessionID), ReviewRunID: row.ReviewRunID, PRURL: row.PRURL, TargetSHA: row.TargetSHA, Status: string(row.Status), LeaseID: row.LeaseID, MergeSHA: row.MergeCommitSHA})
	}
	if len(queue) != 2 {
		return nil, errors.New("dcp arbiter: frozen Stage 2 cohort is not exactly complete")
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].Sequence < queue[j].Sequence })
	seen := map[string]bool{}
	for _, row := range queue {
		seen[row.SessionID] = true
	}
	if !seen[ArbiterSessionA] || !seen[ArbiterSessionB] {
		return nil, errors.New("dcp arbiter: frozen Stage 2 cohort identity is incomplete")
	}
	return queue, nil
}

func canonicalDigest(value any) (string, []byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	return digestBytes(data), data, nil
}

func digestString(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func ArbiterDecisionJSONSchema(incident domain.DCPReleaseArbiterIncident) ([]byte, error) {
	if incident.IncidentID == "" || !validDigest(incident.IdentityDigest) || !validDigest(incident.InputDigest) || !validSHA(incident.TargetSHA) || !validSHA(incident.CurrentBaseSHA) {
		return nil, errors.New("dcp arbiter: decision schema identity is invalid")
	}
	constValue := func(value any) map[string]any { return map[string]any{"const": value} }
	properties := map[string]any{
		"schemaVersion": constValue(ArbiterDecisionSchema), "incidentId": constValue(incident.IncidentID),
		"generation": constValue(int64(1)), "identityDigest": constValue(incident.IdentityDigest), "inputDigest": constValue(incident.InputDigest),
		"admissionId": constValue(incident.AdmissionID), "taskId": constValue(incident.TaskID), "sessionId": constValue(string(incident.SessionID)),
		"repository": constValue(RepositoryFullName), "prUrl": constValue(incident.PRURL), "prNumber": constValue(incident.PRNumber),
		"targetSha": constValue(incident.TargetSHA), "currentBaseSha": constValue(incident.CurrentBaseSHA),
		"verdict":         map[string]any{"enum": []string{"assign_recovery", "safe_stop"}},
		"recoveryOwner":   map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "sessionId"}, "properties": map[string]any{"kind": constValue("same_worker"), "sessionId": constValue(string(incident.SessionID))}},
		"recoveryPath":    map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "maxWorkerCalls", "maxFreshReviews"}, "properties": map[string]any{"kind": constValue("same_worker_conflict_repair"), "maxWorkerCalls": constValue(int64(1)), "maxFreshReviews": constValue(int64(1))}},
		"safeStopCode":    map[string]any{"enum": []string{"scope_not_proven", "identity_ambiguous", "evidence_incomplete", "no_safe_bounded_path"}},
		"summary":         map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
		"evidenceDigests": map[string]any{"type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": true, "items": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}},
	}
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": false,
		"required":   []string{"schemaVersion", "incidentId", "generation", "identityDigest", "inputDigest", "admissionId", "taskId", "sessionId", "repository", "prUrl", "prNumber", "targetSha", "currentBaseSha", "verdict", "summary", "evidenceDigests"},
		"properties": properties,
		"oneOf": []any{
			map[string]any{"properties": map[string]any{"verdict": constValue("assign_recovery")}, "required": []string{"recoveryOwner", "recoveryPath"}, "not": map[string]any{"required": []string{"safeStopCode"}}},
			map[string]any{"properties": map[string]any{"verdict": constValue("safe_stop")}, "required": []string{"safeStopCode"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"recoveryOwner"}}, map[string]any{"required": []string{"recoveryPath"}}}}},
		},
	}
	return json.Marshal(schema)
}

func ReadArbiterDecision(path string, incident domain.DCPReleaseArbiterIncident) (ArbiterDecision, []byte, error) {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > arbiterMaxResultBytes || info.Mode().Perm()&0o022 != 0 {
		return ArbiterDecision{}, nil, errors.New("dcp arbiter: result is not an exact owner-controlled bounded file")
	}
	file, err := os.Open(path)
	if err != nil {
		return ArbiterDecision{}, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, arbiterMaxResultBytes+1))
	if err != nil || len(data) > arbiterMaxResultBytes {
		return ArbiterDecision{}, nil, errors.New("dcp arbiter: result exceeds its bound")
	}
	return ParseArbiterDecision(data, incident)
}

func ParseArbiterDecision(data []byte, incident domain.DCPReleaseArbiterIncident) (ArbiterDecision, []byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ArbiterDecision{}, nil, fmt.Errorf("dcp arbiter: malformed decision: %w", err)
	}
	var decision ArbiterDecision
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decision); err != nil {
		return ArbiterDecision{}, nil, fmt.Errorf("dcp arbiter: malformed decision: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return ArbiterDecision{}, nil, errors.New("dcp arbiter: decision has trailing JSON")
	}
	if err := validateArbiterDecision(decision, incident); err != nil {
		return ArbiterDecision{}, nil, err
	}
	_, hasOwner := raw["recoveryOwner"]
	_, hasPath := raw["recoveryPath"]
	_, hasSafeStop := raw["safeStopCode"]
	if (decision.Verdict == "assign_recovery" && (!hasOwner || !hasPath || hasSafeStop)) ||
		(decision.Verdict == "safe_stop" && (hasOwner || hasPath || !hasSafeStop)) {
		return ArbiterDecision{}, nil, errors.New("dcp arbiter: optional decision fields do not match the selected verdict")
	}
	canonical, err := json.Marshal(decision)
	if err != nil {
		return ArbiterDecision{}, nil, err
	}
	return decision, canonical, nil
}

func validateArbiterDecision(d ArbiterDecision, incident domain.DCPReleaseArbiterIncident) error {
	if d.SchemaVersion != ArbiterDecisionSchema || d.IncidentID != incident.IncidentID || d.Generation != 1 ||
		d.IdentityDigest != incident.IdentityDigest || d.InputDigest != incident.InputDigest || d.AdmissionID != incident.AdmissionID ||
		d.TaskID != incident.TaskID || d.SessionID != string(incident.SessionID) || d.Repository != RepositoryFullName ||
		d.PRURL != incident.PRURL || d.PRNumber != incident.PRNumber || !strings.EqualFold(d.TargetSHA, incident.TargetSHA) ||
		!strings.EqualFold(d.CurrentBaseSHA, incident.CurrentBaseSHA) || len(d.Summary) == 0 || len(d.Summary) > 512 ||
		strings.TrimSpace(d.Summary) != d.Summary || len(d.EvidenceDigests) < 1 || len(d.EvidenceDigests) > 8 {
		return errors.New("dcp arbiter: decision identity or bounds are invalid")
	}
	allowedEvidence := map[string]bool{
		incident.SourcePacketDigest: true, incident.ScopeDigest: true, incident.HistoryDigest: true,
		incident.DiffDigest: true, incident.CheckSetDigest: true, incident.ReviewSetDigest: true,
		incident.FrozenQueueDigest: true, incident.MechanicalDigest: true, incident.InputDigest: true,
	}
	seen := map[string]bool{}
	for _, digest := range d.EvidenceDigests {
		if !validDigest(digest) || !allowedEvidence[digest] || seen[digest] {
			return errors.New("dcp arbiter: decision cites foreign or duplicate evidence")
		}
		seen[digest] = true
	}
	switch d.Verdict {
	case "assign_recovery":
		if d.RecoveryOwner == nil || d.RecoveryPath == nil || d.SafeStopCode != "" ||
			d.RecoveryOwner.Kind != "same_worker" || d.RecoveryOwner.SessionID != string(incident.SessionID) ||
			d.RecoveryPath.Kind != "same_worker_conflict_repair" || d.RecoveryPath.MaxWorkerCalls != 1 || d.RecoveryPath.MaxFreshReviews != 1 {
			return errors.New("dcp arbiter: recovery owner or path is outside the allowlist")
		}
	case "safe_stop":
		if d.RecoveryOwner != nil || d.RecoveryPath != nil || !allowedSafeStop(d.SafeStopCode) {
			return errors.New("dcp arbiter: safe-stop decision is malformed")
		}
	default:
		return errors.New("dcp arbiter: verdict is not allowed")
	}
	return nil
}

func allowedSafeStop(code string) bool {
	switch code {
	case "scope_not_proven", "identity_ambiguous", "evidence_incomplete", "no_safe_bounded_path":
		return true
	default:
		return false
	}
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
