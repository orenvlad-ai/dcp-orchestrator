import { describe, expect, it } from "vitest";
import { requiredAppOwnerError } from "./daemon-owner";

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
