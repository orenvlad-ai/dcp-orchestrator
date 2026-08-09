import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceSession } from "../types/workspace";
import { RestoreUnavailableDialog } from "./RestoreUnavailableDialog";

const session: WorkspaceSession = {
	id: "orch-old",
	workspaceId: "proj-1",
	workspaceName: "Project One",
	title: "orchestrator",
	provider: "codex",
	kind: "orchestrator",
	status: "terminated",
	updatedAt: "2026-07-26T00:00:00Z",
	prs: [],
};

describe("RestoreUnavailableDialog", () => {
	it("never exposes manual orchestrator recreation or configuration", () => {
		render(
			<RestoreUnavailableDialog
				open
				session={session}
				onOpenChange={vi.fn()}
				onRecreated={vi.fn()}
			/>,
		);

		expect(screen.queryByRole("button", { name: "Create new orchestrator" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Configure orchestrator agent" })).not.toBeInTheDocument();
		expect(screen.queryByText(/You can create a new orchestrator/i)).not.toBeInTheDocument();
		expect(screen.getByText(/no saved agent session or prompt/i)).toBeInTheDocument();
	});
});
