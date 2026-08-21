package dcptask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var errPolicyPRFactsPending = errors.New("policy PR provider facts are incomplete")

type wbcReadmissionPolicyStore interface {
	GetOpenDCPWBCReadmissionGenerationByTask(context.Context, string) (domain.DCPWBCReadmissionGeneration, bool, error)
	QueueDCPWBCReadmissionReview(context.Context, domain.DCPReviewLabPolicyTask, domain.DCPReviewLabPolicyTask, domain.DCPModelAction, domain.DCPWBCReadmissionGeneration) (domain.DCPModelAction, bool, error)
}

// DrainModelActions performs one event-driven FIFO drain. It owns no goroutine,
// timer, heartbeat, or poll loop; callers invoke it only after submission,
// action release, a lifecycle/SCM event, or startup reconciliation.
func (s *Service) DrainModelActions(ctx context.Context) error {
	if s == nil || s.policyStore == nil || s.policyRuntime == nil {
		return errors.New("DCP policy runtime is unavailable")
	}
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	for {
		action, ok, err := s.policyStore.ClaimNextDCPModelAction(ctx, s.now().UTC())
		if err != nil || !ok {
			return err
		}
		task, found, err := s.policyStore.GetDCPReviewLabPolicyTaskByTaskID(ctx, action.TaskID)
		if err != nil || !found {
			return errors.Join(err, errors.New("claimed DCP action lost its task"))
		}
		switch action.Kind {
		case domain.DCPActionInitialWorker, domain.DCPActionRepairWorker:
			phase := domain.DCPTaskPhaseWorkerRunning
			if action.Kind == domain.DCPActionRepairWorker {
				phase = domain.DCPTaskPhaseRepairRunning
			}
			spec, exact := domain.DCPPolicyTargetForTask(task)
			if !exact {
				if failErr := s.failClaimedAction(ctx, task, action, "worker_target_invalid"); failErr != nil {
					return failErr
				}
				continue
			}
			validate := func(ctx context.Context) error { return s.validatePolicyTarget(ctx, spec) }
			if action.Kind == domain.DCPActionRepairWorker {
				validate = func(ctx context.Context) error { return s.validatePolicyContinuationTarget(ctx, spec) }
			}
			if err := validate(ctx); err != nil {
				if failErr := s.failClaimedAction(ctx, task, action, "worker_target_invalid"); failErr != nil {
					return errors.Join(err, failErr)
				}
				continue
			}
			prompt, err := s.workerActionPrompt(ctx, task, action)
			if err != nil {
				if failErr := s.failClaimedAction(ctx, task, action, "worker_prompt_invalid"); failErr != nil {
					return errors.Join(err, failErr)
				}
				continue
			}
			launched, err := s.policyRuntime.LaunchDCPReviewLabPolicyAction(ctx, action.SessionID, prompt)
			if err != nil {
				if failErr := s.failClaimedAction(ctx, task, action, "worker_launch_failed"); failErr != nil {
					return errors.Join(err, failErr)
				}
				continue
			}
			launchID := launched.Session.Metadata.RuntimeLaunchID
			if launchID == "" {
				return errors.New("DCP worker launched without an exact supervised generation")
			}
			actions, listErr := s.policyStore.ListDCPModelActions(ctx)
			if listErr != nil {
				return listErr
			}
			activeCount := 0
			for _, candidate := range actions {
				if candidate.Status == domain.DCPActionClaimed || candidate.Status == domain.DCPActionRunning {
					activeCount++
				}
			}
			decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
				Task: task, Phase: phase, NativeShell: domain.DCPNativeShellStateForSession(launched.Session), Action: &action,
				ExpectedActionKind: action.Kind, Process: domain.DCPModelProcessExact, GlobalActiveActions: activeCount,
			})
			if !decision.Eligible {
				return fmt.Errorf("DCP launched worker lifecycle drifted: %s", decision.Denial)
			}
			fresh, found, getErr := s.policyStore.GetDCPModelActionByID(ctx, action.ID)
			if getErr != nil || !found {
				return errors.Join(getErr, errors.New("DCP worker action disappeared after launch"))
			}
			switch {
			case fresh.Status == domain.DCPActionClaimed:
				started, startErr := s.policyStore.StartDCPModelAction(ctx, fresh, launchID, "", s.now().UTC())
				if startErr != nil || !started {
					// The runtime is already live. Leave the durable claim intact so
					// startup can adopt this exact generation instead of releasing its
					// slot and risking a duplicate.
					return errors.Join(startErr, errors.New("DCP worker generation could not be bound to its claimed action"))
				}
				fresh.Status, fresh.LaunchID = domain.DCPActionRunning, launchID
			case fresh.Status == domain.DCPActionRunning && fresh.LaunchID == launchID:
				// A concurrent exact process-start signal already bound it.
			case fresh.Status == domain.DCPActionSucceeded:
				// A very short worker may publish its exact process-exit fact before
				// Launch returns. The exit handler owns that atomic transition.
			default:
				return errors.New("DCP worker action drifted during launch")
			}
		case domain.DCPActionReviewer:
			if s.policyReviewer == nil {
				if err := s.failClaimedAction(ctx, task, action, "reviewer_unavailable"); err != nil {
					return err
				}
				continue
			}
			result, triggerErr := s.policyReviewer.AutoTrigger(ctx, action.SessionID)
			if triggerErr != nil {
				fresh, active, getErr := s.policyStore.GetActiveDCPModelActionBySession(ctx, action.SessionID)
				switch {
				case getErr != nil:
					return errors.Join(triggerErr, getErr)
				case !active:
					// The review engine already persisted the exact terminal launch
					// failure through FailPolicyReviewLaunch.
					continue
				case fresh.Status == domain.DCPActionRunning:
					// A reviewer process exists. Keep its durable slot and let startup
					// recover the exact run binding; releasing here could duplicate it.
					return triggerErr
				default:
					if failErr := s.failClaimedAction(ctx, task, fresh, "reviewer_launch_failed"); failErr != nil {
						return errors.Join(triggerErr, failErr)
					}
				}
				continue
			}
			fresh, found, getErr := s.policyStore.GetDCPModelActionByID(ctx, action.ID)
			if getErr != nil || !found {
				return errors.Join(getErr, errors.New("reviewer action disappeared after trigger"))
			}
			if fresh.Status != domain.DCPActionRunning || fresh.ReviewRunID == "" || !result.Created {
				if failErr := s.failClaimedAction(ctx, task, action, "reviewer_not_started"); failErr != nil {
					return failErr
				}
			}
		case domain.DCPActionArbiter:
			if s.policyArbiter == nil {
				return errors.New("DCP future arbiter handler is unavailable")
			}
			if launchErr := s.policyArbiter.LaunchPolicyArbiterAction(ctx, action); launchErr != nil {
				fresh, found, getErr := s.policyStore.GetDCPModelActionByID(ctx, action.ID)
				if getErr != nil {
					return errors.Join(launchErr, getErr)
				}
				if !found || (fresh.Status != domain.DCPActionFailed && fresh.Status != domain.DCPActionSucceeded) {
					return launchErr
				}
				continue
			}
		default:
			return fmt.Errorf("unknown DCP model action kind %q", action.Kind)
		}
	}
}

