#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$repo_root"

upstream_commit='1df40e93772c2c48e916870d9c3ddf8f29a69f84'
upstream_tree='36bf30cc4960c10f0d94fc63a8ff0a4dd22bb8a8'
i8_parity_commit='23fe9bba77873075f32b813fb0a3c936598882fb'
i8_patch_sha256='047c9f74902ede19b6e3a3ba753fc7b2702a322a9be709fb0e975cc5628314d2'
i11_commit='417a844e7b85b6b14ae9a1855009d8bf139ee43d'
license_sha256='1a2219722b7ef58364065e9073a2cb2831891eb147a785742a31431c9cddad1d'
control_plane_commit='eb9ca41f23b2cfef51bda37f291cd44d6d29c173'
operating_contract_revision='2026-08-08.10'

fail() {
	printf 'DCP CI gate: %s\n' "$*" >&2
	exit 1
}

sha256_file() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		sha256sum "$1" | awk '{print $1}'
	fi
}

sha256_stream() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 | awk '{print $1}'
	else
		sha256sum | awk '{print $1}'
	fi
}

source_gates() {
	git cat-file -e "$upstream_commit^{commit}" 2>/dev/null || fail 'upstream commit is absent from Git history'
	git cat-file -e "$i8_parity_commit^{commit}" 2>/dev/null || fail 'I8 parity anchor is absent from Git history'
	[[ "$(git rev-parse "$upstream_commit^{tree}")" == "$upstream_tree" ]] || fail 'upstream tree mismatch'
	git merge-base --is-ancestor "$upstream_commit" "$i8_parity_commit" || fail 'I8 parity anchor does not descend from upstream'
	git merge-base --is-ancestor "$i8_parity_commit" HEAD || fail 'current source does not descend from the I8 parity anchor'
	git merge-base --is-ancestor "$i11_commit" HEAD || fail 'current source does not descend from the I11 baseline'
	[[ "$(git rev-list --count "$upstream_commit..$i8_parity_commit")" -eq 7 ]] || fail 'I8 queue is not the seven reviewed commits'
	actual_patch_sha256="$(git diff "$upstream_commit" "$i8_parity_commit" --binary --full-index --no-ext-diff | sha256_stream)"
	[[ "$actual_patch_sha256" == "$i8_patch_sha256" ]] || fail 'I8 parity diff digest mismatch'

	[[ "$(sha256_file LICENSE)" == "$license_sha256" ]] || fail 'Apache-2.0 LICENSE digest mismatch'
	if git ls-tree -r --name-only "$upstream_commit" | awk 'BEGIN{IGNORECASE=1} /(^|\/)NOTICE([^\/]*$)/ {found=1} END{exit found?0:1}'; then
		fail 'upstream NOTICE result changed'
	fi
	[[ -s NOTICE && -s DCP_PROVENANCE.md ]] || fail 'NOTICE or provenance is absent'
	grep -Fq "$upstream_commit" NOTICE || fail 'NOTICE lacks exact upstream commit'
	grep -Fq "$i8_parity_commit" DCP_PROVENANCE.md || fail 'provenance lacks I8 parity anchor'
	grep -Fq "$i8_patch_sha256" DCP_PROVENANCE.md || fail 'provenance lacks exact I8 patch digest'

	[[ -s AGENTS.md && -s CLAUDE.md ]] || fail 'DCP coding-agent operational entry is absent'
	grep -Fq "dev-control-plane/blob/$control_plane_commit/docs/CURRENT_OPERATING_CONTRACT.md" AGENTS.md || fail 'AGENTS.md lacks exact dev-control-plane operating contract'
	grep -Fq "dev-control-plane/blob/$control_plane_commit/docs/TARGET_ARCHITECTURE_V1.md" AGENTS.md || fail 'AGENTS.md lacks exact dev-control-plane target contract'
	grep -Fq "current operating contract revision \`$operating_contract_revision\`" AGENTS.md || fail 'AGENTS.md operating contract revision mismatch'
	grep -Fq 'DCP_AO_LAB_ROOT' AGENTS.md || fail 'AGENTS.md lacks explicit DCP lab root contract'
	grep -Fq 'pro.devcontrol.dcp-orchestrator' AGENTS.md || fail 'AGENTS.md lacks DCP application identity'
	grep -Fq 'Current implemented scope' AGENTS.md || fail 'AGENTS.md does not separate implemented and future scope'
	grep -Fq 'I12 activates only the existing stock Review/ReviewRun/Engine contour' AGENTS.md || fail 'AGENTS.md does not bound the I12 reviewer contour'
	grep -Fq 'adds no reviewer' AGENTS.md || fail 'AGENTS.md does not exclude a new reviewer service'
	grep -Fq 'Read and follow [`AGENTS.md`](AGENTS.md)' CLAUDE.md || fail 'compatibility agent entry does not defer to DCP AGENTS.md'
	if grep -Fq 'All app state lives under `~/.ao` only' AGENTS.md CLAUDE.md \
		|| grep -Fq 'canonical, auto-updating install path' AGENTS.md CLAUDE.md \
		|| grep -Fq 'Hard rule: exactly one publisher' AGENTS.md CLAUDE.md \
		|| grep -Fq '`0.0.0.0` **only while explicitly enabled**' AGENTS.md CLAUDE.md \
		|| grep -Fq 'go run ./cmd/ao start' AGENTS.md CLAUDE.md; then
		fail 'conflicting upstream operational rules returned'
	fi

	workflow_list="$(find .github/workflows -maxdepth 1 -type f -print | sort)"
	[[ "$workflow_list" == '.github/workflows/dcp-ci.yml' ]] || fail 'only the bounded DCP CI workflow may be active'
	! grep -Eiq 'workflow_dispatch|pull_request_target|repository_dispatch|^[[:space:]]*schedule:|^[[:space:]]*release:|gh[[:space:]]+release|upload-artifact|electron-forge[[:space:]]+publish' .github/workflows/dcp-ci.yml || fail 'release, schedule, privileged PR, or publishing path is active'
	! grep -Eq '^[[:space:]]+(actions|contents|deployments|id-token|issues|packages|pages|pull-requests|statuses):[[:space:]]*write' .github/workflows/dcp-ci.yml || fail 'workflow requests write permission'

	grep -Fq '"name": "dcp-orchestrator"' frontend/package.json || fail 'frontend package identity mismatch'
	grep -Fq '"productName": "DCP Orchestrator"' frontend/package.json || fail 'product name mismatch'
	grep -Fq 'https://github.com/orenvlad-ai/dcp-orchestrator' frontend/package.json || fail 'repository metadata mismatch'
	! grep -Eq '"(electron-updater|posthog-js|@electron-forge/publisher-github)"[[:space:]]*:' frontend/package.json || fail 'forbidden updater, analytics, or publisher dependency is declared'
	! grep -Eq '"publish"[[:space:]]*:' frontend/package.json || fail 'publish script is present'
	! grep -Eq '"node_modules/(electron-updater|posthog-js|@electron-forge/publisher-github)"[[:space:]]*:' frontend/package-lock.json || fail 'forbidden desktop dependency remains locked'

	grep -Fq 'appBundleId: "pro.devcontrol.dcp-orchestrator"' frontend/forge.config.ts || fail 'bundle id mismatch'
	grep -Fq 'executableName: "dcp-orchestrator"' frontend/forge.config.ts || fail 'executable identity mismatch'
	grep -Fq 'makers: []' frontend/forge.config.ts || fail 'maker is configured'
	grep -Fq 'publishers: []' frontend/forge.config.ts || fail 'publisher is configured'
	grep -Fq '"../LICENSE"' frontend/forge.config.ts || fail 'LICENSE is not packaged'
	grep -Fq '"../NOTICE"' frontend/forge.config.ts || fail 'NOTICE is not packaged'
	grep -Fq '"../DCP_PROVENANCE.md"' frontend/forge.config.ts || fail 'provenance is not packaged'
	[[ ! -e frontend/src/main/auto-updater.ts && ! -e frontend/src/main/auto-updater.test.ts ]] || fail 'auto-updater source remains active'

	grep -Fq 'app.commandLine.appendSwitch("disable-breakpad")' frontend/src/main.ts || fail 'breakpad disable switch is absent'
	grep -Fq 'app.commandLine.appendSwitch("disable-crash-reporter")' frontend/src/main.ts || fail 'crash reporter disable switch is absent'
	! grep -Eq 'crashReporter[[:space:]]*\.[[:space:]]*(start|submit)' frontend/src/main.ts || fail 'crash reporter initialization is active'
	grep -Fq 'export async function initTelemetry(): Promise<boolean>' frontend/src/renderer/lib/telemetry.ts || fail 'renderer telemetry seam is absent'
	grep -A2 -F 'export async function initTelemetry(): Promise<boolean>' frontend/src/renderer/lib/telemetry.ts | grep -Fq 'return false;' || fail 'renderer telemetry can initialize'
	grep -Fq 'return disabledEventSink{}' backend/internal/daemon/telemetry_wiring.go || fail 'daemon telemetry sink is not disabled'
	grep -Fq 'Telemetry environment variables are intentionally ignored by the DCP build.' backend/internal/config/config.go || fail 'telemetry environment isolation is absent'
	grep -Fq 'if sink == nil || !cfg.Telemetry.Events {' backend/internal/httpd/router.go || fail 'telemetry control routes are not fail-closed'

	grep -Fq 'const ServiceName = "dcp-orchestrator-daemon"' backend/internal/daemonmeta/meta.go || fail 'daemon service namespace mismatch'
	grep -Fq '"exec", "--ignore-user-config", "--ephemeral", "--strict-config"' backend/internal/adapters/agent/codex/codex.go || fail 'Codex worker isolation flags are absent'
	! grep -Fq 'appendHookTrustBypassFlag(&cmd)' backend/internal/adapters/agent/codex/codex.go || fail 'forbidden hook-trust bypass is reachable'

	i11_migration='backend/internal/storage/sqlite/migrations/0048_dcp_task_foundation.sql'
	[[ -s "$i11_migration" ]] || fail 'I11 additive migration is absent'
	grep -Fq 'CREATE TABLE dcp_tasks' "$i11_migration" || fail 'I11 durable task table is absent'
	grep -Fq 'CREATE TABLE dcp_task_events' "$i11_migration" || fail 'I11 durable event stream is absent'
	grep -Fq "CHECK (state = 'SUBMITTED')" "$i11_migration" || fail 'I11 physical state is not restricted to SUBMITTED'
	grep -Fq "CHECK (target_project_id = 'dcp-lab')" "$i11_migration" || fail 'I11 physical target is not restricted to dcp-lab'
	grep -Fq 'dcp_task_events_monotonic' "$i11_migration" || fail 'I11 monotonic event guard is absent'
	grep -Fq 'dcp_tasks_immutable_contract' "$i11_migration" || fail 'I11 immutable/CAS guard is absent'
	if sed '/-- +goose Down/,$d' "$i11_migration" | grep -Eiq '(^|[[:space:]])(ALTER|DROP)[[:space:]]'; then
		fail 'I11 up migration is not strictly additive'
	fi
	grep -Fq 'r.Post("/dcp/tasks", c.submit)' backend/internal/httpd/controllers/dcp_tasks.go || fail 'I11 internal submit route is absent'
	grep -Fq 'ValidateDCPTaskSchema' backend/internal/storage/sqlite/store/dcp_task_store.go || fail 'I11 startup schema validation is absent'
	grep -Fq 'DCPTasks:            dcpTaskSvc' backend/internal/daemon/daemon.go || fail 'I11 daemon/API wiring is absent'
	! grep -Fq 'refetchInterval' frontend/src/renderer/hooks/useDCPTasksQuery.ts || fail 'I11 renderer introduced a polling loop'
	grep -Fq 'retry: false' frontend/src/renderer/hooks/useDCPTasksQuery.ts || fail 'I11 renderer task reads may create a retry loop'
	grep -A2 -F 'export function manualOrchestratorSpawnHidden(): boolean' frontend/src/renderer/lib/orchestrator-spawn-sources.ts | grep -Fq 'return true;' || fail 'manual orchestrator affordances can be re-enabled'
	grep -A2 -F 'export function showOrchestratorControl' frontend/src/renderer/lib/orchestrator-spawn-sources.ts | grep -Fq 'return false;' || fail 'existing orchestrators can reactivate manual UI'
	grep -Fq 'export async function spawnOrchestrator' frontend/src/renderer/lib/spawn-orchestrator.ts || fail 'programmatic orchestrator helper was removed'
	grep -Fq 'r.Post("/orchestrators", c.spawnOrchestrator)' backend/internal/httpd/controllers/sessions.go || fail 'programmatic orchestrator API was removed'

	grep -Fq 'approval_policy="never"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer approval policy is not non-interactive'
	grep -Fq '"--sandbox", "read-only"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer sandbox is not read-only'
	grep -Fq 'case "--ask-for-approval":' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer does not strip unsupported approval argv'
	grep -Fq '"review", "supervise"' backend/internal/review/launcher.go || fail 'reviewer process is not supervised'
	grep -Fq 'reviews/process-exit' backend/internal/httpd/controllers/reviews.go || fail 'reviewer process exit is not persisted through the daemon'
	grep -Fq 'func (e *Engine) AutoTrigger' backend/internal/review/review.go || fail 'shared automatic review trigger is absent'
	grep -Fq 'func (e *Engine) ReconcileStartup' backend/internal/review/review.go || fail 'review restart reconciliation is absent'
	! git diff --name-only "$i11_commit"..HEAD -- backend/internal/storage/sqlite/migrations | grep -q . || fail 'I12 added a second storage schema instead of reusing ReviewRun'

	unexpected_paths="$(git diff --name-only "$i8_parity_commit"..HEAD | grep -Ev '^(\.github/workflows/.*|AGENTS\.md|CLAUDE\.md|DCP_PROVENANCE\.md|NOTICE|README\.md|scripts/dcp-ci-gates\.sh|frontend/forge\.config\.ts|frontend/package(-lock)?\.json|backend/internal/adapters/reviewer/codex/codex(_test)?\.go|backend/internal/adapters/runtime/tmux/tmux(_test)?\.go|backend/internal/cli/(review|review_supervise_unix_test|root|root_test)\.go|backend/internal/daemon/(daemon|lifecycle_wiring)\.go|backend/internal/domain/(dcp_task|review|session|status)\.go|backend/internal/httpd/api\.go|backend/internal/httpd/apispec/openapi\.yaml|backend/internal/httpd/apispec/specgen/build\.go|backend/internal/httpd/controllers/(dcp_tasks(_test)?|dto|reviews(_test)?)\.go|backend/internal/lifecycle/(manager|manager_test|reactions)\.go|backend/internal/observe/scm/observer(_test)?\.go|backend/internal/review/(launcher|launcher_test|planner|review|review_test)\.go|backend/internal/service/dcptask/(repository|repository_test|service|service_test)\.go|backend/internal/service/review/review(_test)?\.go|backend/internal/service/session/(service|status_test)\.go|backend/internal/storage/sqlite/gen/(dcp_tasks\.sql\.go|models\.go|sessions\.sql\.go)|backend/internal/storage/sqlite/migrate(_burned_versions)?_test\.go|backend/internal/storage/sqlite/migrations/0048_dcp_task_foundation\.sql|backend/internal/storage/sqlite/queries/(dcp_tasks|sessions)\.sql|backend/internal/storage/sqlite/store/dcp_task_store(_test)?\.go|backend/sqlc\.yaml|frontend/src/api/schema\.ts|frontend/src/renderer/__tests__/integration/board-empty-states\.test\.tsx|frontend/src/renderer/components/(CommandPalette|ProjectSettingsForm|RestoreUnavailableDialog|SessionInspector|SessionsBoard|ShellTopbar|Sidebar)(\.test)?\.tsx|frontend/src/renderer/hooks/useDCPTasksQuery\.ts|frontend/src/renderer/i18n/(de|en|es|fr|ja|ko|pt-BR|zh-CN)\.json|frontend/src/renderer/lib/(api-client|command-palette|orchestrator-spawn-sources|session-presentation|spawn-orchestrator)(\.test)?\.ts|frontend/src/renderer/types/workspace\.ts)$' || true)"
	[[ -z "$unexpected_paths" ]] || fail "post-parity runtime source changed outside the governance allowlist: $unexpected_paths"

	git diff --check
	printf 'PASS DCP source, provenance, identity, and absence gates\n'
}

