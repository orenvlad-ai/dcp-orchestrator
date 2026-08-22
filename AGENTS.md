# DCP Orchestrator repository rules

This public managed fork owns DCP Orchestrator application source. It is not
ordinary Agent Orchestrator, not a production control plane, and not an
independent source of DCP architecture policy.

## Required authority before any change

Read these immutable `orenvlad-ai/dev-control-plane` sources at merged commit
`a1bfdd9328566dc630587220b60b7faa7ba1d745` before designing or editing this
repository:

1. [root repository rules](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/AGENTS.md)
2. [current operating contract revision `2026-08-16.1`](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/CURRENT_OPERATING_CONTRACT.md)
3. [DCP Lab happy-path v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_LAB_HAPPY_PATH_V1_CONTRACT.md)
4. [Phase UI and ordinary-card arbiter v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_LAB_PHASE_UI_ARBITER_V1_CONTRACT.md)
5. [Phase 1 installed evidence](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_LAB_PHASE_UI_V1_INSTALL_EVIDENCE.md)
6. [exact managed-fork lock before this source change](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/upstream/dcp-orchestrator.lock)
7. [first real repo-only target v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_REAL_TARGET_V1_CONTRACT.md)
8. [runtime provider identity v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_REAL_TARGET_PROVIDER_IDENTITY_V1_CONTRACT.md)
9. [exact first-submit recovery v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_REAL_TARGET_SUBMIT_RECOVERY_V1_CONTRACT.md)
10. [repo-only repository rename v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/a1bfdd9328566dc630587220b60b7faa7ba1d745/docs/DCP_REAL_TARGET_REPOSITORY_RENAME_V1_CONTRACT.md)
11. [`wb-core` Release Train handoff v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/036b1101284f626c931f7edb1750ddd228634832/docs/DCP_WB_CORE_RELEASE_TRAIN_HANDOFF_V1_CONTRACT.md)
12. [`wb-core` CI truth and lifecycle UX v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/1ca282408bec53a1d696cb58d247e33285209ee9/docs/DCP_WB_CORE_CI_TRUTH_LIFECYCLE_UX_V1_CONTRACT.md)
13. [`wb-core` end-to-end release and deploy v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/4f7775f375a612a38e96496f09908ab48e3598c5/docs/DCP_WB_CORE_END_TO_END_RELEASE_DEPLOY_V1_CONTRACT.md)
14. [task-first native lifecycle v1 contract](https://github.com/orenvlad-ai/dev-control-plane/blob/5075235780b9c38d95faa9657a70265069d3a5c5/docs/DCP_TASK_FIRST_NATIVE_LIFECYCLE_V1_CONTRACT.md)
15. [WBC integration twin and DCP v2 architecture contract](https://github.com/orenvlad-ai/dev-control-plane/blob/8be08577673722edc9ae036dedea46c88ceac129/docs/DCP_WBC_INTEGRATION_TWIN_DCP_V2_ARCHITECTURE_CONTRACT.md)
16. [Stage 3 terminal evidence and conditional Stage 4 source authority](https://github.com/orenvlad-ai/dev-control-plane/blob/8be08577673722edc9ae036dedea46c88ceac129/docs/DCP_WBC_INTEGRATION_TWIN_STAGE3_4_COMBINED_EXECUTION_CONTRACT.md)
17. [Stage 5 adapter, install and preflight authority](https://github.com/orenvlad-ai/dev-control-plane/blob/4143982eb054a40537d963356c209bfe8447ba31/docs/DCP_WBC_INTEGRATION_TWIN_STAGE5_INSTALL_ACTIVATION_CONTRACT.md)
18. [Stage 6 post-submit native-shell correction](https://github.com/orenvlad-ai/dev-control-plane/blob/9be4575e9eed39ae5bea9f1665f71fd4f64cd89b/docs/DCP_WBC_INTEGRATION_TWIN_STAGE6_POST_SUBMIT_NATIVE_SHELL_CORRECTION_CONTRACT.md)
19. [Stage 6 direct DCP-v2 model authority](https://github.com/orenvlad-ai/dev-control-plane/blob/3aa42b7afda620331d111ba24299e2917821e720/docs/DCP_WBC_INTEGRATION_TWIN_STAGE6_DIRECT_MODEL_AUTHORITY_CONTRACT.md)
20. [Stage 6 final viability authority](https://github.com/orenvlad-ai/dev-control-plane/blob/03d9f9943d06e5507dc1fc9c02c53cee782407c8/docs/DCP_WBC_INTEGRATION_TWIN_STAGE6_FINAL_VIABILITY_CONTRACT.md)

Item 11 and the merged root/current authorities at exact
`036b1101284f626c931f7edb1750ddd228634832`, operating revision
`2026-08-17.1`, supersede only the prior exclusion of `wb-core`. They authorize
one fail-closed exact repo-only target and WBC Release Train handoff; every
other predecessor boundary remains unchanged.

Item 12 and the merged root/current authorities at exact
`1ca282408bec53a1d696cb58d247e33285209ee9`, operating revision
`2026-08-17.8`, supersede only two later boundaries: the DCP CI verdict is the
configured required check on the exact head rather than every observational
Release Train job, and workflow activity remains visibly live across
zero-model autonomous waits. It also authorizes the exact model-free
`wbc-canary-v1` recovery after a separately reviewed source/pin/install chain.
It does not widen model, merge, Release Train, target or runtime authority.

Item 13 and the merged root/current authorities at exact
`4f7775f375a612a38e96496f09908ab48e3598c5`, operating revision
`2026-08-18.2`, supersede only the proven post-admission WBC blocker. They
authorize one Actions-event-driven fresh-readmission generation per exact
immutable marker on the same task/PR, plus exact profile `live-runtime` with
Actions-owned `release:production` proof. DCP model roles remain repository-
only; DCP direct merge and deploy remain forbidden. Source remains inactive
until the separate immutable pin and governed deterministic install pass.

Item 14 and the merged root/current authorities at exact
`5075235780b9c38d95faa9657a70265069d3a5c5`, operating revision
`2026-08-18.11`, replace only the duplicated native-shell liveness predicates
with one provider-neutral task-first lifecycle policy. The durable task/card
remains authoritative through its profile terminal proof. An exact model
runtime is required only for an exact fenced launching/running model action;
CI, review queue, admission, Release Train, Human Gate, incident and terminal
observation remain passive and may retain the exact archived native shell.
Action/process asymmetry remains fail-closed. This source and schema-83
recovery remain inactive until a separate reviewed pin/install pass.

Items 15-17 and the merged root/current authorities at exact
`4143982eb054a40537d963356c209bfe8447ba31`, operating revision
`2026-08-20.5`, activate only the exact Stage 5 integration-twin adapter and
install preparation after the independently green Stage 3 qualification. The
new Task -> immutable
exact-head Revision -> durable Command -> bounded Action -> FIFO Admission ->
verified Release/Deployment Result core remains additive; the exact twin
adapter is source-active but cannot become runtime-effective before the
separate reviewed pin/install/preflight gates. Every
authoritative transition persists its required next Command in the same SQLite
transaction; startup and provider events enter one bounded drain with durable
leases, effect fences and dedupe. There is no timer, watcher, heartbeat,
unbounded poll, automatic retry or direct merge/deploy method. Schema 0084
creates empty v2 tables with `adapter_activated=0` and
`installed=0`; it does not submit a task or mutate predecessor schema-83 facts.
The session projection field remains nil until the exact installed adapter
binds a future v2 Task to its native runtime shell. The adapter has no merge, deploy, SSH or
install method; Stage 4 source completion alone was not install,
activation, submission, canary, production authority or owner acceptance.

Item 18 and the merged root/current authority at exact
`9be4575e9eed39ae5bea9f1665f71fd4f64cd89b`, operating revision
`2026-08-20.10`, authorize only the one post-submit Stage 6 native-shell
correction in
[`DCP_WBC_INTEGRATION_TWIN_STAGE6_POST_SUBMIT_NATIVE_SHELL_CORRECTION_CONTRACT.md`](https://github.com/orenvlad-ai/dev-control-plane/blob/9be4575e9eed39ae5bea9f1665f71fd4f64cd89b/docs/DCP_WBC_INTEGRATION_TWIN_STAGE6_POST_SUBMIT_NATIVE_SHELL_CORRECTION_CONTRACT.md).
Migration 0085 may extend the predecessor native policy-task table only for
the already-durable exact `dcp-v2-twin-canary-v1` Task and exact twin tuple.
Startup may reserve its missing native card/session and initial worker once
only when the exact Task, Revision, leased Command, launching Action, empty
native state and immutable Stage 5 activation all match. The stopped recovery
preflight applies no model, GitHub, Release Train or deployment effect. It
does not authorize a second submit, replacement identity, broader target,
credential change, direct merge/deploy, WBC mutation or Stage 7.

Item 19 and the merged root/current authority at exact
`3aa42b7afda620331d111ba24299e2917821e720`, operating revision
`2026-08-21.4`, remove the DCP-v2 dependency on that native lifecycle. DCP
SQLite and the daemon are the sole durable Task/Revision/Command/Action and
model-runtime authority; a typed provider-neutral runner is stateless
transport. The exact frozen Worker output may be consumed once by its bounded
identity-specific adoption record, then only a model-free publication Command
may continue it. Schema 0086 and this source package remain inactive until a
separate reviewed pin, install/migration and stopped-preflight authority.

Item 20 and the merged root/current authority at exact
`03d9f9943d06e5507dc1fc9c02c53cee782407c8`, operating revision
`2026-08-22.4`, authorize one aggregate managed-source correction for the
stopped Stage 6 identity. Forward migration 0087 adds an immutable
`provider_bound` Revision: the Worker/repair output keeps `PRNumber=0`, while
publication atomically creates one same-head successor bound to the real PR,
advances the same Task and enqueues exact checks. The same package closes
review-effect reconciliation, lineage-safe worktree reuse, deterministic
readmission, exact Admission bindings and complete target-owned terminal proof
validation including `artifact_source_sha = merge_sha = deployed_sha`. It
does not itself authorize installation, adoption, startup, provider effects,
WBC or production; those remain behind one separately reviewed exact pin and
live authority.

The happy-path v1 contract remains the rule for future synthetic review-lab
tasks; the current real-target entry is exact target `wb-browser-extension`,
profile `repo-only`, and repository `orenvlad-ai/wb-browser-extension`. Exact
completed `wb-price-extension-1` remains only a terminal legacy restore alias;
the old target is not a future submit authority. The
I11-I13 qualification and recovery contracts remain
immutable evidence for historical cards 1-12 only; their card/cohort ceilings,
consumed allowances and quarantine rows are not future-task policy. The live
`dev-control-plane` repository remains the authority for later architecture,
operating, integration, qualification and exact-pin changes. Do not copy or
reinterpret its contracts here as a competing source of truth. This source PR
does not authorize runtime until its separately reviewed exact pin,
deterministic install and stopped preflight complete.

## Repository and runtime boundary

- `orenvlad-ai/dcp-orchestrator` is the public managed application-source repository.
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
- The normal DCP adapter target remains the disposable, remote-free `dcp-lab`
  repository beneath the explicit lab root. Only exact tuples
  `dcp-review-lab` / `synthetic-pr` / `orenvlad-ai/dcp-review-lab` and
  `wb-browser-extension` / `repo-only` / `orenvlad-ai/wb-browser-extension` and
  `wb-core` / `repo-only` / `orenvlad-ai/wb-core` and `wb-core` /
  `live-runtime` / `orenvlad-ai/wb-core` may receive their exact typed contours.
  The `wb-core` target remains locked before native/model mutation until its
  repository-owned v2 compatibility marker is present, is permanently
  ineligible for DCP direct merge or deploy, and may hand off only
  `release:ready` to the WBC GitHub Actions Release Train. Existing
  PRs and cards 1-12 are immutable audit evidence and must not be changed or
  reused. Every other repository or remote, WBC production, hosted
  systems and public distribution remain out of scope. The real target ends at
  trusted `MERGED`; it has no deploy, Release Train, production apply, or
  owner-acceptance implication.
- The DCP package has no updater, feed, maker, publisher, analytics, telemetry,
  crash collection/upload, release, or external service path. Do not restore
  inherited upstream paths for any of them.
- Existing standard Codex authentication remains the only model credential
  input. The exact synthetic-PR worker may use the host's already configured
  GitHub transport only after its typed profile marker, native session,
  data/worktree/Git paths, branch and exact fetch/push remote all validate;
  never copy/expose credentials or load user Codex configuration into a lab
  worker. The DCP-v2 direct Worker and every Reviewer remain network-disabled;
  only a separately fenced model-free publication Command may use GitHub.

## Current implemented scope

Ordinary legacy/non-DCP policy tasks retain their stock native card/session and
historical lifecycle. DCP v2 instead owns its Task, immutable Revision, durable
Command, bounded Action, runtime, terminal receipt and next transition solely
in the additive v2 tables. Its typed runner is stateless transport, future
DCP-v2 tasks create no legacy policy-task/session/model-action authority rows,
and the one frozen Stage 6 native Worker receipt is consumable only by the
exact immutable one-time adoption record. The current native card/session/action
then remain historical display/evidence only.

Both contours enforce at most three active model slots globally, one initial
worker, one fresh context-free review per exact head, at most one same-task
findings repair and one shared durable FIFO admission/release lease. Queued
work, model-free publication, CI, readmission, admission and release waits own
no model or new loop. Only current exact public provider facts for one of the
four compile-time target/profile/repository identities may enter the applicable
line. The synthetic and
browser-extension targets may reach their ordinary daemon-owned expected-head
terminal merge; `wb-core` may only enter the typed zero-action Release Train
wait and receive `release:ready`, never a DCP merge or deploy call. Its
repo-only profile is terminal only on exact Actions-owned `release:done`; its
live-runtime profile is terminal only on exact Actions-owned
`release:production` plus matching merge/deployed SHA, canonical target/service
and required probe proof. Any other identity,
stale head,
second findings cycle, conflict or ambiguity fails closed without arbiter,
HumanGate, manual bypass or replacement card. Historical rows below remain
implemented immutable evidence, not an alternative future policy.

Phase 2 cards 18-20 proved one transient provider-enrichment bug: the stock SCM
observer may persist a structural PR row before its provider identity fields.
That incomplete snapshot is a passive model-free `ci_waiting` fact, not an
identity contradiction. Complete conflicting identity still fails closed. The
one already-recorded `night-ui-b` false incident may be re-armed only by exact
migration 0068 after its immutable task/session/worker/PR/head/base/named-check
conjunction is preserved in one audit row; the migration itself launches no
model action, push, review, admission or merge.

Stock UI Terminate may archive an exact native shell without terminating its
durable policy task. The common lifecycle evaluator, not a target-specific
shell exception, decides whether the current task phase requires a runtime:
worker/reviewer/repair/arbiter queue states require one exact queued action and
no process; fenced launching/running states require the matching action and
exact process; CI, admission, Release Train, Human Gate, incident and terminal
observation require no active action or process and may retain the exact
exited/terminated shell. Contradictory shell facts, a live process without its
action, an active action without its runtime, crossed identity, or more than
three global active actions remain hard failures. Project/repository/card/
session/branch/worktree/prompt/display/provider/profile/PR/head checks remain
separate strict gates.

The same exact open `wb-core` readmission lifecycle must remain visible to the
stock SCM observer after its shell is archived. This is now a provider-specific
generation/marker/review/admission binding layered after the common lifecycle
decision, not shell-liveness authority. It closes when the generation becomes
terminal, conflicted or failed and cannot authorize a foreign/direct target,
crossed generation or DCP merge/deploy.
The first installed observer correction proved that the preserved repo-only
generation still carries valid immutable v1 marker evidence while the native
project is correctly registered against v2. Observer eligibility must use the
same versioned envelope as the readmission engine: exact repo-only accepts v1
or the configured v2 marker, exact live-runtime accepts only configured v2,
and every foreign/direct-target combination remains ineligible. This changes
no marker parsing, generation creation, project registration, model, merge or
deploy authority.

The installed marker-envelope correction then persisted the fresh exact-head
required check, but the review engine's historical preserved-shell predicate
still rejected the terminated policy session because its predecessor head had
an approved review. An exact policy-owned preserved session may bypass only
that generic predecessor predicate: the existing policy gate must separately
authorize the current PR and exact head before any workspace preparation,
review row, model slot or launch. Ordinary preserved sessions retain the
single-use missing-workspace rule, and an uncontracted policy head remains
inert. This adds no reviewer retry, replacement task/session or broader
terminated-session authority.

That fresh reviewer then approved exact readmission head `26044c696651ce5873748ec3f920d40e77c5686c`
once, but the admission candidate retained the older nonterminal-shell rule and
recorded `admission_identity_drift` before a new FIFO row could drain. Migration
0081 preserves that exact false incident and re-arms only the already-approved
head with zero new model authority. For WBC readmission, admission eligibility
additionally requires the same open generation to be exactly `reviewed` and
its task, session, repository, scope, PR, branch, head, review action, review
run and empty admission identity all match; native-shell liveness comes only
from the common task-first evaluator. If WBC main
advances again before admission, DCP may hand the exact reviewed head to Release
Train while provider status is `BEHIND`; DCP still does not update or merge the
branch. Actions must publish the next immutable readmission marker, after which
the same task/PR repeats baseline, review and FIFO admission. Every other policy
target remains CLEAN-only and every drift remains fail-closed.

After migration 0081, the exact reviewed generation atomically allocated and
bound admission sequence 32, then the waiting selector re-evaluated the same
terminated shell after the durable generation had advanced from `reviewed` to
`admitted`. The original helper accepted only the pre-enqueue state and recorded
`waiting_identity_drift`. Migration 0082 preserves both exact incident packets,
the bound generation/admission/review identities and action sequence 73, then
re-arms only that same admission as `waiting`; it creates no task, session,
action, review, admission, generation or release fact. The shell may survive
the one reviewed-to-admitted transition only when the task admission ID and
generation admission ID are the same non-empty value. A missing, crossed or
later-generation binding remains ineligible.

Migration 0083 is the inactive, immutable task-first activation for the exact
stopped schema-82 `wbc-canary-v1` identity. It proves the archived shell, task
revision 22, action 73, review, admission sequence 32, generation 1 and all
zero-active/global row counts before preserving FIFO sequence 32 and advancing
only the task revision to 23. It creates no task, session, model action,
ReviewRun, admission, generation or release row, preserves both predecessor
incident packets, is restart-idempotent and fails atomically on any drift.

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

The terminal fresh-worker row at exact source
`75a14431a3433f581755f2e0ec096814e3e9ecb1` is immutable after its sole worker
call exhausted the rollout budget with the permitted two-line conflict
resolution still present in one stopped rebase. Migration 0059 adds only one
subordinate model-free continuation row. After exact evidence, runtime and
provider preflight, the daemon may consume one action fence to stage that one
path, continue the one stopped commit non-interactively, update the same local
branch ref and perform one exact old-head force-with-lease push. It adds zero
worker calls and zero arbiter calls. Only the resulting exact head may consume
one context-free reviewer fence and pass the existing admission and terminal
merge gates. Drift, uncertain one-shot state or any second action/reviewer
fails closed without replacement identity or general Git recovery authority.

Exact source `a7b5476fb886bcbb6bbd91aa89da17966547b3b8` was built, installed and
preflighted without starting runtime. The final stopped pre-action proof showed
that PR #9's provider-base snapshot is exact
`dbaf01b05e85ffffa4c843a905e2fe5229eaf0da` while current main is exact
`b34b31b5443890e69128db2862726950a6bbac0d`, with the former an ancestor of
the latter. Migration 0060 may add only one immutable correction row bound to
contract `9610bf1a8fa41f631ca5ed336d0d9b0313d7d73f`. The pre-action validator
must check the provider snapshot and current ref independently plus exact
ancestry. This correction consumes no action/model/reviewer fence and adds no
general base compatibility or retry authority.

The first controlled source-0060 bundle start violated that zero-worker-call
authority before the governed action fenced: stock native restoration launched
card 11 and card 12 ordinary Codex workers for 33,238 and 33,573 tokens
respectively (66,811 total), then replaced the preserved detached rebase with
an attached one-path `UU` conflict-marker state. Those calls and the terminal
`failed/identity_drift` continuation are immutable evidence. Migration 0061
creates a separate recovery row and a durable startup quarantine for only
cards 11/12. The daemon must establish that exact quarantine transactionally
before constructing runtime/session restoration; missing, ambiguous or
unreadable governed state aborts cold start. Stock restoration remains
unchanged for unrelated sessions. The new daemon-owned action may byte-back up
and reconstruct only the verified attached card-12 state, replay its one commit
onto exact current main, write the authorized two-line file, and push the same
branch once with an exact old-head force-with-lease. It owns zero worker and
zero arbiter calls and at most one fresh context-free exact-head reviewer,
followed only by the existing admission and terminal-merge gates. Every drift,
duplicate, later action/reviewer or broadened identity fails closed.

The first exact source-0061 live start proved that quarantine ordering: cards
11/12 remained bare shells with zero descendants and no worker call. The
recovery then failed before backup/action because the trusted gh constant
named Homebrew symlink `/opt/homebrew/bin/gh`, while the existing verifier
correctly accepts only a physical regular file. That row is exact
`failed/preflight_or_backup_failed`, revision 1, with 0/0/0/0 calls/actions,
empty backup/head/review fields and two quarantine rows verified once each.
Migration 0062 must preserve that failure in one immutable audit and may re-arm
only the same recovery row once at revision 2. The corrected constant is only
`/opt/homebrew/Cellar/gh/2.87.2/bin/gh` with existing digest
`f392d9ad8d2260c671566936b127f5436772ce16e25b091cf1fa7b301987f27e`.
This direct-path correction adds no identity, model call, action, reviewer,
retry policy or authority; every absent or mismatched audit fails closed.

The source-0062 live start again held that fence with no governed worker and
failed before backup/action at revision 3 with 0/0/0/0 counters. The newly
proven direct-path fact is the one regular `AUTO_MERGE` ref already created by
the original stock Git conflict: exact tree
`3eba7b0dec18c759875b2b33a8d7d2379caaa6a1`, file digest
`dac6e5a895aed94e8cd5a0f1a39b1c23f0201393e621c635ed228070710c13ed`
and conflict blob `1af18aad20e3aab90ea7f1c617d330abc3b08de9` reproduce the exact preserved
marker bytes. It is evidence, not an active mutator. Migration 0063 may preserve
only that exact second zero-call failure and re-arm only the same row at
revision 4. The daemon must include the exact ref in its immutable backup, then
normal `reset --hard` removes it before reconstruction. Any missing, foreign or
changed ref remains terminal; no identity, model call, action, reviewer, retry
policy or authority is added.

The exact source-0063 terminal start preserved that quarantine, consumed its
one model-free action and produced clean local commit
`4de6ff1a0b80223a9b32a05ba68cf0b665296081`, but normal regular
`REBASE_HEAD` remained and the broad postcondition rejected it before push.
Migration 0064 creates one new finalization row only from the exact immutable
failed revision-7 predecessor, sealed backup and quarantine 4/4 state. The
trusted daemon may accept the byte-exact regular `REBASE_HEAD`/`ORIG_HEAD`
pair only together with the exact clean candidate and absence of every active
operation residue, consume one action fence and perform one old-head force-
with-lease push. It makes no local Git write, starts no worker or arbiter and
may fence at most one fresh exact-head reviewer before existing admission and
merge gates. The predecessor executor and general residue guard remain
unchanged; every mismatch or second action/reviewer fails closed.

The first source-0064 finalization start held the quarantine at 5/5 and failed
before its action fence with `identity_drift`, revision 1 and counters
`0/0/0/0`. The exact defect is model-free: finalization predecessor validation
reused the historical tool-path and `AUTO_MERGE` queries whose state predicates
describe the earlier rev2/rev4 authorized recovery, so both returned zero for
the required immutable failed rev7 predecessor even though both exact audit
rows remain present once. Migration 0065 preserves that failure in one
immutable correction audit and may re-arm only the same finalization row at
revision 2. The validator then binds that audit, both original audit identities,
the unchanged terminal predecessor and quarantine 6/6+ without weakening either
historical query. This direct-path correction adds no identity, action, worker,
arbiter, reviewer, push or retry authority.

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
- The ordinary CLI and renderer are thin daemon clients. Exact hidden stopped
  activation/adoption commands may open SQLite only for their separately
  authorized atomic transition; they cannot spawn a runtime or create an
  alternate state/display authority.
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
