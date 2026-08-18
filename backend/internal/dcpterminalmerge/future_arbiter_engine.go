package dcpterminalmerge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var errFutureArbiterCohortPending = errors.New("DCP future arbiter cohort is not yet complete")

// FutureArbiterStore is the bounded durable surface used by the ordinary-card
// arbiter inside the existing policy-task and terminal-merge authority.
type FutureArbiterStore interface {
	GetDCPFutureArbiterIncidentByID(context.Context, string) (domain.DCPFutureArbiterIncident, bool, error)
	GetDCPFutureArbiterIncidentByAdmission(context.Context, string) (domain.DCPFutureArbiterIncident, bool, error)
	GetDCPFutureArbiterIncidentByTask(context.Context, string) (domain.DCPFutureArbiterIncident, bool, error)
	ListDCPFutureArbiterIncidents(context.Context) ([]domain.DCPFutureArbiterIncident, error)
	CountDCPFutureArbiterGenerationsForTask(context.Context, string) (int64, error)
	GetDCPFutureArbiterSchemaRecoveryByPredecessor(context.Context, string) (domain.DCPFutureArbiterSchemaRecovery, bool, error)
	GetDCPFutureArbiterResultRecovery(context.Context, string) (domain.DCPFutureArbiterResultRecovery, bool, error)
	OpenDCPFutureArbiterIncident(context.Context, domain.DCPFutureArbiterIncident, domain.DCPModelAction) (domain.DCPFutureArbiterIncident, bool, error)
	OpenDCPFutureArbiterSchemaRecovery(context.Context, domain.DCPFutureArbiterIncident, domain.DCPFutureArbiterSchemaRecovery, domain.DCPFutureArbiterIncident, domain.DCPModelAction) (domain.DCPFutureArbiterIncident, bool, error)
	FailDCPFutureArbiterIncident(context.Context, domain.DCPFutureArbiterIncident, domain.DCPModelAction, string, time.Time) (bool, error)
	RecordDCPFutureArbiterDecision(context.Context, domain.DCPFutureArbiterIncident, domain.DCPModelAction, string, string, domain.DCPFutureArbiterVerdict, string, string, string, string, time.Time) (bool, error)
	RecoverDCPFutureArbiterExactDecision(context.Context, domain.DCPFutureArbiterIncident, string, string, string, string, string, time.Time) (bool, error)
	RecoverDCPFutureArbiterExactHumanGate(context.Context, domain.DCPFutureArbiterIncident, string, string, string, string, string, time.Time) (bool, error)
	FailDCPFutureArbiterResultRecovery(context.Context, string, string, time.Time) (bool, error)
	RebindDCPFutureArbiterAdmission(context.Context, domain.DCPFutureArbiterIncident, domain.DCPReviewLabPolicyTask, domain.ReviewRun, string, time.Time) (bool, error)
	GetDCPReviewLabPolicyTaskByTaskID(context.Context, string) (domain.DCPReviewLabPolicyTask, bool, error)
	GetDCPReviewLabPolicyTaskBySession(context.Context, domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error)
	ListDCPReviewLabPolicyTasks(context.Context) ([]domain.DCPReviewLabPolicyTask, error)
	GetDCPReviewLabAdmissionByID(context.Context, string) (domain.DCPReviewLabAdmission, bool, error)
	GetDCPModelActionByID(context.Context, string) (domain.DCPModelAction, bool, error)
	StartDCPModelAction(context.Context, domain.DCPModelAction, string, string, time.Time) (bool, error)
}

func (e *Engine) futureArbiterStore() (FutureArbiterStore, error) {
	store, ok := e.store.(FutureArbiterStore)
	if !ok {
		return nil, errors.New("DCP future arbiter durable store is unavailable")
	}
	return store, nil
}

// SetFutureArbiterLauncher installs the isolated one-shot model launcher.
func (e *Engine) SetFutureArbiterLauncher(launcher FutureArbiterLauncher) { e.futureArbiter = launcher }

// SetPolicyActionDrain connects arbiter actions to the existing global action queue.
func (e *Engine) SetPolicyActionDrain(drain func(context.Context) error) { e.policyActionDrain = drain }

func eligibleFutureArbiterKind(code string) bool {
	return code == "merge_conflict_or_ambiguity" || code == "canonical_main_diverged" || code == "provider_not_clean"
}