func (s *Service) workerActionPrompt(ctx context.Context, task domain.DCPReviewLabPolicyTask, action domain.DCPModelAction) (string, error) {
	if action.Kind == domain.DCPActionInitialWorker {
		return policyPrompt(task), nil
	}
	if action.Kind == domain.DCPActionRepairWorker && action.IncidentID != "" {
		if s.policyArbiter == nil || task.RepairCount != 1 || task.State != domain.DCPPolicyRepairRunning || action.ExactHeadSHA != task.CurrentHeadSHA {
			return "", errors.New("arbiter-approved repair identity is incomplete")
		}
		return s.policyArbiter.FutureArbiterRepairPrompt(ctx, task, action)
	}
	if action.Kind != domain.DCPActionRepairWorker || task.RepairCount != 1 || task.CurrentHeadSHA == "" || action.ExactHeadSHA != task.CurrentHeadSHA || task.ReviewRunID == "" {
		return "", errors.New("bounded repair identity is incomplete")
	}
	run, ok, err := s.policyStore.GetReviewRun(ctx, task.ReviewRunID)
	if err != nil || !ok || run.SessionID != task.SessionID || run.TargetSHA != task.CurrentHeadSHA || run.Verdict != domain.VerdictChangesRequested || run.Body == "" {
		return "", errors.Join(err, errors.New("bounded repair findings are unavailable"))
	}
	envelope, err := json.Marshal(struct {
		SchemaVersion string `json:"schemaVersion"`
		TaskID        string `json:"taskId"`
		Task          string `json:"task"`
		PRURL         string `json:"prUrl"`
		PriorHead     string `json:"priorHead"`
		ReviewRunID   string `json:"reviewRunId"`
		Findings      string `json:"findings"`
	}{"dcp.review-lab.repair/v1", task.TaskID, task.Prompt, task.PRURL, task.CurrentHeadSHA, run.ID, run.Body})
	if err != nil || len(envelope) > 12*1024 {
		return "", errors.Join(err, errors.New("bounded repair envelope is too large"))
	}
	return "DCP bounded findings repair. Keep the exact existing task, session, worktree, branch and ready PR. Resolve only the supplied structured findings, produce one new commit/head, push the same branch, create no PR, and stop. Immutable envelope:\n" + string(envelope), nil
}

func (s *Service) failClaimedAction(ctx context.Context, task domain.DCPReviewLabPolicyTask, action domain.DCPModelAction, code string) error {
	next := task
	next.State = domain.DCPPolicyFailed
	next.ErrorCode = code
	changed, err := s.policyStore.FinishDCPModelAction(ctx, action, next, domain.DCPActionFailed, code, s.now().UTC())
	if err != nil || !changed {
		return errors.Join(err, errors.New("claimed DCP action could not fail closed"))
	}
	return nil
}

