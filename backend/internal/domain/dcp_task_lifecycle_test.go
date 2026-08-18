package domain

import "testing"

func lifecycleTask() DCPReviewLabPolicyTask {
	return DCPReviewLabPolicyTask{TaskID: "task-first-v1", SessionID: "wb-core-1", CurrentHeadSHA: "26044c696651ce5873748ec3f920d40e77c5686c"}
}

func lifecycleAction(task DCPReviewLabPolicyTask, kind DCPModelActionKind, status DCPModelActionStatus) *DCPModelAction {
	head := task.CurrentHeadSHA
	if kind == DCPActionInitialWorker {
		head = ""
	}
	action := &DCPModelAction{ID: "action-1", TaskID: task.TaskID, SessionID: task.SessionID, Kind: kind, ExactHeadSHA: head, Status: status}
	if status == DCPActionClaimed || status == DCPActionRunning {
		action.Slot = 1
	}
	if status == DCPActionRunning {
		action.LaunchID = "exact-launch"
		if kind == DCPActionReviewer {
			action.ReviewRunID = "exact-review-run"
		}
	}
	return action
}

func TestDCPTaskLifecycleExhaustivePhaseShellActionProcessMatrix(t *testing.T) {
	task := lifecycleTask()
	phases := []struct {
		phase DCPTaskLifecyclePhase
		kind  DCPModelActionKind
		mode  string
	}{
		{DCPTaskPhaseReserved, "", "passive"}, {DCPTaskPhaseWorkerQueued, DCPActionInitialWorker, "queued"},
		{DCPTaskPhaseWorkerRunning, DCPActionInitialWorker, "running"}, {DCPTaskPhaseCIWaiting, "", "passive"},
		{DCPTaskPhaseReviewQueued, DCPActionReviewer, "queued"}, {DCPTaskPhaseReviewRunning, DCPActionReviewer, "running"},
		{DCPTaskPhaseRepairQueued, DCPActionRepairWorker, "queued"}, {DCPTaskPhaseRepairRunning, DCPActionRepairWorker, "running"},
		{DCPTaskPhaseArbiterQueued, DCPActionArbiter, "queued"}, {DCPTaskPhaseArbiterRunning, DCPActionArbiter, "running"},
		{DCPTaskPhaseAdmissionWaiting, "", "passive"}, {DCPTaskPhaseReleaseWaiting, "", "passive"},
		{DCPTaskPhaseHumanGate, "", "passive"}, {DCPTaskPhaseIncident, "", "passive"},
		{DCPTaskPhaseTerminalObserve, "", "passive"},
	}
	shells := []DCPNativeShellState{DCPNativeShellLive, DCPNativeShellArchived, DCPNativeShellInvalid}
	statuses := []DCPModelActionStatus{"", DCPActionQueued, DCPActionClaimed, DCPActionRunning, DCPActionSucceeded, DCPActionFailed}
	processes := []DCPModelProcessState{DCPModelProcessNone, DCPModelProcessLaunching, DCPModelProcessExact, DCPModelProcessUnexpected}
	for _, p := range phases {
		for _, shell := range shells {
			for _, status := range statuses {
				for _, process := range processes {
					var action *DCPModelAction
					if status != "" {
						kind := p.kind
						if kind == "" {
							kind = DCPActionReviewer
						}
						action = lifecycleAction(task, kind, status)
					}
					active := 0
					if status == DCPActionClaimed || status == DCPActionRunning {
						active = 1
					}
					got := EvaluateDCPTaskLifecycle(DCPTaskLifecycleInput{Task: task, Phase: p.phase, NativeShell: shell, Action: action, ExpectedActionKind: p.kind, Process: process, GlobalActiveActions: active})
					want := shell != DCPNativeShellInvalid && process != DCPModelProcessUnexpected
					switch p.mode {
					case "queued":
						want = want && status == DCPActionQueued && process == DCPModelProcessNone
					case "running":
						want = want && ((status == DCPActionClaimed && (process == DCPModelProcessLaunching || process == DCPModelProcessExact)) ||
							(status == DCPActionRunning && process == DCPModelProcessExact))
					case "passive":
						want = want && process == DCPModelProcessNone && status != DCPActionQueued && status != DCPActionClaimed && status != DCPActionRunning
					}
					if got.Eligible != want {
						t.Fatalf("phase=%s shell=%s status=%s process=%s: eligible=%v denial=%s want=%v", p.phase, shell, status, process, got.Eligible, got.Denial, want)
					}
				}
			}
		}
	}
}