func (e *Engine) reconcileFutureArbiters(ctx context.Context) error {
	store, ok := e.store.(FutureArbiterStore)
	if !ok {
		return nil
	}
	recovered, err := e.recoverFutureArbiterExactResult(ctx, store)
	if err != nil {
		return err
	}
	if recovered && e.policyActionDrain != nil {
		if err := e.policyActionDrain(ctx); err != nil {
			return err
		}
	}
	admissions, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return err
	}
	opened := false
	for _, admission := range admissions {
		if admission.Status != domain.DCPAdmissionIncident || !eligibleFutureArbiterKind(admission.ErrorCode) {
			continue
		}
		task, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, admission.SessionID)
		if err != nil {
			return err
		}
		if !found || task.State != domain.DCPPolicyIncident || task.AdmissionID != admission.ID {
			continue
		}
		existing, found, err := store.GetDCPFutureArbiterIncidentByAdmission(ctx, admission.ID)
		if err != nil {
			return err
		}
		var schemaRecovery domain.DCPFutureArbiterSchemaRecovery
		recoveringSchema := false
		if found && existing.Status != domain.DCPFutureArbiterHold {
			if existing.Status != domain.DCPFutureArbiterFailed {
				continue
			}
			schemaRecovery, recoveringSchema, err = store.GetDCPFutureArbiterSchemaRecoveryByPredecessor(ctx, existing.IncidentID)
			if err != nil {
				return err
			}
			if !recoveringSchema || schemaRecovery.Status != "authorized" {
				continue
			}
			if e.futureArbiter == nil {
				return errors.New("DCP future arbiter schema recovery launcher is unavailable")
			}
			if err := e.futureArbiter.PreflightSchemaRecovery(ctx, existing, schemaRecovery); err != nil {
				return err
			}
		}
		candidate, exact, err := e.candidateForFutureArbiterAdmission(ctx, admission)
		if err != nil || !exact {
			return errors.Join(err, errors.New("DCP future arbiter candidate is not exact"))
		}
		observation, review, err := e.fresh(ctx, candidate.pr)
		if err != nil {
			return err
		}
		generationCount, err := store.CountDCPFutureArbiterGenerationsForTask(ctx, task.TaskID)
		if err != nil {
			return err
		}
		generation := generationCount + 1
		if recoveringSchema && generation != schemaRecovery.SuccessorGeneration {
			return errors.New("DCP future arbiter schema recovery generation drifted")
		}
		incident, action, err := e.deriveFutureArbiterIncident(ctx, admission, candidate, observation, review, generation, e.clock())
		if errors.Is(err, errFutureArbiterCohortPending) {
			continue
		}
		if err != nil {
			return err
		}
		if found && !recoveringSchema && existing.CurrentMainSHA == incident.CurrentMainSHA {
			continue
		}
		var created bool
		if recoveringSchema {
			_, created, err = store.OpenDCPFutureArbiterSchemaRecovery(ctx, existing, schemaRecovery, incident, action)
		} else {
			_, created, err = store.OpenDCPFutureArbiterIncident(ctx, incident, action)
		}
		if err != nil {
			return err
		}
		opened = opened || created
	}
	if opened && e.policyActionDrain != nil {
		return e.policyActionDrain(ctx)
	}
	incidents, err := store.ListDCPFutureArbiterIncidents(ctx)
	if err != nil {
		return err
	}
	for _, incident := range incidents {
		switch incident.Status {
		case domain.DCPFutureArbiterClaimed:
			action, found, err := store.GetDCPModelActionByID(ctx, incident.ModelActionID)
			if err != nil || !found {
				return errors.Join(err, errors.New("DCP future arbiter claimed action disappeared"))
			}
			if err := e.LaunchPolicyArbiterAction(ctx, action); err != nil {
				return err
			}
		case domain.DCPFutureArbiterRunning:
			if e.futureArbiter == nil {
				return e.failFutureArbiter(ctx, incident, "launcher_unavailable")
			}
			resultPath, err := e.futureArbiter.FutureResultPath(incident)
			if err != nil {
				return err
			}
			if _, statErr := os.Lstat(resultPath); statErr == nil {
				data, readErr := os.ReadFile(resultPath)
				if readErr != nil {
					return readErr
				}
				if err := e.SubmitFutureArbiterDecision(ctx, incident.IncidentID, data); err != nil {
					return err
				}
			} else if !os.IsNotExist(statErr) {
				return statErr
			} else {
				alive, inspectErr := e.futureArbiter.FutureProcessAlive(ctx, incident)
				if inspectErr != nil {
					return inspectErr
				}
				if !alive {
					return e.failFutureArbiter(ctx, incident, "missing_result")
				}
				action, found, actionErr := store.GetDCPModelActionByID(ctx, incident.ModelActionID)
				if actionErr != nil || !found {
					return errors.Join(actionErr, errors.New("DCP future arbiter running action disappeared"))
				}
				if lifecycleErr := e.validateFutureArbiterLifecycle(ctx, incident, action, domain.DCPModelProcessExact); lifecycleErr != nil {
					return lifecycleErr
				}
			}
		}
	}
	return nil
}

