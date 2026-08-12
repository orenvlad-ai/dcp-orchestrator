package dcpterminalmerge

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type ArbiterSuccessorStore interface {
	GetDCPReleaseArbiterSuccessorAttemptByID(context.Context, string) (domain.DCPReleaseArbiterSuccessorAttempt, bool, error)
	GetDCPReleaseArbiterSuccessorAttemptByIncident(context.Context, string) (domain.DCPReleaseArbiterSuccessorAttempt, bool, error)
	ListDCPReleaseArbiterSuccessorAttempts(context.Context) ([]domain.DCPReleaseArbiterSuccessorAttempt, error)
	PrepareDCPReleaseArbiterSuccessorAttempt(context.Context, domain.DCPReleaseArbiterSuccessorAttempt, time.Time) (bool, error)
	StartDCPReleaseArbiterSuccessorCall(context.Context, domain.DCPReleaseArbiterSuccessorAttempt, time.Time) (bool, error)
	FailDCPReleaseArbiterSuccessorPreflight(context.Context, string, string, time.Time) (bool, error)
	RecordDCPReleaseArbiterSuccessorDecision(context.Context, domain.DCPReleaseArbiterSuccessorAttempt, string, string, bool, string, time.Time) (bool, error)
	ConsumeDCPReleaseArbiterSuccessorRepair(context.Context, domain.DCPReleaseArbiterSuccessorAttempt, time.Time) (bool, error)
	FailDCPReleaseArbiterSuccessorCall(context.Context, string, string, time.Time) (bool, error)
	FailDCPReleaseArbiterSuccessorAfterDecision(context.Context, string, string, time.Time) (bool, error)
	RebindDCPAdmissionAfterArbiterSuccessorRepair(context.Context, domain.DCPReviewLabAdmission, domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt, domain.ReviewRun, string, time.Time) (bool, error)
}

func (e *Engine) arbiterSuccessorStore() (ArbiterSuccessorStore, error) {
	store, ok := e.store.(ArbiterSuccessorStore)
	if !ok {
		return nil, errors.New("dcp arbiter successor: durable store surface is unavailable")
	}
	return store, nil
}

func (e *Engine) exactSuccessorState(ctx context.Context) (domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt, bool, error) {
	originalStore, originalErr := e.arbiterStore()
	if originalErr != nil {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, originalErr
	}
	successorStore, successorErr := e.arbiterSuccessorStore()
	if successorErr != nil {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, successorErr
	}
	attempts, err := successorStore.ListDCPReleaseArbiterSuccessorAttempts(ctx)
	if err != nil {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, err
	}
	if len(attempts) == 0 {
		original, ok, getErr := originalStore.GetDCPReleaseArbiterIncidentByID(ctx, exactSuccessorIncidentID)
		if getErr != nil {
			return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, getErr
		}
		if ok {
			if exactSuccessorOriginal(original) {
				return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, errors.New("dcp arbiter successor: exact frozen incident lacks its authorized attempt")
			}
			return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, errors.New("dcp arbiter successor: frozen incident exists but its rejected evidence drifted")
		}
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, nil
	}
	if len(attempts) != 1 {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, errors.New("dcp arbiter successor: more than one successor attempt exists")
	}
	attempt := attempts[0]
	original, ok, err := originalStore.GetDCPReleaseArbiterIncidentByID(ctx, attempt.IncidentID)
	if err != nil || !ok {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, errors.New("dcp arbiter successor: original incident is unavailable")
	}
	if !exactSuccessorAuthorization(attempt, original) {
		return domain.DCPReleaseArbiterIncident{}, domain.DCPReleaseArbiterSuccessorAttempt{}, false, errors.New("dcp arbiter successor: persisted authorization drifted")
	}
	return original, attempt, true, nil
}

// reconcileStage2ArbiterSuccessor is called only by startup reconciliation.
// Persisting a model result deliberately stops at decided; the required
// controlled restart is the only event that may consume the one recovery wake.
func (e *Engine) reconcileStage2ArbiterSuccessor(ctx context.Context) error {
	if _, available := e.store.(ArbiterSuccessorStore); !available {
		return nil
	}
	incident, attempt, ok, err := e.exactSuccessorState(ctx)
	if err != nil || !ok {
		return err
	}
	return e.advanceArbiterSuccessorLocked(ctx, incident, attempt, true)
}

