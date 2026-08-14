package dcpterminalmerge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type ArbiterProcessExitReport struct {
	Started       bool
	ExitCode      int
	ResultFailure string
}

type ArbiterStore interface {
	GetDCPReviewLabAdmissionByID(context.Context, string) (domain.DCPReviewLabAdmission, bool, error)
	OpenDCPReleaseArbiterIncident(context.Context, domain.DCPReviewLabAdmission, domain.DCPReleaseArbiterIncident) (domain.DCPReleaseArbiterIncident, bool, error)
	GetDCPReleaseArbiterIncidentByID(context.Context, string) (domain.DCPReleaseArbiterIncident, bool, error)
	GetDCPReleaseArbiterIncidentByAdmission(context.Context, string) (domain.DCPReleaseArbiterIncident, bool, error)
	GetDCPReleaseArbiterIncidentBySession(context.Context, domain.SessionID) (domain.DCPReleaseArbiterIncident, bool, error)
	ListDCPReleaseArbiterIncidents(context.Context) ([]domain.DCPReleaseArbiterIncident, error)
	StartDCPReleaseArbiterCall(context.Context, domain.DCPReleaseArbiterIncident, time.Time) (bool, error)
	FailDCPReleaseArbiterPreflight(context.Context, string, string, time.Time) (bool, error)
	RecordDCPReleaseArbiterDecision(context.Context, domain.DCPReleaseArbiterIncident, string, string, bool, string, time.Time) (bool, error)
	ConsumeDCPReleaseArbiterRepair(context.Context, domain.DCPReleaseArbiterIncident, time.Time) (bool, error)
	FailDCPReleaseArbiterCall(context.Context, string, string, time.Time) (bool, error)
	FailDCPReleaseArbiterAfterDecision(context.Context, string, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterArbiterRepair(context.Context, domain.DCPReviewLabAdmission, domain.DCPReleaseArbiterIncident, domain.ReviewRun, string, time.Time) (bool, error)
}

func (e *Engine) arbiterStore() (ArbiterStore, error) {
	store, ok := e.store.(ArbiterStore)
	if !ok {
		return nil, errors.New("dcp arbiter: durable store surface is unavailable")
	}
	return store, nil
}

func (e *Engine) SetArbiterLauncher(launcher ArbiterLauncher) { e.arbiter = launcher }

func (e *Engine) reconcileStage2Arbiter(ctx context.Context) error {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return err
	}
	store, storeOK := e.store.(ArbiterStore)
	for _, admission := range rows {
		if admission.Status != domain.DCPAdmissionIncident || admission.ErrorCode != "merge_conflict_or_ambiguity" ||
			(admission.SessionID != ArbiterSessionA && admission.SessionID != ArbiterSessionB) {
			continue
		}
		if !storeOK {
			return errors.New("dcp arbiter: eligible source incident has no durable store surface")
		}
		if _, ok, err := store.GetDCPReleaseArbiterIncidentByAdmission(ctx, admission.ID); err != nil {
			return err
		} else if !ok {
			candidate, exact, err := e.candidateForAdmission(ctx, admission)
			if err != nil || !exact {
				return errors.New("dcp arbiter: persisted source incident candidate is not exact")
			}
			observation, review, err := e.fresh(ctx, candidate.pr)
			if err != nil {
				return err
			}
			incident, err := e.deriveArbiterIncident(ctx, admission, candidate, observation, review, e.clock())
			if err != nil {
				return err
			}
			if _, _, err := store.OpenDCPReleaseArbiterIncident(ctx, admission, incident); err != nil {
				return err
			}
		}
	}
	if !storeOK {
		return nil
	}
	incidents, err := store.ListDCPReleaseArbiterIncidents(ctx)
	if err != nil {
		return err
	}
	if len(incidents) > 1 {
		return errors.New("dcp arbiter: more than one Stage 2 incident exists")
	}
	if len(incidents) == 1 {
		return e.advanceArbiterLocked(ctx, incidents[0])
	}
	return nil
}

func (e *Engine) revalidateArbiter(ctx context.Context, incident domain.DCPReleaseArbiterIncident) error {
	store, storeErr := e.arbiterStore()
	if storeErr != nil {
		return storeErr
	}
	admission, ok, err := store.GetDCPReviewLabAdmissionByID(ctx, incident.AdmissionID)
	if err != nil || !ok {
		return errors.New("dcp arbiter: admission disappeared during exact revalidation")
	}
	candidate, ok, err := e.candidateForAdmission(ctx, admission)
	if err != nil || !ok {
		return errors.New("dcp arbiter: candidate drifted during exact revalidation")
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return err
	}
	rebuilt, err := e.deriveArbiterIncident(ctx, admission, candidate, observation, review, incident.CreatedAt)
	if err != nil {
		return err
	}
	if !sameArbiterImmutable(incident, rebuilt) {
		return errors.New("dcp arbiter: immutable input drifted before trusted action")
	}
	return nil
}