func (e *Engine) recoverFutureArbiterExactResult(ctx context.Context, store FutureArbiterStore) (bool, error) {
	incidents, err := store.ListDCPFutureArbiterIncidents(ctx)
	if err != nil {
		return false, err
	}
	for _, incident := range incidents {
		recovery, found, err := store.GetDCPFutureArbiterResultRecovery(ctx, incident.IncidentID)
		if err != nil {
			return false, err
		}
		if !found || recovery.Status != "pending" {
			continue
		}
		fail := func(code string, cause error) (bool, error) {
			_, persistErr := store.FailDCPFutureArbiterResultRecovery(ctx, incident.IncidentID, code, e.clock())
			return false, errors.Join(cause, persistErr)
		}
		if e.futureArbiter == nil {
			return fail("launcher_unavailable", errors.New("DCP future arbiter result recovery launcher is unavailable"))
		}
		if err := e.revalidateFutureArbiter(ctx, incident); err != nil {
			return fail("identity_drift", err)
		}
		if err := e.futureArbiter.PreflightResultRecovery(ctx, incident, recovery); err != nil {
			return fail("artifact_or_process_drift", err)
		}
		resultPath, err := e.futureArbiter.FutureResultPath(incident)
		if err != nil {
			return fail("result_path_invalid", err)
		}
		data, err := os.ReadFile(resultPath)
		if err != nil || int64(len(data)) != recovery.ResultArtifactSize || digestBytes(data) != recovery.ResultArtifactDigest {
			return fail("result_digest_drift", errors.Join(err, errors.New("DCP future arbiter recovered result drifted after preflight")))
		}
		decision, canonical, err := ParseFutureArbiterDecision(data, incident)
		if err != nil {
			return fail("result_still_invalid", err)
		}
		verdict := domain.DCPFutureArbiterVerdict(decision.Verdict)
		if verdict != domain.DCPFutureVerdictRepair && verdict != domain.DCPFutureVerdictHumanGate {
			return fail("result_still_invalid", errors.New("DCP future arbiter recovered decision is outside an authorized terminal contour"))
		}
		if verdict == domain.DCPFutureVerdictRepair && decision.RepairTaskID != incident.TaskID {
			return fail("result_still_invalid", errors.New("DCP future arbiter recovered repair task drifted"))
		}
		changed, err := e.persistRecoveredFutureArbiterDecision(ctx, store, incident, decision, canonical)
		if err != nil || !changed {
			return false, errors.Join(err, errors.New("DCP future arbiter exact model-free result recovery was unavailable"))
		}
		return true, nil
	}
	return false, nil
}

func (e *Engine) persistRecoveredFutureArbiterDecision(ctx context.Context, store FutureArbiterStore, incident domain.DCPFutureArbiterIncident, decision FutureArbiterDecision, canonical []byte) (bool, error) {
	orderJSON, _ := json.Marshal(decision.Order)
	pathsJSON, _ := json.Marshal(decision.AffectedPaths)
	switch domain.DCPFutureArbiterVerdict(decision.Verdict) {
	case domain.DCPFutureVerdictRepair:
		if decision.RepairTaskID != incident.TaskID {
			return false, errors.New("DCP future arbiter recovered repair task drifted")
		}
		return store.RecoverDCPFutureArbiterExactDecision(ctx, incident, string(canonical), digestBytes(canonical), string(orderJSON), decision.RepairObjective, string(pathsJSON), e.clock())
	case domain.DCPFutureVerdictHumanGate:
		return store.RecoverDCPFutureArbiterExactHumanGate(ctx, incident, string(canonical), digestBytes(canonical), string(orderJSON), string(pathsJSON), decision.HumanQuestion, e.clock())
	default:
		return false, errors.New("DCP future arbiter recovered decision is outside an authorized terminal contour")
	}
}

