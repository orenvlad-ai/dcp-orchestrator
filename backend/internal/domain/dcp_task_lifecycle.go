package domain

// DCPTaskLifecyclePhase is the provider-neutral durable phase consumed by
// startup, review recovery, admission, release observation, and arbiter
// recovery. It deliberately separates workflow progress from model runtime.
type DCPTaskLifecyclePhase string

const (
	DCPTaskPhaseReserved         DCPTaskLifecyclePhase = "reserved"
	DCPTaskPhaseWorkerQueued     DCPTaskLifecyclePhase = "worker_queued"
	DCPTaskPhaseWorkerRunning    DCPTaskLifecyclePhase = "worker_running"
	DCPTaskPhaseCIWaiting        DCPTaskLifecyclePhase = "ci_waiting"
	DCPTaskPhaseReviewQueued     DCPTaskLifecyclePhase = "review_queued"
	DCPTaskPhaseReviewRunning    DCPTaskLifecyclePhase = "review_running"
	DCPTaskPhaseRepairQueued     DCPTaskLifecyclePhase = "repair_queued"
	DCPTaskPhaseRepairRunning    DCPTaskLifecyclePhase = "repair_running"
	DCPTaskPhaseArbiterQueued    DCPTaskLifecyclePhase = "arbiter_queued"
	DCPTaskPhaseArbiterRunning   DCPTaskLifecyclePhase = "arbiter_running"
	DCPTaskPhaseAdmissionWaiting DCPTaskLifecyclePhase = "admission_waiting"
	DCPTaskPhaseReleaseWaiting   DCPTaskLifecyclePhase = "release_waiting"
	DCPTaskPhaseHumanGate        DCPTaskLifecyclePhase = "human_gate"
	DCPTaskPhaseIncident         DCPTaskLifecyclePhase = "incident"
	DCPTaskPhaseTerminalObserve  DCPTaskLifecyclePhase = "terminal_observation"
)

// DCPNativeShellState describes only the registered task shell. A reviewer or
// arbiter may own a separate exact process while this shell remains archived.
type DCPNativeShellState string

const (
	DCPNativeShellLive     DCPNativeShellState = "live"
	DCPNativeShellArchived DCPNativeShellState = "archived"
	DCPNativeShellInvalid  DCPNativeShellState = "invalid"
)

// DCPModelProcessState describes the process belonging to the action, not the
// native shell record.
type DCPModelProcessState string

const (
	DCPModelProcessNone       DCPModelProcessState = "none"
	DCPModelProcessLaunching  DCPModelProcessState = "launching"
	DCPModelProcessExact      DCPModelProcessState = "exact"
	DCPModelProcessUnexpected DCPModelProcessState = "unexpected"
)

type DCPTaskLifecycleDenial string

const (
	DCPTaskLifecycleAllowed             DCPTaskLifecycleDenial = ""
	DCPTaskLifecycleUnknownPhase        DCPTaskLifecycleDenial = "unknown_phase"
	DCPTaskLifecycleNativeIdentityDrift DCPTaskLifecycleDenial = "native_identity_drift"
	DCPTaskLifecycleActionIdentityDrift DCPTaskLifecycleDenial = "action_identity_drift"
	DCPTaskLifecycleActionStateDrift    DCPTaskLifecycleDenial = "action_state_drift"
	DCPTaskLifecycleProcessDrift        DCPTaskLifecycleDenial = "process_drift"
	DCPTaskLifecycleSlotAccountingDrift DCPTaskLifecycleDenial = "slot_accounting_drift"
)

// DCPTaskLifecycleInput binds one exact task/session/action/process snapshot.
// Action may be nil only in phases that own no queued or active model work.
type DCPTaskLifecycleInput struct {
	Task                DCPReviewLabPolicyTask
	Phase               DCPTaskLifecyclePhase
	NativeShell         DCPNativeShellState
	Action              *DCPModelAction
	ExpectedActionKind  DCPModelActionKind
	Process             DCPModelProcessState
	GlobalActiveActions int
}

// DCPTaskLifecycleDecision is the single typed liveness verdict. ModelActive
// and WorkflowActive are intentionally independent UI/operational facts.
type DCPTaskLifecycleDecision struct {
	Eligible              bool
	Denial                DCPTaskLifecycleDenial
	RuntimeRequired       bool
	PreserveArchivedShell bool
	ModelActive           bool
	WorkflowActive        bool
}

// DCPTaskLifecyclePhaseForState maps the durable policy state without looking
// at any provider-specific generation, PR, marker, label, or release proof.
func DCPTaskLifecyclePhaseForState(state DCPReviewLabPolicyState) (DCPTaskLifecyclePhase, bool) {
	switch state {
	case DCPPolicyReserved:
		return DCPTaskPhaseReserved, true
	case DCPPolicyWorkerQueued:
		return DCPTaskPhaseWorkerQueued, true
	case DCPPolicyWorkerRunning:
		return DCPTaskPhaseWorkerRunning, true
	case DCPPolicyCIWaiting:
		return DCPTaskPhaseCIWaiting, true
	case DCPPolicyReviewQueued:
		return DCPTaskPhaseReviewQueued, true
	case DCPPolicyReviewRunning:
		return DCPTaskPhaseReviewRunning, true
	case DCPPolicyRepairQueued:
		return DCPTaskPhaseRepairQueued, true
	case DCPPolicyRepairRunning:
		return DCPTaskPhaseRepairRunning, true
	case DCPPolicyAdmissionWait:
		return DCPTaskPhaseAdmissionWaiting, true
	case DCPPolicyReleaseWaiting:
		return DCPTaskPhaseReleaseWaiting, true
	case DCPPolicyIncident:
		return DCPTaskPhaseIncident, true
	case DCPPolicyMerged, DCPPolicyFailed:
		return DCPTaskPhaseTerminalObserve, true
	default:
		return "", false
	}
}