func sameArbiterImmutable(a, b domain.DCPReleaseArbiterIncident) bool {
	return a.IncidentID == b.IncidentID && a.Generation == b.Generation && a.IdentityDigest == b.IdentityDigest &&
		a.AdmissionID == b.AdmissionID && a.IncidentLeaseID == b.IncidentLeaseID &&
		a.SourcePacketJSON == b.SourcePacketJSON && a.SourcePacketDigest == b.SourcePacketDigest &&
		a.InputJSON == b.InputJSON && a.InputDigest == b.InputDigest && a.TaskID == b.TaskID &&
		a.SessionID == b.SessionID && a.WorktreePath == b.WorktreePath && a.SourceBranch == b.SourceBranch &&
		a.PRURL == b.PRURL && a.PRNumber == b.PRNumber && a.TargetSHA == b.TargetSHA &&
		a.ReviewedBaseSHA == b.ReviewedBaseSHA && a.CurrentBaseSHA == b.CurrentBaseSHA &&
		a.ReviewID == b.ReviewID && a.ReviewRunID == b.ReviewRunID && a.BatchID == b.BatchID &&
		a.ScopeDigest == b.ScopeDigest && a.HistoryDigest == b.HistoryDigest && a.DiffDigest == b.DiffDigest &&
		a.CheckSetDigest == b.CheckSetDigest && a.ReviewSetDigest == b.ReviewSetDigest &&
		a.FrozenQueueDigest == b.FrozenQueueDigest && a.MechanicalDigest == b.MechanicalDigest &&
		a.Model == b.Model && a.Reasoning == b.Reasoning && a.TokenBudget == b.TokenBudget &&
		a.RuntimeHandleID == b.RuntimeHandleID && a.LaunchID == b.LaunchID
}

func (e *Engine) advanceArbiterLocked(ctx context.Context, incident domain.DCPReleaseArbiterIncident) error {
	store, storeErr := e.arbiterStore()
	if storeErr != nil {
		return storeErr
	}
	switch incident.Status {
	case domain.DCPArbiterRequested:
		if e.arbiter == nil {
			_, err := store.FailDCPReleaseArbiterPreflight(ctx, incident.IncidentID, "launcher_unavailable", e.clock())
			return err
		}
		if err := e.revalidateArbiter(ctx, incident); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterPreflight(ctx, incident.IncidentID, "identity_drift", e.clock())
			return errors.Join(err, persistErr)
		}
		if err := e.arbiter.Preflight(ctx, incident); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterPreflight(ctx, incident.IncidentID, "preflight_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		started, err := store.StartDCPReleaseArbiterCall(ctx, incident, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("dcp arbiter: exact one-call fence was unavailable"))
		}
		if err := e.arbiter.Launch(ctx, incident); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, "launch_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		return nil
	case domain.DCPArbiterRunning:
		if e.arbiter == nil {
			_, err := store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, "launcher_unavailable", e.clock())
			return err
		}
		resultPath, err := e.arbiter.ResultPath(incident)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(resultPath); err == nil {
			decision, canonical, readErr := ReadArbiterDecision(resultPath, incident)
			if readErr != nil {
				_, persistErr := store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, "malformed_result", e.clock())
				return errors.Join(readErr, persistErr)
			}
			return e.persistArbiterDecisionLocked(ctx, incident, decision, canonical)
		} else if !os.IsNotExist(err) {
			return err
		}
		alive, err := e.arbiter.ProcessAlive(ctx, incident)
		if err != nil {
			return err
		}
		if !alive {
			_, err := store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, "missing_result", e.clock())
			return err
		}
		return nil
	case domain.DCPArbiterDecided:
		if e.wake == nil {
			_, err := store.FailDCPReleaseArbiterAfterDecision(ctx, incident.IncidentID, "repair_waker_unavailable", e.clock())
			return err
		}
		consumed, err := store.ConsumeDCPReleaseArbiterRepair(ctx, incident, e.clock())
		if err != nil || !consumed {
			return errors.Join(err, errors.New("dcp arbiter: exact recovery path was unavailable"))
		}
		prompt := arbiterRepairPrompt(incident)
		if err := e.wake(ctx, incident.SessionID, prompt); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterAfterDecision(ctx, incident.IncidentID, "repair_launch_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		return nil
	case domain.DCPArbiterPreflightFailed, domain.DCPArbiterSafeStopped, domain.DCPArbiterRepairing,
		domain.DCPArbiterRecoveryReviewed, domain.DCPArbiterSucceeded, domain.DCPArbiterFailed:
		return nil
	default:
		return errors.New("dcp arbiter: unknown durable state")
	}
}

