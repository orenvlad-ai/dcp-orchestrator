/**
 * Whether the user opted the app's own daemon out of the app-lifetime link via
 * the AO_KEEP_DAEMON env var. When set to an explicit truthy value ("1",
 * "true", "yes", "on"), the app spawns the daemon but does NOT hold the
 * supervisor link, so the daemon survives the window closing and stops only on
 * an explicit `ao stop`. Any other value — including conventional falsy ones
 * like "0"/"false"/"off"/"no" and unrecognized strings — is treated as unset, so
 * the default desktop behavior holds: the daemon self-stops shortly after the
 * app quits. An allowlist (rather than "anything but 0/false") keeps "off"/"no"
 * from unexpectedly retaining the daemon.
 */
export function keepDaemonAlive(env: { AO_KEEP_DAEMON?: string }): boolean {
	const raw = env.AO_KEEP_DAEMON?.trim().toLowerCase();
	return raw === "1" || raw === "true" || raw === "yes" || raw === "on";
}

/**
 * Whether the app should hold a supervisor link to a daemon it ATTACHED to
 * (did not spawn). The decision is read from the daemon's durable owner record
 * in running.json — NOT the current Electron process env, which can differ
 * across launches (a cross-launch regression: a daemon spawned keep-alive must
 * stay unlinked even when the app is later reopened without AO_KEEP_DAEMON).
 * Only a normal app-owned daemon ("app") is linked; a keep-alive daemon
 * ("persistent") and headless `ao start` daemons (owner unset/empty) stay
 * persistent across app quit.
 */
export function shouldLinkOnAttach(owner: string | undefined): boolean {
	return owner === "app";
}

/** DCP's canonical source contour never attaches its UI to a headless daemon. */
export function requiredAppOwnerError(owner: string | undefined, required: boolean): string | null {
	if (!required || owner === "app") return null;
	return "A daemon is running without the canonical source UI owner identity. Use the DCP submit entrypoint; no process was replaced.";
}

/** DCP never lets the desktop's upstream wedged-daemon path kill and replace a process. */
export function failClosedReplacementError(replacementNeeded: boolean, required: boolean): string | null {
	if (!replacementNeeded || !required) return null;
	return "The daemon contour is stale, wedged, or ambiguous. Use the DCP submit entrypoint; no process was killed or replaced.";
}