// HandleWorkerProcessExit releases only the active action whose exact
// supervised generation produced the lifecycle fact, then drains once.
func (s *Service) HandleWorkerProcessExit(ctx context.Context, id domain.SessionID, launchID string, success bool) error {
	if s == nil || s.policyStore == nil {
		return nil
	}
	task, policy, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !policy {
		return err
	}
	action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
	if err != nil || !ok {
		return errors.Join(err, errors.New("policy worker exit has no active action"))
	}
	if (action.Kind != domain.DCPActionInitialWorker && action.Kind != domain.DCPActionRepairWorker) ||
		(action.LaunchID != "" && action.LaunchID != launchID) || launchID == "" {
		return errors.New("policy worker exit identity drifted")
	}
	if action.Status == domain.DCPActionClaimed {
		started, startErr := s.policyStore.StartDCPModelAction(ctx, action, launchID, "", s.now().UTC())
		if startErr != nil || !started {
			return errors.Join(startErr, errors.New("policy worker exit could not bind its exact launch"))
		}
		action.Status, action.LaunchID = domain.DCPActionRunning, launchID
	}
	next := task
	status, code := domain.DCPActionSucceeded, ""
	if success {
		next.State = domain.DCPPolicyCIWaiting
		next.ErrorCode = ""
	} else {
		status = domain.DCPActionFailed
		code = "worker_process_failed"
		next.State = domain.DCPPolicyFailed
		next.ErrorCode = code
	}
	changed, err := s.policyStore.FinishDCPModelAction(ctx, action, next, status, code, s.now().UTC())
	if err != nil || !changed {
		return errors.Join(err, errors.New("policy worker exit was stale or duplicate"))
	}
	return s.DrainModelActions(ctx)
}

// AuthorizePolicyReview is called by the stock review engine for one due PR
// head. It returns policy=false for ordinary/historical sessions. The first
// call queues the exact-head action without a model; a later drain claims it
// and the second call authorizes the launch.
func (s *Service) AuthorizePolicyReview(ctx context.Context, id domain.SessionID, prURL, head string) (policy, authorized bool, err error) {
	if s == nil || s.policyStore == nil {
		return false, false, nil
	}
	task, policy, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !policy {
		return policy, false, err
	}
	spec, exact := domain.DCPPolicyTargetForTask(task)
	if !exact {
		return true, false, errors.New("policy target identity drifted")
	}
	if err := s.validatePolicyContinuationTarget(ctx, spec); err != nil {
		if task.State == domain.DCPPolicyReviewRunning {
			if action, active, actionErr := s.policyStore.GetActiveDCPModelActionBySession(ctx, id); actionErr == nil && active && action.Kind == domain.DCPActionReviewer && action.Status == domain.DCPActionClaimed {
				_ = s.failClaimedAction(ctx, task, action, "provider_identity_drift")
			} else {
				_ = s.failPolicyTask(ctx, task, "provider_identity_drift", err.Error())
			}
		} else {
			_ = s.failPolicyTask(ctx, task, "provider_identity_drift", err.Error())
		}
		return true, false, err
	}
	head = strings.ToLower(head)
	pr, err := s.exactPolicyPR(ctx, task, prURL, head)
	if err != nil {
		if errors.Is(err, errPolicyPRFactsPending) {
			// The stock SCM observer persists a structural PR row before its
			// provider enrichment. That partial snapshot is not contradictory
			// evidence and must remain a model-free CI wait until the next stock
			// state-change event completes the exact identity.
			return true, false, nil
		}
		_ = s.failPolicyTask(ctx, task, "provider_identity_drift", err.Error())
		return true, false, err
	}
	switch task.State {
	case domain.DCPPolicyCIWaiting:
		if err := s.evaluatePolicyTaskLifecycle(ctx, task, domain.DCPTaskPhaseCIWaiting, "", nil, domain.DCPModelProcessNone); err != nil {
			return true, false, err
		}
		ready, terminal, checkErr := s.exactPolicyNamedCI(ctx, pr, head)
		if checkErr != nil {
			if terminal {
				_ = s.failPolicyTask(ctx, task, "ci_identity_failed", checkErr.Error())
			}
			return true, false, checkErr
		}
		if !ready {
			return true, false, nil
		}
		var readmission domain.DCPWBCReadmissionGeneration
		var hasReadmission bool
		readmissionStore, supportsReadmission := s.policyStore.(wbcReadmissionPolicyStore)
		if supportsReadmission {
			readmission, hasReadmission, err = readmissionStore.GetOpenDCPWBCReadmissionGenerationByTask(ctx, task.TaskID)
			if err != nil {
				return true, false, err
			}
		}
		freshReadmission := hasReadmission && exactWBCReadmissionFreshReview(task, readmission, head)
		if !freshReadmission && task.RepairCount == 0 {
			if task.CurrentHeadSHA != "" {
				return true, false, errors.New("initial review head was already bound")
			}
		} else if !freshReadmission && (task.CurrentHeadSHA == "" || task.CurrentHeadSHA == head) {
			_ = s.failPolicyTask(ctx, task, "repair_head_unchanged", "repair did not produce a fresh exact head")
			return true, false, errors.New("repair did not produce a fresh exact head")
		}
		next := task
		next.State = domain.DCPPolicyReviewQueued
		next.PRURL, next.PRNumber = pr.URL, int64(pr.Number)
		if freshReadmission {
			next.CurrentHeadSHA = head
		} else {
			next.PreviousHeadSHA, next.CurrentHeadSHA = task.CurrentHeadSHA, head
		}
		next.ReviewRunID = ""
		now := s.now().UTC()
		actionID := "dcp-model-" + task.TaskID + "-review-" + strconv.FormatInt(task.RepairCount+1, 10)
		if hasReadmission {
			actionID = "dcp-model-" + task.TaskID + "-readmission-" + strconv.FormatInt(readmission.Sequence, 10) + "-review-" + strconv.FormatInt(task.RepairCount+1, 10)
		}
		action := domain.DCPModelAction{
			ID:     actionID,
			TaskID: task.TaskID, SessionID: id, Kind: domain.DCPActionReviewer,
			ExactHeadSHA: head, Status: domain.DCPActionQueued, CreatedAt: now, UpdatedAt: now,
		}
		if hasReadmission {
			_, _, err = readmissionStore.QueueDCPWBCReadmissionReview(ctx, task, next, action, readmission)
		} else {
			_, _, err = s.policyStore.QueueDCPModelAction(ctx, task, next, action)
		}
		return true, false, err
	case domain.DCPPolicyReviewQueued:
		if task.CurrentHeadSHA != head || task.PRURL != prURL {
			return true, false, errors.New("queued review head drifted")
		}
		action, found, actionErr := s.policyStore.GetDCPModelActionByIdentity(ctx, task.TaskID, domain.DCPActionReviewer, head)
		if actionErr != nil || !found {
			return true, false, errors.Join(actionErr, errors.New("queued review action disappeared"))
		}
		if lifecycleErr := s.evaluatePolicyTaskLifecycle(ctx, task, domain.DCPTaskPhaseReviewQueued, domain.DCPActionReviewer, &action, domain.DCPModelProcessNone); lifecycleErr != nil {
			return true, false, lifecycleErr
		}
		return true, false, nil
	case domain.DCPPolicyReviewRunning:
		action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
		if err != nil || !ok || action.Kind != domain.DCPActionReviewer || action.Status != domain.DCPActionClaimed || action.ExactHeadSHA != head {
			return true, false, errors.Join(err, errors.New("review action lease is not exact"))
		}
		if lifecycleErr := s.evaluatePolicyTaskLifecycle(ctx, task, domain.DCPTaskPhaseReviewRunning, domain.DCPActionReviewer, &action, domain.DCPModelProcessLaunching); lifecycleErr != nil {
			return true, false, lifecycleErr
		}
		return true, true, nil
	default:
		return true, false, nil
	}
}

