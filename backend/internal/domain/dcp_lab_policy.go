package domain

import (
	"errors"
	"strings"
	"time"
)

// DCPReviewLabPolicyVersion is the sole current policy for future synthetic
// review-lab cards. Historical cards 1-12 never receive one of these rows.
const DCPReviewLabPolicyVersion = "dcp.review-lab.happy-path/v1"

// DCPRepoOnlyPolicyVersion is the exact first-real-target policy. It reuses
// the durable future-task/action/admission authority without widening it into
// a caller-defined repository launcher.
const DCPRepoOnlyPolicyVersion = "dcp.repo-only.happy-path/v1"

// DCPWBCRepoOnlyPolicyVersion is the exact wb-core policy. Its release is
// handed to the repository-owned GitHub Actions Release Train; the DCP direct
// merger is never eligible for this policy.
const DCPWBCRepoOnlyPolicyVersion = "dcp.wb-core.repo-only.release-train/v1"

const DCPReviewLabPolicyAgentRules = "DCP synthetic PR profile v4. Work only in this exact public synthetic repository, current native worktree and current AO branch. Do not create subagents, extra branches, worktrees, remotes, network services or additional pull requests. On the initial policy action implement only the direct task, create one commit lineage, push the current branch, open one ready pull request targeting main, and stop. If the trusted daemon supplies the one bounded findings-repair envelope, change only that task on the same branch and pull request, create one new head, push, and stop. Never merge or manually review; only the trusted daemon may perform exact-head review, FIFO admission and terminal merge."

const DCPRepoOnlyPolicyAgentRules = "DCP repo-only profile v1. Work only in this exact public wb-browser-extension repository, current native worktree and current AO branch. Read and obey the repository AGENTS.md. Do not access or mutate wb-core, dev-control-plane, dcp-orchestrator, production, secrets, other repositories, deployments, servers, telemetry, or live Wildberries APIs. Do not create subagents, extra branches, worktrees, remotes, network services or additional pull requests. On the initial policy action implement only the direct task, run the repository baseline, create one commit lineage, push the current branch, open one ready pull request targeting main, and stop. If the trusted daemon supplies the one bounded findings-repair envelope, change only that task on the same branch and pull request, create one new head, run the repository baseline, push, and stop. Never merge or manually review; only the trusted daemon may perform exact-head review, FIFO admission and terminal merge."

const DCPWBCRepoOnlyPolicyAgentRules = "DCP wb-core repo-only profile v1. Work only in this exact public wb-core repository, current native worktree and current AO branch. Read and obey the repository AGENTS.md. The task must remain task:standard with exactly scope:repo-only. Do not access live runtime, production, SSH, secrets, runtime data, business data, other repositories, deployments, servers, telemetry, or live Wildberries APIs. Do not create subagents, extra branches, worktrees, remotes, network services or additional pull requests. On the initial policy action implement only the direct task, run baseline, create one commit lineage, push the current branch, open one ready pull request targeting main with exactly task:standard and scope:repo-only and no release label, and stop. If the trusted daemon supplies the one bounded findings-repair envelope, change only that task on the same branch and pull request, create one new head, run baseline, push, and stop. Never add release:ready, merge, release or manually review; only the trusted daemon may perform exact-head review and FIFO admission, and only the WBC GitHub Actions Release Train may merge and add release:done."

const dcpLegacyRepoOnlyPolicyAgentRules = "DCP repo-only profile v1. Work only in this exact public wb-price-extension repository, current native worktree and current AO branch. Read and obey the repository AGENTS.md. Do not access or mutate wb-core, dev-control-plane, dcp-orchestrator, production, secrets, other repositories, deployments, servers, telemetry, or live Wildberries APIs. Do not create subagents, extra branches, worktrees, remotes, network services or additional pull requests. On the initial policy action implement only the direct task, run the repository baseline, create one commit lineage, push the current branch, open one ready pull request targeting main, and stop. If the trusted daemon supplies the one bounded findings-repair envelope, change only that task on the same branch and pull request, create one new head, run the repository baseline, push, and stop. Never merge or manually review; only the trusted daemon may perform exact-head review, FIFO admission and terminal merge."

