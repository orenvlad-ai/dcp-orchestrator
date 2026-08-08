import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";
import { RestoreUnavailableDialog } from "./RestoreUnavailableDialog";

const { navigateMock, spawnMock, workspaceQueryMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	spawnMock: vi.fn(),
	workspaceQueryMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => navigateMock,
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => workspaceQueryMock(),
}));

vi.mock("../lib/spawn-orchestrator", () => ({
	spawnOrchestrator: spawnMock,
}));

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

const workspace: WorkspaceSummary = {
	id: "proj-1",
	name: "Project One",
	path: "/repo/project-one",
	orchestratorAgent: "codex",
	sessions: [session],
};

beforeEach(() => {
	vi.clearAllMocks();
	workspaceQueryMock.mockReturnValue({ data: [workspace], isLoading: false });
});
afterEach(() => vi.unstubAllEnvs());

describe("RestoreUnavailableDialog", () => {
	it("hides manual orchestrator recreation and its hint in the DCP contour", () => {
		vi.stubEnv("VITE_DCP_HIDE_MANUAL_ORCHESTRATOR_SPAWN", "1");
		render(
			<RestoreUnavailableDialog
				open
				session={session}
				onOpenChange={vi.fn()}
				onRecreated={vi.fn()}
			/>,
		);

		expect(screen.queryByRole("button", { name: "Create new orchestrator" })).not.toBeInTheDocument();
		expect(screen.queryByText(/You can create a new orchestrator/i)).not.toBeInTheDocument();
		expect(screen.getByText(/no saved agent session or prompt/i)).toBeInTheDocument();
		expect(spawnMock).not.toHaveBeenCalled();
	});

	it("opens project settings instead of recreating when no orchestrator agent is configured", async () => {
		const onOpenChange = vi.fn();
		const onRecreated = vi.fn();
		workspaceQueryMock.mockReturnValue({
			data: [{ ...workspace, orchestratorAgent: undefined }],
			isLoading: false,
		});
		render(
			<RestoreUnavailableDialog
				open
				session={session}
				onOpenChange={onOpenChange}
				onRecreated={onRecreated}
			/>,
		);

		await userEvent.click(screen.getByRole("button", { name: "Configure orchestrator agent" }));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/settings",
			params: { projectId: "proj-1" },
		});
		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(spawnMock).not.toHaveBeenCalled();
		expect(onRecreated).not.toHaveBeenCalled();
	});

	it("preserves clean recreation when an orchestrator agent is configured", async () => {
		const onOpenChange = vi.fn();
		const onRecreated = vi.fn();
		spawnMock.mockResolvedValue("orch-new");
		render(
			<RestoreUnavailableDialog
				open
				session={session}
				onOpenChange={onOpenChange}
				onRecreated={onRecreated}
			/>,
		);

		await userEvent.click(screen.getByRole("button", { name: "Create new orchestrator" }));

		await waitFor(() => expect(onRecreated).toHaveBeenCalledWith("orch-new"));
		expect(spawnMock).toHaveBeenCalledWith("proj-1", "restore_dialog", true);
		expect(onOpenChange).toHaveBeenCalledWith(false);
	});
});