// futureArbiterAllowsAdmission is the passive cohort hold. It performs only
// SQLite reads and is called from the existing terminal-merge event drain; it
// owns no timer, watcher, or model work.
func (e *Engine) futureArbiterAllowsAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	store, ok := e.store.(FutureArbiterStore)
	if !ok {
		return true, nil
	}
	incidents, err := store.ListDCPFutureArbiterIncidents(ctx)
	if err != nil {
		return false, err
	}
	tasks, err := store.ListDCPReviewLabPolicyTasks(ctx)
	if err != nil {
		return false, err
	}
	byID := make(map[string]domain.DCPReviewLabPolicyTask, len(tasks))
	byAdmission := make(map[string]domain.DCPReviewLabPolicyTask, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
		if task.AdmissionID != "" {
			byAdmission[task.AdmissionID] = task
		}
	}
	// Only the latest immutable generation controls a given admission. An older
	// failed generation remains audit evidence but must not shadow a separately
	// authorized successor forever.
	latestGenerationByAdmission := make(map[string]int64)
	for _, incident := range incidents {
		if incident.Generation > latestGenerationByAdmission[incident.AdmissionID] {
			latestGenerationByAdmission[incident.AdmissionID] = incident.Generation
		}
	}
	for _, incident := range incidents {
		if incident.Generation != latestGenerationByAdmission[incident.AdmissionID] {
			continue
		}
		switch incident.Status {
		case domain.DCPFutureArbiterRequested, domain.DCPFutureArbiterClaimed, domain.DCPFutureArbiterRunning,
			domain.DCPFutureArbiterRepairQueued, domain.DCPFutureArbiterRecoveryReviewed:
			if incident.AdmissionID != admission.ID {
				return false, nil
			}
		case domain.DCPFutureArbiterHold:
			var order []string
			if err := json.Unmarshal([]byte(incident.OrderJSON), &order); err != nil || len(order) == 0 {
				return false, errors.New("DCP future arbiter hold order is unavailable")
			}
			for _, taskID := range order {
				task, found := byID[taskID]
				if !found {
					return false, errors.New("DCP future arbiter hold cohort task disappeared")
				}
				if task.State == domain.DCPPolicyMerged || task.State == domain.DCPPolicyFailed {
					continue
				}
				if task.AdmissionID != admission.ID {
					return false, nil
				}
				break
			}
		case domain.DCPFutureArbiterHumanGate, domain.DCPFutureArbiterFailed:
			candidateTask, found := byAdmission[admission.ID]
			if !found {
				continue
			}
			var cohort []futureArbiterCohortMember
			if err := json.Unmarshal([]byte(incident.CohortJSON), &cohort); err != nil {
				return false, errors.New("DCP future arbiter terminal cohort is unavailable")
			}
			for _, member := range cohort {
				if member.TaskID == candidateTask.TaskID {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func (e *Engine) deriveFutureArbiterIncident(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation, generation int64, derivedAt time.Time) (domain.DCPFutureArbiterIncident, domain.DCPModelAction, error) {
	if generation < 1 || admission.Status != domain.DCPAdmissionIncident || !eligibleFutureArbiterKind(admission.ErrorCode) ||
		candidate.policyTask.State != domain.DCPPolicyIncident || candidate.policyTask.AdmissionID != admission.ID ||
		candidate.run.ID != admission.ReviewRunID || !admissionFacts(candidate, observation, review) {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, errors.New("DCP future arbiter source incident is ineligible")
	}
	var source incidentPacket
	decoder := json.NewDecoder(strings.NewReader(admission.IncidentPacket))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || source.SchemaVersion != "dcp.review-lab.arbiter-needed/v1" || source.Reason != admission.ErrorCode ||
		source.AdmissionID != admission.ID || source.SessionID != string(admission.SessionID) || source.TargetSHA != admission.TargetSHA {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, errors.New("DCP future arbiter source packet is not exact")
	}
	if _, err := e.git(ctx, candidate.project.Path, "fetch", "--prune", "origin", candidate.spec.DefaultBranch); err != nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, err
	}
	currentMain, err := e.git(ctx, candidate.project.Path, "rev-parse", "refs/remotes/origin/"+candidate.spec.DefaultBranch)
	if err != nil || !validSHA(currentMain) {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, errors.New("DCP future arbiter current main is unavailable")
	}
	affected, diff, err := e.futureArbiterDiff(ctx, candidate.project.Path, admission.ReviewBaseSHA, admission.TargetSHA)
	if err != nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, err
	}
	cohort, err := e.futureArbiterCohort(ctx, candidate.project.Path, candidate.policyTask, affected, derivedAt)
	if err != nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, err
	}
	cohortDigest, cohortJSON, err := canonicalDigest(cohort)
	if err != nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, err
	}
	mergeTree, mergeErr := e.git(ctx, candidate.project.Path, "merge-tree", "--write-tree", currentMain, admission.TargetSHA)
	mainPaths, pathErr := e.futureArbiterPaths(ctx, candidate.project.Path, admission.ReviewBaseSHA, currentMain, true)
	if pathErr != nil {
		mainPaths = nil
	}
	conflicts := intersectStrings(affected, mainPaths)
	if admission.ErrorCode == "merge_conflict_or_ambiguity" && mergeErr == nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, errors.New("DCP future arbiter conflict is mechanically clean")
	}
	evidence := futureArbiterEvidence{
		Repository: candidate.spec.Repository, TargetBranch: candidate.spec.DefaultBranch, IncidentKind: admission.ErrorCode,
		CurrentMain: strings.ToLower(currentMain), ProviderHead: strings.ToLower(observation.PR.HeadSHA), ProviderBase: strings.ToLower(observation.PR.BaseSHA),
		Mergeable: observation.PR.ProviderMergeable, MergeState: observation.PR.ProviderMergeStateStatus, NormalizedState: observation.Mergeability.State,
		NamedCheck: candidate.spec.RequiredCheck, ReviewRunID: candidate.run.ID, ReviewVerdict: string(candidate.run.Verdict), ReviewBodyDigest: digestString(candidate.run.Body),
		MergeTreeClean: mergeErr == nil, ConflictPaths: conflicts, MergeTreeDigest: digestString(mergeTree), FIFOOwner: admission.ID,
	}
	evidenceDigest, evidenceJSON, err := canonicalDigest(evidence)
	if err != nil {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, err
	}
	sourceDigest := digestString(admission.IncidentPacket)
	pathsJSON, _ := json.Marshal(affected)
	identityParts := []string{futureArbiterInputSchema, strconv.FormatInt(generation, 10), candidate.spec.Repository, admission.ID,
		strconv.FormatInt(admission.Sequence, 10), admission.LeaseID, candidate.policyTask.TaskID, string(admission.SessionID),
		admission.TargetSHA, admission.ReviewBaseSHA, currentMain, admission.ReviewRunID, sourceDigest, cohortDigest, evidenceDigest, digestString(diff)}
	identityDigest := digestString(strings.Join(identityParts, "\x00"))
	incidentID := "dcp-future-arbiter-" + identityDigest
	input := futureArbiterInput{
		SchemaVersion: futureArbiterInputSchema, IncidentID: incidentID, Generation: generation, IdentityDigest: identityDigest,
		SourcePacketDigest: sourceDigest, AnchorTaskID: candidate.policyTask.TaskID, AnchorSessionID: string(admission.SessionID),
		AffectedPaths: affected, Cohort: cohort, CohortDigest: cohortDigest, Evidence: evidence, EvidenceDigest: evidenceDigest,
		AllowedVerdicts: []string{string(domain.DCPFutureVerdictOrderHold), string(domain.DCPFutureVerdictRepair), string(domain.DCPFutureVerdictHumanGate)},
	}
	inputDigest, inputJSON, err := canonicalDigest(input)
	if err != nil || len(inputJSON) > futureArbiterMaxInputBytes {
		return domain.DCPFutureArbiterIncident{}, domain.DCPModelAction{}, errors.Join(err, errors.New("DCP future arbiter input is unbounded"))
	}
	actionID := "dcp-model-" + candidate.policyTask.TaskID + "-arbiter-" + strconv.FormatInt(generation, 10)
	incident := domain.DCPFutureArbiterIncident{
		IncidentID: incidentID, Generation: generation, IdentityDigest: identityDigest, TaskID: candidate.policyTask.TaskID,
		SessionID: admission.SessionID, AdmissionID: admission.ID, AdmissionSequence: admission.Sequence, IncidentLeaseID: admission.LeaseID,
		IncidentKind: admission.ErrorCode, SourcePacketJSON: admission.IncidentPacket, SourcePacketDigest: sourceDigest,
		PRURL: admission.PRURL, PRNumber: admission.PRNumber, CandidateHeadSHA: strings.ToLower(admission.TargetSHA), ReviewedBaseSHA: strings.ToLower(admission.ReviewBaseSHA),
		CurrentMainSHA: strings.ToLower(currentMain), ReviewRunID: admission.ReviewRunID, AffectedPathsJSON: string(pathsJSON), CohortJSON: string(cohortJSON),
		CohortDigest: cohortDigest, EvidenceJSON: string(evidenceJSON), EvidenceDigest: evidenceDigest, InputJSON: string(inputJSON), InputDigest: inputDigest,
		ModelActionID: actionID, RuntimeHandleID: incidentID, Status: domain.DCPFutureArbiterRequested, CreatedAt: derivedAt.UTC(), UpdatedAt: derivedAt.UTC(),
	}
	action := domain.DCPModelAction{ID: actionID, TaskID: incident.TaskID, SessionID: incident.SessionID, Kind: domain.DCPActionArbiter,
		ExactHeadSHA: incident.CandidateHeadSHA, IncidentID: incident.IncidentID, Status: domain.DCPActionQueued, CreatedAt: derivedAt.UTC(), UpdatedAt: derivedAt.UTC()}
	return incident, action, nil
}