// DCPPolicyTargetSpec is one immutable, compile-time allowlist entry. No
// repository, path, branch, provider identity, check, or policy value is
// accepted from a task after this lookup succeeds.
type DCPPolicyTargetSpec struct {
	Target               string
	Profile              string
	Repository           string
	OriginURL            string
	ProviderRepositoryID int64
	ProviderOwnerID      int64
	DefaultBranch        string
	RequiredCheck        string
	SessionPrefix        string
	PolicyVersion        string
	AgentRules           string
	MinimumCardNumber    int64
	ReleaseAuthority     DCPPolicyReleaseAuthority
	CompatibilityMarker  string
	CompatibilityFiles   [3]string
}

// DCPRequiredCheck is the provider-neutral evidence for one check attached to
// one exact pull-request head. The policy layer deliberately gives no meaning
// to check names other than DCPPolicyTargetSpec.RequiredCheck.
type DCPRequiredCheck struct {
	Name       string
	HeadSHA    string
	Status     string
	Conclusion string
	URL        string
}

// DCPRequiredCheckGate is the stable CI handoff between SCM observation and
// DCP policy. Missing/wrong-head and still-running evidence waits without a
// model action; rejected evidence is terminal for the current exact head.
type DCPRequiredCheckGate string

const (
	// DCPRequiredCheckMissing is a durable zero-action wait for exact evidence.
	DCPRequiredCheckMissing DCPRequiredCheckGate = "missing"
	// DCPRequiredCheckPending is a durable zero-action wait for terminal evidence.
	DCPRequiredCheckPending DCPRequiredCheckGate = "pending"
	// DCPRequiredCheckPassed is exact configured-check success.
	DCPRequiredCheckPassed DCPRequiredCheckGate = "passed"
	// DCPRequiredCheckRejected is terminal malformed or unsuccessful evidence.
	DCPRequiredCheckRejected DCPRequiredCheckGate = "rejected"
)

// EvaluateDCPRequiredCheck evaluates exactly one configured named check on the
// expected head. Extra checks are observational: their names, status and
// conclusions cannot change this verdict. Provider URL identity remains an
// independent caller-owned gate because it is repository-specific.
func EvaluateDCPRequiredCheck(requiredName, expectedHead string, checks []DCPRequiredCheck) (DCPRequiredCheckGate, DCPRequiredCheck, error) {
	if requiredName == "" || expectedHead == "" {
		return DCPRequiredCheckRejected, DCPRequiredCheck{}, errors.New("required check identity is incomplete")
	}
	var matched []DCPRequiredCheck
	for _, check := range checks {
		if check.Name == requiredName && strings.EqualFold(check.HeadSHA, expectedHead) {
			matched = append(matched, check)
		}
	}
	if len(matched) == 0 {
		return DCPRequiredCheckMissing, DCPRequiredCheck{}, nil
	}
	if len(matched) != 1 {
		return DCPRequiredCheckRejected, DCPRequiredCheck{}, errors.New("required check cardinality is not exact")
	}
	check := matched[0]
	switch PRCheckStatus(check.Status) {
	case PRCheckQueued, PRCheckInProgress:
		if check.Conclusion != "" {
			return DCPRequiredCheckRejected, check, errors.New("pending required check has a terminal conclusion")
		}
		return DCPRequiredCheckPending, check, nil
	case PRCheckPassed:
		if check.Conclusion != "success" {
			return DCPRequiredCheckRejected, check, errors.New("passed required check lacks successful conclusion")
		}
		return DCPRequiredCheckPassed, check, nil
	case PRCheckFailed, PRCheckSkipped, PRCheckCancelled:
		return DCPRequiredCheckRejected, check, errors.New("required check did not succeed")
	default:
		return DCPRequiredCheckRejected, check, errors.New("required check status is malformed")
	}
}

