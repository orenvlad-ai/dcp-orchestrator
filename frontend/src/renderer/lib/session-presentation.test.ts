import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
	attentionZone,
	getAgentActivityView,
	getAttentionZoneView,
	getSessionAccessibilityStatus,
	getSessionStatusView,
	getSessionStatusViewForSession,
	getSessionVisualStatus,
	getSessionTimelinePillView,
	isAgentActivityWorking,
	isSessionIdle,
} from "./session-presentation";
import type { WorkspaceSession } from "../types/workspace";

function sessionWith(overrides: Partial<WorkspaceSession>): WorkspaceSession {
	return {
		id: "sess-1",
		workspaceId: "ws-1",
		workspaceName: "my-app",
		title: "fix-bug",
		provider: "claude-code",
		branch: "feat/x",
		status: "working",
		updatedAt: "2026-01-01T00:00:00Z",
		prs: [],
		...overrides,
	};
}

const openPr: WorkspaceSession["prs"][number] = {
	number: 7,
	url: "https://github.com/acme/app/pull/7",
	state: "open",
	ci: "unknown",
	review: "none",
	mergeability: "unknown",
	reviewComments: false,
	updatedAt: "2026-01-01T00:00:00Z",
};

describe("session presentation", () => {
	it.each([
		["active", "Working", true, "bg-status-working animate-status-pulse"],
		["idle", "Idle", false, "bg-status-idle"],
		["waiting_input", "Input Needed", false, "bg-status-needs-you"],
		["blocked", "Awaiting Decision", false, "bg-status-needs-you"],
		["exited", "Exited", false, "bg-status-exited"],
		["unknown", "Unknown", false, "bg-status-unknown"],
	] as const)("maps %s agent activity to %s", (state, label, breathe, indicatorClassName) => {
		expect(getAgentActivityView({ state, lastActivityAt: "" })).toMatchObject({
			label,
			breathe,
			indicatorClassName,
		});
	});

	it("uses raw agent activity, not session status, for working indicators", () => {
		expect(isAgentActivityWorking({ state: "active", lastActivityAt: "" })).toBe(true);
		expect(isAgentActivityWorking({ state: "idle", lastActivityAt: "" })).toBe(false);
		expect(isAgentActivityWorking(undefined)).toBe(false);
	});

	it.each([
		["working", "Working"],
		["idle", "Idle"],
		["needs_input", "Input needed"],
		["no_signal", "No signal"],
		["ci_failed", "CI failed"],
		["changes_requested", "Changes requested"],
		["review_pending", "Review pending"],
		["draft", "Draft PR"],
		["pr_open", "PR open"],
		["approved", "Approved"],
		["mergeable", "Ready"],
		["merged", "Merged"],
		["exited", "Exited"],
		["terminated", "Terminated"],
		["unknown", "Unknown status"],
	] as const)("maps %s session status to %s", (status, label) => {
		expect(getSessionStatusView(status).label).toBe(label);
	});

	it("uses distinct session-card tones for idle, no signal, and PR waiting states", () => {
		expect(getSessionStatusView("idle").className).toBe("text-status-idle");
		expect(getSessionStatusView("no_signal").className).toBe("text-status-unknown");
		expect(getSessionStatusView("draft").className).toBe("text-status-in-review");
		expect(getSessionStatusView("pr_open").className).toBe("text-status-in-review");
		expect(getSessionStatusView("review_pending").className).toBe("text-status-in-review");
		expect(getSessionStatusView("exited").className).toBe("text-status-exited");
	});

	it("presents a successful one-shot exit as ordinary Idle and a failure as red Exited", () => {
		expect(getSessionStatusView("idle")).toMatchObject({
			label: "Idle",
			className: "text-status-idle",
		});
		expect(getSessionStatusView("exited")).toMatchObject({
			label: "Exited",
			className: "text-status-exited",
		});
		expect(attentionZone(sessionWith({ status: "idle" }))).toBe("working");
		expect(attentionZone(sessionWith({ status: "exited" }))).toBe("action");
	});

	it.each([
		["approved", "merge", "Ready to merge"],
		["mergeable", "merge", "Ready to merge"],
		["needs_input", "action", "Needs you"],
		["exited", "action", "Needs you"],
		["no_signal", "action", "Needs you"],
		["ci_failed", "action", "Needs you"],
		["changes_requested", "action", "Needs you"],
		["unknown", "action", "Needs you"],
		["review_pending", "pending", "In review"],
		["pr_open", "pending", "In review"],
		["draft", "pending", "In review"],
		["working", "working", "Working"],
		["idle", "working", "Working"],
		["merged", "merge", "Ready to merge"],
		["terminated", "done", "Terminated"],
	] as const)("maps %s to the %s attention zone", (status, zone, label) => {
		expect(attentionZone(sessionWith({ status }))).toBe(zone);
		expect(getAttentionZoneView(status)).toMatchObject({ zone, label });
	});

	it("keeps activity indicator color independent from PR and CI presentation", () => {
		const active = getAgentActivityView({ state: "active", lastActivityAt: "" });
		const idle = getAgentActivityView({ state: "idle", lastActivityAt: "" });

		expect(active.indicatorClassName).toBe("bg-status-working animate-status-pulse");
		expect(idle.indicatorClassName).toBe("bg-status-idle");
	});

	it("does not change stock board placement from raw activity alone", () => {
		expect(
			attentionZone(
				sessionWith({
					status: "merged",
					activity: { state: "active", lastActivityAt: "" },
				}),
			),
		).toBe("merge");
	});

	it.each([
		["worker active", "worker_running", true, "working", "working", "working", "bg-status-working", true],
		["worker queued", "worker_queued", false, "working", "working", "working", "bg-status-working", false],
		["review active", "review_running", true, "in_review", "pending", "review", "bg-status-in-review", true],
		["review queued", "review_queued", false, "in_review", "pending", "review", "bg-status-in-review", false],
		["review inactive", "review_running", false, "in_review", "pending", "review", "bg-status-in-review", false],
		["CI waiting", "ci_waiting", false, "working", "working", "working", "bg-status-working", false],
		["admission waiting", "admission_waiting", false, "ready_to_merge", "merge", "ready", "bg-status-ready", false],
		["merged", "merged", false, "merged", "merge", "merged", "bg-status-merged", false],
		["failed", "failed", false, "needs_you", "action", "failed", "bg-status-exited", false],
		["incident", "incident", false, "needs_you", "action", "failed", "bg-status-exited", false],
		["incident arbiter active", "incident", true, "needs_you", "action", "failed", "bg-status-exited", false],
		["reserved", "reserved", false, "working", "working", "working", "bg-status-working", false],
	] as const)(
		"maps policy %s to one shared projection",
		(_name, state, actionActive, policyPhase, zone, tone, dotClassName, active) => {
			const visual = getSessionVisualStatus(
				sessionWith({
					dcpPolicyState: state,
					dcpPolicyActionActive: actionActive,
					activity: { state: "idle", lastActivityAt: "" },
				}),
			);
			expect(visual).toMatchObject({ policyPhase, zone, tone, dotClassName, active });
			expect(visual.indicatorClassName).toBe(`${dotClassName}${active ? " animate-status-pulse" : ""}`);
		},
	);

	it("projects an exact terminal Human Gate above stale Review failed state", () => {
		const session = sessionWith({
			status: "review_failed",
			dcpPolicyState: "incident",
			dcpPolicyActionActive: true,
			dcpArbiterStatus: "human_gate",
			dcpArbiterGeneration: 1,
			dcpArbiterIncidentKind: "merge_conflict_or_ambiguity",
			dcpArbiterCohort: ["arb-c-left", "arb-c-right"],
			dcpArbiterActionStatus: "failed",
			dcpHumanGateQuestion: "Should the shared value remain left or be replaced with right?",
		});
		expect(getSessionVisualStatus(session)).toMatchObject({
			policyPhase: "needs_you",
			zone: "action",
			displayStatus: "review_failed",
			statusLabelKey: "status.human_gate",
			statusClassName: "text-status-needs-you",
			tone: "attention",
			dotClassName: "bg-status-needs-you",
			indicatorClassName: "bg-status-needs-you",
			active: false,
		});
		expect(getSessionStatusViewForSession(session)).toMatchObject({
			label: "Needs your decision",
			className: "text-status-needs-you",
		});

		const failure = { ...session, dcpArbiterStatus: "failed" as const };
		expect(getSessionVisualStatus(failure)).toMatchObject({
			tone: "failed",
			dotClassName: "bg-status-exited",
			indicatorClassName: "bg-status-exited",
			active: false,
		});
		expect(getSessionStatusViewForSession(failure).label).toBe("Review failed");
	});

	it.each([
		["waiting", "requested", "queued", false, "Waiting for arbiter", false],
		["claimed", "claimed", "claimed", true, "Arbiter evaluating", false],
		["running", "running", "running", true, "Arbiter evaluating", true],
		["running without active fence", "running", "running", false, "Arbiter evaluating", false],
		["passive hold", "hold", "succeeded", false, "Arbiter decision pending", false],
		["accepted decision", "succeeded", "failed", false, "Arbiter decision pending", false],
	] as const)(
		"projects an automatic arbiter %s into the shared review lane",
		(_name, arbiterStatus, actionStatus, policyActive, label, active) => {
			const session = sessionWith({
				status: "review_failed",
				dcpPolicyState: "incident",
				dcpPolicyActionActive: policyActive,
				dcpArbiterStatus: arbiterStatus,
				dcpArbiterActionStatus: actionStatus,
				dcpArbiterGeneration: 2,
				dcpArbiterIncidentKind: "merge_conflict_or_ambiguity",
			});
			const visual = getSessionVisualStatus(session);

			expect(visual).toMatchObject({
				policyPhase: "arbiter",
				laneSection: "arbiter",
				zone: "pending",
				tone: "arbiter",
				dotClassName: "bg-status-arbiter",
				active,
			});
			expect(visual.indicatorClassName).toBe(`bg-status-arbiter${active ? " animate-status-pulse" : ""}`);
			expect(getSessionStatusViewForSession(session).label).toBe(label);
			expect(getSessionAccessibilityStatus(session)).toContain(label);
		},
	);

	it("gives successor repair lifecycle precedence over the older arbiter decision", () => {
		const repair = sessionWith({
			status: "review_failed",
			dcpPolicyState: "repair_running",
			dcpPolicyActionActive: true,
			dcpArbiterStatus: "hold",
			dcpArbiterActionStatus: "succeeded",
		});

		expect(getSessionVisualStatus(repair)).toMatchObject({
			policyPhase: "working",
			zone: "working",
			tone: "working",
			dotClassName: "bg-status-working",
			active: true,
		});
	});

	it("returns the same arbiter-approved task through repair, review, ready, and merged", () => {
		const arbiterDecision = {
			id: "same-task",
			dcpArbiterStatus: "repair_queued" as const,
			dcpArbiterActionStatus: "succeeded" as const,
		};
		const frames = [
			sessionWith({ ...arbiterDecision, dcpPolicyState: "repair_queued" }),
			sessionWith({ ...arbiterDecision, dcpPolicyState: "repair_running", dcpPolicyActionActive: true }),
			sessionWith({ ...arbiterDecision, dcpPolicyState: "review_queued" }),
			sessionWith({ ...arbiterDecision, dcpPolicyState: "admission_waiting" }),
			sessionWith({ ...arbiterDecision, dcpPolicyState: "merged" }),
		];

		expect(frames.map((frame) => frame.id)).toEqual(Array(5).fill("same-task"));
		expect(frames.map((frame) => getSessionVisualStatus(frame).policyPhase)).toEqual([
			"working",
			"working",
			"in_review",
			"ready_to_merge",
			"merged",
		]);
	});

	it("reprojects one durable task across restart snapshots without bouncing through failure", () => {
		const base = {
			id: "same-task",
			status: "review_failed" as const,
			dcpPolicyState: "incident" as const,
			dcpArbiterGeneration: 1,
			dcpArbiterIncidentKind: "merge_conflict_or_ambiguity",
		};
		const snapshots = [
			sessionWith({ ...base, dcpArbiterStatus: "requested", dcpArbiterActionStatus: "queued" }),
			sessionWith({
				...base,
				dcpPolicyActionActive: true,
				dcpArbiterStatus: "running",
				dcpArbiterActionStatus: "running",
			}),
			sessionWith({ ...base, dcpArbiterStatus: "hold", dcpArbiterActionStatus: "succeeded" }),
			sessionWith({
				...base,
				dcpArbiterStatus: "human_gate",
				dcpArbiterActionStatus: "succeeded",
				dcpHumanGateQuestion: "Choose left or right?",
			}),
		];
		const projections = snapshots.map(getSessionVisualStatus);

		expect(snapshots.map((snapshot) => snapshot.id)).toEqual(["same-task", "same-task", "same-task", "same-task"]);
		expect(projections.map((projection) => projection.policyPhase)).toEqual([
			"arbiter",
			"arbiter",
			"arbiter",
			"needs_you",
		]);
		expect(projections.map((projection) => projection.zone)).toEqual(["pending", "pending", "pending", "action"]);
		expect(projections.map((projection) => projection.active)).toEqual([false, true, false, false]);
		expect(projections.slice(0, 3).every((projection) => projection.tone === "arbiter")).toBe(true);
		expect(projections[3]).toMatchObject({ tone: "attention", dotClassName: "bg-status-needs-you" });
	});

	it("keeps the normal policy sequence forward when stock status frames are stale", () => {
		const frames = [
			sessionWith({ status: "idle", dcpPolicyState: "reserved" }),
			sessionWith({ status: "idle", dcpPolicyState: "worker_queued" }),
			sessionWith({ status: "working", dcpPolicyState: "worker_running", dcpPolicyActionActive: true }),
			sessionWith({ status: "pr_open", dcpPolicyState: "ci_waiting" }),
			sessionWith({ status: "pr_open", dcpPolicyState: "review_queued" }),
			sessionWith({ status: "review_pending", dcpPolicyState: "review_running", dcpPolicyActionActive: true }),
			sessionWith({ status: "review_pending", dcpPolicyState: "admission_waiting" }),
			sessionWith({ status: "pr_open", dcpPolicyState: "merged" }),
		];
		const phaseRank = { working: 0, in_review: 1, arbiter: 2, ready_to_merge: 3, merged: 4, needs_you: 5 } as const;
		const projections = frames.map(getSessionVisualStatus);
		const ranks = projections.map((projection) => phaseRank[projection.policyPhase!]);

		expect(ranks).toEqual([...ranks].sort((left, right) => left - right));
		expect(projections.map((projection) => projection.zone)).toEqual([
			"working",
			"working",
			"working",
			"working",
			"pending",
			"pending",
			"merge",
			"merge",
		]);
		expect(getSessionStatusViewForSession(frames[3])).toMatchObject({
			label: "PR open",
			className: "text-status-working",
		});
		expect(getSessionStatusViewForSession(frames[7])).toMatchObject({
			label: "Merged",
			className: "text-status-merged",
		});
	});

	it("keeps non-policy human and merge readiness steady orange", () => {
		for (const status of ["needs_input", "review_pending", "approved", "mergeable"] as const) {
			expect(getSessionVisualStatus(sessionWith({ status, activity: { state: "idle", lastActivityAt: "" } }))).toMatchObject({
				tone: "attention",
				dotClassName: "bg-status-needs-you",
				active: false,
			});
		}
	});

	it("disables status pulse under reduced motion without changing the steady color class", () => {
		const css = readFileSync("src/renderer/styles.css", "utf8");
		expect(css).toMatch(
			/@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?\.animate-status-pulse,[\s\S]*?animation:\s*none;/,
		);
	});

	it("defines a dedicated high-contrast purple arbiter signal for both themes", () => {
		const tokens = readFileSync("src/styles/tokens.css", "utf8");
		const renderer = readFileSync("src/renderer/styles.css", "utf8");
		expect(tokens.match(/--color-status-arbiter:\s*#c084fc;/g)).toHaveLength(1);
		expect(tokens.match(/--color-status-arbiter:\s*#7e22ce;/g)).toHaveLength(1);
		expect(tokens).toContain("--bridge-status-arbiter: var(--color-status-arbiter);");
		expect(renderer).toContain("--color-status-arbiter: var(--bridge-status-arbiter);");
		expect(new Set(["#c084fc", "#60a5fa", "#fb923c", "#facc15", "#4ade80"]).size).toBe(5);
	});

	it("uses a muted accent treatment for In Review instead of idle gray", () => {
		expect(getAttentionZoneView("review_pending")).toMatchObject({
			dot: "var(--color-status-in-review)",
			titleClassName: "text-status-in-review",
			dotClassName: "bg-status-in-review",
		});
	});

	it("classifies only backend-derived idle sessions for the work lane", () => {
		expect(isSessionIdle(sessionWith({ status: "idle" }))).toBe(true);
		expect(
			isSessionIdle(
				sessionWith({
					status: "idle",
					activity: { state: "active", lastActivityAt: "" },
					prs: [openPr],
				}),
			),
		).toBe(true);
		expect(
			isSessionIdle(
				sessionWith({
					status: "working",
					activity: { state: "idle", lastActivityAt: "" },
					prs: [openPr],
				}),
			),
		).toBe(false);
		expect(
			isSessionIdle(
				sessionWith({
					status: "working",
					activity: { state: "active", lastActivityAt: "" },
				}),
			),
		).toBe(false);
		expect(isSessionIdle(sessionWith({ status: "working" }))).toBe(false);
	});

	it.each([
		["no_signal", "No Signal", "var(--color-status-unknown)"],
		["ci_failed", "CI Failed", "var(--color-status-exited)"],
		["changes_requested", "Changes Requested", "var(--color-status-needs-you)"],
	] as const)("centralizes the %s timeline pill", (status, label, tone) => {
		expect(getSessionTimelinePillView(status)).toMatchObject({ label, tone, breathe: false });
	});
});
