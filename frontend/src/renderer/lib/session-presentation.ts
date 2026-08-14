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
	zone: AttentionZone;
	displayStatus: SessionStatus;
	statusClassName?: string;
	tone: "working" | "review" | "ready" | "attention" | "merged" | "failed" | "idle";
	dotClassName: string;
	indicatorClassName: string;
	active: boolean;
};

export type DCPPolicyPhase = "working" | "in_review" | "ready_to_merge" | "merged" | "needs_you";

const policyPhaseStatusClassNames: Record<DCPPolicyPhase, string> = {
	working: "text-status-working",
	in_review: "text-status-in-review",
	ready_to_merge: "text-status-ready",
	merged: "text-status-merged",
	needs_you: "text-status-exited",
};

function visualStatus(
	tone: SessionVisualStatus["tone"],
	dotClassName: string,
	options: Pick<SessionVisualStatus, "zone" | "displayStatus"> &
		Partial<Pick<SessionVisualStatus, "policyPhase" | "statusClassName" | "active">>,
): SessionVisualStatus {
	const active = options.active === true;
	return {
		...options,
		tone,
		dotClassName,
		indicatorClassName: `${dotClassName}${active ? " animate-status-pulse" : ""}`,
		active,
	};
}

function policyVisualStatus(
	state: DCPPolicyState,
	session: WorkspaceSession,
	policyActive: boolean,
): SessionVisualStatus {
	const policy = (
		policyPhase: DCPPolicyPhase,
		zone: AttentionZone,
		displayStatus: SessionStatus,
		tone: SessionVisualStatus["tone"],
		dotClassName: string,
		active = false,
	) =>
		visualStatus(tone, dotClassName, {
			policyPhase,
			zone,
			displayStatus,
			statusClassName: policyPhaseStatusClassNames[policyPhase],
			active,
		});

	switch (state) {
		case "reserved":
		case "worker_queued":
		case "repair_queued":
			return policy("working", "working", "working", "working", "bg-status-working");
		case "worker_running":
		case "repair_running":
			return policy("working", "working", "working", "working", "bg-status-working", policyActive);
		case "ci_waiting":
			return policy(
				"working",
				"working",
				session.status === "pr_open" || session.status === "draft" ? session.status : "working",
				"working",
				"bg-status-working",
			);
		case "review_queued":
			return policy("in_review", "pending", "review_pending", "review", "bg-status-in-review");
		case "review_running":
			return policy(
				"in_review",
				"pending",
				"review_pending",
				"review",
				"bg-status-in-review",
				policyActive,
			);
		case "admission_waiting":
			return policy("ready_to_merge", "merge", "mergeable", "ready", "bg-status-ready");
		case "merged":
			return policy("merged", "merge", "merged", "merged", "bg-status-merged");
		case "failed":
		case "incident":
			return policy("needs_you", "action", "review_failed", "failed", "bg-status-exited");
	}
}

// One typed projection serves board placement, board card status and sidebar
// dot. Durable policy lifecycle wins over retained shells and stale stock SCM;
// motion is reserved for a model action whose durable action row is running.
export function getSessionVisualStatus(session: WorkspaceSession): SessionVisualStatus {
	const policyActive = session.dcpPolicyActionActive === true;
	if (session.dcpPolicyState) {
		return policyVisualStatus(session.dcpPolicyState, session, policyActive);
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
	return projection.statusClassName ? { ...view, className: projection.statusClassName } : view;
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
