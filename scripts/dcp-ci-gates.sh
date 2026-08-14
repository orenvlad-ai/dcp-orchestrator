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
control_plane_commit='b94c5b8cbb0dae50e81cbd8e118cbc3c785f8e19'
operating_contract_revision='2026-08-14.1'

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
	grep -Fq "dev-control-plane/blob/$control_plane_commit/docs/DCP_LAB_HAPPY_PATH_V1_CONTRACT.md" AGENTS.md || fail 'AGENTS.md lacks exact happy-path v1 contract'
	grep -Fq "current operating contract revision \`$operating_contract_revision\`" AGENTS.md || fail 'AGENTS.md operating contract revision mismatch'
	grep -Fq 'DCP_AO_LAB_ROOT' AGENTS.md || fail 'AGENTS.md lacks explicit DCP lab root contract'
	grep -Fq 'pro.devcontrol.dcp-orchestrator' AGENTS.md || fail 'AGENTS.md lacks DCP application identity'
	grep -Fq 'Current implemented scope' AGENTS.md || fail 'AGENTS.md does not separate implemented and future scope'
	grep -Fq 'I12 activates only the existing stock Review/ReviewRun/Engine contour' AGENTS.md || fail 'AGENTS.md does not bound the I12 reviewer contour'
	grep -Fq 'at most three active' AGENTS.md || fail 'AGENTS.md lacks the global DCP model-action cap'
	grep -Fq 'not an alternative future policy' AGENTS.md || fail 'AGENTS.md does not retire qualification ceilings as future policy'
	grep -Fq 'does not add a second' AGENTS.md || fail 'AGENTS.md does not exclude parallel state/runtime authorities'
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
	! grep -Fq '"--ask-for-approval"' backend/internal/adapters/agent/codex/codex.go || fail 'Codex worker emits unsupported exec-level approval argv'
	grep -Fq 'approval_policy="on-request"' backend/internal/adapters/agent/codex/codex.go || fail 'Codex worker approval policy override is absent'
	grep -Fq '"--sandbox", "workspace-write"' backend/internal/adapters/agent/codex/codex.go || fail 'Codex worker sandbox policy is not explicit'
	grep -Fq '"--add-dir", gitDir, "--add-dir", commonDir' backend/internal/adapters/agent/codex/codex.go || fail 'Codex worker lacks exact linked-worktree Git metadata roots'
	grep -Fq 'sandbox_workspace_write.network_access=true' backend/internal/adapters/agent/codex/codex.go || fail 'exact synthetic-PR worker network capability is absent'
	grep -Fq 'DCPReviewLabNetwork bool' backend/internal/domain/agentconfig.go || fail 'exact synthetic-PR worker profile marker is absent'
	grep -Fq 'dcpReviewLabOrigin = "https://github.com/orenvlad-ai/dcp-review-lab.git"' backend/internal/adapters/agent/codex/codex.go || fail 'synthetic-PR worker remote is not exact'

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
	grep -Fq 'web_search="disabled"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer web search is not disabled'
	grep -Fq '"--sandbox", "read-only"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer sandbox is not read-only'
	grep -Fq '"--output-schema"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer schema output is absent'
	grep -Fq '"--output-last-message"' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer result artifact is absent'
	grep -Fq 'case "--ask-for-approval":' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer does not strip unsupported approval argv'
	grep -Fq 'case "--add-dir":' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer does not strip worker Git metadata write roots'
	grep -Fq 'attempted to enable worker network access' backend/internal/adapters/reviewer/codex/codex.go || fail 'Codex reviewer does not reject worker network access'
	grep -Fq '"review", "supervise"' backend/internal/review/launcher.go || fail 'reviewer process is not supervised'
	grep -Fq 'reviewerSubmitBinaryName = "ao"' backend/internal/review/launcher.go || fail 'stock reviewer verdict callback alias is absent'
	grep -Fq 'os.SameFile(aliasInfo, exeInfo)' backend/internal/review/launcher.go || fail 'reviewer verdict callback is not bound to the exact supervisor executable'
	grep -Fq 'func ReadStructuredResult' backend/internal/review/result.go || fail 'trusted structured reviewer result validation is absent'
	grep -Fq 'supervisedReviewerChildEnv' backend/internal/cli/review.go || fail 'reviewer credentials are not stripped by the trusted supervisor'
	grep -Fq 'UpdateBoundReviewRunResult' backend/internal/storage/sqlite/queries/review.sql || fail 'structured verdict is not atomically bound to the current ReviewRun/head'
	grep -Fq 'reviews/process-exit' backend/internal/httpd/controllers/reviews.go || fail 'reviewer process exit is not persisted through the daemon'
	grep -Fq 'func (e *Engine) AutoTrigger' backend/internal/review/review.go || fail 'shared automatic review trigger is absent'
	grep -Fq 'func (e *Engine) ReconcileStartup' backend/internal/review/review.go || fail 'review restart reconciliation is absent'
	terminal_merge_migration='backend/internal/storage/sqlite/migrations/0049_dcp_review_lab_terminal_merge.sql'
	admission_migration='backend/internal/storage/sqlite/migrations/0050_dcp_review_lab_admission.sql'
	recovered_incident_migration='backend/internal/storage/sqlite/migrations/0051_dcp_review_lab_recovered_incident.sql'
	arbiter_migration='backend/internal/storage/sqlite/migrations/0052_dcp_review_lab_arbiter_v1.sql'
	arbiter_prelaunch_recovery_migration='backend/internal/storage/sqlite/migrations/0053_dcp_arbiter_prelaunch_config_recovery.sql'
	arbiter_schema_recovery_migration='backend/internal/storage/sqlite/migrations/0054_dcp_arbiter_response_schema_recovery.sql'
	arbiter_successor_migration='backend/internal/storage/sqlite/migrations/0055_dcp_arbiter_successor_attempt.sql'
	arbiter_successor_validation_recovery_migration='backend/internal/storage/sqlite/migrations/0056_dcp_arbiter_successor_result_validation_recovery.sql'
	fresh_worker_recovery_migration='backend/internal/storage/sqlite/migrations/0057_dcp_review_lab_card12_fresh_worker_recovery.sql'
	fresh_worker_preflight_recovery_migration='backend/internal/storage/sqlite/migrations/0058_dcp_review_lab_card12_fresh_worker_preflight_recovery.sql'
	model_free_rebase_migration='backend/internal/storage/sqlite/migrations/0059_dcp_review_lab_card12_model_free_rebase_continuation.sql'
	provider_base_correction_migration='backend/internal/storage/sqlite/migrations/0060_dcp_review_lab_card12_model_free_provider_base_correction.sql'
	cold_start_recovery_migration='backend/internal/storage/sqlite/migrations/0061_dcp_card12_cold_start_quarantined_recovery.sql'
	cold_start_tool_path_recovery_migration='backend/internal/storage/sqlite/migrations/0062_dcp_card12_cold_start_tool_path_recovery.sql'
	cold_start_auto_merge_recovery_migration='backend/internal/storage/sqlite/migrations/0063_dcp_card12_cold_start_auto_merge_recovery.sql'
	rebase_head_finalization_migration='backend/internal/storage/sqlite/migrations/0064_dcp_card12_rebase_head_finalization.sql'
	rebase_head_finalization_audit_recovery_migration='backend/internal/storage/sqlite/migrations/0065_dcp_card12_rebase_head_finalization_audit_recovery.sql'
	rebase_head_finalization_provider_base_recovery_migration='backend/internal/storage/sqlite/migrations/0066_dcp_card12_rebase_head_finalization_provider_base_recovery.sql'
	happy_path_migration='backend/internal/storage/sqlite/migrations/0067_dcp_review_lab_happy_path_v1.sql'
	printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' "$terminal_merge_migration" "$admission_migration" "$recovered_incident_migration" "$arbiter_migration" "$arbiter_prelaunch_recovery_migration" "$arbiter_schema_recovery_migration" "$arbiter_successor_migration" "$arbiter_successor_validation_recovery_migration" "$fresh_worker_recovery_migration" "$fresh_worker_preflight_recovery_migration" "$model_free_rebase_migration" "$provider_base_correction_migration" "$cold_start_recovery_migration" "$cold_start_tool_path_recovery_migration" "$cold_start_auto_merge_recovery_migration" "$rebase_head_finalization_migration" "$rebase_head_finalization_audit_recovery_migration" "$rebase_head_finalization_provider_base_recovery_migration" "$happy_path_migration" > "${TMPDIR:-/tmp}/dcp-authorized-migrations.$$"
	trap 'rm -f "${TMPDIR:-/tmp}/dcp-authorized-migrations.$$"' EXIT
	{
		git diff --name-only "$i11_commit"..HEAD -- backend/internal/storage/sqlite/migrations
		git ls-files --others --exclude-standard -- backend/internal/storage/sqlite/migrations
	} | sort -u > "${TMPDIR:-/tmp}/dcp-actual-migrations.$$"
	trap 'rm -f "${TMPDIR:-/tmp}/dcp-authorized-migrations.$$" "${TMPDIR:-/tmp}/dcp-actual-migrations.$$"' EXIT
	diff -u "${TMPDIR:-/tmp}/dcp-authorized-migrations.$$" "${TMPDIR:-/tmp}/dcp-actual-migrations.$$" >/dev/null || fail 'post-I11 source added an unauthorized storage migration'
	[[ -s "$terminal_merge_migration" ]] || fail 'bounded terminal-merge ReviewRun migration is absent'
	[[ "$(sed '/-- +goose Down/,$d' "$terminal_merge_migration" | grep -Ec '^ALTER TABLE review_run ADD COLUMN ')" -eq 4 ]] || fail 'terminal-merge migration does not add exactly four ReviewRun columns'
	! sed '/-- +goose Down/,$d' "$terminal_merge_migration" | grep -Eiq 'CREATE[[:space:]]+TABLE|ALTER[[:space:]]+TABLE[[:space:]]+([^r]|r[^e]|re[^v]|rev[^i]|revi[^e]|revie[^w]|review[^_]|review_[^r])' || fail 'terminal merge added a second storage schema instead of reusing ReviewRun'
	grep -Fq 'RepositoryFullName = "orenvlad-ai/dcp-review-lab"' backend/internal/dcpterminalmerge/merge.go || fail 'terminal merge repository identity is not exact'
	grep -Fq 'RequiredCheckName  = "dcp-review-lab"' backend/internal/dcpterminalmerge/merge.go || fail 'terminal merge named check is absent'
	[[ -s "$happy_path_migration" ]] || fail 'future DCP happy-path migration is absent'
	grep -Fq 'CREATE TABLE dcp_review_lab_policy_task' "$happy_path_migration" || fail 'future task identity table is absent'
	grep -Fq 'CREATE TABLE dcp_model_action' "$happy_path_migration" || fail 'future model-action FIFO is absent'
	grep -Fq 'idx_dcp_model_action_one_slot' "$happy_path_migration" || fail 'three-slot physical uniqueness is absent'
	grep -Fq 'slot BETWEEN 0 AND 3' "$happy_path_migration" || fail 'three-slot physical ceiling is absent'
	grep -Fq 'card_number > 12' "$happy_path_migration" || fail 'future task rows do not preserve historical card identities'
	grep -Fq 'UNIQUE (task_id, kind, exact_head_sha)' "$happy_path_migration" || fail 'fresh-review exact-head uniqueness is absent'
	grep -Fq 'func (s *Service) SubmitPolicy' backend/internal/service/dcptask/service.go || fail 'policy submit path is absent'
	grep -Fq 'func (s *Service) DrainModelActions' backend/internal/service/dcptask/policy.go || fail 'event-driven model-action drain is absent'
	grep -Fq 'CompleteDCPReviewLabPolicyAdmission' backend/internal/storage/sqlite/store/dcp_admission_store.go || fail 'atomic policy terminal persistence is absent'
	grep -Fq 'DCPReviewLabPolicyAuthorized: true' backend/internal/session_manager/manager.go || fail 'typed future worker network proof is absent'
	! grep -ERq 'time\.(NewTicker|Tick)|for[[:space:]]*\{[[:space:]]*time\.Sleep' backend/internal/service/dcptask || fail 'future policy introduced a poll or heartbeat loop'
	grep -Fq 'headRepository{ nameWithOwner }' backend/internal/adapters/scm/github/observer_provider.go || fail 'SCM observation does not request exact PR head repository identity'
	grep -Fq 'HeadRepo:                 repositoryNameWithOwner(pr["headRepository"])' backend/internal/adapters/scm/github/observer_provider.go || fail 'SCM observation does not preserve exact PR head repository identity'
	grep -Fq 'Method:          ports.SCMMergeSquash' backend/internal/dcpterminalmerge/merge.go || fail 'terminal merge method is not exact squash'
	grep -Fq 'result_channel = '\''structured_dcp_v1'\''' backend/internal/storage/sqlite/queries/review.sql || fail 'terminal merge is not bound to the trusted structured result channel'
	[[ -s "$admission_migration" ]] || fail 'I13 admission migration is absent'
	grep -Fq 'CREATE TABLE dcp_review_lab_admission' "$admission_migration" || fail 'I13 durable admission state is absent'
	grep -Fq 'idx_dcp_review_lab_admission_one_claim' "$admission_migration" || fail 'I13 global single-owner guard is absent'
	grep -Fq 'idx_dcp_review_lab_admission_one_active_per_session' "$admission_migration" || fail 'I13 per-task active admission guard is absent'
	grep -Fq 'dcp.review-lab.arbiter-needed/v1' "$admission_migration" || fail 'I13 structured incident schema is absent'
	grep -Fq 'recovered_incident_packet' "$recovered_incident_migration" || fail 'I13 audited false-incident recovery field is absent'
	grep -Fq 'RecoverDCPReviewLabCanonicalBaseIncident' backend/internal/dcpterminalmerge/merge.go || fail 'I13 exact canonical-base recovery is absent'
	grep -Fq 'merge-tree", "--write-tree"' backend/internal/dcpterminalmerge/merge.go || fail 'I13 current-base compatibility proof is absent'
	grep -Fq 'func (e *Engine) drain' backend/internal/dcpterminalmerge/merge.go || fail 'I13 admission drain is absent'
	grep -Fq 'AdmissionSessionA  = "dcp-review-lab-9"' backend/internal/dcpterminalmerge/merge.go || fail 'I13 first fresh card identity is not exact'
	grep -Fq 'AdmissionSessionB  = "dcp-review-lab-10"' backend/internal/dcpterminalmerge/merge.go || fail 'I13 second fresh card identity is not exact'
	grep -Fq 'ResumeDCPReviewLabIdleAgent' backend/internal/session_manager/manager.go || fail 'I13 bounded same-worker wake is absent'
	grep -Fq "OR (s.activity_state = 'exited' AND s.is_terminated = 1)" backend/internal/storage/sqlite/queries/dcp_card12_cold_start_recovery.sql || fail 'terminal governed sessions cannot preserve startup quarantine'
	[[ -s "$arbiter_migration" ]] || fail 'I13 Stage 2 bounded arbiter migration is absent'
	grep -Fq 'CREATE TABLE dcp_review_lab_arbiter_v1' "$arbiter_migration" || fail 'I13 Stage 2 durable incident/action row is absent'
	grep -Fq 'idx_dcp_review_lab_arbiter_v1_single_incident' "$arbiter_migration" || fail 'I13 Stage 2 global one-incident guard is absent'
	grep -Fq "model = 'gpt-5.6-sol'" "$arbiter_migration" || fail 'I13 Stage 2 model is not physically exact'
	grep -Fq "reasoning = 'xhigh'" "$arbiter_migration" || fail 'I13 Stage 2 reasoning is not physically exact'
	grep -Fq 'token_budget = 16384' "$arbiter_migration" || fail 'I13 Stage 2 token budget is not physically exact'
	grep -Fq 'model_call_count IN (0, 1)' "$arbiter_migration" || fail 'I13 Stage 2 one-call bound is absent'
	[[ -s "$arbiter_successor_migration" ]] || fail 'I13 Stage 2 successor migration is absent'
	grep -Fq 'CREATE TABLE dcp_review_lab_arbiter_v1_successor_attempt' "$arbiter_successor_migration" || fail 'successor attempt has no separate durable identity'
	grep -Fq 'attempt_generation               INTEGER NOT NULL CHECK (attempt_generation = 2)' "$arbiter_successor_migration" || fail 'successor generation is not exact'
	grep -Fq 'model_call_count IN (0, 1)' "$arbiter_successor_migration" || fail 'successor one-call fence is absent'
	grep -Fq 'token_budget                     INTEGER NOT NULL CHECK (token_budget = 16384)' "$arbiter_successor_migration" || fail 'successor token ceiling is not hard'
	grep -Fq 'policy_max_worker_calls          INTEGER NOT NULL CHECK (policy_max_worker_calls = 1)' "$arbiter_successor_migration" || fail 'trusted worker policy is not exact'
	grep -Fq 'policy_max_fresh_reviews         INTEGER NOT NULL CHECK (policy_max_fresh_reviews = 1)' "$arbiter_successor_migration" || fail 'trusted reviewer policy is not exact'
	grep -Fq 'original_result_artifact_digest' "$arbiter_successor_migration" || fail 'first rejected artifact is not pinned'
	[[ -s "$arbiter_successor_validation_recovery_migration" ]] || fail 'exact successor validation-recovery migration is absent'
	grep -Fq '9b5ff7847db2533e56bdbbc424114e5bea8e5e3c352ad1d029a99deaba05c172' "$arbiter_successor_validation_recovery_migration" || fail 'observed successor result artifact is not exact'
	grep -Fq 'a19c64060d0f41320b6bf652c47ff5c58810ebec0416d003963bc1b4fcdf524f' "$arbiter_successor_validation_recovery_migration" || fail 'frozen nested merge-tree digest is not exact'
	grep -Fq '28546ce0cc2be84349221464c4938c98ed11d32a' "$arbiter_successor_validation_recovery_migration" || fail 'validation-recovery authority is not exact'
	grep -Fq 'recoverArbiterSuccessorExactResultLocked' backend/internal/dcpterminalmerge/arbiter_successor_engine.go || fail 'exact model-free successor validation recovery is absent'
	grep -Fq 'Deliberately stop at decided/zero wake' backend/internal/dcpterminalmerge/arbiter_successor_engine.go || fail 'validation recovery does not stop before worker wake'
	[[ -s "$fresh_worker_recovery_migration" ]] || fail 'card-12 fresh-worker recovery migration is absent'
	grep -Fq 'd2b7142bc9e5844ba165abe24d3222b3e1a94c3577fba5f6f8d97ec3dbad151b' "$fresh_worker_recovery_migration" || fail 'card-12 recovery identity digest is not exact'
	grep -Fq 'worker_model_call_count IN (0, 1)' "$fresh_worker_recovery_migration" || fail 'card-12 worker one-call fence is absent'
	grep -Fq 'reviewer_model_call_count IN (0, 1)' "$fresh_worker_recovery_migration" || fail 'card-12 reviewer one-call fence is absent'
	grep -Fq 'token_budget = 16384' "$fresh_worker_recovery_migration" || fail 'card-12 worker token ceiling is not hard'
	grep -Fq 'ProcessFreshWorkerExit' backend/internal/dcpterminalmerge/fresh_worker_engine.go || fail 'card-12 trusted worker result path is absent'
	grep -Fq 'validateFreshWorkerCommandLog' backend/internal/dcpterminalmerge/fresh_worker_engine.go || fail 'card-12 guarded push proof is absent'
	[[ -s "$fresh_worker_preflight_recovery_migration" ]] || fail 'card-12 zero-call preflight recovery migration is absent'
	grep -Fq 'fbcf4929f9192f7cce9c5097b0bc6a449d28e663' "$fresh_worker_preflight_recovery_migration" || fail 'card-12 failed preflight source is not exact'
	grep -Fq 'prior_worker_calls = 0' "$fresh_worker_preflight_recovery_migration" || fail 'card-12 preflight recovery does not prove zero prior worker calls'
	grep -Fq "observed_diff_status = 'M'" "$fresh_worker_preflight_recovery_migration" || fail 'card-12 exact modified-path evidence is absent'
	[[ -s "$model_free_rebase_migration" ]] || fail 'card-12 exact model-free rebase migration is absent'
	grep -Fq '66eb630c1995f90b37429a2f6c57c57794dda9fc98a29149c88bdb2f01131060' "$model_free_rebase_migration" || fail 'card-12 model-free continuation identity is not exact'
	grep -Fq 'model_free_action_count IN (0, 1)' "$model_free_rebase_migration" || fail 'card-12 model-free one-action fence is absent'
	grep -Fq 'worker_model_call_count = 0' "$model_free_rebase_migration" || fail 'card-12 model-free worker-call zero is not physical'
	grep -Fq 'arbiter_model_call_count = 0' "$model_free_rebase_migration" || fail 'card-12 model-free arbiter-call zero is not physical'
	grep -Fq 'reviewer_model_call_count IN (0, 1)' "$model_free_rebase_migration" || fail 'card-12 model-free reviewer fence is absent'
	[[ -s "$provider_base_correction_migration" ]] || fail 'card-12 exact provider-base correction migration is absent'
	grep -Fq '25663a5a551fce7ec0d6d9055588b4c4d1d1294fd926e2c7c2347cacd799ab59' "$provider_base_correction_migration" || fail 'card-12 provider-base correction identity is not exact'
	grep -Fq 'dbaf01b05e85ffffa4c843a905e2fe5229eaf0da' "$provider_base_correction_migration" || fail 'card-12 provider-base snapshot is not exact'
	grep -Fq 'b34b31b5443890e69128db2862726950a6bbac0d' "$provider_base_correction_migration" || fail 'card-12 current-main correction fact is not exact'
	grep -Fq 'merge-base", "--is-ancestor", modelFreeProviderBaseSHA, row.CurrentMain' backend/internal/dcpterminalmerge/model_free_rebase_executor.go || fail 'card-12 provider-base ancestry proof is absent'
	[[ -s "$cold_start_recovery_migration" ]] || fail 'card-12 cold-start quarantined recovery migration is absent'
	grep -Fq '087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f' "$cold_start_recovery_migration" || fail 'card-12 cold-start recovery identity is not exact'
	grep -Fq 'CREATE TABLE dcp_governed_startup_quarantine' "$cold_start_recovery_migration" || fail 'pre-restoration startup quarantine is not durable'
	grep -Fq 'worker_model_call_count = 0' "$cold_start_recovery_migration" || fail 'cold-start worker-call zero is not physical'
	grep -Fq 'arbiter_model_call_count = 0' "$cold_start_recovery_migration" || fail 'cold-start arbiter-call zero is not physical'
	grep -Fq 'model_free_action_count IN (0, 1)' "$cold_start_recovery_migration" || fail 'cold-start one-action fence is absent'
	grep -Fq 'reviewer_model_call_count IN (0, 1)' "$cold_start_recovery_migration" || fail 'cold-start reviewer fence is absent'
	grep -Fq '33238' "$cold_start_recovery_migration" || fail 'card-11 unauthorized token evidence is absent'
	grep -Fq '33573' "$cold_start_recovery_migration" || fail 'card-12 unauthorized token evidence is absent'
	[[ -s "$cold_start_tool_path_recovery_migration" ]] || fail 'card-12 cold-start tool-path recovery migration is absent'
	grep -Fq 'preflight_or_backup_failed' "$cold_start_tool_path_recovery_migration" || fail 'cold-start tool-path failure audit is absent'
	grep -Fq '/opt/homebrew/bin/gh' "$cold_start_tool_path_recovery_migration" || fail 'rejected gh symlink evidence is absent'
	grep -Fq '/opt/homebrew/Cellar/gh/2.87.2/bin/gh' "$cold_start_tool_path_recovery_migration" backend/internal/dcpterminalmerge/model_free_rebase_executor.go || fail 'physical trusted gh path is absent'
	grep -Fq 'f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e' "$cold_start_tool_path_recovery_migration" backend/internal/dcpterminalmerge/model_free_rebase_executor.go || fail 'physical trusted gh digest is absent'
	grep -Fq 'HasExactDCPCard12ColdStartToolPathRecovery' backend/internal/dcpterminalmerge/cold_start_recovery_engine.go backend/internal/storage/sqlite/store/dcp_card12_cold_start_recovery_store.go || fail 'cold-start tool-path re-arm audit validation is absent'
	[[ -s "$cold_start_auto_merge_recovery_migration" ]] || fail 'card-12 cold-start AUTO_MERGE recovery migration is absent'
	grep -Fq '3eba7b0dec18c759875b2b33a8d7d2379caaa6a1' "$cold_start_auto_merge_recovery_migration" backend/internal/dcpterminalmerge/cold_start_recovery_executor.go || fail 'exact preserved AUTO_MERGE tree is absent'
	grep -Fq 'dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed' "$cold_start_auto_merge_recovery_migration" backend/internal/dcpterminalmerge/cold_start_recovery_executor.go || fail 'exact preserved AUTO_MERGE file digest is absent'
	grep -Fq 'HasExactDCPCard12ColdStartAutoMergeRecovery' backend/internal/dcpterminalmerge/cold_start_recovery_engine.go backend/internal/storage/sqlite/store/dcp_card12_cold_start_recovery_store.go || fail 'cold-start AUTO_MERGE re-arm audit validation is absent'
	[[ -s "$rebase_head_finalization_migration" ]] || fail 'card-12 REBASE_HEAD finalization migration is absent'
	grep -Fq 'a073fb250a5343cffa210614247c76a080bb9e7db6a6cd8d052909611a75e50b' "$rebase_head_finalization_migration" || fail 'card-12 REBASE_HEAD finalization identity is not exact'
	grep -Fq 'model_free_action_count IN (0, 1)' "$rebase_head_finalization_migration" || fail 'card-12 REBASE_HEAD one-action fence is absent'
	grep -Fq 'worker_model_call_count = 0' "$rebase_head_finalization_migration" || fail 'card-12 REBASE_HEAD worker zero is not physical'
	grep -Fq 'arbiter_model_call_count = 0' "$rebase_head_finalization_migration" || fail 'card-12 REBASE_HEAD arbiter zero is not physical'
	grep -Fq 'reviewer_model_call_count IN (0, 1)' "$rebase_head_finalization_migration" || fail 'card-12 REBASE_HEAD reviewer fence is absent'
	[[ -s "$rebase_head_finalization_audit_recovery_migration" ]] || fail 'card-12 REBASE_HEAD audit recovery migration is absent'
	grep -Fq '52490d8c01eccc8f02984ec4d863895c0215950590cfc5309d00a1525eb8f11b' "$rebase_head_finalization_audit_recovery_migration" || fail 'card-12 REBASE_HEAD audit recovery identity is not exact'
	grep -Fq '6f53f74f456b869c98bb82d928f671b54672808a' "$rebase_head_finalization_audit_recovery_migration" || fail 'card-12 REBASE_HEAD failed source is not exact'
	grep -Fq 'prior_action_count = 0' "$rebase_head_finalization_audit_recovery_migration" || fail 'card-12 REBASE_HEAD audit recovery does not prove zero prior actions'
	grep -Fq 'prior_reviewer_calls = 0' "$rebase_head_finalization_audit_recovery_migration" || fail 'card-12 REBASE_HEAD audit recovery does not prove zero prior reviewers'
	[[ -s "$rebase_head_finalization_provider_base_recovery_migration" ]] || fail 'card-12 REBASE_HEAD provider-base recovery migration is absent'
	grep -Fq 'd140ac8daec5f311a278050c6e1e0b33011e28b0ee2ee9b52bb357f3b34ac923' "$rebase_head_finalization_provider_base_recovery_migration" || fail 'card-12 REBASE_HEAD provider-base recovery identity is not exact'
	grep -Fq 'prior_action_count = 1' "$rebase_head_finalization_provider_base_recovery_migration" || fail 'card-12 REBASE_HEAD provider-base recovery does not preserve the consumed action'
	grep -Fq 'prior_reviewer_calls = 0' "$rebase_head_finalization_provider_base_recovery_migration" || fail 'card-12 REBASE_HEAD provider-base recovery does not prove zero reviewers'
	grep -Fq 'post_push_provider_base_advanced_from_historical_base_to_current_main' "$rebase_head_finalization_provider_base_recovery_migration" || fail 'card-12 REBASE_HEAD provider-base correction reason is not exact'
	grep -Fq '"push", lease, "origin", "HEAD:"+row.PushRef' backend/internal/dcpterminalmerge/rebase_head_finalization_executor.go || fail 'card-12 REBASE_HEAD push is not exact force-with-lease'
	grep -Fq 'for name, digest := range map[string]string{"REBASE_HEAD": row.RebaseHeadDigest, "ORIG_HEAD": row.OrigHeadDigest}' backend/internal/dcpterminalmerge/rebase_head_finalization_executor.go || fail 'exact inert pseudoref conjunction is absent'
	grep -Fq 'e.rebaseHeadFinalization.Preflight(ctx, row)' backend/internal/dcpterminalmerge/rebase_head_finalization_engine.go || fail 'retained candidate is not validated before the action fence'
	grep -Fq "finalization.status = 'succeeded'" backend/internal/storage/sqlite/queries/dcp_card12_cold_start_recovery.sql || fail 'terminal finalization cannot preserve quarantine across restart'
	grep -Fq 'EstablishDCPGovernedStartupQuarantine' backend/internal/daemon/daemon.go || fail 'daemon does not establish startup quarantine before restoration'
	grep -Fq 'RestoreQuarantine:   restoreQuarantine' backend/internal/daemon/lifecycle_wiring.go || fail 'session manager does not receive the established startup quarantine'
	grep -Fq 'if m.isRestoreQuarantined(rec.ID)' backend/internal/session_manager/manager.go || fail 'stock session restoration is not fenced by quarantine'
	grep -Fq '"push", lease, "origin", "HEAD:"+row.PushRef' backend/internal/dcpterminalmerge/cold_start_recovery_executor.go || fail 'cold-start push is not exact force-with-lease'
	[[ "$(grep -Fc 'status != "M\t"+arbiterConflictPath' backend/internal/dcpterminalmerge/fresh_worker_engine.go)" -eq 2 ]] || fail 'card-12 conflict path is not validated as modified before and after repair'
	grep -Fq 'ArbiterSuccessorDecision struct' backend/internal/dcpterminalmerge/arbiter_successor.go || fail 'successor decision contract is absent'
	! sed -n '/type ArbiterSuccessorDecision struct/,/^}/p' backend/internal/dcpterminalmerge/arbiter_successor.go | grep -Fq 'MaxFreshReviews' || fail 'model still owns successor review policy'
	grep -Fq 'opts.handle != dcpArbiterSuccessorHandle' backend/internal/cli/arbiter.go || fail 'successor result evidence is not retained'
	grep -Fq 'ArbiterSessionA' backend/internal/dcpterminalmerge/arbiter_protocol.go || fail 'I13 Stage 2 fresh card identity is absent'
	grep -Fq 'features.rollout_budget={enabled=true,limit_tokens=16384,reminder_at_remaining_tokens=[2048],sampling_token_weight=1.0,prefill_token_weight=1.0}' backend/internal/dcpterminalmerge/arbiter_launcher.go || fail 'I13 Stage 2 hard rollout budget is absent'
	grep -Fq 'strict_config_top_level_rollout_budget_rejected' "$arbiter_prelaunch_recovery_migration" || fail 'I13 Stage 2 pre-provider config rejection audit is absent'
	grep -Fq 'unsupported_root_oneof_rejected_before_inference' "$arbiter_schema_recovery_migration" || fail 'I13 Stage 2 provider schema rejection audit is absent'
	grep -Fq '019ff21d-4cde-72d1-b70d-49efd3cd1c17' "$arbiter_schema_recovery_migration" || fail 'I13 Stage 2 rejected Codex session identity is absent'
	! grep -Fq '"oneOf"' backend/internal/dcpterminalmerge/arbiter_protocol.go || fail 'I13 Stage 2 response schema retains unsupported root composition'
	grep -Fq 'SubmitArbiterDecision' backend/internal/dcpterminalmerge/arbiter_engine.go || fail 'I13 Stage 2 trusted decision path is absent'
	grep -Fq 'same_worker_conflict_repair' backend/internal/dcpterminalmerge/arbiter_protocol.go || fail 'I13 Stage 2 sole recovery path is absent'
	grep -Fq 'localControlRequest(req)' backend/internal/httpd/router.go || fail 'I13 Stage 2 callback is not loopback-gated'
	! grep -ERq 'time\.(NewTicker|Tick)|for[[:space:]]*\{[[:space:]]*time\.Sleep' backend/internal/dcpterminalmerge || fail 'I13 admission/arbiter introduced a poll or heartbeat loop'
	grep -Fq 'ApplySCMEligibilityObservation' backend/internal/observe/scm/observer.go || fail 'happy-path admission catch-up is not driven by the stock SCM event'
	grep -Fq 'admission.Status != domain.DCPAdmissionWaiting' backend/internal/lifecycle/reactions.go || fail 'SCM admission catch-up is not fenced to one durable waiting identity'
	grep -Fq 'set(&base.DiffBaseSHA, in.DiffBaseSHA)' backend/internal/lifecycle/manager.go || fail 'native provisioning still drops the durable task creation base SHA'
	grep -Fq 'repairDCPReviewLabCard13CreationBase' backend/internal/storage/sqlite/db.go || fail 'exact live card-13 creation-base repair is absent'
	grep -Fq 'AND 0 = (' backend/internal/storage/sqlite/db.go || fail 'card-13 base repair is not fenced to zero active model actions'
	grep -Fq 'getSessionVisualStatus(session)' frontend/src/renderer/components/SessionsBoard.tsx || fail 'native card does not use the shared policy status dot'
	grep -Fq 'getSessionVisualStatus(session)' frontend/src/renderer/components/Sidebar.tsx || fail 'sidebar does not use the shared policy status dot'
	grep -A5 -F '@media (prefers-reduced-motion: reduce)' frontend/src/renderer/styles.css | grep -Fq '.animate-status-pulse,' || fail 'status pulse is not disabled for reduced motion'