func (e *Engine) revalidateArbiterSuccessor(ctx context.Context, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) error {
	if !exactSuccessorAuthorization(attempt, incident) {
		return errors.New("dcp arbiter successor: immutable attempt identity drifted")
	}
	if err := e.revalidateArbiter(ctx, incident); err != nil {
		return err
	}
	store, err := e.arbiterSuccessorStore()
	if err != nil {
		return err
	}
	stored, ok, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, attempt.AttemptID)
	if err != nil || !ok || !sameArbiterSuccessorImmutable(attempt, stored) {
		return errors.New("dcp arbiter successor: immutable successor input drifted")
	}
	return nil
}

func (e *Engine) advanceArbiterSuccessorLocked(ctx context.Context, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt, allowWake bool) error {
	store, storeErr := e.arbiterSuccessorStore()
	if storeErr != nil {
		return storeErr
	}
	launcher, launcherOK := e.arbiter.(ArbiterSuccessorLauncher)
	switch attempt.Status {
	case domain.DCPArbiterSuccessorAuthorized:
		if err := e.revalidateArbiter(ctx, incident); err != nil {
			return err
		}
		prepared, err := deriveArbiterSuccessorAttempt(incident, attempt)
		if err != nil {
			return err
		}
		updated, err := store.PrepareDCPReleaseArbiterSuccessorAttempt(ctx, prepared, e.clock())
		if err != nil || !updated {
			return errors.Join(err, errors.New("dcp arbiter successor: exact input preparation was unavailable"))
		}
		stored, ok, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, attempt.AttemptID)
		if err != nil || !ok || !sameArbiterSuccessorImmutable(prepared, stored) {
			return errors.New("dcp arbiter successor: prepared input could not be reloaded exactly")
		}
		return e.advanceArbiterSuccessorLocked(ctx, incident, stored, false)
	case domain.DCPArbiterSuccessorRequested:
		if !launcherOK {
			_, err := store.FailDCPReleaseArbiterSuccessorPreflight(ctx, attempt.AttemptID, "launcher_unavailable", e.clock())
			return err
		}
		if err := e.revalidateArbiterSuccessor(ctx, incident, attempt); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorPreflight(ctx, attempt.AttemptID, "identity_drift", e.clock())
			return errors.Join(err, persistErr)
		}
		if err := launcher.PreflightSuccessor(ctx, incident, attempt); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorPreflight(ctx, attempt.AttemptID, "preflight_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		started, err := store.StartDCPReleaseArbiterSuccessorCall(ctx, attempt, e.clock())
		if err != nil || !started {
			return errors.Join(err, errors.New("dcp arbiter successor: exact one-call fence was unavailable"))
		}
		fenced, ok, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, attempt.AttemptID)
		if err != nil || !ok || fenced.Status != domain.DCPArbiterSuccessorRunning || fenced.ModelCallCount != 1 {
			return errors.New("dcp arbiter successor: call fence could not be reloaded")
		}
		if err := launcher.LaunchSuccessor(ctx, incident, fenced); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, "launch_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		return nil
	case domain.DCPArbiterSuccessorRunning:
		if !launcherOK {
			_, err := store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, "launcher_unavailable", e.clock())
			return err
		}
		resultPath, err := launcher.SuccessorResultPath(attempt)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(resultPath); err == nil {
			decision, canonical, readErr := ReadArbiterSuccessorDecision(resultPath, incident, attempt)
			if readErr != nil {
				_, persistErr := store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, "malformed_result", e.clock())
				return errors.Join(readErr, persistErr)
			}
			return e.persistArbiterSuccessorDecisionLocked(ctx, incident, attempt, decision, canonical)
		} else if !os.IsNotExist(err) {
			return err
		}
		alive, err := launcher.SuccessorProcessAlive(ctx, attempt)
		if err != nil {
			return err
		}
		if !alive {
			_, err := store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, "missing_result", e.clock())
			return err
		}
		return nil
	case domain.DCPArbiterSuccessorDecided:
		if !allowWake {
			return nil
		}
		if e.wake == nil {
			_, err := store.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, attempt.AttemptID, "repair_waker_unavailable", e.clock())
			return err
		}
		if err := e.revalidateArbiterSuccessor(ctx, incident, attempt); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, attempt.AttemptID, "identity_drift", e.clock())
			return errors.Join(err, persistErr)
		}
		if attempt.PolicyMaxWorkerCalls != 1 || attempt.PolicyMaxFreshReviews != 1 {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, attempt.AttemptID, "policy_drift", e.clock())
			return errors.Join(errors.New("dcp arbiter successor: deterministic policy drifted"), persistErr)
		}
		consumed, err := store.ConsumeDCPReleaseArbiterSuccessorRepair(ctx, attempt, e.clock())
		if err != nil || !consumed {
			return errors.Join(err, errors.New("dcp arbiter successor: exact recovery path was unavailable"))
		}
		if err := e.wake(ctx, incident.SessionID, arbiterRepairPrompt(incident)); err != nil {
			_, persistErr := store.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, attempt.AttemptID, "repair_launch_failed", e.clock())
			return errors.Join(err, persistErr)
		}
		return nil
	case domain.DCPArbiterSuccessorPreflightFailed, domain.DCPArbiterSuccessorSafeStopped,
		domain.DCPArbiterSuccessorRepairing, domain.DCPArbiterSuccessorRecoveryReviewed,
		domain.DCPArbiterSuccessorSucceeded, domain.DCPArbiterSuccessorFailed:
		return nil
	default:
		return errors.New("dcp arbiter successor: unknown durable state")
	}
}

