## Context

See proposal.md, "Why". The cli solved the same problem in cli#344 with `.github/scripts/lint-config-check.sh`; this design reuses that script's shape and names where the operator differs.

How the operator runs the linter (observed in the tree at 45c6a12):

- No GitHub action. `.tasks/tools.yaml` runs `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>`, then `golangci-lint custom` builds a binary with the `logcheck` plugin from `.custom-gcl.yml`, into `bin/`. CI and a laptop use the same task.
- The version literal `v2.8.0` is in `Taskfile.yml:18`, `Makefile:282` and `.custom-gcl.yml:6`.
- The custom binary reports `v2.8.0-custom-gcl-<hash>` for `version --short`.
- `lint.yml:107-108` runs `task dev:lint:config`, which runs `bin/golangci-lint config verify`. `Makefile:98-99` has the same command.

## Goals / Non-Goals

**Goals:** the requirements of `specs/lint-config-check/spec.md`.

**Non-Goals:** changing lint rules, the linter version or how the linter is installed (the install still needs the network: the Go module proxy and the plugin build); replacing the `Makefile`; adding a rollup job, timeouts or workflow lint to `lint.yml`.

## Research & Decisions

### The hidden `--schema` flag works with the custom binary

**Context**: the cli uses a stock binary; the operator's is a custom build with a plugin linter (`logcheck`, `type: module`) in its settings.
**Explored**: in the worktree, with `bin/golangci-lint` (v2.8.0-custom-gcl): `unshare -rn bin/golangci-lint config verify` fails with `failing loading "https://golangci-lint.run/jsonschema/golangci.v2.8.jsonschema.json"`; `unshare -rn bin/golangci-lint config verify --schema <module cache>/jsonschema/golangci.jsonschema.json` exits 0.
**Decision**: the check MUST run `config verify --schema <committed file>`.
**Rationale**: same command, same schema content, no download. No spike section is needed: the assumption is verified.

### Where the schema comes from

**Context**: the brief allows no fetch, and a schema copied from a website has no checksum to compare with.
**Explored**: the Go module `github.com/golangci/golangci-lint/v2@v2.8.0`, which the install task already downloads and the Go checksum database verifies, holds `jsonschema/golangci.jsonschema.json` (sha256 `204952a9...c59c`). At a release tag that file is the schema of that release; the website serves it as `golangci.v2.8.jsonschema.json`.
**Decision**: commit that file as `.github/golangci-lint/golangci.v2.8.jsonschema.json` with a one-line `SHA256SUMS`. `AGENTS.md` names `go mod download` as the way to get the next one.
**Rationale**: a reviewer can reproduce the checksum from a source the Go toolchain already authenticates.

### One version file; `.custom-gcl.yml` keeps a checked literal

**Decision**: `.golangci-lint-version` holds `v2.8.0`. `Taskfile.yml` reads it with a `sh:` variable and `Makefile` with `$(shell ...)`. `.custom-gcl.yml` keeps `version: v2.8.0`, and the check MUST refuse a different value.
**Alternatives**: generate `.custom-gcl.yml` at install time (more moving parts in the install task, and the file is kubebuilder scaffolding that tools read); keep the literal in `Taskfile.yml` as the source (a YAML key inside a large file is harder for a script and for `Makefile` to read than a one-line file, and the cli already uses this file name).
**Rationale**: one edit point for the install; the one literal that cannot be removed is compared on every Lint run.

### The installed linter must be the exact version

**Decision**: the check MUST accept `vX.Y.Z` or `vX.Y.Z-custom-gcl-<suffix>` from `version --short`, and nothing else. The cli accepts any patch of the minor line because its developers install the linter by hand; here the task installs exactly the named version on every machine.

### What "the workflow's use" means without the action

**Decision**: the cli's check parses `uses: golangci/golangci-lint-action` steps. The operator has none, so its check MUST instead require: `lint.yml` has a step `run: task dev:lint:config`; `.tasks/dev.yaml` runs the script from `lint:config`; no line in `.github/workflows/`, `Taskfile.yml`, `.tasks/*.yaml` or `Makefile` runs `config verify` without `--schema`, uses `golangci/golangci-lint-action`, or carries a literal linter version (`golangci-lint...@vN` or `GOLANGCI_LINT_VERSION` followed by a `vN.N` literal).
**Rationale**: these are the ways the download or a second version can come back.

### The linter binary is passed in, not found on PATH

**Decision**: the script reads `GOLANGCI_LINT` (the task passes `bin/golangci-lint`) and falls back to `golangci-lint` on `PATH`. The proxy variables point at `127.0.0.1:9` for the linter run, as in the cli.

### Layout

`.github/scripts/` (already holds the repo's guard scripts and is under CODEOWNERS) for the check and its test; `.github/golangci-lint/` for the schema. Tasks: `dev:lint:config` (existing name, so `lint.yml`'s step and `AGENTS.md` stay valid) and `dev:lint:config:test`. `Makefile`'s `lint-config` runs the same script.

Reconcile phase impact (Source, Render, Apply, Prune, Status): none; no controller code changes.

## Risks / Trade-offs

- [`--schema` is a hidden flag and a release can drop it] → the check fails on the bump PR, not silently; `AGENTS.md` says that this needs a new design, never a dropped check.
- [The committed schema is 150 kB of vendored JSON] → one file per minor line, replaced on a minor bump; the check refuses a second one.
- [`Makefile` is edited although `Taskfile.yml` is authoritative] → two lines, so that it cannot name another version or bring the download back.
- [The install still needs the network] → out of scope; a failed install is a different failure than a failed schema download and existed before.
