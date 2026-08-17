import type {
	DCPPolicyState,
	SessionActivity,
	SessionActivityState,
	SessionStatus,
	WorkspaceSession,
} from "../types/workspace";
import { appI18n, type MessageKey } from "../i18n";
import type { TFunction } from "i18next";

export type AgentActivityView = {
	state: SessionActivityState;
	label: string;
	tone: string;
	dotClassName: string;
	indicatorClassName: string;
	breathe: boolean;
};

type AgentActivityBase = Omit<AgentActivityView, "label" | "indicatorClassName"> & { labelKey: MessageKey };

const agentActivityBases: Record<SessionActivityState, AgentActivityBase> = {
	active: {
		state: "active",
		labelKey: "activity.active",
		tone: "var(--color-status-working)",
		dotClassName: "bg-status-working",
		breathe: true,
	},
	idle: {
		state: "idle",
		labelKey: "activity.idle",
		tone: "var(--color-status-idle)",
		dotClassName: "bg-status-idle",
		breathe: false,
	},
	waiting_input: {
		state: "waiting_input",
		labelKey: "activity.waiting_input",
		tone: "var(--color-status-needs-you)",
		dotClassName: "bg-status-needs-you",
		breathe: false,
	},
	blocked: {
		state: "blocked",
		labelKey: "activity.blocked",
		tone: "var(--color-status-needs-you)",
		dotClassName: "bg-status-needs-you",
		breathe: false,
	},
	exited: {
		state: "exited",
		labelKey: "activity.exited",
		tone: "var(--color-status-exited)",
		dotClassName: "bg-status-exited",
		breathe: false,
	},
	unknown: {
		state: "unknown",
		labelKey: "activity.unknown",
		tone: "var(--color-status-unknown)",
		dotClassName: "bg-status-unknown",
		breathe: false,
	},
};

export function getAgentActivityView(activity?: SessionActivity | null, t: TFunction = appI18n.t): AgentActivityView {
	const state = activity?.state ?? "unknown";
	const base = agentActivityBases[state] ?? agentActivityBases.unknown;
	const { labelKey, ...rest } = base;
	return {
		...rest,
		label: t(labelKey),
		indicatorClassName: `${rest.dotClassName}${rest.breathe ? " animate-status-pulse" : ""}`,
	};
}

export function isAgentActivityWorking(activity?: SessionActivity | null): boolean {
	return getAgentActivityView(activity).state === "active";
}

export type SessionVisualStatus = {
	policyPhase?: DCPPolicyPhase;
	laneSection?: DCPReviewLaneSection;
	zone: AttentionZone;
	displayStatus: SessionStatus;
	statusLabelKey?: MessageKey;
	statusClassName?: string;
	detail?: string;
	tone: "working" | "review" | "arbiter" | "ready" | "attention" | "merged" | "failed" | "idle";
	dotClassName: string;
	indicatorClassName: string;
	modelActive: boolean;
	workflowActive: boolean;
	/** @deprecated Use workflowActive; retained for existing component data attributes. */
	active: boolean;
};

export type DCPPolicyPhase = "working" | "in_review" | "arbiter" | "ready_to_merge" | "merged" | "needs_you";
export type DCPReviewLaneSection = "in_review" | "arbiter";

const policyPhaseStatusClassNames: Record<DCPPolicyPhase, string> = {
	working: "text-status-working",
	in_review: "text-status-in-review",
	arbiter: "text-status-arbiter",
	ready_to_merge: "text-status-ready",
	merged: "text-status-merged",
	needs_you: "text-status-exited",
};

function visualStatus(
	tone: SessionVisualStatus["tone"],
	dotClassName: string,
	options: Pick<SessionVisualStatus, "zone" | "displayStatus"> &
		Partial<
			Pick<
				SessionVisualStatus,
				| "policyPhase"
				| "laneSection"
				| "statusLabelKey"
				| "statusClassName"
				| "detail"
				| "active"
				| "modelActive"
				| "workflowActive"
			>
		>,
): SessionVisualStatus {
	const workflowActive = options.workflowActive ?? options.active === true;
	const modelActive = options.modelActive ?? options.active === true;
	return {
		...options,
		tone,
		dotClassName,
		indicatorClassName: `${dotClassName}${workflowActive ? " animate-status-pulse" : ""}`,
		modelActive,
		workflowActive,
		active: workflowActive,
	};
}