unexpected_paths="$(git diff --name-only "$i8_parity_commit"..HEAD | grep -Ev '^(\.github/workflows/.*|AGENTS\.md|CLAUDE\.md|DCP_PROVENANCE\.md|NOTICE|README\.md|scripts/dcp-ci-gates\.sh|frontend/forge\.config\.ts|frontend/package(-lock)?\.json|backend/internal/adapters/(agent|reviewer)/codex/codex(_test)?\.go|backend/internal/adapters/runtime/(tmux/tmux(_test)?|conpty/runtime)\.go|backend/internal/adapters/scm/github/(observer_provider|provider_test)\.go|backend/internal/browserruntime/broker(_test)?\.go|backend/internal/cli/(arbiter(_supervise_unix_test)?|recovery(_supervise_unix_test)?|project|project_test|review|review_supervise_unix_test|root|root_test)\.go|backend/internal/daemon/(daemon|lifecycle_wiring|scm_wiring|wiring_test)\.go|backend/internal/dcpterminalmerge/(arbiter_(engine|launcher|protocol|successor|successor_engine|successor_launcher)(_test)?|fresh_worker_(engine|launcher|test)|model_free_rebase_(engine|executor|test)|cold_start_recovery_(engine|executor|test)|merge|merge_test)\.go|backend/internal/domain/(agentconfig|dcp_admission|dcp_arbiter|dcp_fresh_worker_recovery|dcp_model_free_rebase_continuation|dcp_card12_cold_start_recovery|dcp_task|review|session|status)\.go|backend/internal/httpd/(api|router)\.go|backend/internal/httpd/apispec/openapi\.yaml|backend/internal/httpd/apispec/specgen/build\.go|backend/internal/httpd/controllers/(dcp_tasks(_test)?|dto|reviews(_test)?)\.go|backend/internal/lifecycle/(manager|manager_test|reactions)\.go|backend/internal/observe/scm/observer(_test)?\.go|backend/internal/ports/(agent|reviewer|outbound)\.go|backend/internal/review/(launcher|launcher_test|planner|prompt|result|result_test|review|review_test)\.go|backend/internal/service/dcptask/(repository|repository_test|service|service_test)\.go|backend/internal/service/review/review(_test)?\.go|backend/internal/service/session/(service|status_test)\.go|backend/internal/session_manager/manager(_test)?\.go|backend/internal/storage/sqlite/gen/((dcp_admission|dcp_arbiter|dcp_fresh_worker_recovery|dcp_model_free_rebase_continuation|dcp_card12_cold_start_recovery|dcp_tasks|review|sessions)\.sql\.go|models\.go)|backend/internal/storage/sqlite/(migrate(_burned_versions)?|cold_start_tool_path_migration)_test\.go|backend/internal/storage/sqlite/migrations/(004(8_dcp_task_foundation|9_dcp_review_lab_terminal_merge)|005(0_dcp_review_lab_admission|1_dcp_review_lab_recovered_incident|2_dcp_review_lab_arbiter_v1|3_dcp_arbiter_prelaunch_config_recovery|4_dcp_arbiter_response_schema_recovery|5_dcp_arbiter_successor_attempt|6_dcp_arbiter_successor_result_validation_recovery|7_dcp_review_lab_card12_fresh_worker_recovery|8_dcp_review_lab_card12_fresh_worker_preflight_recovery|9_dcp_review_lab_card12_model_free_rebase_continuation)|006(0_dcp_review_lab_card12_model_free_provider_base_correction|1_dcp_card12_cold_start_quarantined_recovery|2_dcp_card12_cold_start_tool_path_recovery))\.sql|backend/internal/storage/sqlite/queries/(dcp_admission|dcp_arbiter|dcp_fresh_worker_recovery|dcp_model_free_rebase_continuation|dcp_card12_cold_start_recovery|dcp_tasks|review|sessions)\.sql|backend/internal/storage/sqlite/store/(dcp_admission_store|dcp_arbiter_store|dcp_arbiter_successor_store|dcp_fresh_worker_recovery_store|dcp_model_free_rebase_continuation_store|dcp_card12_cold_start_recovery_store|dcp_task_store|review_store)(_test)?\.go|backend/internal/telemetrymeta/cli\.go|backend/sqlc\.yaml|frontend/src/api/schema\.ts|frontend/src/renderer/__tests__/integration/board-empty-states\.test\.tsx|frontend/src/renderer/components/(CommandPalette|ProjectSettingsForm|RestoreUnavailableDialog|SessionInspector|SessionsBoard|ShellTopbar|Sidebar)(\.test)?\.tsx|frontend/src/renderer/hooks/useDCPTasksQuery\.ts|frontend/src/renderer/i18n/(de|en|es|fr|ja|ko|pt-BR|zh-CN)\.json|frontend/src/renderer/lib/(api-client|command-palette|orchestrator-spawn-sources|pr-display|session-presentation|spawn-orchestrator)(\.test)?\.ts|frontend/src/renderer/types/workspace\.ts)$' || true)"
unexpected_paths="$( { printf '%s\n' "$unexpected_paths"; git ls-files --others --exclude-standard; } | sort -u)"
unexpected_paths="$(printf '%s\n' "$unexpected_paths" | grep -Ev '^(backend/internal/storage/sqlite/cold_start_auto_merge_migration_test\.go|backend/internal/storage/sqlite/migrations/0063_dcp_card12_cold_start_auto_merge_recovery\.sql|backend/internal/dcpterminalmerge/rebase_head_finalization_(engine|executor|test)\.go|backend/internal/domain/dcp_card12_rebase_head_finalization\.go|backend/internal/storage/sqlite/(rebase_head_finalization(_provider_base)?_migration_test\.go|migrations/006(4_dcp_card12_rebase_head_finalization|5_dcp_card12_rebase_head_finalization_audit_recovery|6_dcp_card12_rebase_head_finalization_provider_base_recovery)\.sql|queries/dcp_card12_rebase_head_finalization\.sql|gen/dcp_card12_rebase_head_finalization\.sql\.go|store/dcp_card12_rebase_head_finalization_store(_test)?\.go))$' || true)"
unexpected_paths="$(printf '%s\n' "$unexpected_paths" | grep -Ev '^(backend/internal/cli/dcp\.go|backend/internal/domain/dcp_lab_policy\.go|backend/internal/service/dcptask/(policy|review_repository)\.go|backend/internal/storage/sqlite/gen/dcp_lab_policy\.sql\.go|backend/internal/storage/sqlite/migrations/0067_dcp_review_lab_happy_path_v1\.sql|backend/internal/storage/sqlite/queries/dcp_lab_policy\.sql|backend/internal/storage/sqlite/store/dcp_lab_policy_store(_test)?\.go)$' || true)"
unexpected_paths="$(printf '%s\n' "$unexpected_paths" | grep -Ev '^backend/internal/storage/sqlite/gen/dcp_terminal_quarantine_test\.go$' || true)"
unexpected_paths="$(printf '%s\n' "$unexpected_paths" | grep -Ev '^(backend/internal/storage/sqlite/db\.go|backend/internal/storage/sqlite/card13_creation_base_migration_test\.go)$' || true)"
unexpected_paths="$(printf '%s\n' "$unexpected_paths" | grep -Ev '^(frontend/src/renderer/hooks/useWorkspaceQuery(\.test)?\.tsx?|frontend/src/renderer/styles\.css)$' || true)"
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