func (e *Engine) submitArbiterSuccessorDecision(ctx context.Context, attemptID string, data []byte) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, storeErr := e.arbiterSuccessorStore()
	if storeErr != nil {
		return storeErr
	}
	attempt, ok, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, strings.TrimSpace(attemptID))
	if err != nil || !ok {
		return errors.New("dcp arbiter successor: exact attempt was not found")
	}
	originalStore, err := e.arbiterStore()
	if err != nil {
		return err
	}
	incident, ok, err := originalStore.GetDCPReleaseArbiterIncidentByID(ctx, attempt.IncidentID)
	if err != nil || !ok {
		return errors.New("dcp arbiter successor: original incident was not found")
	}
	decision, canonical, err := ParseArbiterSuccessorDecision(data, incident, attempt)
	if err != nil {
		return err
	}
	digest := digestBytes(canonical)
	if attempt.Status != domain.DCPArbiterSuccessorRunning {
		if attempt.DecisionDigest == digest && attempt.DecisionJSON == string(canonical) {
			return nil
		}
		return errors.New("dcp arbiter successor: late, stale, or duplicate foreign decision is inert")
	}
	return e.persistArbiterSuccessorDecisionLocked(ctx, incident, attempt, decision, canonical)
}

func (e *Engine) persistArbiterSuccessorDecisionLocked(ctx context.Context, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt, decision ArbiterSuccessorDecision, canonical []byte) error {
	store, storeErr := e.arbiterSuccessorStore()
	if storeErr != nil {
		return storeErr
	}
	if err := e.revalidateArbiterSuccessor(ctx, incident, attempt); err != nil {
		_, persistErr := store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, "identity_drift", e.clock())
		return errors.Join(err, persistErr)
	}
	digest := digestBytes(canonical)
	safeStop := decision.Verdict == "safe_stop"
	errorCode := ""
	if safeStop {
		errorCode = decision.SafeStopCode
	}
	accepted, err := store.RecordDCPReleaseArbiterSuccessorDecision(ctx, attempt, string(canonical), digest, safeStop, errorCode, e.clock())
	if err != nil || !accepted {
		return errors.Join(err, errors.New("dcp arbiter successor: decision compare-and-set rejected"))
	}
	// Do not consume recovery here. The reviewed qualification requires one
	// controlled restart at the durable decided/zero-wake boundary.
	return nil
}

func (e *Engine) reportArbiterSuccessorProcessExit(ctx context.Context, attemptID string, report ArbiterProcessExitReport) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, storeErr := e.arbiterSuccessorStore()
	if storeErr != nil {
		return storeErr
	}
	attempt, ok, err := store.GetDCPReleaseArbiterSuccessorAttemptByID(ctx, strings.TrimSpace(attemptID))
	if err != nil || !ok {
		return errors.New("dcp arbiter successor: exact attempt was not found")
	}
	if attempt.Status != domain.DCPArbiterSuccessorRunning {
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
	_, err = store.FailDCPReleaseArbiterSuccessorCall(ctx, attempt.AttemptID, code, e.clock())
	return err
}