func exactWBCReadmissionFreshReview(task domain.DCPReviewLabPolicyTask, generation domain.DCPWBCReadmissionGeneration, head string) bool {
	return task.Target == "wb-core" && task.TaskID == generation.TaskID && task.SessionID == generation.SessionID &&
		generation.Status == domain.DCPWBCReadmissionHeadPushed && generation.NewHeadSHA != "" &&
		strings.EqualFold(generation.NewHeadSHA, head) && strings.EqualFold(task.CurrentHeadSHA, head)
}

func (s *Service) IsPolicyReviewSession(ctx context.Context, id domain.SessionID) (bool, error) {
	if s == nil || s.policyStore == nil {
		return false, nil
	}
	_, found, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	return found, err
}

func (s *Service) MarkPolicyReviewStarted(ctx context.Context, id domain.SessionID, head, runID, handleID string) error {
	action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
	if err != nil || !ok || action.Kind != domain.DCPActionReviewer || action.Status != domain.DCPActionClaimed || action.ExactHeadSHA != strings.ToLower(head) || runID == "" || handleID == "" {
		return errors.Join(err, errors.New("reviewer start identity is not exact"))
	}
	started, err := s.policyStore.StartDCPModelAction(ctx, action, handleID, runID, s.now().UTC())
	if err != nil || !started {
		return errors.Join(err, errors.New("reviewer start could not be persisted"))
	}
	task, found, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !found || task.State != domain.DCPPolicyReviewRunning || task.CurrentHeadSHA != action.ExactHeadSHA {
		return errors.Join(err, errors.New("reviewer task projection drifted"))
	}
	action.Status, action.LaunchID, action.ReviewRunID = domain.DCPActionRunning, handleID, runID
	if err := s.evaluatePolicyTaskLifecycle(ctx, task, domain.DCPTaskPhaseReviewRunning, domain.DCPActionReviewer, &action, domain.DCPModelProcessExact); err != nil {
		return err
	}
	next := task
	next.ReviewRunID = runID
	next.UpdatedAt = s.now().UTC()
	updated, err := s.policyStore.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, next)
	if err != nil || !updated {
		return errors.Join(err, errors.New("review run binding could not be persisted"))
	}
	return nil
}

func (s *Service) FailPolicyReviewLaunch(ctx context.Context, id domain.SessionID, head, code string) error {
	task, found, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !found {
		return err
	}
	action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
	if err != nil || !ok || action.Kind != domain.DCPActionReviewer || action.ExactHeadSHA != strings.ToLower(head) {
		return errors.Join(err, errors.New("failed review launch has no exact action"))
	}
	return s.failClaimedAction(ctx, task, action, code)
}

