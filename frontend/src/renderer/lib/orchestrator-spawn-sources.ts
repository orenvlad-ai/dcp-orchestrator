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

/** DCP hides every manual Spawn/Open affordance; programmatic backend APIs stay intact. */
export function manualOrchestratorSpawnHidden(): boolean {
	return true;
}

/** Existing orchestrators do not re-enable a normal manual operating path. */
export function showOrchestratorControl(_hasOrchestrator: boolean): boolean {
	return false;
}