function policyVisualStatus(
	state: DCPPolicyState,
	session: WorkspaceSession,
	modelActive: boolean,
	workflowActive: boolean,
): SessionVisualStatus {
	const policy = (
		policyPhase: DCPPolicyPhase,
		zone: AttentionZone,
		displayStatus: SessionStatus,
		tone: SessionVisualStatus["tone"],
		dotClassName: string,
		statusLabelKey: MessageKey,
		detail?: string,
	) =>
		visualStatus(tone, dotClassName, {
			policyPhase,
			zone,
			displayStatus,
			statusLabelKey,
			statusClassName: policyPhaseStatusClassNames[policyPhase],
			detail,
			modelActive,
			workflowActive,
		});

	if (state === "incident" && session.dcpArbiterStatus === "human_gate") {
		return visualStatus("attention", "bg-status-needs-you", {
			policyPhase: "needs_you",
			zone: "action",
			displayStatus: "review_failed",
			statusLabelKey: "status.human_gate",
			statusClassName: "text-status-needs-you",
			detail: dcpArbiterDetail(session),
			modelActive: false,
			workflowActive: false,
		});
	}

	if (state === "incident" && isAutomaticArbiterContinuation(session.dcpArbiterStatus)) {
		const evaluating = session.dcpArbiterStatus === "claimed" || session.dcpArbiterStatus === "running";
		return visualStatus("arbiter", "bg-status-arbiter", {
			policyPhase: "arbiter",
			laneSection: "arbiter",
			zone: "pending",
			displayStatus: "review_pending",
			statusLabelKey: evaluating ? "status.arbiter_evaluating" : arbiterStatusLabelKey(session),
			statusClassName: "text-status-arbiter",
			detail: dcpArbiterDetail(session),
			modelActive:
				modelActive && session.dcpArbiterStatus === "running" && session.dcpArbiterActionStatus === "running",
			workflowActive,
		});
	}

	switch (state) {
		case "reserved":
			return policy("working", "working", "working", "working", "bg-status-working", "status.policy_preparing", "Preparing the governed task");
		case "worker_queued":
			return policy("working", "working", "working", "working", "bg-status-working", "status.worker_queued", "Waiting for a worker model slot");
		case "repair_queued":
			return policy("working", "working", "working", "working", "bg-status-working", "status.repair_queued", "Bounded repair queued");
		case "worker_running":
			return policy("working", "working", "working", "working", "bg-status-working", "status.worker_running", "Worker model action is running");
		case "repair_running":
			return policy("working", "working", "working", "working", "bg-status-working", "status.repair_running", "Bounded repair model action is running");
		case "ci_waiting":
			return policy(
				"working",
				"working",
				session.status === "pr_open" || session.status === "draft" ? session.status : "working",
				"working",
				"bg-status-working",
				"status.ci_waiting",
				"Waiting for CI/GitHub update",
			);
		case "review_queued":
			return {
				...policy("in_review", "pending", "review_pending", "review", "bg-status-in-review", "status.review_queued", "Fresh context-free reviewer queued"),
				laneSection: "in_review",
			};
		case "review_running":
			return {
				...policy("in_review", "pending", "review_pending", "review", "bg-status-in-review", "status.review_running", "Fresh context-free reviewer is running"),
				laneSection: "in_review",
			};
		case "admission_waiting":
			return policy("ready_to_merge", "merge", "mergeable", "ready", "bg-status-ready", "status.admission_waiting", "Waiting for FIFO admission");
		case "release_waiting":
			return policy("ready_to_merge", "merge", "mergeable", "ready", "bg-status-ready", "status.release_waiting", "Waiting for Release Train");
		case "merged":
			return policy("merged", "merge", "merged", "merged", "bg-status-merged", "status.policy_complete");
		case "failed":
			return visualStatus("failed", "bg-status-exited", {
				policyPhase: "needs_you", zone: "action", displayStatus: "review_failed",
				statusLabelKey: "status.policy_failed", statusClassName: "text-status-exited",
				detail: "DCP stopped on an actionable technical error", modelActive: false, workflowActive: false,
			});
		case "incident":
			return {
				...visualStatus("failed", "bg-status-exited", {
					policyPhase: "needs_you", zone: "action", displayStatus: "review_failed",
					statusLabelKey: "status.policy_incident", statusClassName: "text-status-exited",
					modelActive: false, workflowActive: false,
				}),
				detail: dcpArbiterDetail(session),
			};
	}
}