// DCPPolicyReleaseAuthority selects one compile-time terminal actor. It is not
// accepted from task input.
type DCPPolicyReleaseAuthority string

const (
	DCPReleaseDirect       DCPPolicyReleaseAuthority = "dcp-direct"
	DCPReleaseWBCTrainOnly DCPPolicyReleaseAuthority = "wbc-release-train-only"
)

func (s DCPPolicyTargetSpec) UsesWBCReleaseTrain() bool {
	return s.ReleaseAuthority == DCPReleaseWBCTrainOnly
}

var dcpPolicyTargetSpecs = [...]DCPPolicyTargetSpec{
	{
		Target: "dcp-review-lab", Profile: "synthetic-pr", Repository: "orenvlad-ai/dcp-review-lab",
		OriginURL: "https://github.com/orenvlad-ai/dcp-review-lab.git", ProviderRepositoryID: 1329007118,
		ProviderOwnerID: 237411244, DefaultBranch: "main", RequiredCheck: "dcp-review-lab",
		SessionPrefix: "dcp-review-lab", PolicyVersion: DCPReviewLabPolicyVersion,
		AgentRules: DCPReviewLabPolicyAgentRules, MinimumCardNumber: 13, ReleaseAuthority: DCPReleaseDirect,
	},
	{
		Target: "wb-browser-extension", Profile: "repo-only", Repository: "orenvlad-ai/wb-browser-extension",
		OriginURL: "https://github.com/orenvlad-ai/wb-browser-extension.git", ProviderRepositoryID: 1335072844,
		ProviderOwnerID: 237411244, DefaultBranch: "main", RequiredCheck: "baseline",
		SessionPrefix: "wb-browser-extension", PolicyVersion: DCPRepoOnlyPolicyVersion,
		AgentRules: DCPRepoOnlyPolicyAgentRules, MinimumCardNumber: 1, ReleaseAuthority: DCPReleaseDirect,
	},
	{
		Target: "wb-core", Profile: "repo-only", Repository: "orenvlad-ai/wb-core",
		OriginURL: "https://github.com/orenvlad-ai/wb-core.git", ProviderRepositoryID: 1201929580,
		ProviderOwnerID: 237411244, DefaultBranch: "main", RequiredCheck: "baseline",
		SessionPrefix: "wb-core", PolicyVersion: DCPWBCRepoOnlyPolicyVersion,
		AgentRules: DCPWBCRepoOnlyPolicyAgentRules, MinimumCardNumber: 1,
		ReleaseAuthority: DCPReleaseWBCTrainOnly, CompatibilityMarker: "wb-core.dcp-release-handoff/v1",
		CompatibilityFiles: [3]string{
			"docs/architecture/11_github_release_train.md",
			"apps/github_release_train.py",
			"apps/github_release_train_spec.py",
		},
	},
}

var dcpLegacyRepoOnlyTerminalSpec = DCPPolicyTargetSpec{
	Target: "wb-price-extension", Profile: "repo-only", Repository: "orenvlad-ai/wb-price-extension",
	OriginURL: "https://github.com/orenvlad-ai/wb-price-extension.git", ProviderRepositoryID: 1335072844,
	ProviderOwnerID: 237411244, DefaultBranch: "main", RequiredCheck: "baseline",
	SessionPrefix: "wb-price-extension", PolicyVersion: DCPRepoOnlyPolicyVersion,
	AgentRules: dcpLegacyRepoOnlyPolicyAgentRules, MinimumCardNumber: 1, ReleaseAuthority: DCPReleaseDirect,
}

func DCPPolicyTarget(target, profile string) (DCPPolicyTargetSpec, bool) {
	for _, spec := range dcpPolicyTargetSpecs {
		if spec.Target == target && spec.Profile == profile {
			return spec, true
		}
	}
	return DCPPolicyTargetSpec{}, false
}

func DCPPolicyTargetForProject(target string) (DCPPolicyTargetSpec, bool) {
	for _, spec := range dcpPolicyTargetSpecs {
		if spec.Target == target {
			return spec, true
		}
	}
	return DCPPolicyTargetSpec{}, false
}