func (e *Engine) SubmitArbiterDecision(ctx context.Context, incidentID string, data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(incidentID), "dcp-future-arbiter-") {
		return e.SubmitFutureArbiterDecision(ctx, incidentID, data)
	}
	if strings.HasPrefix(strings.TrimSpace(incidentID), "dcp-arbiter-successor-") {
		return e.submitArbiterSuccessorDecision(ctx, incidentID, data)
	}
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, storeErr := e.arbiterStore()
	if storeErr != nil {
		return storeErr
	}
	incident, ok, err := store.GetDCPReleaseArbiterIncidentByID(ctx, strings.TrimSpace(incidentID))
	if err != nil || !ok {
		return errors.New("dcp arbiter: exact incident was not found")
	}
	decision, canonical, err := ParseArbiterDecision(data, incident)
	if err != nil {
		return err
	}
	decisionDigest := digestBytes(canonical)
	if incident.Status != domain.DCPArbiterRunning {
		if incident.DecisionDigest == decisionDigest && incident.DecisionJSON == string(canonical) {
			return nil
		}
		return errors.New("dcp arbiter: late, stale, or duplicate foreign decision is inert")
	}
	return e.persistArbiterDecisionLocked(ctx, incident, decision, canonical)
}

func (e *Engine) persistArbiterDecisionLocked(ctx context.Context, incident domain.DCPReleaseArbiterIncident, decision ArbiterDecision, canonical []byte) error {
	store, storeErr := e.arbiterStore()
	if storeErr != nil {
		return storeErr
	}
	// Both the live supervisor callback and startup-only result replay converge
	// here. Rebuild the complete frozen incident immediately before the one
	// durable decision CAS so restart recovery cannot bypass exact evidence
	// revalidation.
	if err := e.revalidateArbiter(ctx, incident); err != nil {
		_, persistErr := store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, "identity_drift", e.clock())
		return errors.Join(err, persistErr)
	}
	digest := digestBytes(canonical)
	safeStop := decision.Verdict == "safe_stop"
	errorCode := ""
	if safeStop {
		errorCode = decision.SafeStopCode
	}
	accepted, err := store.RecordDCPReleaseArbiterDecision(ctx, incident, string(canonical), digest, safeStop, errorCode, e.clock())
	if err != nil || !accepted {
		return errors.Join(err, errors.New("dcp arbiter: decision compare-and-set rejected"))
	}
	if safeStop {
		return nil
	}
	updated, ok, err := store.GetDCPReleaseArbiterIncidentByID(ctx, incident.IncidentID)
	if err != nil || !ok {
		return errors.New("dcp arbiter: accepted decision could not be reloaded")
	}
	return e.advanceArbiterLocked(ctx, updated)
}

func (e *Engine) ReportArbiterProcessExit(ctx context.Context, incidentID string, report ArbiterProcessExitReport) error {
	if strings.HasPrefix(strings.TrimSpace(incidentID), "dcp-future-arbiter-") {
		return e.ReportFutureArbiterProcessExit(ctx, incidentID, report)
	}
	if strings.HasPrefix(strings.TrimSpace(incidentID), "dcp-arbiter-successor-") {
		return e.reportArbiterSuccessorProcessExit(ctx, incidentID, report)
	}
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, storeErr := e.arbiterStore()
	if storeErr != nil {
		return storeErr
	}
	incident, ok, err := store.GetDCPReleaseArbiterIncidentByID(ctx, strings.TrimSpace(incidentID))
	if err != nil || !ok {
		return errors.New("dcp arbiter: exact incident was not found")
	}
	if incident.Status != domain.DCPArbiterRunning {
		return nil
	}
	code := strings.TrimSpace(report.ResultFailure)
	if code == "" {
		switch {
		case !report.Started:
			code = "child_not_started"
		case report.ExitCode != 0:
			code = "child_failed"
		default:
			code = "missing_result"
		}
	}
	switch code {
	case "child_not_started", "child_failed", "missing_result", "malformed_result", "submit_failed", "budget_exhausted":
	default:
		code = "malformed_exit_report"
	}
	_, err = store.FailDCPReleaseArbiterCall(ctx, incident.IncidentID, code, e.clock())
	return err
}

func arbiterRepairPrompt(incident domain.DCPReleaseArbiterIncident) string {
	return fmt.Sprintf("DCP bounded arbiter recovery for %s: approved scope digest %s, old exact head %s, exact current origin/main %s, sole conflict path %s. Resolve only this add/add conflict inside the original task: preserve both the exact current-main line and the original distinct one-line intent, leaving exactly those two lines and no other file change. On the existing branch and PR, fetch and rebase onto that exact main, create exactly one direct repaired commit, run the repository check, then push only the same branch with --force-with-lease bound to %s. Create no task, card, branch, worktree or PR; change no scope; stop after the push.",
		incident.TaskID, incident.ScopeDigest, incident.TargetSHA, incident.CurrentBaseSHA, arbiterConflictPath, incident.TargetSHA)
}
