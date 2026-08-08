# DCP Orchestrator source provenance

This repository is a private standalone managed-source repository. GitHub does
not permit changing a public fork's visibility, so it is deliberately not a
member of the public upstream fork network. It nevertheless preserves the
upstream Git history and uses these remotes in managed local checkouts:

- `origin`: `https://github.com/orenvlad-ai/dcp-orchestrator.git`;
- `upstream`: `https://github.com/Untrivial-ai/agent-orchestrator.git`, fetch
  reference only.

## Exact foundation

- upstream release: `v0.12.1`;
- upstream commit: `1df40e93772c2c48e916870d9c3ddf8f29a69f84`;
- upstream tree: `36bf30cc4960c10f0d94fc63a8ff0a4dd22bb8a8`;
- upstream commit verification at qualification: `verified=true`,
  `reason=valid`;
- root Apache-2.0 `LICENSE` SHA-256:
  `1a2219722b7ef58364065e9073a2cb2831891eb147a785742a31431c9cddad1d`;
- tracked upstream NOTICE result: absent (case-insensitive full-tree scan).

The exact I8 parity anchor in this repository is
`23fe9bba77873075f32b813fb0a3c936598882fb`. Its binary full-index diff from
the upstream commit has SHA-256
`047c9f74902ede19b6e3a3ba753fc7b2702a322a9be709fb0e975cc5628314d2`, exactly
matching the final repository-owned patch queue in
`orenvlad-ai/dev-control-plane` at commit
`1fed97d4cd586313d5287e6f66871a5ddf7f6e63`.

The parity queue is reviewable as seven cumulative application commits:

1. I3 native-laboratory isolation;
2. I5 Codex worker isolation;
3. I6 process-outcome classification;
4. I7 single-entry app-owned contour foundation;
5. I7 fail-closed gateway semantics;
6. I7 UI/daemon run-file identity;
7. I8 canonical packaged application.

Each commit message identifies the corresponding `dev-control-plane` evidence
commit. `git diff --name-status
1df40e93772c2c48e916870d9c3ddf8f29a69f84..23fe9bba77873075f32b813fb0a3c936598882fb`
is the exact prominent modified-file notice for that queue. Later fork-owned
source-governance changes are separately visible after the parity anchor.

## Authority and build boundary

`orenvlad-ai/dev-control-plane` remains authoritative for DCP architecture,
integration policy, the current operating contract, qualification, and the
exact approved fork commit. This repository owns Electron/Go application code
and tests only. It must not copy the architecture contract into a competing
policy source.

The active package consists of the Electron desktop entry/preload/renderer and
the Go `cmd/ao` daemon embedded by `frontend/scripts/build-daemon.mjs`. Inherited
landing-site, mobile, maker, feed, release, and updater-support source is not
built, deployed, published, or authorized by the DCP workflow. The package has
no configured maker or publisher, no updater dependency or initialization, no
active analytics client or remote telemetry sink, and no crash reporter
initialization. CI has read-only repository permissions, performs no release
or artifact upload, and discards its temporary package with the runner.

Source refresh is manual only: fetch an exact upstream commit through the
read-only remote, re-audit LICENSE/NOTICE/dependencies, review every DCP
divergence, and merge through a separate checked change. No floating upstream
branch, updater feed, workflow publisher, or installed application is source
authority.
