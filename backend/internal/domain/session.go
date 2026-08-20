package domain

import "time"

// These ID types are distinct string types so they can't be swapped at a call
// site by accident.
type (
	// SessionID identifies a session.
	SessionID string
	// ProjectID identifies a project.
	ProjectID string
	// IssueID identifies a tracker issue.
	IssueID string
)

// SessionKind distinguishes a worker session from an orchestrator session.
type SessionKind string

// Session kinds.
const (
	KindWorker       SessionKind = "worker"
	KindOrchestrator SessionKind = "orchestrator"
)

// SessionMetadata is the typed, off-status metadata for a session: operational
// handles and seed inputs used by Session Manager and reaper.
type SessionMetadata struct {
	Branch            string `json:"branch,omitempty"`
	WorkspacePath     string `json:"workspacePath,omitempty"`
	WorkspaceRepoPath string `json:"workspaceRepoPath,omitempty"`
	DiffBaseSHA       string `json:"diffBaseSha,omitempty"`
	DiffBaseRef       string `json:"diffBaseRef,omitempty"`
	RuntimeHandleID   string `json:"runtimeHandleId,omitempty"`
	RuntimeLaunchID   string `json:"runtimeLaunchId,omitempty"`
	AgentSessionID    string `json:"agentSessionId,omitempty"`
	Prompt            string `json:"prompt,omitempty"`
	// PreviewURL is the browser preview target the desktop app opens for this
	// session. Set via `ao preview` (POST /sessions/{id}/preview); persisted so
	// it survives a daemon restart. Empty means no preview has been requested.
	PreviewURL string `json:"previewUrl,omitempty"`
	// PreviewRevision is a monotonic counter bumped on every `ao preview` call,
	// even when PreviewURL is unchanged. The desktop browser panel keys
	// navigation on it so a repeated `ao preview <same-url>` still refreshes.
	PreviewRevision int64 `json:"previewRevision,omitempty"`
}

// SessionRecord is the persistence shape. It intentionally stores only durable
// facts: identity, agent harness, activity_state, is_terminated, and operational
// metadata. The user-facing Status is derived from these facts plus PR facts.
type SessionRecord struct {
	ID        SessionID    `json:"id"`
	ProjectID ProjectID    `json:"projectId"`
	IssueID   IssueID      `json:"issueId,omitempty"`
	Kind      SessionKind  `json:"kind"`
	Harness   AgentHarness `json:"harness,omitempty"`
	// ReviewerHarness is this session's preferred reviewer. Empty delegates to
	// the project configuration.
	ReviewerHarness ReviewerHarness `json:"reviewerHarness,omitempty" enum:"claude-code,codex,opencode"`
	DisplayName     string          `json:"displayName,omitempty"`
	Activity        Activity        `json:"activity"`
	// FirstSignalAt is when the FIRST agent hook callback arrived for the
	// current spawn/restore: raw signal receipt, independent of the derived
	// activity state. Zero means no hook has ever reported, which deriveStatus
	// surfaces as StatusNoSignal after a grace period. Internal fact, not part
	// of the API read model.
	FirstSignalAt time.Time `json:"-"`
	IsTerminated  bool      `json:"isTerminated"`
	// TerminateOnPRMerge is a user-controlled lifecycle policy. When enabled,
	// completing the session's PR set through a merge tears down the session.
	TerminateOnPRMerge bool            `json:"terminateOnPrMerge"`
	Metadata           SessionMetadata `json:"-"`
	// CleanupGeneration is a monotonic counter bumped each time the session is
	// un-terminated (spawn/restore). The terminal-resource reconciler stamps its
	// durable cleanup facts with the generation they were written for so a
	// finalize started under an earlier terminal episode cannot satisfy a later
	// one. Internal fact, not part of the API read model.
	CleanupGeneration int64      `json:"-"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	IsPinned          bool       `json:"isPinned"`
	PinnedAt          *time.Time `json:"pinnedAt,omitempty"`
}

// Session is the read-model returned across the API boundary: a SessionRecord
// plus derived display facts. Neither Status nor SCMStatus is persisted.
type Session struct {
	SessionRecord
	Status           SessionStatus `json:"status" enum:"working,pr_open,draft,ci_failed,review_pending,review_failed,changes_requested,approved,mergeable,merged,needs_input,exited,idle,terminated,no_signal"`
	SCMStatus        SessionStatus `json:"scmStatus,omitempty" enum:"pr_open,draft,ci_failed,review_pending,changes_requested,approved,mergeable,merged"`
	TerminalHandleID string        `json:"terminalHandleId,omitempty"`
	// DCPV2 is the single provider-neutral lifecycle projection. Stage 4 does
	// not populate it: a later reviewed adapter/install stage must bind an exact
	// DCP v2 Task to a native Session before this pointer can become non-nil.
	DCPV2                      *DCPV2LifecycleProjection `json:"dcpV2,omitempty"`
	DCPPolicyState             DCPReviewLabPolicyState   `json:"dcpPolicyState,omitempty" enum:"reserved,worker_queued,worker_running,ci_waiting,review_queued,review_running,repair_queued,repair_running,admission_waiting,release_waiting,merged,failed,incident"`
	DCPPolicyProfile           string                    `json:"dcpPolicyProfile,omitempty" enum:"synthetic-pr,repo-only,live-runtime"`
	DCPPolicyReleasePhase      DCPWBCReleasePhase        `json:"dcpPolicyReleasePhase,omitempty" enum:"waiting_release_train,release_train_running,waiting_deploy,deploy_running"`
	DCPPolicyReadmissionStatus DCPWBCReadmissionStatus   `json:"dcpPolicyReadmissionStatus,omitempty" enum:"observed,claimed,prepared,head_pushed,review_queued,reviewed,admitted,release_waiting"`
	// DCPPolicyModelActive is true only while a durable model action is
	// running. DCPPolicyWorkflowActive remains true across zero-action waits so
	// the UI can show lifecycle motion without claiming a model slot.
	DCPPolicyModelActive    bool `json:"dcpPolicyModelActive,omitempty"`
	DCPPolicyWorkflowActive bool `json:"dcpPolicyWorkflowActive,omitempty"`
	// DCPPolicyActionActive is retained as a wire-compatible alias for older
	// renderer bundles. New projections must use the two typed facts above.
	DCPPolicyActionActive  bool                   `json:"dcpPolicyActionActive,omitempty"`
	DCPArbiterStatus       DCPFutureArbiterStatus `json:"dcpArbiterStatus,omitempty" enum:"requested,claimed,running,hold,repair_queued,recovery_reviewed,human_gate,succeeded,failed"`
	DCPArbiterGeneration   int64                  `json:"dcpArbiterGeneration,omitempty"`
	DCPArbiterIncidentKind string                 `json:"dcpArbiterIncidentKind,omitempty"`
	DCPArbiterCohort       []string               `json:"dcpArbiterCohort,omitempty"`
	DCPArbiterActionStatus DCPModelActionStatus   `json:"dcpArbiterActionStatus,omitempty" enum:"queued,claimed,running,succeeded,failed"`
	DCPHumanGateQuestion   string                 `json:"dcpHumanGateQuestion,omitempty"`
	// PRs are the session's attributed pull requests (one session can own many).
	// They feed status derivation and are surfaced on the API read model. Not
	// serialized here: the HTTP boundary maps them to the curated wire shape.
	PRs []PRFacts `json:"-"`
}
