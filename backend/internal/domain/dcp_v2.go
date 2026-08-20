package domain

import "time"

// DCP v2 is a dormant provider-neutral protocol until a later reviewed target
// adapter and install pass activate it. These records deliberately do not
// reuse the predecessor policy tables: a native shell is a runtime resource,
// never task or command authority.

type DCPV2TaskState string

const (
	DCPV2TaskWorkerQueued      DCPV2TaskState = "worker_queued"
	DCPV2TaskWorkerRunning     DCPV2TaskState = "worker_running"
	DCPV2TaskChecksWaiting     DCPV2TaskState = "checks_waiting"
	DCPV2TaskReviewQueued      DCPV2TaskState = "review_queued"
	DCPV2TaskReviewRunning     DCPV2TaskState = "review_running"
	DCPV2TaskRepairQueued      DCPV2TaskState = "repair_queued"
	DCPV2TaskRepairRunning     DCPV2TaskState = "repair_running"
	DCPV2TaskArbiterQueued     DCPV2TaskState = "arbiter_queued"
	DCPV2TaskArbiterRunning    DCPV2TaskState = "arbiter_running"
	DCPV2TaskAdmissionWaiting  DCPV2TaskState = "admission_waiting"
	DCPV2TaskReadmission       DCPV2TaskState = "readmission"
	DCPV2TaskReleaseWaiting    DCPV2TaskState = "release_waiting"
	DCPV2TaskMergeObserving    DCPV2TaskState = "merge_observing"
	DCPV2TaskReleaseVerified   DCPV2TaskState = "release_verified"
	DCPV2TaskDeploymentWaiting DCPV2TaskState = "deployment_waiting"
	DCPV2TaskDeploymentObserve DCPV2TaskState = "deployment_observing"
	DCPV2TaskHumanGate         DCPV2TaskState = "human_gate"
	DCPV2TaskFailed            DCPV2TaskState = "failed"
	DCPV2TaskMerged            DCPV2TaskState = "merged"
	DCPV2TaskDeployed          DCPV2TaskState = "deployed"
)

func (s DCPV2TaskState) Terminal() bool {
	return s == DCPV2TaskFailed || s == DCPV2TaskMerged || s == DCPV2TaskDeployed
}

func (s DCPV2TaskState) RequiresCommand() bool {
	return !s.Terminal() && s != DCPV2TaskHumanGate
}

type DCPV2RevisionKind string

const (
	DCPV2RevisionWorkInput   DCPV2RevisionKind = "work_input"
	DCPV2RevisionWorker      DCPV2RevisionKind = "worker_output"
	DCPV2RevisionRepair      DCPV2RevisionKind = "repair_output"
	DCPV2RevisionReadmission DCPV2RevisionKind = "readmission_output"
)

type DCPV2CommandKind string

const (
	DCPV2CommandWorkerExecute     DCPV2CommandKind = "worker.execute/v1"
	DCPV2CommandChecksObserve     DCPV2CommandKind = "checks.observe/v1"
	DCPV2CommandReviewExecute     DCPV2CommandKind = "review.execute/v1"
	DCPV2CommandRepairExecute     DCPV2CommandKind = "repair.execute/v1"
	DCPV2CommandArbiterExecute    DCPV2CommandKind = "arbiter.execute/v1"
	DCPV2CommandHumanGateOpen     DCPV2CommandKind = "human_gate.open/v1"
	DCPV2CommandAdmissionEnqueue  DCPV2CommandKind = "admission.enqueue/v1"
	DCPV2CommandReadmission       DCPV2CommandKind = "readmission.materialize/v1"
	DCPV2CommandReleaseDispatch   DCPV2CommandKind = "release.dispatch/v1"
	DCPV2CommandMergeObserve      DCPV2CommandKind = "merge.observe/v1"
	DCPV2CommandDeploymentObserve DCPV2CommandKind = "deployment.observe/v1"
	DCPV2CommandTerminalVerify    DCPV2CommandKind = "terminal.verify/v1"
)

func (k DCPV2CommandKind) ModelBacked() bool {
	switch k {
	case DCPV2CommandWorkerExecute, DCPV2CommandReviewExecute, DCPV2CommandRepairExecute, DCPV2CommandArbiterExecute:
		return true
	default:
		return false
	}
}

// RequiresEffectFence names commands that may cross an external side-effect
// boundary. Observation and purely local persistence commands are one-shot
// reads/transactions and do not need an external fence.
func (k DCPV2CommandKind) RequiresEffectFence() bool {
	switch k {
	case DCPV2CommandWorkerExecute, DCPV2CommandReviewExecute, DCPV2CommandRepairExecute,
		DCPV2CommandArbiterExecute, DCPV2CommandReadmission, DCPV2CommandReleaseDispatch:
		return true
	default:
		return false
	}
}