// DCPNativeShellStateForSession rejects contradictory native records. The
// exact task/project/branch/worktree/prompt/display checks remain at callers.
func DCPNativeShellStateForSession(session SessionRecord) DCPNativeShellState {
	switch {
	case session.IsTerminated && session.Activity.State == ActivityExited && session.Metadata.RuntimeLaunchID == "":
		return DCPNativeShellArchived
	case !session.IsTerminated && session.Activity.State != ActivityExited:
		return DCPNativeShellLive
	default:
		return DCPNativeShellInvalid
	}
}

// EvaluateDCPTaskLifecycle is the common task-first runtime policy. Provider
// and repository identity are intentionally outside this evaluator and remain
// mandatory narrower gates at every caller.
func EvaluateDCPTaskLifecycle(in DCPTaskLifecycleInput) DCPTaskLifecycleDecision {
	decision := DCPTaskLifecycleDecision{WorkflowActive: true, PreserveArchivedShell: in.NativeShell == DCPNativeShellArchived}
	deny := func(reason DCPTaskLifecycleDenial) DCPTaskLifecycleDecision {
		decision.Denial = reason
		return decision
	}
	if in.NativeShell != DCPNativeShellLive && in.NativeShell != DCPNativeShellArchived {
		return deny(DCPTaskLifecycleNativeIdentityDrift)
	}
	if in.GlobalActiveActions < 0 || in.GlobalActiveActions > 3 {
		return deny(DCPTaskLifecycleSlotAccountingDrift)
	}
	queued, running, passive := false, false, false
	switch in.Phase {
	case DCPTaskPhaseWorkerQueued, DCPTaskPhaseReviewQueued, DCPTaskPhaseRepairQueued, DCPTaskPhaseArbiterQueued:
		queued = true
	case DCPTaskPhaseWorkerRunning, DCPTaskPhaseReviewRunning, DCPTaskPhaseRepairRunning, DCPTaskPhaseArbiterRunning:
		running = true
	case DCPTaskPhaseReserved, DCPTaskPhaseCIWaiting, DCPTaskPhaseAdmissionWaiting, DCPTaskPhaseReleaseWaiting,
		DCPTaskPhaseHumanGate, DCPTaskPhaseIncident, DCPTaskPhaseTerminalObserve:
		passive = true
	default:
		return deny(DCPTaskLifecycleUnknownPhase)
	}
	if in.Process == DCPModelProcessUnexpected {
		return deny(DCPTaskLifecycleProcessDrift)
	}
	if queued || running {
		if in.Action == nil || in.ExpectedActionKind == "" || in.Action.TaskID != in.Task.TaskID ||
			in.Action.SessionID != in.Task.SessionID || in.Action.Kind != in.ExpectedActionKind {
			return deny(DCPTaskLifecycleActionIdentityDrift)
		}
		if in.ExpectedActionKind == DCPActionInitialWorker {
			if in.Action.ExactHeadSHA != "" {
				return deny(DCPTaskLifecycleActionIdentityDrift)
			}
		} else if in.ExpectedActionKind != DCPActionArbiter && (in.Task.CurrentHeadSHA == "" || in.Action.ExactHeadSHA != in.Task.CurrentHeadSHA) {
			return deny(DCPTaskLifecycleActionIdentityDrift)
		}
	}
	if queued {
		if in.Action.Status != DCPActionQueued || in.Action.Slot != 0 || in.Action.LaunchID != "" || in.Action.ReviewRunID != "" {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.Process != DCPModelProcessNone {
			return deny(DCPTaskLifecycleProcessDrift)
		}
		decision.Eligible = true
		return decision
	}
	if running {
		decision.RuntimeRequired = true
		decision.ModelActive = true
		if in.Action.Status != DCPActionClaimed && in.Action.Status != DCPActionRunning {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.Action.Slot < 1 || in.Action.Slot > 3 {
			return deny(DCPTaskLifecycleSlotAccountingDrift)
		}
		if in.Action.Status == DCPActionClaimed && (in.Action.LaunchID != "" || in.Action.ReviewRunID != "") {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.Action.Status == DCPActionRunning && in.Action.LaunchID == "" {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.Action.Status == DCPActionRunning && in.ExpectedActionKind == DCPActionReviewer && in.Action.ReviewRunID == "" {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.GlobalActiveActions == 0 {
			return deny(DCPTaskLifecycleSlotAccountingDrift)
		}
		if in.Action.Status == DCPActionClaimed && in.Process != DCPModelProcessLaunching && in.Process != DCPModelProcessExact {
			return deny(DCPTaskLifecycleProcessDrift)
		}
		if in.Action.Status == DCPActionRunning && in.Process != DCPModelProcessExact {
			return deny(DCPTaskLifecycleProcessDrift)
		}
		decision.Eligible = true
		return decision
	}
	if passive {
		if in.Action != nil && (in.Action.Status == DCPActionQueued || in.Action.Status == DCPActionClaimed || in.Action.Status == DCPActionRunning) {
			return deny(DCPTaskLifecycleActionStateDrift)
		}
		if in.Process != DCPModelProcessNone {
			return deny(DCPTaskLifecycleProcessDrift)
		}
		if in.Phase == DCPTaskPhaseTerminalObserve {
			decision.WorkflowActive = false
		}
		decision.Eligible = true
		return decision
	}
	return deny(DCPTaskLifecycleUnknownPhase)
}
