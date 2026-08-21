package domain

import "testing"

func TestDCPV2CommandTransitionLaw(t *testing.T) {
	tests := []struct {
		name     string
		kind     DCPV2CommandKind
		from     DCPV2TaskState
		to       DCPV2TaskState
		revision bool
		want     bool
	}{
		{"worker exact successor", DCPV2CommandWorkerExecute, DCPV2TaskWorkerQueued, DCPV2TaskChecksWaiting, true, true},
		{"worker cannot omit revision", DCPV2CommandWorkerExecute, DCPV2TaskWorkerQueued, DCPV2TaskChecksWaiting, false, false},
		{"checks lead to fresh review", DCPV2CommandChecksObserve, DCPV2TaskChecksWaiting, DCPV2TaskReviewQueued, false, true},
		{"review may consume shared repair", DCPV2CommandReviewExecute, DCPV2TaskReviewQueued, DCPV2TaskRepairQueued, false, true},
		{"repair makes successor revision", DCPV2CommandRepairExecute, DCPV2TaskRepairQueued, DCPV2TaskChecksWaiting, true, true},
		{"admission waits for release", DCPV2CommandAdmissionEnqueue, DCPV2TaskAdmissionWaiting, DCPV2TaskReleaseWaiting, false, true},
		{"release cannot claim merge", DCPV2CommandReleaseDispatch, DCPV2TaskReleaseWaiting, DCPV2TaskMerged, false, false},
		{"release dispatch is observed", DCPV2CommandReleaseDispatch, DCPV2TaskReleaseWaiting, DCPV2TaskMergeObserving, false, true},
		{"release result awaits terminal verification", DCPV2CommandMergeObserve, DCPV2TaskMergeObserving, DCPV2TaskReleaseVerified, false, true},
		{"release terminalization is separate", DCPV2CommandTerminalVerify, DCPV2TaskReleaseVerified, DCPV2TaskMerged, false, true},
		{"deployment result awaits terminal verification", DCPV2CommandDeploymentObserve, DCPV2TaskDeploymentWaiting, DCPV2TaskDeploymentObserve, false, true},
		{"deployment terminalization is separate", DCPV2CommandTerminalVerify, DCPV2TaskDeploymentObserve, DCPV2TaskDeployed, false, true},
		{"readmission is finite revision", DCPV2CommandReadmission, DCPV2TaskReadmission, DCPV2TaskChecksWaiting, true, true},
		{"failure is always fail closed", DCPV2CommandChecksObserve, DCPV2TaskChecksWaiting, DCPV2TaskHumanGate, false, true},
		{"failure cannot invent revision", DCPV2CommandChecksObserve, DCPV2TaskChecksWaiting, DCPV2TaskHumanGate, true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.kind.AllowsTransition(test.from, test.to, test.revision); got != test.want {
				t.Fatalf("AllowsTransition()=%v want=%v", got, test.want)
			}
		})
	}
}

func TestDCPV2ProjectionSeparatesModelWorkflowMergedAndDeployed(t *testing.T) {
	task := DCPV2Task{TaskID: "t", CurrentRevisionID: "r", State: DCPV2TaskReviewQueued}
	command := DCPV2Command{TaskID: "t", RevisionID: "r", Kind: DCPV2CommandReviewExecute, Status: DCPV2CommandPending}
	action := DCPV2Action{TaskID: "t", RevisionID: "r", Role: DCPV2ActionReviewer, Status: DCPV2ActionQueued}
	projection := ProjectDCPV2Lifecycle(task, &command, &action, nil, nil)
	if projection.ModelActive || !projection.WorkflowActive || projection.StatusLabel != "Reviewer queued" {
		t.Fatalf("queued projection=%+v", projection)
	}
	action.Status = DCPV2ActionLaunching
	projection = ProjectDCPV2Lifecycle(task, &command, &action, nil, nil)
	if projection.ModelActive || !projection.WorkflowActive {
		t.Fatalf("launching-without-runtime projection=%+v", projection)
	}
	action.Status = DCPV2ActionRunning
	projection = ProjectDCPV2Lifecycle(task, &command, &action, nil, nil)
	if projection.ModelActive || !projection.WorkflowActive {
		t.Fatalf("asymmetric running projection=%+v", projection)
	}
	action.RuntimeID = "native-runtime-1"
	projection = ProjectDCPV2Lifecycle(task, &command, &action, nil, nil)
	if !projection.ModelActive || !projection.WorkflowActive {
		t.Fatalf("running projection=%+v", projection)
	}

	task.State = DCPV2TaskDeploymentWaiting
	projection = ProjectDCPV2Lifecycle(task, nil, nil, nil, nil)
	if !projection.Merged || projection.Deployed || projection.StatusLabel != "Merged; waiting for verified deployment" {
		t.Fatalf("merged-not-deployed projection=%+v", projection)
	}
	task.State = DCPV2TaskDeployed
	projection = ProjectDCPV2Lifecycle(task, nil, nil, nil, &DCPV2Result{TaskID: "t", RevisionID: "r", ResultID: "result"})
	if !projection.Merged || !projection.Deployed || projection.WorkflowActive || projection.StatusLabel != "Deployed" {
		t.Fatalf("deployed projection=%+v", projection)
	}
}

func TestDCPV2ProjectionHumanGateAndFailureAreSteady(t *testing.T) {
	for _, state := range []DCPV2TaskState{DCPV2TaskHumanGate, DCPV2TaskFailed} {
		task := DCPV2Task{TaskID: "t", CurrentRevisionID: "r", State: state, HumanGateQuestion: "choose", ErrorCode: "broken"}
		command := DCPV2Command{TaskID: "t", RevisionID: "r", Status: DCPV2CommandPending}
		action := DCPV2Action{TaskID: "t", RevisionID: "r", Status: DCPV2ActionRunning}
		projection := ProjectDCPV2Lifecycle(task, &command, &action, nil, nil)
		if projection.ModelActive || projection.WorkflowActive {
			t.Fatalf("terminal attention projection pulses: %+v", projection)
		}
	}
}