function isAutomaticArbiterContinuation(status: WorkspaceSession["dcpArbiterStatus"]): boolean {
	return (
		status === "requested" ||
		status === "claimed" ||
		status === "running" ||
		status === "hold" ||
		status === "repair_queued" ||
		status === "recovery_reviewed" ||
		status === "succeeded"
	);
}

function arbiterStatusLabelKey(session: WorkspaceSession): MessageKey {
	switch (session.dcpArbiterStatus) {
		case "requested":
			return "status.arbiter_waiting";
		case "hold":
		case "repair_queued":
		case "recovery_reviewed":
		case "succeeded":
			return "status.arbiter_decision_pending";
		default:
			return "status.arbiter_evaluating";
	}
}

function dcpArbiterDetail(session: WorkspaceSession): string | undefined {
	if (!session.dcpArbiterStatus) return undefined;
	const metadata = [
		session.dcpArbiterIncidentKind ? `incident ${session.dcpArbiterIncidentKind}` : null,
		session.dcpArbiterGeneration ? `generation ${session.dcpArbiterGeneration}` : null,
		session.dcpArbiterCohort?.length ? `cohort ${session.dcpArbiterCohort.join(" → ")}` : null,
		session.dcpArbiterActionStatus ? `action ${session.dcpArbiterActionStatus}` : null,
	]
		.filter(Boolean)
		.join(" · ");
	const suffix = metadata ? ` · ${metadata}` : "";
	switch (session.dcpArbiterStatus) {
		case "requested":
			return `Waiting for the automatic arbiter${suffix}`;
		case "claimed":
		case "running":
			return `Arbiter evaluating this incident${suffix}`;
		case "hold":
			return `Held passively for the arbiter-approved order${suffix}`;
		case "repair_queued":
			return `Arbiter-approved bounded repair is pending${suffix}`;
		case "recovery_reviewed":
			return `Repaired head approved; waiting for trusted merge${suffix}`;
		case "human_gate":
			return `${session.dcpHumanGateQuestion || "Owner decision required"}${suffix}`;
		case "failed":
			return `Arbiter failed closed${suffix}`;
		case "succeeded":
			return `Automatic arbiter decision accepted; deterministic transition pending${suffix}`;
	}
}

// One typed projection serves board placement, board card status and sidebar
// dot. Durable policy lifecycle wins over retained shells and stale stock SCM;
// workflow motion spans autonomous zero-model waits, while modelActive remains
// a separate exact accounting fact.
export function getSessionVisualStatus(session: WorkspaceSession): SessionVisualStatus {
	const modelActive = session.dcpPolicyModelActive ?? session.dcpPolicyActionActive === true;
	const workflowActive = session.dcpPolicyWorkflowActive ?? fallbackPolicyWorkflowActive(session);
	if (session.dcpPolicyState) {
		return policyVisualStatus(session.dcpPolicyState, session, modelActive, workflowActive);
	}

	if (session.activity?.state === "active") {
		return visualStatus("working", "bg-status-working", {
			zone: "working",
			displayStatus: session.status,
			active: true,
		});
	}
	if (session.activity?.state === "waiting_input" || session.activity?.state === "blocked") {
		return visualStatus("attention", "bg-status-needs-you", {
			zone: "action",
			displayStatus: session.status,
		});
	}
	switch (session.status) {
		case "working":
			return visualStatus("working", "bg-status-working", { zone: "working", displayStatus: session.status });
		case "needs_input":
		case "review_pending":
		case "changes_requested":
		case "draft":
		case "pr_open":
		case "approved":
		case "mergeable":
			return visualStatus("attention", "bg-status-needs-you", {
				zone: stockAttentionZone(session.status),
				displayStatus: session.status,
				laneSection: stockAttentionZone(session.status) === "pending" ? "in_review" : undefined,
			});
		case "merged":
			return visualStatus("merged", "bg-status-merged", { zone: "merge", displayStatus: session.status });
		case "ci_failed":
		case "review_failed":
		case "exited":
		case "terminated":
			return visualStatus("failed", "bg-status-exited", {
				zone: stockAttentionZone(session.status),
				displayStatus: session.status,
			});
		default:
			return visualStatus("idle", "bg-status-idle", {
				zone: stockAttentionZone(session.status),
				displayStatus: session.status,
			});
	}
}

