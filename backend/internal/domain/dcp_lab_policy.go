package domain

import "time"

// DCPReviewLabPolicyVersion is the sole current policy for future synthetic
// review-lab cards. Historical cards 1-12 never receive one of these rows.
const DCPReviewLabPolicyVersion = "dcp.review-lab.happy-path/v1"

const DCPReviewLabPolicyAgentRules = "DCP synthetic PR profile v4. Work only in this exact public synthetic repository, current native worktree and current AO branch. Do not create subagents, extra branches, worktrees, remotes, network services or additional pull requests. On the initial policy action implement only the direct task, create one commit lineage, push the current branch, open one ready pull request targeting main, and stop. If the trusted daemon supplies the one bounded findings-repair envelope, change only that task on the same branch and pull request, create one new head, push, and stop. Never merge or manually review; only the trusted daemon may perform exact-head review, FIFO admission and terminal merge."

// DCPReviewLabPolicyTask binds one immutable external task id and canonical
// payload to one stock native session/card/worktree/branch identity.
type DCPReviewLabPolicyTask struct {
	TaskID          string                  `json:"taskId"`
	PayloadJSON     string                  `json:"-"`
	PayloadDigest   string                  `json:"payloadDigest"`
	Target          string                  `json:"target"`
	Profile         string                  `json:"profile"`
	Repository      string                  `json:"repository"`
	PolicyVersion   string                  `json:"policyVersion"`
	SessionID       SessionID               `json:"sessionId"`
	CardNumber      int64                   `json:"cardNumber"`
	WorktreePath    string                  `json:"worktreePath"`
	SourceBranch    string                  `json:"sourceBranch"`
	Prompt          string                  `json:"prompt"`
	State           DCPReviewLabPolicyState `json:"state"`
	Revision        int64                   `json:"revision"`
	RepairCount     int64                   `json:"repairCount"`
	PRURL           string                  `json:"prUrl,omitempty"`
	PRNumber        int64                   `json:"prNumber,omitempty"`
	CurrentHeadSHA  string                  `json:"currentHeadSha,omitempty"`
	PreviousHeadSHA string                  `json:"previousHeadSha,omitempty"`
	ReviewRunID     string                  `json:"reviewRunId,omitempty"`
	AdmissionID     string                  `json:"admissionId,omitempty"`
	MergeCommitSHA  string                  `json:"mergeCommitSha,omitempty"`
	ErrorCode       string                  `json:"errorCode,omitempty"`
	IncidentPacket  string                  `json:"incidentPacket,omitempty"`
	CreatedAt       time.Time               `json:"createdAt"`
	UpdatedAt       time.Time               `json:"updatedAt"`
}

// DCPReviewLabPolicyState is the durable projection shown through the same
// native card. Waiting states own no process, timer, heartbeat, or poller.
type DCPReviewLabPolicyState string

const (
	DCPPolicyReserved      DCPReviewLabPolicyState = "reserved"
	DCPPolicyWorkerQueued  DCPReviewLabPolicyState = "worker_queued"
	DCPPolicyWorkerRunning DCPReviewLabPolicyState = "worker_running"
	DCPPolicyCIWaiting     DCPReviewLabPolicyState = "ci_waiting"
	DCPPolicyReviewQueued  DCPReviewLabPolicyState = "review_queued"
	DCPPolicyReviewRunning DCPReviewLabPolicyState = "review_running"
	DCPPolicyRepairQueued  DCPReviewLabPolicyState = "repair_queued"
	DCPPolicyRepairRunning DCPReviewLabPolicyState = "repair_running"
	DCPPolicyAdmissionWait DCPReviewLabPolicyState = "admission_waiting"
	DCPPolicyMerged        DCPReviewLabPolicyState = "merged"
	DCPPolicyFailed        DCPReviewLabPolicyState = "failed"
	DCPPolicyIncident      DCPReviewLabPolicyState = "incident"
)

// DCPModelActionKind is one bounded model-bearing action. CI and admission are
// deliberately absent because they never own a model slot.
type DCPModelActionKind string

const (
	DCPActionInitialWorker DCPModelActionKind = "initial_worker"
	DCPActionRepairWorker  DCPModelActionKind = "repair_worker"
	DCPActionReviewer      DCPModelActionKind = "reviewer"
	DCPActionArbiter       DCPModelActionKind = "arbiter"
)

// DCPModelActionStatus is the durable FIFO/slot lifecycle. Only claimed and
// running rows own one of the three global slots.
type DCPModelActionStatus string

const (
	DCPActionQueued    DCPModelActionStatus = "queued"
	DCPActionClaimed   DCPModelActionStatus = "claimed"
	DCPActionRunning   DCPModelActionStatus = "running"
	DCPActionSucceeded DCPModelActionStatus = "succeeded"
	DCPActionFailed    DCPModelActionStatus = "failed"
)

// DCPModelAction is one immutable action identity with mutable guarded
// execution facts. ExactHead is empty only for the initial worker.
type DCPModelAction struct {
	Sequence     int64
	ID           string
	TaskID       string
	SessionID    SessionID
	Kind         DCPModelActionKind
	ExactHeadSHA string
	Status       DCPModelActionStatus
	Slot         int64
	LaunchID     string
	ReviewRunID  string
	IncidentID   string
	ErrorCode    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s DCPReviewLabPolicyState) Terminal() bool {
	return s == DCPPolicyMerged || s == DCPPolicyFailed || s == DCPPolicyIncident
}