func (e *Engine) futureArbiterDiff(ctx context.Context, repo, base, head string) ([]string, string, error) {
	paths, err := e.futureArbiterPaths(ctx, repo, base, head, false)
	if err != nil {
		return nil, "", err
	}
	diff, err := e.git(ctx, repo, "diff", "--no-ext-diff", "--unified=3", strings.ToLower(base)+".."+strings.ToLower(head), "--")
	if err != nil || diff == "" || len(diff) > 8192 {
		return nil, "", errors.Join(err, errors.New("DCP future arbiter diff is empty or unbounded"))
	}
	return paths, diff, nil
}

func (e *Engine) futureArbiterPaths(ctx context.Context, repo, base, head string, allowEmpty bool) ([]string, error) {
	if !validSHA(base) || !validSHA(head) {
		return nil, errors.New("DCP future arbiter diff identity is invalid")
	}
	pathsText, err := e.git(ctx, repo, "diff", "--name-only", strings.ToLower(base)+".."+strings.ToLower(head))
	if err != nil {
		return nil, err
	}
	paths := nonemptyLines(pathsText)
	if len(paths) > 32 || (!allowEmpty && len(paths) == 0) {
		return nil, errors.New("DCP future arbiter affected path set is empty or unbounded")
	}
	for _, path := range paths {
		if filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || strings.HasPrefix(path, "../") || strings.ContainsRune(path, '\x00') {
			return nil, errors.New("DCP future arbiter affected path is unsafe")
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (e *Engine) futureArbiterCohort(ctx context.Context, repo string, anchor domain.DCPReviewLabPolicyTask, anchorPaths []string, cutoff time.Time) ([]futureArbiterCohortMember, error) {
	store, err := e.futureArbiterStore()
	if err != nil {
		return nil, err
	}
	tasks, err := store.ListDCPReviewLabPolicyTasks(ctx)
	if err != nil {
		return nil, err
	}
	admissions, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.DCPReviewLabAdmission{}
	for _, admission := range admissions {
		byID[admission.ID] = admission
	}
	cohort := []futureArbiterCohortMember{}
	for _, task := range tasks {
		if task.Target != anchor.Target || task.Profile != anchor.Profile || task.Repository != anchor.Repository {
			continue
		}
		// A generation freezes the cohort visible when it was derived. Later
		// submissions are held by the same admission authority but cannot mutate
		// or invalidate this context-free input during model execution/restart.
		if task.CreatedAt.After(cutoff) {
			continue
		}
		if task.State == domain.DCPPolicyFailed {
			continue
		}
		if task.AdmissionID == "" {
			if task.State != domain.DCPPolicyMerged {
				return nil, errFutureArbiterCohortPending
			}
			continue
		}
		admission, ok := byID[task.AdmissionID]
		if !ok {
			return nil, errors.New("DCP future arbiter cohort admission disappeared")
		}
		paths, diff, err := e.futureArbiterDiff(ctx, repo, admission.ReviewBaseSHA, admission.TargetSHA)
		if err != nil {
			return nil, err
		}
		if len(intersectStrings(anchorPaths, paths)) == 0 {
			continue
		}
		cohort = append(cohort, futureArbiterCohortMember{
			TaskID: task.TaskID, SessionID: string(task.SessionID), Intent: task.Prompt, TaskDigest: task.PayloadDigest,
			TaskState:   string(task.State),
			AdmissionID: admission.ID, AdmissionSequence: admission.Sequence, AdmissionStatus: string(admission.Status),
			PRURL: admission.PRURL, CandidateHead: admission.TargetSHA, ReviewedBase: admission.ReviewBaseSHA,
			ReviewRunID: admission.ReviewRunID, AffectedPaths: paths, Diff: diff, DiffDigest: digestString(diff),
		})
	}
	sort.Slice(cohort, func(i, j int) bool {
		if cohort[i].AdmissionSequence == cohort[j].AdmissionSequence {
			return cohort[i].TaskID < cohort[j].TaskID
		}
		return cohort[i].AdmissionSequence < cohort[j].AdmissionSequence
	})
	if len(cohort) == 0 || len(cohort) > 8 {
		return nil, errors.New("DCP future arbiter cohort is empty or unbounded")
	}
	return cohort, nil
}

func nonemptyLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func intersectStrings(a, b []string) []string {
	out := []string{}
	for _, value := range a {
		if slices.Contains(b, value) {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

//nolint:dupl // The explicit field-by-field comparison is a fail-closed immutable identity fence.
func sameFutureArbiterImmutable(a, b domain.DCPFutureArbiterIncident) bool {
	return a.IncidentID == b.IncidentID && a.Generation == b.Generation && a.IdentityDigest == b.IdentityDigest &&
		a.TaskID == b.TaskID && a.SessionID == b.SessionID && a.AdmissionID == b.AdmissionID && a.AdmissionSequence == b.AdmissionSequence &&
		a.IncidentLeaseID == b.IncidentLeaseID && a.IncidentKind == b.IncidentKind && a.SourcePacketJSON == b.SourcePacketJSON &&
		a.SourcePacketDigest == b.SourcePacketDigest && a.PRURL == b.PRURL && a.PRNumber == b.PRNumber && a.CandidateHeadSHA == b.CandidateHeadSHA &&
		a.ReviewedBaseSHA == b.ReviewedBaseSHA && a.CurrentMainSHA == b.CurrentMainSHA && a.ReviewRunID == b.ReviewRunID &&
		a.AffectedPathsJSON == b.AffectedPathsJSON && a.CohortJSON == b.CohortJSON && a.CohortDigest == b.CohortDigest &&
		a.EvidenceJSON == b.EvidenceJSON && a.EvidenceDigest == b.EvidenceDigest && a.InputJSON == b.InputJSON && a.InputDigest == b.InputDigest &&
		a.ModelActionID == b.ModelActionID && a.RuntimeHandleID == b.RuntimeHandleID
}

func (e *Engine) revalidateFutureArbiter(ctx context.Context, incident domain.DCPFutureArbiterIncident) error {
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	admission, found, err := store.GetDCPReviewLabAdmissionByID(ctx, incident.AdmissionID)
	if err != nil || !found {
		return errors.Join(err, errors.New("DCP future arbiter admission disappeared"))
	}
	candidate, exact, err := e.candidateForFutureArbiterAdmission(ctx, admission)
	if err != nil || !exact {
		return errors.Join(err, errors.New("DCP future arbiter candidate drifted"))
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return err
	}
	rebuilt, _, err := e.deriveFutureArbiterIncident(ctx, admission, candidate, observation, review, incident.Generation, incident.CreatedAt)
	if err != nil {
		return err
	}
	if !sameFutureArbiterImmutable(incident, rebuilt) {
		return errors.New("DCP future arbiter immutable input drifted")
	}
	return nil
}

// LaunchPolicyArbiterAction starts one already-claimed exact incident generation.
func (e *Engine) LaunchPolicyArbiterAction(ctx context.Context, action domain.DCPModelAction) error {
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	if action.Kind != domain.DCPActionArbiter || action.Status != domain.DCPActionClaimed || action.Slot == 0 || action.IncidentID == "" {
		return errors.New("DCP future arbiter action claim is invalid")
	}
	incident, found, err := store.GetDCPFutureArbiterIncidentByID(ctx, action.IncidentID)
	if err != nil || !found || incident.Status != domain.DCPFutureArbiterClaimed || incident.ModelActionID != action.ID {
		return errors.Join(err, errors.New("DCP future arbiter incident claim is invalid"))
	}
	fail := func(code string, cause error) error {
		_, persistErr := store.FailDCPFutureArbiterIncident(ctx, incident, action, code, e.clock())
		return errors.Join(cause, persistErr)
	}
	if e.futureArbiter == nil {
		return fail("launcher_unavailable", errors.New("DCP future arbiter launcher is unavailable"))
	}
	if err := e.revalidateFutureArbiter(ctx, incident); err != nil {
		return fail("identity_drift", err)
	}
	if err := e.futureArbiter.PreflightFuture(ctx, incident); err != nil {
		return fail("preflight_failed", err)
	}
	if err := e.validateFutureArbiterLifecycle(ctx, incident, action, domain.DCPModelProcessLaunching); err != nil {
		return fail("lifecycle_drift", err)
	}
	started, err := store.StartDCPModelAction(ctx, action, incident.IncidentID, "", e.clock())
	if err != nil || !started {
		return errors.Join(err, errors.New("DCP future arbiter one-call fence was unavailable"))
	}
	action.Status, action.LaunchID = domain.DCPActionRunning, incident.IncidentID
	incident.Status, incident.ModelCallCount = domain.DCPFutureArbiterRunning, 1
	if err := e.futureArbiter.LaunchFuture(ctx, incident); err != nil {
		return fail("launch_failed", err)
	}
	return e.validateFutureArbiterLifecycle(ctx, incident, action, domain.DCPModelProcessExact)
}

func (e *Engine) validateFutureArbiterLifecycle(ctx context.Context, incident domain.DCPFutureArbiterIncident, action domain.DCPModelAction, process domain.DCPModelProcessState) error {
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	task, found, err := store.GetDCPReviewLabPolicyTaskByTaskID(ctx, incident.TaskID)
	if err != nil || !found || task.SessionID != action.SessionID || incident.ModelActionID != action.ID || action.IncidentID != incident.IncidentID {
		return errors.Join(err, errors.New("DCP future arbiter task/action identity drifted"))
	}
	session, found, err := e.store.GetSession(ctx, task.SessionID)
	if err != nil || !found {
		return errors.Join(err, errors.New("DCP future arbiter native shell disappeared"))
	}
	globalActive := 1
	if lister, ok := e.store.(interface {
		ListActiveDCPModelActions(context.Context) ([]domain.DCPModelAction, error)
	}); ok {
		active, listErr := lister.ListActiveDCPModelActions(ctx)
		if listErr != nil {
			return listErr
		}
		globalActive = len(active)
	}
	decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
		Task: task, Phase: domain.DCPTaskPhaseArbiterRunning, NativeShell: domain.DCPNativeShellStateForSession(session),
		Action: &action, ExpectedActionKind: domain.DCPActionArbiter, Process: process, GlobalActiveActions: globalActive,
	})
	if !decision.Eligible {
		return errors.New("DCP future arbiter lifecycle drifted: " + string(decision.Denial))
	}
	return nil
}

func (e *Engine) failFutureArbiter(ctx context.Context, incident domain.DCPFutureArbiterIncident, code string) error {
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	action, found, err := store.GetDCPModelActionByID(ctx, incident.ModelActionID)
	if err != nil || !found {
		return errors.Join(err, errors.New("DCP future arbiter action disappeared"))
	}
	_, err = store.FailDCPFutureArbiterIncident(ctx, incident, action, code, e.clock())
	return err
}

// SubmitFutureArbiterDecision validates and persists one exact structured verdict.
func (e *Engine) SubmitFutureArbiterDecision(ctx context.Context, incidentID string, data []byte) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	incident, found, err := store.GetDCPFutureArbiterIncidentByID(ctx, strings.TrimSpace(incidentID))
	if err != nil || !found {
		return errors.Join(err, errors.New("DCP future arbiter incident was not found"))
	}
	decision, canonical, err := ParseFutureArbiterDecision(data, incident)
	if err != nil {
		return err
	}
	digest := digestBytes(canonical)
	if incident.Status != domain.DCPFutureArbiterRunning {
		if incident.DecisionDigest == digest && incident.DecisionJSON == string(canonical) {
			return nil
		}
		return errors.New("DCP future arbiter late or foreign result is inert")
	}
	if err := e.revalidateFutureArbiter(ctx, incident); err != nil {
		return e.failFutureArbiter(ctx, incident, "identity_drift")
	}
	action, found, err := store.GetDCPModelActionByID(ctx, incident.ModelActionID)
	if err != nil || !found || action.Status != domain.DCPActionRunning {
		return errors.Join(err, errors.New("DCP future arbiter action does not own result"))
	}
	orderJSON, _ := json.Marshal(decision.Order)
	pathsJSON, _ := json.Marshal(decision.AffectedPaths)
	changed, err := store.RecordDCPFutureArbiterDecision(ctx, incident, action, string(canonical), digest, domain.DCPFutureArbiterVerdict(decision.Verdict), string(orderJSON), decision.RepairObjective, string(pathsJSON), decision.HumanQuestion, e.clock())
	if err != nil || !changed {
		return errors.Join(err, errors.New("DCP future arbiter decision was rejected"))
	}
	if e.policyActionDrain != nil {
		return e.policyActionDrain(ctx)
	}
	return nil
}

// ReportFutureArbiterProcessExit closes an exact supervised arbiter process outcome.
func (e *Engine) ReportFutureArbiterProcessExit(ctx context.Context, incidentID string, report ArbiterProcessExitReport) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, err := e.futureArbiterStore()
	if err != nil {
		return err
	}
	incident, found, err := store.GetDCPFutureArbiterIncidentByID(ctx, incidentID)
	if err != nil || !found {
		return errors.Join(err, errors.New("DCP future arbiter process incident was not found"))
	}
	if incident.Status != domain.DCPFutureArbiterRunning {
		return nil
	}
	if !report.Started || report.ExitCode != 0 || report.ResultFailure != "" {
		code := report.ResultFailure
		if code == "" {
			code = "process_failed"
		}
		return e.failFutureArbiter(ctx, incident, code)
	}
	return nil
}

// FutureArbiterRepairPrompt returns the frozen bounded successor-repair objective.
func (e *Engine) FutureArbiterRepairPrompt(ctx context.Context, task domain.DCPReviewLabPolicyTask, action domain.DCPModelAction) (string, error) {
	store, err := e.futureArbiterStore()
	if err != nil {
		return "", err
	}
	incident, found, err := store.GetDCPFutureArbiterIncidentByID(ctx, action.IncidentID)
	if err != nil || !found || incident.Status != domain.DCPFutureArbiterRepairQueued || incident.Verdict != domain.DCPFutureVerdictRepair ||
		incident.RepairTaskID != task.TaskID || incident.RepairActionID != action.ID || action.Kind != domain.DCPActionRepairWorker {
		return "", errors.Join(err, errors.New("DCP future arbiter repair identity is invalid"))
	}
	envelope := map[string]any{"schemaVersion": "dcp.review-lab.future-arbiter-repair/v1", "incidentId": incident.IncidentID,
		"generation": incident.Generation, "taskId": task.TaskID, "taskIntent": task.Prompt, "currentMain": incident.CurrentMainSHA,
		"oldHead": incident.CandidateHeadSHA, "samePR": incident.PRURL, "objective": incident.RepairObjective,
		"affectedPaths": json.RawMessage(incident.RepairPathsJSON), "cohortDigest": incident.CohortDigest, "evidenceDigest": incident.EvidenceDigest}
	data, err := json.Marshal(envelope)
	if err != nil || len(data) > 4096 {
		return "", errors.Join(err, errors.New("DCP future arbiter repair envelope is unbounded"))
	}
	return "DCP bounded arbiter-approved successor repair. Keep the exact task, native card/session, worktree, current branch and ready PR. Rebuild the same task intent directly on exact current main " + incident.CurrentMainSHA + ", preserve every compatible cohort intent, change only the approved paths, create one new commit/head, guarded-push the same branch/PR, create no new PR, and stop. Do not merge or review. Immutable envelope:\n" + string(data), nil
}

func (e *Engine) tryRebindFutureArbiter(ctx context.Context, candidate mergeCandidate, observation ports.SCMObservation, now time.Time) (bool, error) {
	if !candidate.policy || candidate.policyTask.AdmissionID == "" {
		return false, nil
	}
	store, err := e.futureArbiterStore()
	if err != nil {
		return false, err
	}
	incident, found, err := store.GetDCPFutureArbiterIncidentByTask(ctx, candidate.policyTask.TaskID)
	if err != nil || !found || incident.Status != domain.DCPFutureArbiterRepairQueued {
		return false, err
	}
	if candidate.policyTask.RepairCount != 1 || candidate.policyTask.State != domain.DCPPolicyAdmissionWait || candidate.run.Verdict != domain.VerdictApproved ||
		candidate.run.TargetSHA == incident.CandidateHeadSHA || !strings.EqualFold(observation.PR.BaseSHA, incident.CurrentMainSHA) {
		return true, errors.New("DCP future arbiter recovery identity drifted")
	}
	if _, err := e.git(ctx, candidate.project.Path, "merge-base", "--is-ancestor", incident.CurrentMainSHA, candidate.run.TargetSHA); err != nil {
		return true, errors.New("DCP future arbiter recovery is not based on current main")
	}
	paths, _, err := e.futureArbiterDiff(ctx, candidate.project.Path, incident.CurrentMainSHA, candidate.run.TargetSHA)
	if err != nil {
		return true, err
	}
	var allowed []string
	if json.Unmarshal([]byte(incident.RepairPathsJSON), &allowed) != nil || !stringSubset(paths, allowed) {
		return true, errors.New("DCP future arbiter recovery changed unapproved paths")
	}
	changed, err := store.RebindDCPFutureArbiterAdmission(ctx, incident, candidate.policyTask, candidate.run, observation.PR.BaseSHA, now)
	if err != nil || !changed {
		return true, errors.Join(err, errors.New("DCP future arbiter recovery rebind was rejected"))
	}
	return true, nil
}