function fallbackPolicyWorkflowActive(session: WorkspaceSession): boolean {
	if (!session.dcpPolicyState || session.dcpPolicyState === "merged" || session.dcpPolicyState === "failed") return false;
	if (session.dcpPolicyState !== "incident") return true;
	return isAutomaticArbiterContinuation(session.dcpArbiterStatus);
}

export type SessionStatusView = {
	label: string;
	className: string;
	cardClassName?: string;
};

const sessionStatusLabelKeys: Record<SessionStatus, MessageKey> = {
	working: "status.working",
	idle: "status.idle",
	needs_input: "status.needs_input",
	exited: "status.exited",
	no_signal: "status.no_signal",
	ci_failed: "status.ci_failed",
	changes_requested: "status.changes_requested",
	review_pending: "status.review_pending",
	review_failed: "status.review_failed",
	draft: "status.draft",
	pr_open: "status.pr_open",
	approved: "status.approved",
	mergeable: "status.mergeable",
	merged: "status.merged",
	terminated: "status.terminated",
	unknown: "status.unknown",
};

const sessionStatusStyles: Record<SessionStatus, Omit<SessionStatusView, "label">> = {
	working: { className: "text-status-working" },
	idle: { className: "text-status-idle" },
	needs_input: { className: "text-status-needs-you" },
	exited: { className: "text-status-exited" },
	no_signal: { className: "text-status-unknown" },
	ci_failed: { className: "text-status-exited" },
	changes_requested: { className: "text-status-needs-you" },
	review_pending: { className: "text-status-in-review" },
	review_failed: { className: "text-status-needs-you" },
	draft: { className: "text-status-in-review" },
	pr_open: { className: "text-status-in-review" },
	approved: { className: "text-status-ready" },
	mergeable: { className: "text-status-ready" },
	merged: { className: "text-status-merged" },
	terminated: {
		className: "text-status-terminated-foreground",
		cardClassName: "session-card-terminated",
	},
	unknown: { className: "text-status-unknown" },
};

export function getSessionStatusView(status: SessionStatus, t: TFunction = appI18n.t): SessionStatusView {
	const key = sessionStatusLabelKeys[status] ?? sessionStatusLabelKeys.unknown;
	const style = sessionStatusStyles[status] ?? sessionStatusStyles.unknown;
	return { ...style, label: t(key) };
}

export function getSessionStatusViewForSession(
	session: WorkspaceSession,
	t: TFunction = appI18n.t,
): SessionStatusView {
	const projection = getSessionVisualStatus(session);
	const view = getSessionStatusView(projection.displayStatus, t);
	return {
		...view,
		label: projection.statusLabelKey ? t(projection.statusLabelKey) : view.label,
		className: projection.statusClassName ?? view.className,
	};
}

export function getSessionAccessibilityStatus(
	session: WorkspaceSession,
	t: TFunction = appI18n.t,
): string {
	const projection = getSessionVisualStatus(session);
	const label = getSessionStatusViewForSession(session, t).label;
	return projection.detail ? `${label}. ${projection.detail}` : label;
}

export type AttentionZone = "merge" | "action" | "pending" | "working" | "done";

export type AttentionZoneView = {
	zone: AttentionZone;
	label: string;
	glow: string;
	dot: string;
	dotGlow: boolean;
	titleClassName: string;
	dotClassName: string;
};

type AttentionZoneBase = Omit<AttentionZoneView, "label"> & { labelKey: MessageKey };

