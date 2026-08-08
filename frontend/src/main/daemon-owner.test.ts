// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
	failClosedReplacementError,
	keepDaemonAlive,
	requiredAppOwnerError,
	shouldLinkOnAttach,
} from "./daemon-owner";

describe("shouldLinkOnAttach", () => {
	it('returns true when owner is "app"', () => {
		expect(shouldLinkOnAttach("app")).toBe(true);
	});

	it("returns false when owner is undefined (headless ao start)", () => {
		expect(shouldLinkOnAttach(undefined)).toBe(false);
	});

	it('returns false when owner is "" (empty string)', () => {
		expect(shouldLinkOnAttach("")).toBe(false);
	});

	it('returns false when owner is "cli"', () => {
		expect(shouldLinkOnAttach("cli")).toBe(false);
	});

	it("does NOT re-link a persistent daemon on attach, even when AO_KEEP_DAEMON is unset now", () => {
		expect(shouldLinkOnAttach("persistent")).toBe(false);
	});
});

describe("keepDaemonAlive", () => {
	it("returns false when AO_KEEP_DAEMON is unset", () => {
		expect(keepDaemonAlive({})).toBe(false);
	});

	it("returns false when AO_KEEP_DAEMON is empty", () => {
		expect(keepDaemonAlive({ AO_KEEP_DAEMON: "" })).toBe(false);
	});

	it.each(["1", "true", "TRUE", "yes", "on", "ON", "Yes"])("returns true for truthy value %j", (value) => {
		expect(keepDaemonAlive({ AO_KEEP_DAEMON: value })).toBe(true);
	});

	it.each(["0", "false", "FALSE", "off", "OFF", "no", "No"])("returns false for conventional off value %j", (value) => {
		expect(keepDaemonAlive({ AO_KEEP_DAEMON: value })).toBe(false);
	});

	it.each(["2", "random", "yep", "disable"])(
		"returns false for unrecognized value %j (allowlist, not truthiness)",
		(value) => {
			expect(keepDaemonAlive({ AO_KEEP_DAEMON: value })).toBe(false);
		},
	);

	it("trims surrounding whitespace before evaluating", () => {
		expect(keepDaemonAlive({ AO_KEEP_DAEMON: "  0  " })).toBe(false);
		expect(keepDaemonAlive({ AO_KEEP_DAEMON: "  1  " })).toBe(true);
	});
});

describe("requiredAppOwnerError", () => {
	it("accepts only an app-owned daemon when the DCP contour requires it", () => {
		expect(requiredAppOwnerError("app", true)).toBeNull();
		expect(requiredAppOwnerError(undefined, true)).toContain("without the canonical source UI owner identity");
		expect(requiredAppOwnerError("persistent", true)).toContain("without the canonical source UI owner identity");
	});

	it("preserves upstream attach behavior outside the DCP contour", () => {
		expect(requiredAppOwnerError(undefined, false)).toBeNull();
		expect(requiredAppOwnerError("persistent", false)).toBeNull();
	});
});

describe("failClosedReplacementError", () => {
	it("blocks the kill-and-replace path only inside the DCP contour", () => {
		expect(failClosedReplacementError(true, true)).toContain("no process was killed or replaced");
		expect(failClosedReplacementError(false, true)).toBeNull();
		expect(failClosedReplacementError(true, false)).toBeNull();
	});
});