// HandleStructuredPolicyReview consumes future-policy verdicts before the
// stock findings messenger. Approved heads enter admission; first findings
// queue one same-card repair; second findings are terminal.
func (s *Service) HandleStructuredPolicyReview(ctx context.Context, id domain.SessionID, run domain.ReviewRun) (bool, error) {
	if s == nil || s.policyStore == nil {
		return false, nil
	}
	task, policy, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !policy {
		return false, err
	}
	action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
	if err != nil || !ok || action.Kind != domain.DCPActionReviewer || action.Status != domain.DCPActionRunning ||
		action.ExactHeadSHA != run.TargetSHA || action.ReviewRunID != run.ID || task.ReviewRunID != run.ID || task.State != domain.DCPPolicyReviewRunning {
		return true, errors.Join(err, errors.New("structured review result does not own the exact policy action"))
	}
	next := task
	status, code := domain.DCPActionSucceeded, ""
	switch run.Verdict {
	case domain.VerdictApproved:
		next.State = domain.DCPPolicyAdmissionWait
	case domain.VerdictChangesRequested:
		if task.RepairCount != 0 {
			next.State = domain.DCPPolicyFailed
			next.ErrorCode = "second_review_findings"
			code = next.ErrorCode
			status = domain.DCPActionSucceeded
		} else {
			next.State = domain.DCPPolicyRepairQueued
			next.RepairCount = 1
		}
	default:
		status = domain.DCPActionFailed
		code = "review_verdict_invalid"
		next.State = domain.DCPPolicyFailed
		next.ErrorCode = code
	}
	now := s.now().UTC()
	var changed bool
	if next.State == domain.DCPPolicyRepairQueued {
		repair := domain.DCPModelAction{
			ID: "dcp-model-" + task.TaskID + "-worker-2", TaskID: task.TaskID, SessionID: id,
			Kind: domain.DCPActionRepairWorker, ExactHeadSHA: task.CurrentHeadSHA,
			Status: domain.DCPActionQueued, CreatedAt: now, UpdatedAt: now,
		}
		changed, err = s.policyStore.FinishDCPModelActionAndQueue(ctx, action, next, repair, now)
	} else {
		changed, err = s.policyStore.FinishDCPModelAction(ctx, action, next, status, code, now)
	}
	if err != nil || !changed {
		return true, errors.Join(err, errors.New("structured policy review release was rejected"))
	}
	return true, s.DrainModelActions(ctx)
}

func (s *Service) HandlePolicyReviewProcessFailure(ctx context.Context, id domain.SessionID, runID string) error {
	task, policy, err := s.policyStore.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !policy {
		return err
	}
	action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, id)
	if err != nil || !ok || action.Kind != domain.DCPActionReviewer || action.ReviewRunID != runID {
		return errors.Join(err, errors.New("review process failure identity drifted"))
	}
	next := task
	next.State, next.ErrorCode = domain.DCPPolicyFailed, "reviewer_process_failed"
	changed, err := s.policyStore.FinishDCPModelAction(ctx, action, next, domain.DCPActionFailed, next.ErrorCode, s.now().UTC())
	if err != nil || !changed {
		return errors.Join(err, errors.New("review process failure was stale"))
	}
	return s.DrainModelActions(ctx)
}