func DCPPolicyTargetForRepository(repository string) (DCPPolicyTargetSpec, bool) {
	for _, spec := range dcpPolicyTargetSpecs {
		if spec.Repository == repository {
			return spec, true
		}
	}
	return DCPPolicyTargetSpec{}, false
}

func DCPPolicyTargetForTask(task DCPReviewLabPolicyTask) (DCPPolicyTargetSpec, bool) {
	spec, ok := DCPPolicyTarget(task.Target, task.Profile)
	if ok && task.Repository == spec.Repository && task.PolicyVersion == spec.PolicyVersion {
		return spec, true
	}
	if IsExactDCPRepoOnlyLegacyTerminalTask(task) {
		return dcpLegacyRepoOnlyTerminalSpec, true
	}
	return DCPPolicyTargetSpec{}, false
}

// IsExactDCPRepoOnlyLegacyTerminalTask recognizes only the immutable completed
// first real-target row after the provider repository rename. It is deliberately
// absent from DCPPolicyTarget, so it can be restored and rendered but can never
// authorize a future submit.
func IsExactDCPRepoOnlyLegacyTerminalTask(task DCPReviewLabPolicyTask) bool {
	return task.TaskID == "price-arch-v1" &&
		task.PayloadDigest == "efe6a81cfff28be89cc327bdc9e2380ca585fcc6b03064c0290b6aaf4c7b59fe" &&
		task.Target == "wb-price-extension" && task.Profile == "repo-only" &&
		task.Repository == "orenvlad-ai/wb-price-extension" && task.PolicyVersion == DCPRepoOnlyPolicyVersion &&
		task.SessionID == "wb-price-extension-1" && task.CardNumber == 1 &&
		task.WorktreePath == "/Users/ovlmacbook/Library/Application Support/DCP Orchestrator/data/worktrees/wb-price-extension/wb-price-extension-1" &&
		task.SourceBranch == "ao/wb-price-extension-1/root" && task.State == DCPPolicyMerged &&
		task.Revision == 7 && task.RepairCount == 0 &&
		task.PRURL == "https://github.com/orenvlad-ai/wb-price-extension/pull/1" && task.PRNumber == 1 &&
		task.CurrentHeadSHA == "afc748eba5ff05c0dc24d3002c690ec9f44984fb" && task.PreviousHeadSHA == "" &&
		task.ReviewRunID == "b0acfb9e-600c-4816-bb2f-02a67817ea05" &&
		task.AdmissionID == "dcp-admission-b0acfb9e-600c-4816-bb2f-02a67817ea05" &&
		task.MergeCommitSHA == "62853496837f64522bb08ba56169f60f3b0f9a2c" &&
		task.ErrorCode == "" && task.IncidentPacket == ""
}

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
	DCPPolicyReserved       DCPReviewLabPolicyState = "reserved"
	DCPPolicyWorkerQueued   DCPReviewLabPolicyState = "worker_queued"
	DCPPolicyWorkerRunning  DCPReviewLabPolicyState = "worker_running"
	DCPPolicyCIWaiting      DCPReviewLabPolicyState = "ci_waiting"
	DCPPolicyReviewQueued   DCPReviewLabPolicyState = "review_queued"
	DCPPolicyReviewRunning  DCPReviewLabPolicyState = "review_running"
	DCPPolicyRepairQueued   DCPReviewLabPolicyState = "repair_queued"
	DCPPolicyRepairRunning  DCPReviewLabPolicyState = "repair_running"
	DCPPolicyAdmissionWait  DCPReviewLabPolicyState = "admission_waiting"
	DCPPolicyReleaseWaiting DCPReviewLabPolicyState = "release_waiting"
	DCPPolicyMerged         DCPReviewLabPolicyState = "merged"
	DCPPolicyFailed         DCPReviewLabPolicyState = "failed"
	DCPPolicyIncident       DCPReviewLabPolicyState = "incident"
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
