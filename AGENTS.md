# DCP Orchestrator repository rules

This public managed fork owns DCP Orchestrator application source. It is not
ordinary Agent Orchestrator, not a production control plane, and not an
independent source of DCP architecture policy.

## Required authority before any change

Read these immutable `orenvlad-ai/dev-control-plane` sources before designing
or editing this repository:

1. [root repository rules at `2a174899ae72bf1db548c3b2f172d963488191f1`](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/AGENTS.md)
2. [current operating contract revision `2026-08-12.6`](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/CURRENT_OPERATING_CONTRACT.md)
3. [I13 Stage 2 card-12 fresh-worker recovery contract](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/I13_STAGE2_CARD12_FRESH_WORKER_RECOVERY_CONTRACT.md)
4. [I13 Stage 2 successor terminal evidence](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/I13_STAGE2_SUCCESSOR_TERMINAL_EVIDENCE.md)
5. [I13 Stage 2 successor validation-recovery contract](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/I13_STAGE2_SUCCESSOR_VALIDATION_RECOVERY_CONTRACT.md)
6. [I13 Stage 2 arbiter successor contract](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/I13_STAGE2_ARBITER_SUCCESSOR_CONTRACT.md)
7. [I13 Stage 2 arbiter v1 contract revision 3](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/I13_STAGE2_ARBITER_V1_CONTRACT.md)
8. [DCP v1 target architecture contract](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/docs/TARGET_ARCHITECTURE_V1.md)
9. [exact managed-fork lock](https://github.com/orenvlad-ai/dev-control-plane/blob/2a174899ae72bf1db548c3b2f172d963488191f1/upstream/dcp-orchestrator.lock)

Those pinned documents are the implementation authority for the I11 baseline.
The owner-delegated I12 change adds only the bounded automatic reviewer contour
described below. The later owner-delegated canonical synthetic-PR lab change
adds one exact-profile terminal merge only after the same trusted reviewer,
required check and provider readiness facts succeed. I13 Stage 1 authorized
exactly two native synthetic tasks and a durable mechanical Admission
Controller around that same terminal merge. Its independently checked result
and the owner-approved Stage 2 contract authorize this fresh source change to
add exactly one bounded global release arbiter v1 for cards 11/12. Its exact
merged pin and updated integration contract are applied sequentially in
`dev-control-plane` after this source PR merges. The live
`dev-control-plane` repository remains the authority for later
architecture, operating, integration, qualification, and exact-pin changes.
Do not copy or reinterpret those contracts here as a competing source of
truth. Before beginning later work, confirm that the checked-out fork revision
is the exact immutable revision pinned by current `dev-control-plane`.

## Repository and runtime boundary

- `orenvlad-ai/dcp-orchestrator` is the private application-source repository.
  Official `Untrivial-ai/agent-orchestrator` is read-only provenance. Integrate
  only an explicitly reviewed immutable upstream commit; never treat a branch,
  tag lookup, feed, installed application, or upstream state as authority.
- `dev-control-plane` owns DCP architecture/integration policy and the exact
  approved fork pin. Do not vendor this application into it or establish a
  second application source tree there.
- The only runtime identity is `DCP Orchestrator`, bundle id
  `pro.devcontrol.dcp-orchestrator`, executable `dcp-orchestrator`, daemon
  `dcp-orchestratord`, service `dcp-orchestrator-daemon`, and loopback port
  `43231`.
- The canonical, explicitly supplied `DCP_AO_LAB_ROOT` is
  `~/Library/Application Support/DCP Orchestrator`. Durable state/data, managed
  source, builds, evidence, worktrees, and the disposable target remain below
  that root. Electron caches use
  `~/Library/Caches/pro.devcontrol.dcp-orchestrator`; logs use
  `~/Library/Logs/DCP Orchestrator`.
- Never read, run, discover, import, migrate, or modify installed upstream Agent
  Orchestrator, its home-directory state, or its application data. Never use an
  upstream launcher/bootstrap path for DCP.
- The DCP adapter target remains the disposable, remote-free `dcp-lab`
  repository beneath the explicit lab root. I12 additionally permits only the
  disposable `orenvlad-ai/dcp-review-lab` repository for its separately
  authorized reviewer canaries. Existing PRs and sessions are immutable audit
  evidence and must not be changed or reused. Real repositories, other remotes,
  `wb-core`, WBC, production, hosted systems, and public distribution are out
  of scope.
- The DCP package has no updater, feed, maker, publisher, analytics, telemetry,
  crash collection/upload, release, or external service path. Do not restore
  inherited upstream paths for any of them.
- Existing standard Codex authentication remains the only model credential
  input. The exact synthetic-PR worker may use the host's already configured
  GitHub transport only after its typed profile marker, native session,
  data/worktree/Git paths, branch and exact fetch/push remote all validate;
  never copy/expose credentials or load user Codex configuration into a lab
  worker. Every ordinary worker and every reviewer remains network-disabled.

## Current implemented scope

I11 implements one model-free foundation only: the existing daemon and its
existing SQLite may durably submit, read, list events for, restore, and display
one synthetic DCP task in exact state `SUBMITTED`. Submission is idempotent,
target-restricted, and records an atomic per-task event. The board may project
that synthetic lab task in Working with its stable task id and exact substate.

I11 by itself does not activate or imply task execution or reviewer execution.
I12 activates only the existing stock Review/ReviewRun/Engine contour: an
eligible exact PR head on a safely idle worker triggers one fresh read-only
reviewer, manual Run Review uses the same trigger, process outcomes are durable,
and restart reconciliation is model-free and fail-closed. The later canonical
synthetic-PR lab contour permits one event-driven terminal squash merge only
for the exact `orenvlad-ai/dcp-review-lab` profile/session/worktree/branch/PR/head,
after an approved structured verdict with no findings, its named green check,
no unresolved threads and provider `MERGEABLE`/`CLEAN`; the outcome is stored
on the same `ReviewRun` and reconciled model-free after restart.

I13 Stage 1 adds one subordinate SQLite FIFO/lease record per admitted native
task and permits cards 9 and 10 to run/review independently. Card 8 is
pre-stage immutable evidence and cannot be enrolled or resumed. Exactly one row may
own terminal merge. The other is durably passive: no heartbeat, watcher,
polling loop, or model activity. A merge completion drains the next row
model-free; `BEHIND` permits exactly one same-worker native continuation and a
fresh exact-head review, while proven conflict/ambiguity records one structured
`dcp.review-lab.arbiter-needed/v1` packet and stops. If the first merge advances
exact `origin/main` while the provider retains the reviewed base SHA on a still
`MERGEABLE`/`CLEAN` second PR, startup may recover only the resulting
`canonical_main_diverged` false incident after proving both fast-forward
ancestry and a clean merge tree; the original packet remains durably retained
and recovery performs no model call. This does not add a second task/card
identity, registry, database, daemon, scheduler, queue service,
reviewer service, Human Gate, Release Train, general retry/recovery or
auto-merge policy, hosted projection, production target, deploy path, or broad
runtime capability.

I13 Stage 2 adds migration 0052 and one subordinate incident/action row for a
fresh exact `merge_conflict_or_ambiguity` packet on native card 11 or 12.
Migration 0053 is the model-free correction for the first package's
strict-config rejection: it preserves that pre-provider failed launch in one
bounded audit row and re-arms only the same incident/generation once. It is not
a retry policy or a second model call. The corrected launcher pins Codex CLI
0.145.0 and the structured `features.rollout_budget` configuration. Migration
0054 separately preserves the corrected package's exact
`invalid_json_schema` rejection before inference, result output or tokens and
performs the contract's final same-identity re-arm. The decision schema uses
only required constants/enums; trusted validation enforces the owner/path or
safe-stop relationship without provider-unsupported root composition. No
further re-arm exists.
The daemon freezes the complete Stage 2 cohort, hashes the bounded task, scope,
candidate, checks, reviews, queue and exhausted mechanical recovery evidence,
and permits exactly one stateless `gpt-5.6-sol`/`xhigh` call after atomically
recording its 16,384-token ceiling and `model_call_count=1`. The model has no
mutation or credential channel and may return only one identity-bound
`assign_recovery` for the same worker/conflict path or a truthful `safe_stop`.
Only the trusted daemon may consume one recovery wake, accept one fresh
exact-head review and rebind the original FIFO row; the arbiter never merges.
Duplicate, late, stale, foreign or malformed results and restart replay are
inert. Cards outside 11/12, failed CI, waiting, ordinary staleness and the
historical false `canonical_main_diverged` packet cannot open an arbiter.

The owner-approved successor correction preserves the first rejected card-12
attempt and all of its artifacts as immutable evidence. Migration 0055 adds
one exact generation-2 attempt for that same incident, with one separately
fenced `gpt-5.6-sol`/`xhigh` call and a hard 16,384-token ceiling. The model
does not own worker or reviewer limits: its successor decision schema omits
those fields, while the trusted daemon deterministically enforces one worker
wake and one fresh review at most. An accepted decision is bound to the exact
incident, attempt, input and decision digests. The `decided` state cannot wake
the worker until a controlled restart; duplicate, late, stale, foreign or
malformed results and every later restart are inert. This authorization does
not create a replacement card, PR, incident, general retry policy or broader
recovery path.
Migration 0056 adds only one exact audit/consumption fence for the already
produced generation-2 result whose immutable artifact digest is
`9b5ff7847db2533e56bdbbc424114e5bea8e5e3c352ad1d029a99deaba05c172`.
One startup may validate that unchanged artifact after admitting only its
already frozen nested merge-tree evidence digest, atomically move the existing
attempt from `failed` to `decided`, and stop with zero recovery wakes. It cannot
launch a model, replace or rewrite the artifact, accept an arbitrary late
result, or create another attempt. The next controlled startup is the sole
existing path that may consume the deterministic 1/1 worker/reviewer policy.
Never synthesize owner acceptance.

The successor's accepted generation-2 decision and one consumed native wake
are immutable. That native resume failed before model launch because card 12
has no historical `agent_session_id` or `runtime_launch_id`; its successor row
is terminal `failed/repair_launch_failed`. Migration 0057 adds one separately
audited fresh stateless worker action for only that existing card/task/worktree/
branch/PR/incident. Its old empty native identities remain unchanged. The new
row fences at most one `gpt-5.6-sol`/`xhigh` worker call with a hard 16,384-token
ceiling, one new runtime/Codex identity, one exact guarded same-branch push,
and at most one context-free reviewer on the resulting exact head. It permits
no card/task/worktree/branch/PR/incident/arbiter replacement, no transcript
replay and no retry. Only the existing trusted admission line may rebind
sequence 4 and terminally merge PR #9 after the one approved/no-findings
review, named green check and current CLEAN/MERGEABLE facts. Every preflight,
worker, reviewer, restart or terminal ambiguity fails closed after its
applicable fence without another model call.

Exact source `fbcf4929f9192f7cce9c5097b0bc6a449d28e663` then failed closed at
the first fresh-worker preflight with zero calls because its model-free Git
check required the conflict path to be added from current main. The exact
current-main/candidate bytes prove the path is modified (`M`). The owner's
delegated direct-path-defect authority permits migration 0058 to retain that
zero-call failure in a separate immutable audit, re-arm only the same unused
generation-1 row once and correct only that exact path-status check. It adds no
identity, worker/reviewer/arbiter call, artifact, transcript or retry policy.

The active Codex reviewer success path is deterministic after model exit. The
model receives no network, daemon connection variables, GitHub credentials, or
control-plane command channel and emits exactly one bounded JSON result through
Codex's native output-schema/last-message files. The trusted AO supervisor
validates schema, worker/reviewer/batch/run identity, the current exact open PR
head, and terminal ownership, then records the verdict once through the existing
daemon and guarded ReviewRun update. Missing, malformed, ambiguous, duplicate,
late, foreign, or stale-head results fail closed without a verdict or retry. The
private exact-binary `ao` alias remains only for compatibility with other stock
reviewer adapters; Codex success never depends on a model-issued command. This
adds no migration, result database, service, watcher, scheduler, or second state
authority.

Manual Spawn/Open Orchestrator affordances are absent from normal DCP UI. Keep
the existing daemon/API/programmatic agent and orchestrator capability needed
by possible future approved roles; do not expose a second manual operating
flow and do not delete legacy sessions/state.

## Code and storage rules

- Keep changes surgical. Avoid drive-by cleanup, broad renames, formatting
  churn, speculative abstractions, and architecture refactors.
- Preserve package boundaries: domain vocabulary in `backend/internal/domain`,
  ports in `backend/internal/ports`, service logic in
  `backend/internal/service`, loopback controllers/DTOs in
  `backend/internal/httpd/controllers`, and SQLite in
  `backend/internal/storage/sqlite`.
- The CLI and renderer are thin daemon clients. They never open SQLite, spawn a
  runtime, or create an alternate state/display authority.
- SQLite is the sole local authority. Add a new additive migration; never edit
  a merged migration. Use one transaction for task state and its required
  semantic task event, reject stale compare-and-set revisions, and leave the
  existing trigger-backed `change_log` as AO change-notification/CDC
  infrastructure rather than turning it into a second DCP event or display
  authority.
- Do not persist full chat/model transcripts, chain-of-thought, secrets,
  credentials, authentication material, or user Codex configuration.
- Change SQL sources/migrations and run `npm run sqlc`; never hand-edit
  `backend/internal/storage/sqlite/gen/*`.
- API changes are code-first in controller DTOs and API spec registration. Run
  `npm run api` and commit both generated OpenAPI and frontend TypeScript
  artifacts. Do not hand-edit generated API artifacts.
- Keep the daemon listener loopback-only for this stage. Add no LAN/mobile,
  webhook, hosted, or other external listener/service.
- Preserve legacy session/activity derivation and existing I8 sessions. A DCP
  task id is not an AO session id and must not be silently merged with it.

## Required checks

Run the narrowest relevant tests first, then all applicable fork gates:

```text
./scripts/dcp-ci-gates.sh source
cd backend && go test -p 1 ./... && go build ./...
cd frontend && npm run typecheck
cd frontend && npx vitest run --config vite.renderer.config.ts <relevant tests>
npm run sqlc
npm run api
```

For packaged proof, build only an isolated temporary arm64 bundle/state first
and run `./scripts/dcp-ci-gates.sh artifact /absolute/path/to/DCP Orchestrator.app`.
Do not touch the canonical installed app/state until migration, compatibility,
rollback, identity, updater/telemetry/crash, and zero-model-call gates are all
green. Build/install only exact merged fork and `dev-control-plane` pins.

Work from exact current `origin/main` in a separate branch/worktree. One direct
executor owns one active DCP change, runs semantic/security self-review, and
opens one ready PR. Use ordinary protected review and CI; never force-push.
Technical completion is not owner acceptance.