// ReconcilePolicyStartup runs once after stock runtime/review reconciliation.
// It adopts exact live generations, folds missed terminal facts, and then
// drains queued actions model-free. Ambiguity becomes terminal failure.
func (s *Service) ReconcilePolicyStartup(ctx context.Context) error {
	if s == nil || s.policyStore == nil || s.policyRuntime == nil {
		return errors.New("DCP policy startup dependencies are unavailable")
	}
	tasks, err := s.policyStore.ListDCPReviewLabPolicyTasks(ctx)
	if err != nil {
		return err
	}
	actions, err := s.policyStore.ListDCPModelActions(ctx)
	if err != nil {
		return err
	}
	globalActive := 0
	for _, action := range actions {
		if action.Status == domain.DCPActionClaimed || action.Status == domain.DCPActionRunning {
			globalActive++
		}
	}
	for _, task := range tasks {
		session, ok, err := s.policyStore.GetSession(ctx, task.SessionID)
		if err != nil || !ok || !exactPolicyNativeIdentity(task, session) {
			return errors.Join(err, fmt.Errorf("DCP policy task %s native identity drifted", task.TaskID))
		}
		if task.State == domain.DCPPolicyReserved {
			spec, exact := domain.DCPPolicyTargetForTask(task)
			if !exact {
				return fmt.Errorf("DCP policy task %s target identity drifted", task.TaskID)
			}
			if err := s.validatePolicyTarget(ctx, spec); err != nil {
				return err
			}
			if _, _, _, err := s.policyRuntime.ProvisionDCPReviewLabPolicySession(ctx, task.SessionID, policySpawnConfig(task)); err != nil {
				return err
			}
			next := task
			next.State, next.UpdatedAt = domain.DCPPolicyWorkerQueued, s.now().UTC()
			if updated, err := s.policyStore.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, next); err != nil || !updated {
				return errors.Join(err, errors.New("reserved policy task could not resume provisioning"))
			}
			continue
		}
		phase, expectedKind, lifecycleAction, lifecycleErr := policyLifecycleSnapshot(task, actions)
		if lifecycleErr != nil {
			return lifecycleErr
		}
		if task.State != domain.DCPPolicyWorkerRunning && task.State != domain.DCPPolicyRepairRunning && task.State != domain.DCPPolicyReviewRunning {
			process := domain.DCPModelProcessNone
			if phase == domain.DCPTaskPhaseArbiterRunning && lifecycleAction != nil && lifecycleAction.Status == domain.DCPActionRunning && lifecycleAction.LaunchID != "" {
				process = domain.DCPModelProcessExact
			} else if session.Metadata.RuntimeLaunchID != "" || (!session.IsTerminated && session.Activity.State != domain.ActivityIdle) {
				process = domain.DCPModelProcessUnexpected
			}
			decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
				Task: task, Phase: phase, NativeShell: domain.DCPNativeShellStateForSession(session), Action: lifecycleAction,
				ExpectedActionKind: expectedKind, Process: process, GlobalActiveActions: globalActive,
			})
			if !decision.Eligible {
				return fmt.Errorf("DCP policy task %s lifecycle drifted: %s", task.TaskID, decision.Denial)
			}
			continue
		}
		action, ok, err := s.policyStore.GetActiveDCPModelActionBySession(ctx, task.SessionID)
		if err != nil || !ok {
			return errors.Join(err, fmt.Errorf("running DCP policy task %s has no slot owner", task.TaskID))
		}
		if action.Kind == domain.DCPActionReviewer {
			if action.ReviewRunID == "" {
				run, found, getErr := s.policyStore.GetReviewRunBySessionPRAndSHA(ctx, task.SessionID, task.PRURL, action.ExactHeadSHA)
				review, reviewFound, reviewErr := s.policyStore.GetReviewBySession(ctx, task.SessionID)
				if getErr != nil || reviewErr != nil || !found || !reviewFound || run.ReviewID != review.ID || review.ReviewerHandleID == "" {
					if incidentErr := s.failPolicyTask(ctx, task, "reviewer_restart_ambiguous", "claimed reviewer has no unique durable run/handle binding"); incidentErr != nil {
						return errors.Join(getErr, reviewErr, incidentErr)
					}
					continue
				}
				started, startErr := s.policyStore.StartDCPModelAction(ctx, action, review.ReviewerHandleID, run.ID, s.now().UTC())
				if startErr != nil || !started {
					return errors.Join(startErr, errors.New("claimed reviewer restart could not be adopted"))
				}
				action.Status, action.LaunchID, action.ReviewRunID = domain.DCPActionRunning, review.ReviewerHandleID, run.ID
			}
			run, found, err := s.policyStore.GetReviewRun(ctx, action.ReviewRunID)
			if err != nil || !found {
				return errors.Join(err, errors.New("active reviewer run disappeared"))
			}
			if task.ReviewRunID == "" {
				next := task
				next.ReviewRunID, next.UpdatedAt = run.ID, s.now().UTC()
				updated, bindErr := s.policyStore.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, next)
				if bindErr != nil || !updated {
					return errors.Join(bindErr, errors.New("reviewer restart run binding was rejected"))
				}
				next.Revision = task.Revision + 1
				task = next
			}
			decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
				Task: task, Phase: domain.DCPTaskPhaseReviewRunning, NativeShell: domain.DCPNativeShellStateForSession(session),
				Action: &action, ExpectedActionKind: domain.DCPActionReviewer, Process: domain.DCPModelProcessExact, GlobalActiveActions: globalActive,
			})
			if !decision.Eligible {
				return fmt.Errorf("DCP policy reviewer %s lifecycle drifted: %s", task.TaskID, decision.Denial)
			}
			switch run.Status {
			case domain.ReviewRunComplete:
				_, err = s.HandleStructuredPolicyReview(ctx, task.SessionID, run)
				if err != nil {
					return err
				}
			case domain.ReviewRunFailed, domain.ReviewRunCancelled:
				if err := s.HandlePolicyReviewProcessFailure(ctx, task.SessionID, run.ID); err != nil {
					return err
				}
			case domain.ReviewRunRunning:
				// Exact persisted run/action remains the single live owner.
			default:
				return s.failClaimedAction(ctx, task, action, "reviewer_restart_ambiguous")
			}
			continue
		}
		if session.Metadata.RuntimeLaunchID != "" {
			decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
				Task: task, Phase: phase, NativeShell: domain.DCPNativeShellStateForSession(session), Action: &action,
				ExpectedActionKind: expectedKind, Process: domain.DCPModelProcessExact, GlobalActiveActions: globalActive,
			})
			if !decision.Eligible {
				return fmt.Errorf("DCP policy worker %s lifecycle drifted: %s", task.TaskID, decision.Denial)
			}
			alive, inspectErr := s.policyRuntime.DCPReviewLabPolicyActionAlive(ctx, task.SessionID, session.Metadata.RuntimeLaunchID)
			if inspectErr != nil {
				if incidentErr := s.failPolicyTask(ctx, task, "worker_restart_ambiguous", "exact supervised worker generation could not be inspected"); incidentErr != nil {
					return errors.Join(inspectErr, incidentErr)
				}
				continue
			}
			if !alive {
				if err := s.HandleWorkerProcessExit(ctx, task.SessionID, session.Metadata.RuntimeLaunchID, false); err != nil {
					return err
				}
				continue
			}
			if action.Status == domain.DCPActionClaimed {
				started, err := s.policyStore.StartDCPModelAction(ctx, action, session.Metadata.RuntimeLaunchID, "", s.now().UTC())
				if err != nil || !started {
					return errors.Join(err, errors.New("live worker generation could not be adopted"))
				}
			} else if action.LaunchID != session.Metadata.RuntimeLaunchID {
				return errors.New("active worker generation drifted across restart")
			}
			if session.Activity.State == domain.ActivityExited || session.IsTerminated {
				if err := s.HandleWorkerProcessExit(ctx, task.SessionID, session.Metadata.RuntimeLaunchID, false); err != nil {
					return err
				}
			}
			continue
		}
		if action.Status == domain.DCPActionClaimed {
			if incidentErr := s.failPolicyTask(ctx, task, "worker_restart_ambiguous", "claimed worker has no durable runtime generation binding"); incidentErr != nil {
				return incidentErr
			}
			continue
		}
		decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
			Task: task, Phase: phase, NativeShell: domain.DCPNativeShellStateForSession(session), Action: &action,
			ExpectedActionKind: expectedKind, Process: domain.DCPModelProcessExact, GlobalActiveActions: globalActive,
		})
		if !decision.Eligible {
			return fmt.Errorf("DCP policy worker %s terminal lifecycle fact drifted: %s", task.TaskID, decision.Denial)
		}
		if session.Activity.State == domain.ActivityIdle {
			if err := s.HandleWorkerProcessExit(ctx, task.SessionID, action.LaunchID, true); err != nil {
				return err
			}
		} else {
			if err := s.HandleWorkerProcessExit(ctx, task.SessionID, action.LaunchID, false); err != nil {
				return err
			}
		}
	}
	return s.DrainModelActions(ctx)
}

