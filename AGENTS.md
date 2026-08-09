# DCP Orchestrator repository rules

This private managed fork owns DCP Orchestrator application source. It is not
ordinary Agent Orchestrator, not a production control plane, and not an
independent source of DCP architecture policy.

## Required authority before any change

Read these immutable `orenvlad-ai/dev-control-plane` sources before designing
or editing this repository:

1. [root repository rules at `eb9ca41f23b2cfef51bda37f291cd44d6d29c173`](https://github.com/orenvlad-ai/dev-control-plane/blob/eb9ca41f23b2cfef51bda37f291cd44d6d29c173/AGENTS.md)
2. [current operating contract revision `2026-08-08.10`](https://github.com/orenvlad-ai/dev-control-plane/blob/eb9ca41f23b2cfef51bda37f291cd44d6d29c173/docs/CURRENT_OPERATING_CONTRACT.md)
3. [DCP v1 target architecture contract](https://github.com/orenvlad-ai/dev-control-plane/blob/eb9ca41f23b2cfef51bda37f291cd44d6d29c173/docs/TARGET_ARCHITECTURE_V1.md)
4. [exact managed-fork lock](https://github.com/orenvlad-ai/dev-control-plane/blob/eb9ca41f23b2cfef51bda37f291cd44d6d29c173/upstream/dcp-orchestrator.lock)

Those pinned documents are the implementation authority for the I11 baseline.
The owner-delegated I12 change adds only the bounded automatic reviewer contour
described below; its exact merged pin and updated operating contract are applied
sequentially in `dev-control-plane` after this source PR merges. The live
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
  existing disposable `orenvlad-ai/dcp-review-lab#1` and its preserved
  `DCP Review Canary` session for one reviewer proof. Real repositories,
  other remotes, `wb-core`, WBC, production, hosted systems, and public
  distribution are out of scope.
- The DCP package has no updater, feed, maker, publisher, analytics, telemetry,
  crash collection/upload, release, or external service path. Do not restore
  inherited upstream paths for any of them.
- Existing standard Codex authentication is the only external credential
  input. Never copy/expose credentials or load user Codex configuration into a
  lab worker.

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
and restart reconciliation is model-free and fail-closed. It adds no reviewer
service, scheduler, queue, watcher, heartbeat, webhook, second registry/DB,
arbiter, Admission Controller, Release Train, auto-merge, multi-pass repair
loop, hosted projection, production target, or new DCP-task execution path.
Never synthesize owner acceptance.

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