// AllowsTransition is the provider-neutral state-machine law. Running model
// activity is projected from the durable Action; it does not invent a second
// Task transition while the owning model Command is still leased.
func (k DCPV2CommandKind) AllowsTransition(from, to DCPV2TaskState, createsRevision bool) bool {
	if to == DCPV2TaskHumanGate || to == DCPV2TaskFailed {
		return !createsRevision
	}
	switch k {
	case DCPV2CommandWorkerExecute:
		return from == DCPV2TaskWorkerQueued && to == DCPV2TaskChecksWaiting && createsRevision
	case DCPV2CommandChecksObserve:
		return from == DCPV2TaskChecksWaiting && to == DCPV2TaskReviewQueued && !createsRevision
	case DCPV2CommandReviewExecute:
		return from == DCPV2TaskReviewQueued && !createsRevision &&
			(to == DCPV2TaskRepairQueued || to == DCPV2TaskArbiterQueued || to == DCPV2TaskAdmissionWaiting)
	case DCPV2CommandRepairExecute:
		return from == DCPV2TaskRepairQueued && to == DCPV2TaskChecksWaiting && createsRevision
	case DCPV2CommandArbiterExecute:
		return from == DCPV2TaskArbiterQueued && to == DCPV2TaskAdmissionWaiting && !createsRevision
	case DCPV2CommandHumanGateOpen:
		return to == DCPV2TaskHumanGate && !createsRevision
	case DCPV2CommandAdmissionEnqueue:
		return from == DCPV2TaskAdmissionWaiting && to == DCPV2TaskReleaseWaiting && !createsRevision
	case DCPV2CommandReadmission:
		return from == DCPV2TaskReadmission && to == DCPV2TaskChecksWaiting && createsRevision
	case DCPV2CommandReleaseDispatch:
		return from == DCPV2TaskReleaseWaiting && to == DCPV2TaskMergeObserving && !createsRevision
	case DCPV2CommandMergeObserve:
		return from == DCPV2TaskMergeObserving && !createsRevision &&
			(to == DCPV2TaskReadmission || to == DCPV2TaskReleaseVerified || to == DCPV2TaskDeploymentWaiting)
	case DCPV2CommandDeploymentObserve:
		return from == DCPV2TaskDeploymentWaiting && to == DCPV2TaskDeploymentObserve && !createsRevision
	case DCPV2CommandTerminalVerify:
		return !createsRevision && ((from == DCPV2TaskReleaseVerified && to == DCPV2TaskMerged) ||
			(from == DCPV2TaskDeploymentObserve && to == DCPV2TaskDeployed))
	default:
		return false
	}
}

type DCPV2CommandStatus string

const (
	DCPV2CommandPending    DCPV2CommandStatus = "pending"
	DCPV2CommandLeased     DCPV2CommandStatus = "leased"
	DCPV2CommandSucceeded  DCPV2CommandStatus = "succeeded"
	DCPV2CommandFailed     DCPV2CommandStatus = "failed"
	DCPV2CommandSuperseded DCPV2CommandStatus = "superseded"
	DCPV2CommandCancelled  DCPV2CommandStatus = "cancelled"
)

type DCPV2ActionRole string

const (
	DCPV2ActionWorker   DCPV2ActionRole = "worker"
	DCPV2ActionReviewer DCPV2ActionRole = "reviewer"
	DCPV2ActionRepair   DCPV2ActionRole = "repair"
	DCPV2ActionArbiter  DCPV2ActionRole = "arbiter"
)

type DCPV2ActionStatus string

const (
	DCPV2ActionQueued    DCPV2ActionStatus = "queued"
	DCPV2ActionLaunching DCPV2ActionStatus = "launching"
	DCPV2ActionRunning   DCPV2ActionStatus = "running"
	DCPV2ActionSucceeded DCPV2ActionStatus = "succeeded"
	DCPV2ActionFailed    DCPV2ActionStatus = "failed"
)

type DCPV2AdmissionStatus string

const (
	DCPV2AdmissionWaiting             DCPV2AdmissionStatus = "waiting"
	DCPV2AdmissionLeased              DCPV2AdmissionStatus = "leased"
	DCPV2AdmissionDispatched          DCPV2AdmissionStatus = "dispatched"
	DCPV2AdmissionReadmissionRequired DCPV2AdmissionStatus = "readmission_required"
	DCPV2AdmissionSucceeded           DCPV2AdmissionStatus = "succeeded"
	DCPV2AdmissionFailed              DCPV2AdmissionStatus = "failed"
)

type DCPV2ResultKind string