artifact_gates() {
	local app="$1" plist executable daemon resources arch
	[[ -d "$app" ]] || fail "app bundle is absent: $app"
	plist="$app/Contents/Info.plist"
	executable="$app/Contents/MacOS/dcp-orchestrator"
	daemon="$app/Contents/Resources/daemon/dcp-orchestratord"
	resources="$app/Contents/Resources"
	[[ -f "$plist" && -x "$executable" && -x "$daemon" ]] || fail 'app bundle is incomplete'

	[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist")" == 'pro.devcontrol.dcp-orchestrator' ]] || fail 'artifact bundle id mismatch'
	[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleName' "$plist")" == 'DCP Orchestrator' ]] || fail 'artifact bundle name mismatch'
	[[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist")" == 'dcp-orchestrator' ]] || fail 'artifact executable identity mismatch'
	[[ "$(/usr/libexec/PlistBuddy -c 'Print :DCPUpstreamCommit' "$plist")" == "$upstream_commit" ]] || fail 'artifact upstream identity mismatch'
	[[ "$(/usr/libexec/PlistBuddy -c 'Print :DCPContour' "$plist")" == 'dcp-i8-packaged-app-v1' ]] || fail 'artifact contour mismatch'
	arch="$(lipo -archs "$executable")"; [[ "$arch" == arm64 ]] || fail "main executable is not exact arm64: $arch"
	arch="$(lipo -archs "$daemon")"; [[ "$arch" == arm64 ]] || fail "daemon is not exact arm64: $arch"

	[[ "$(sha256_file "$resources/LICENSE")" == "$license_sha256" ]] || fail 'bundled LICENSE mismatch'
	cmp -s NOTICE "$resources/NOTICE" || fail 'bundled NOTICE mismatch'
	cmp -s DCP_PROVENANCE.md "$resources/DCP_PROVENANCE.md" || fail 'bundled provenance mismatch'
	[[ ! -e "$resources/app-update.yml" ]] || fail 'updater feed is packaged'
	if find "$app" \( -iname '*electron-updater*' -o -iname '*posthog*' -o -iname '*sentry*' \) -print -quit | grep -q .; then
		fail 'updater, analytics, or crash-reporting module is packaged'
	fi
	if strings "$resources/app.asar" | grep -Eiq 'us(-assets)?\.i\.posthog\.com|eu\.i\.posthog\.com|phc_[[:alnum:]]+|electron-updater|app-update\.yml|sentry\.io|crashReporter[[:space:]]*\.[[:space:]]*(start|submit)'; then
		fail 'forbidden updater, analytics, or crash-reporting path is present in app.asar'
	fi
	if strings "$daemon" | grep -Eiq 'us\.i\.posthog\.com|eu\.i\.posthog\.com|phc_[[:alnum:]]+|sentry\.io'; then
		fail 'forbidden analytics or crash-reporting endpoint is present in daemon'
	fi
	codesign --verify --deep --strict "$app" >/dev/null 2>&1 || fail 'bundle signature verification failed'
	printf 'PASS DCP arm64 package and artifact absence gates\n'
}

case "${1:-}" in
	source)
		[[ "$#" -eq 1 ]] || fail 'source mode takes no additional arguments'
		source_gates
		;;
	artifact)
		[[ "$#" -eq 2 ]] || fail 'artifact mode requires exactly one app path'
		artifact_gates "$2"
		;;
	*)
		fail 'usage: scripts/dcp-ci-gates.sh source | artifact /absolute/path/to/app'
		;;
esac