const attentionZoneBases: Record<AttentionZone, AttentionZoneBase> = {
	working: {
		zone: "working",
		labelKey: "zone.working",
		glow: "color-mix(in srgb, var(--color-status-working) 7%, transparent)",
		dot: "var(--color-status-working)",
		dotGlow: true,
		titleClassName: "text-status-working",
		dotClassName: "bg-status-working",
	},
	action: {
		zone: "action",
		labelKey: "zone.action",
		glow: "color-mix(in srgb, var(--color-status-needs-you) 6%, transparent)",
		dot: "var(--color-status-needs-you)",
		dotGlow: true,
		titleClassName: "text-status-needs-you",
		dotClassName: "bg-status-needs-you",
	},
	pending: {
		zone: "pending",
		labelKey: "zone.pending",
		glow: "color-mix(in srgb, var(--color-status-in-review) 5%, transparent)",
		dot: "var(--color-status-in-review)",
		dotGlow: false,
		titleClassName: "text-status-in-review",
		dotClassName: "bg-status-in-review",
	},
	merge: {
		zone: "merge",
		labelKey: "zone.merge",
		glow: "color-mix(in srgb, var(--color-status-ready) 7%, transparent)",
		dot: "var(--color-status-ready)",
		dotGlow: true,
		titleClassName: "text-status-ready",
		dotClassName: "bg-status-ready",
	},
	done: {
		zone: "done",
		labelKey: "zone.done",
		glow: "var(--color-overlay-faint)",
		dot: "var(--color-status-terminated)",
		dotGlow: false,
		titleClassName: "text-status-terminated-foreground",
		dotClassName: "bg-status-terminated",
	},
};

export const attentionZoneOrder: AttentionZone[] = ["merge", "action", "pending", "working", "done"];
export const boardAttentionZoneOrder: AttentionZone[] = ["working", "action", "pending", "merge"];

/** Live labels for the current locale (getters re-resolve on each access). */
export const attentionZoneLabel: Record<AttentionZone, string> = {
	get merge() {
		return getAttentionZoneViewForZone("merge").label;
	},
	get action() {
		return getAttentionZoneViewForZone("action").label;
	},
	get pending() {
		return getAttentionZoneViewForZone("pending").label;
	},
	get working() {
		return getAttentionZoneViewForZone("working").label;
	},
	get done() {
		return getAttentionZoneViewForZone("done").label;
	},
};

function stockAttentionZone(status: SessionStatus): AttentionZone {
	switch (status) {
		case "merged":
		case "approved":
		case "mergeable":
			return "merge";
		case "terminated":
			return "done";
		case "needs_input":
		case "exited":
		case "no_signal":
		case "ci_failed":
		case "changes_requested":
		case "review_failed":
		case "unknown":
			return "action";
		case "review_pending":
		case "pr_open":
		case "draft":
			return "pending";
		case "working":
		case "idle":
		default:
			return "working";
	}
}

export function attentionZone(input: SessionStatus | WorkspaceSession): AttentionZone {
	if (typeof input === "string") return stockAttentionZone(input);
	return input.dcpPolicyState ? getSessionVisualStatus(input).zone : stockAttentionZone(input.status);
}

export function getAttentionZoneView(status: SessionStatus, t: TFunction = appI18n.t): AttentionZoneView {
	return getAttentionZoneViewForZone(attentionZone(status), t);
}

export function getAttentionZoneViewForZone(zone: AttentionZone, t: TFunction = appI18n.t): AttentionZoneView {
	const base = attentionZoneBases[zone];
	const { labelKey, ...rest } = base;
	return { ...rest, label: t(labelKey) };
}

export type SessionTimelinePillStatus = Extract<SessionStatus, "no_signal" | "ci_failed" | "changes_requested">;

export type SessionTimelinePillView = {
	label: string;
	tone: string;
	breathe: boolean;
};

const sessionTimelinePillBases: Record<
	SessionTimelinePillStatus,
	{ labelKey: MessageKey; tone: string; breathe: boolean }
> = {
	no_signal: { labelKey: "timeline.no_signal", tone: "var(--color-status-unknown)", breathe: false },
	ci_failed: { labelKey: "timeline.ci_failed", tone: "var(--color-status-exited)", breathe: false },
	changes_requested: {
		labelKey: "timeline.changes_requested",
		tone: "var(--color-status-needs-you)",
		breathe: false,
	},
};

export function getSessionTimelinePillView(
	status: SessionTimelinePillStatus,
	t: TFunction = appI18n.t,
): SessionTimelinePillView {
	const base = sessionTimelinePillBases[status];
	return { label: t(base.labelKey), tone: base.tone, breathe: base.breathe };
}

export function isSessionIdle(session: WorkspaceSession): boolean {
	return getSessionVisualStatus(session).policyPhase === undefined && session.status === "idle";
}