const (
	DCPV2ResultRelease    DCPV2ResultKind = "release"
	DCPV2ResultDeployment DCPV2ResultKind = "deployment"
	DCPV2ResultFailure    DCPV2ResultKind = "failure"
)

type DCPV2IncidentDisposition string

const (
	DCPV2IncidentArbiter   DCPV2IncidentDisposition = "arbiter"
	DCPV2IncidentHumanGate DCPV2IncidentDisposition = "human_gate"
	DCPV2IncidentTerminal  DCPV2IncidentDisposition = "terminal"
)

type DCPV2Task struct {
	TaskID              string
	TargetSpecVersion   string
	Repository          string
	RepositoryID        int64
	OwnerID             int64
	BaseRef             string
	Profile             string
	RequestDigest       string
	ScopeDigest         string
	PolicyDigest        string
	InitialWorkerBudget int64
	RepairBudget        int64
	RepairUsed          int64
	MaxReadmissions     int64
	ReadmissionCount    int64
	CurrentRevisionID   string
	State               DCPV2TaskState
	StateRevision       int64
	TerminalResultID    string
	HumanGateQuestion   string
	ErrorCode           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type DCPV2Revision struct {
	RevisionID            string
	TaskID                string
	Sequence              int64
	Kind                  DCPV2RevisionKind
	Repository            string
	BaseRef               string
	BaseSHA               string
	HeadRef               string
	HeadSHA               string
	PredecessorRevisionID string
	CauseCommandID        string
	PRNumber              int64
	EvidenceDigest        string
	CreatedAt             time.Time
}

type DCPV2Command struct {
	Sequence           int64
	CommandID          string
	TaskID             string
	RevisionID         string
	Kind               DCPV2CommandKind
	PayloadJSON        string
	PayloadDigest      string
	PrerequisiteDigest string
	IdempotencyKey     string
	Status             DCPV2CommandStatus
	LeaseOwner         string
	LeaseEpoch         string
	LeaseToken         string
	EffectFence        string
	RecoveryGeneration int64
	ResultDigest       string
	ErrorCode          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DCPV2Action struct {
	Sequence      int64
	ActionID      string
	CommandID     string
	TaskID        string
	RevisionID    string
	Role          DCPV2ActionRole
	Model         string
	Reasoning     string
	TokenBudget   int64
	TimeBudgetSec int64
	InputDigest   string
	Attempt       int64
	Status        DCPV2ActionStatus
	Slot          int64
	LaunchFence   string
	RuntimeID     string
	ResultDigest  string
	ErrorCode     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DCPV2Admission struct {
	Sequence           int64
	AdmissionID        string
	LineKey            string
	TaskID             string
	RevisionID         string
	PRNumber           int64
	HeadSHA            string
	BaseSHA            string
	MainSHA            string
	RequiredCheckID    string
	ReviewID           string
	ManifestDigest     string
	Status             DCPV2AdmissionStatus
	LeaseOwner         string
	LeaseEpoch         string
	LeaseToken         string
	DispatchFence      string
	RecoveryGeneration int64
	ResultID           string
	ErrorCode          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DCPV2ExternalEvent struct {
	DeliveryID         string
	Provider           string
	TaskID             string
	RevisionID         string
	Kind               string
	ProviderSequence   int64
	PayloadDigest      string
	PrerequisiteDigest string
	Status             string
	CommandID          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DCPV2Incident struct {
	IncidentID     string
	TaskID         string
	RevisionID     string
	CauseCommandID string
	Kind           string
	EvidenceDigest string
	Disposition    DCPV2IncidentDisposition
	OwnerQuestion  string
	CreatedAt      time.Time
}

type DCPV2Result struct {
	ResultID       string
	TaskID         string
	RevisionID     string
	AdmissionID    string
	CommandID      string
	Kind           DCPV2ResultKind
	Provider       string
	ProofID        string
	RunID          string
	Actor          string
	ManifestDigest string
	ProofDigest    string
	MergeSHA       string
	ArtifactDigest string
	DeployedSHA    string
	Environment    string
	Service        string
	ProbeDigest    string
	Verified       bool
	ErrorCode      string
	CreatedAt      time.Time
}

// DCPV2LifecycleProjection is the one provider-neutral source for card,
// sidebar, detail, notification and accessibility presentation.
type DCPV2LifecycleProjection struct {
	Phase             DCPV2TaskState `json:"phase" enum:"worker_queued,worker_running,checks_waiting,review_queued,review_running,repair_queued,repair_running,arbiter_queued,arbiter_running,admission_waiting,readmission,release_waiting,merge_observing,release_verified,deployment_waiting,deployment_observing,human_gate,failed,merged,deployed"`
	Role              string         `json:"role,omitempty"`
	StatusLabel       string         `json:"statusLabel"`
	Detail            string         `json:"detail,omitempty"`
	RevisionID        string         `json:"revisionId"`
	AdmissionID       string         `json:"admissionId,omitempty"`
	ResultID          string         `json:"resultId,omitempty"`
	ModelActive       bool           `json:"modelActive"`
	WorkflowActive    bool           `json:"workflowActive"`
	HumanGate         bool           `json:"humanGate"`
	Error             bool           `json:"error"`
	Merged            bool           `json:"merged"`
	Deployed          bool           `json:"deployed"`
	HumanGateQuestion string         `json:"humanGateQuestion,omitempty"`
}

func ProjectDCPV2Lifecycle(task DCPV2Task, command *DCPV2Command, action *DCPV2Action, admission *DCPV2Admission, result *DCPV2Result) DCPV2LifecycleProjection {
	p := DCPV2LifecycleProjection{
		Phase: task.State, RevisionID: task.CurrentRevisionID, HumanGateQuestion: task.HumanGateQuestion,
		HumanGate: task.State == DCPV2TaskHumanGate, Error: task.State == DCPV2TaskFailed,
		Merged:   task.State == DCPV2TaskReleaseVerified || task.State == DCPV2TaskMerged || task.State == DCPV2TaskDeploymentWaiting || task.State == DCPV2TaskDeploymentObserve || task.State == DCPV2TaskDeployed,
		Deployed: task.State == DCPV2TaskDeployed,
	}
	if command != nil && command.TaskID == task.TaskID && command.RevisionID == task.CurrentRevisionID &&
		(command.Status == DCPV2CommandPending || command.Status == DCPV2CommandLeased) {
		p.WorkflowActive = true
		p.Role = string(command.Kind)
	}
	if action != nil && action.TaskID == task.TaskID && action.RevisionID == task.CurrentRevisionID {
		p.Role = string(action.Role)
		p.ModelActive = action.Status == DCPV2ActionLaunching || action.Status == DCPV2ActionRunning
		p.WorkflowActive = p.WorkflowActive || action.Status == DCPV2ActionQueued || p.ModelActive
	}
	if admission != nil && admission.TaskID == task.TaskID && admission.RevisionID == task.CurrentRevisionID {
		p.AdmissionID = admission.AdmissionID
		p.WorkflowActive = p.WorkflowActive || admission.Status == DCPV2AdmissionWaiting || admission.Status == DCPV2AdmissionLeased || admission.Status == DCPV2AdmissionDispatched
	}
	if result != nil && result.TaskID == task.TaskID && result.RevisionID == task.CurrentRevisionID {
		p.ResultID = result.ResultID
	}
	if p.HumanGate || p.Error || task.State == DCPV2TaskMerged || task.State == DCPV2TaskDeployed {
		p.WorkflowActive, p.ModelActive = false, false
	}
	switch task.State {
	case DCPV2TaskWorkerQueued:
		p.StatusLabel = "Worker queued"
	case DCPV2TaskWorkerRunning:
		p.StatusLabel = "Worker running"
	case DCPV2TaskChecksWaiting:
		p.StatusLabel = "Waiting for CI/provider update"
	case DCPV2TaskReviewQueued:
		p.StatusLabel = "Reviewer queued"
	case DCPV2TaskReviewRunning:
		p.StatusLabel = "Review running"
	case DCPV2TaskRepairQueued:
		p.StatusLabel = "Repair queued"
	case DCPV2TaskRepairRunning:
		p.StatusLabel = "Repair running"
	case DCPV2TaskArbiterQueued:
		p.StatusLabel = "Arbiter queued"
	case DCPV2TaskArbiterRunning:
		p.StatusLabel = "Arbiter running"
	case DCPV2TaskAdmissionWaiting:
		p.StatusLabel = "Waiting for FIFO admission"
	case DCPV2TaskReadmission:
		p.StatusLabel = "Materializing exact-head readmission"
	case DCPV2TaskReleaseWaiting, DCPV2TaskMergeObserving:
		p.StatusLabel = "Waiting for repository Release Train"
	case DCPV2TaskReleaseVerified:
		p.StatusLabel = "Release verified; finalizing"
	case DCPV2TaskDeploymentWaiting, DCPV2TaskDeploymentObserve:
		p.StatusLabel = "Merged; waiting for verified deployment"
	case DCPV2TaskHumanGate:
		p.StatusLabel, p.Detail = "Needs your decision", task.HumanGateQuestion
	case DCPV2TaskFailed:
		p.StatusLabel, p.Detail = "Needs attention", task.ErrorCode
	case DCPV2TaskMerged:
		p.StatusLabel = "Merged"
	case DCPV2TaskDeployed:
		p.StatusLabel = "Deployed"
	}
	return p
}