func TestDCPTaskLifecycleFailClosedActionProcessAndGlobalSlotAsymmetry(t *testing.T) {
	task := lifecycleTask()
	action := lifecycleAction(task, DCPActionReviewer, DCPActionRunning)
	base := DCPTaskLifecycleInput{Task: task, Phase: DCPTaskPhaseReviewRunning, NativeShell: DCPNativeShellArchived,
		Action: action, ExpectedActionKind: DCPActionReviewer, Process: DCPModelProcessExact, GlobalActiveActions: 1}
	if got := EvaluateDCPTaskLifecycle(base); !got.Eligible || !got.RuntimeRequired || !got.ModelActive || !got.PreserveArchivedShell {
		t.Fatalf("exact archived reviewer runtime rejected: %+v", got)
	}
	noProcess := base
	noProcess.Process = DCPModelProcessNone
	if got := EvaluateDCPTaskLifecycle(noProcess); got.Eligible || got.Denial != DCPTaskLifecycleProcessDrift {
		t.Fatalf("active action without process did not fail closed: %+v", got)
	}
	foreign := base
	foreign.Action = nil
	foreign.Process = DCPModelProcessUnexpected
	if got := EvaluateDCPTaskLifecycle(foreign); got.Eligible || got.Denial != DCPTaskLifecycleProcessDrift {
		t.Fatalf("unexpected live process did not fail closed: %+v", got)
	}
	overCapacity := base
	overCapacity.GlobalActiveActions = 4
	if got := EvaluateDCPTaskLifecycle(overCapacity); got.Eligible || got.Denial != DCPTaskLifecycleSlotAccountingDrift {
		t.Fatalf("fourth global action did not fail closed: %+v", got)
	}
	missingLaunch := base
	missingLaunch.Action = lifecycleAction(task, DCPActionReviewer, DCPActionRunning)
	missingLaunch.Action.LaunchID = ""
	if got := EvaluateDCPTaskLifecycle(missingLaunch); got.Eligible || got.Denial != DCPTaskLifecycleActionStateDrift {
		t.Fatalf("running action without exact launch binding did not fail closed: %+v", got)
	}
	crossedSlot := base
	crossedSlot.Action = lifecycleAction(task, DCPActionReviewer, DCPActionRunning)
	crossedSlot.Action.Slot = 4
	if got := EvaluateDCPTaskLifecycle(crossedSlot); got.Eligible || got.Denial != DCPTaskLifecycleSlotAccountingDrift {
		t.Fatalf("action outside the physical three-slot range did not fail closed: %+v", got)
	}
}

func TestDCPTaskLifecycleMapsAllDurablePolicyStates(t *testing.T) {
	states := []DCPReviewLabPolicyState{DCPPolicyReserved, DCPPolicyWorkerQueued, DCPPolicyWorkerRunning, DCPPolicyCIWaiting,
		DCPPolicyReviewQueued, DCPPolicyReviewRunning, DCPPolicyRepairQueued, DCPPolicyRepairRunning,
		DCPPolicyAdmissionWait, DCPPolicyReleaseWaiting, DCPPolicyMerged, DCPPolicyFailed, DCPPolicyIncident}
	for _, state := range states {
		if _, ok := DCPTaskLifecyclePhaseForState(state); !ok {
			t.Fatalf("durable state %q has no central lifecycle mapping", state)
		}
	}
	if _, ok := DCPTaskLifecyclePhaseForState("foreign"); ok {
		t.Fatal("foreign state unexpectedly mapped")
	}
}
