export const ORCHESTRATOR_SPAWN_SOURCES = [
	"board",
	"restore_dialog",
	"topbar",
	"sidebar",
	"project_add",
	"settings",
	"restart",
	"command_palette",
] as const;

export type OrchestratorSpawnSource = (typeof ORCHESTRATOR_SPAWN_SOURCES)[number];

/** DCP hides only manual spawn affordances; daemon APIs and automatic orchestration stay intact. */
export function manualOrchestratorSpawnHidden(): boolean {
	return import.meta.env.VITE_DCP_HIDE_MANUAL_ORCHESTRATOR_SPAWN === "1";
}

/** Existing orchestrators remain navigable even when creating one manually is hidden. */
export function showOrchestratorControl(hasOrchestrator: boolean): boolean {
	return hasOrchestrator || !manualOrchestratorSpawnHidden();
}