func policyLifecycleSnapshot(task domain.DCPReviewLabPolicyTask, actions []domain.DCPModelAction) (domain.DCPTaskLifecyclePhase, domain.DCPModelActionKind, *domain.DCPModelAction, error) {
	phase, ok := domain.DCPTaskLifecyclePhaseForState(task.State)
	if !ok {
		return "", "", nil, fmt.Errorf("DCP policy task %s has unknown lifecycle state %q", task.TaskID, task.State)
	}
	expectedKind := domain.DCPModelActionKind("")
	switch task.State {
	case domain.DCPPolicyWorkerQueued, domain.DCPPolicyWorkerRunning:
		expectedKind = domain.DCPActionInitialWorker
	case domain.DCPPolicyReviewQueued, domain.DCPPolicyReviewRunning:
		expectedKind = domain.DCPActionReviewer
	case domain.DCPPolicyRepairQueued, domain.DCPPolicyRepairRunning:
		expectedKind = domain.DCPActionRepairWorker
	}
	var selected *domain.DCPModelAction
	for i := range actions {
		action := &actions[i]
		if action.TaskID != task.TaskID || action.SessionID != task.SessionID ||
			(action.Status != domain.DCPActionQueued && action.Status != domain.DCPActionClaimed && action.Status != domain.DCPActionRunning) {
			continue
		}
		if selected != nil {
			return "", "", nil, fmt.Errorf("DCP policy task %s owns multiple queued/active actions", task.TaskID)
		}
		selected = action
	}
	if selected != nil && selected.Kind == domain.DCPActionArbiter && task.State == domain.DCPPolicyIncident {
		expectedKind = domain.DCPActionArbiter
		if selected.Status == domain.DCPActionQueued {
			phase = domain.DCPTaskPhaseArbiterQueued
		} else {
			phase = domain.DCPTaskPhaseArbiterRunning
		}
	}
	return phase, expectedKind, selected, nil
}

func (s *Service) evaluatePolicyTaskLifecycle(ctx context.Context, task domain.DCPReviewLabPolicyTask, phase domain.DCPTaskLifecyclePhase, expected domain.DCPModelActionKind, action *domain.DCPModelAction, process domain.DCPModelProcessState) error {
	session, found, err := s.policyStore.GetSession(ctx, task.SessionID)
	if err != nil || !found || !exactPolicyNativeIdentity(task, session) {
		return errors.Join(err, errors.New("DCP policy lifecycle native identity drifted"))
	}
	actions, err := s.policyStore.ListDCPModelActions(ctx)
	if err != nil {
		return err
	}
	globalActive := 0
	for _, candidate := range actions {
		if candidate.Status == domain.DCPActionClaimed || candidate.Status == domain.DCPActionRunning {
			globalActive++
		}
	}
	decision := domain.EvaluateDCPTaskLifecycle(domain.DCPTaskLifecycleInput{
		Task: task, Phase: phase, NativeShell: domain.DCPNativeShellStateForSession(session), Action: action,
		ExpectedActionKind: expected, Process: process, GlobalActiveActions: globalActive,
	})
	if !decision.Eligible {
		return fmt.Errorf("DCP policy task %s lifecycle drifted: %s", task.TaskID, decision.Denial)
	}
	return nil
}

func (s *Service) exactPolicyPR(ctx context.Context, task domain.DCPReviewLabPolicyTask, prURL, head string) (domain.PullRequest, error) {
	prs, err := s.policyStore.ListPRsBySession(ctx, task.SessionID)
	if err != nil {
		return domain.PullRequest{}, err
	}
	if len(prs) == 0 {
		return domain.PullRequest{}, errPolicyPRFactsPending
	}
	if len(prs) != 1 {
		return domain.PullRequest{}, errors.New("policy task must own exactly one PR")
	}
	pr := prs[0]
	if err := validateExactPolicyPR(task, pr, prURL, head); err != nil {
		return domain.PullRequest{}, err
	}
	return pr, nil
}

func validateExactPolicyPR(task domain.DCPReviewLabPolicyTask, pr domain.PullRequest, prURL, head string) error {
	spec, exact := domain.DCPPolicyTargetForTask(task)
	if !exact {
		return errors.New("policy target identity is not exact")
	}
	if prURL == "" || head == "" || pr.URL == "" || pr.HTMLURL == "" || pr.Number == 0 ||
		pr.Provider == "" || pr.Host == "" || pr.Repo == "" || pr.SourceBranch == "" ||
		pr.TargetBranch == "" || pr.Author == "" || pr.ProviderState == "" || pr.HeadSHA == "" {
		return errPolicyPRFactsPending
	}
	if pr.URL != prURL || pr.HTMLURL != pr.URL || pr.Provider != "github" || pr.Host != "github.com" ||
		pr.Repo != spec.Repository || pr.SourceBranch != task.SourceBranch || pr.TargetBranch != spec.DefaultBranch ||
		pr.Author != "orenvlad-ai" || pr.Draft || pr.Merged || pr.Closed || pr.ProviderState != "OPEN" ||
		!strings.EqualFold(pr.HeadSHA, head) || !validPolicyPRURL(spec, pr.URL, pr.Number) || !validPolicySHA(head) {
		return errors.New("policy PR provider identity is not exact")
	}
	return nil
}

func (s *Service) exactPolicyNamedCI(ctx context.Context, pr domain.PullRequest, head string) (ready, terminal bool, err error) {
	checks, err := s.policyStore.ListChecks(ctx, pr.URL)
	if err != nil {
		return false, false, err
	}
	return validateExactPolicyNamedCI(pr, checks, head)
}

func validateExactPolicyNamedCI(pr domain.PullRequest, checks []domain.PullRequestCheck, head string) (ready, terminal bool, err error) {
	spec, exact := policySpecForPR(pr)
	if !exact {
		return false, true, errors.New("named CI repository identity is not exact")
	}
	evidence := make([]domain.DCPRequiredCheck, 0, len(checks))
	for _, check := range checks {
		evidence = append(evidence, domain.DCPRequiredCheck{
			Name: check.Name, HeadSHA: check.CommitHash, Status: string(check.Status),
			Conclusion: check.Conclusion, URL: check.URL,
		})
	}
	gate, required, gateErr := domain.EvaluateDCPRequiredCheck(spec.RequiredCheck, head, evidence)
	switch gate {
	case domain.DCPRequiredCheckMissing, domain.DCPRequiredCheckPending:
		// The enriched PR row can arrive before its current-head required check,
		// and stock history remains durable across head changes. Both conditions
		// are passive waits and consume no model slot.
		return false, false, nil
	case domain.DCPRequiredCheckRejected:
		return false, true, gateErr
	case domain.DCPRequiredCheckPassed:
		if gateErr != nil || !validPolicyCheckURL(spec, required.URL) {
			return false, true, errors.New("named CI provider identity drifted")
		}
		return true, false, nil
	default:
		return false, true, errors.New("named CI gate returned an unknown verdict")
	}
}

func (s *Service) failPolicyTask(ctx context.Context, task domain.DCPReviewLabPolicyTask, code, packet string) error {
	if task.State.Terminal() {
		return nil
	}
	next := task
	next.State, next.ErrorCode, next.UpdatedAt = domain.DCPPolicyFailed, code, s.now().UTC()
	if packet != "" {
		data, _ := json.Marshal(map[string]string{"schemaVersion": "dcp.review-lab.policy-incident/v1", "reason": code, "detail": packet})
		next.IncidentPacket = string(data)
		next.State = domain.DCPPolicyIncident
	}
	updated, err := s.policyStore.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, next)
	if err != nil || !updated {
		return errors.Join(err, errors.New("policy failure could not be persisted"))
	}
	return nil
}

func exactPolicyNativeIdentity(task domain.DCPReviewLabPolicyTask, session domain.SessionRecord) bool {
	spec, exact := domain.DCPPolicyTargetForTask(task)
	base := exact && task.CardNumber >= spec.MinimumCardNumber && task.SessionID == session.ID &&
		session.ProjectID == domain.ProjectID(spec.Target) && session.Kind == domain.KindWorker && session.Harness == domain.HarnessCodex &&
		session.DisplayName == "DCP:"+task.TaskID
	if !base {
		return false
	}
	if domain.DCPNativeShellStateForSession(session) == domain.DCPNativeShellInvalid {
		return false
	}
	// Liveness is evaluated centrally from durable task phase, action and exact
	// process facts. This function remains identity-only so an archived shell
	// cannot erase a nonterminal task.
	if task.State == domain.DCPPolicyReserved {
		seed := session.Metadata.Branch == "" && session.Metadata.WorkspacePath == "" && session.Metadata.Prompt == "" && session.Metadata.RuntimeLaunchID == ""
		provisioned := session.Metadata.Branch == task.SourceBranch && session.Metadata.WorkspacePath == task.WorktreePath &&
			session.Metadata.Prompt == policyPrompt(task) && session.Metadata.RuntimeLaunchID == ""
		return seed || provisioned
	}
	return session.Metadata.Branch == task.SourceBranch &&
		session.Metadata.WorkspacePath == task.WorktreePath && session.Metadata.Prompt == policyPrompt(task) &&
		true
}

func validPolicySHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func policySpecForPR(pr domain.PullRequest) (domain.DCPPolicyTargetSpec, bool) {
	return domain.DCPPolicyTargetForRepository(pr.Repo)
}

func validPolicyPRURL(spec domain.DCPPolicyTargetSpec, raw string, number int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/"+spec.Repository+"/pull/"+strconv.Itoa(number)
}

func validPolicyCheckURL(spec domain.DCPPolicyTargetSpec, raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		strings.HasPrefix(u.Path, "/"+spec.Repository+"/actions/runs/")
}
