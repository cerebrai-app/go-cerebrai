# Plan 003: A `CLAUDE.md` records the commands and the contracts other repositories depend on

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat d4d3d48..HEAD -- .goreleaser.yaml internal/buildinfo/buildinfo.go internal/cli/cli.go brainconfig/config.go cmd/workctl/main.go .github/workflows/ README.md`
> These are the files `CLAUDE.md` describes. If any changed, re-check each fact
> in Step 1 against the live files before writing; on a mismatch, treat it as a
> STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none. Plan 002 also edits `README.md`, but this plan
  doesn't touch it, so they can run in either order.
- **Category**: dx
- **Planned at**: commit `d4d3d48`, 2026-11-18 (refreshed after plans 001 and 002 merged; first written at `8928536`)

## Why this matters

Most commits in this repository are written by coding agents. Several rules
that other repositories and third-party programs depend on live only in code
comments or in the long `README.md`, so an agent changing one file can break
something it never opened. For example:

- Renaming `internal/buildinfo.version` silently drops the version from every
  released binary.
- Changing what `brainctl version` prints fails every release.
- Changing the config file's lock or replace rules breaks extensions written in
  other languages.

Claude Code reads `CLAUDE.md` at the repository root automatically. A short one
that lists the verification commands and these contracts, pointing to where
each is documented, is a cheap guard. The repository has neither `CLAUDE.md`
nor `AGENTS.md` today.

## Current state

Each fact below was checked by the advisor at `d4d3d48`. Step 1 re-checks them.

- **Commands** (`README.md`, section "## Development", lines 401-414):
  `gofmt -l .`, `go vet ./...`, `go test -race ./...`, `go mod tidy -diff`,
  `go run ./cmd/brainctl version`. CI also runs golangci-lint (no config file in
  the repo, so it uses the defaults), govulncheck, a cross-build plus `go vet`
  for every release platform, and the tests on the `go` version from `go.mod`.
  It runs all of these through the reusable workflow
  `cerebrai-app/actions/.github/workflows/go-ci.yml`, pinned by commit in
  `.github/workflows/ci.yml:19`.
- **Version contract**: `.goreleaser.yaml:18` links the version in with
  `-X github.com/cerebrai-app/go-cerebrai/internal/buildinfo.version={{ .Version }}`.
  `internal/buildinfo/buildinfo.go:19-21` says: "Its name is part of the release
  workflow's contract: renaming it silently drops the version from released
  binaries." `internal/cli/cli.go:74-75` prints `<name> <version>` for
  `version`, with the comment "The release workflow checks this exact output."
  The shared `go-release.yml` runs each linux/amd64 binary with `version` and
  requires exactly `<binary> <version>`.
- **Release contract** (`.goreleaser.yaml:1-7` and `README.md` "## Releasing"):
  GoReleaser's `release` and `changelog` are disabled, because the shared
  workflow publishes the release itself. Archives must rebuild byte for byte
  (`mod_timestamp`, `-trimpath`, fixed archive mtimes), because the workflow
  rebuilds them to push Homebrew casks to `cerebrai-app/homebrew-tap` and Scoop
  manifests to `cerebrai-app/scoop-bucket`. Those files are generated, so don't
  edit them by hand.
- **Config file contract** (`README.md` "## Writing an extension", lines
  297-394, and the package doc in `brainconfig/config.go:1-58`): `brainconfig`
  is the module's only public package. Third-party extensions import it, and
  extensions in other languages implement the same rules by hand: the lock on
  `.config.yaml.lock` (flock / LockFileEx) and replacing the file through a
  temporary file and rename. The file format version is
  `brainconfig.SchemaVersion` (`brainconfig/config.go:85`, currently `1`), and
  workctl's settings version is `extensionSchemaVersion`
  (`cmd/workctl/main.go:19`, currently `1`). Raise one only when a change would
  make older releases misread the file.
- **Supported Go** (`README.md` "## Supported Go versions"): the `go` line in
  `go.mod` (`go 1.26.0`) is the oldest supported Go and is tested in CI with
  `GOTOOLCHAIN=local`. Raise it only when the code needs a newer release.
- **Code conventions** (seen throughout `internal/` and `brainconfig/`):
  - Doc comments are full sentences that start with the identifier.
  - Errors are wrapped with `%w` and name the file (`fmt.Errorf("%s: %w", path, err)`),
    and user-facing messages say what to do next ("run brainctl init first").
  - Tests replace package-level variables as seams, e.g. `install` in
    `internal/cli/cli.go:26`, `lookPath` in `internal/cli/extensions.go:18`, and
    `isTerminal`/`stdin` in `internal/cli/config.go`.
  - `README.md` is the user documentation, and behavior changes update it in
    the same commit.
- **Commit style** (`git log`): plain imperative subjects with no prefix, e.g.
  `Keep YAML anchors working; write .brain/.gitignore in init`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Paths in CLAUDE.md exist | see Step 3 | no output |
| Length | `wc -l < CLAUDE.md` | 70 or fewer |
| Nothing else changed | `git status --porcelain` | only `?? CLAUDE.md` (plus `plans/README.md`) |

## Scope

**In scope**:
- `CLAUDE.md` (create, at the repository root)

**Out of scope** (do NOT touch):
- `README.md`. `CLAUDE.md` points into it and doesn't copy it. Keeping the
  detail in one place stops the two from drifting apart.
- `AGENTS.md`. Don't create it as well. If the operator wants one for other
  agents, it's a one-line follow-up (see Maintenance notes).
- Any code, workflow or `.goreleaser.yaml` change, even if writing `CLAUDE.md`
  makes you notice something that looks wrong. Report it instead.

## Git workflow

- Branch: `advisor/003-claude-md`, unless the operator names another.
- One commit, e.g. `Add CLAUDE.md with the checks and cross-repository contracts`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Re-check the facts

Run each command and compare it with the expected result:

- `grep -n 'buildinfo.version' .goreleaser.yaml` → one line containing `-X github.com/cerebrai-app/go-cerebrai/internal/buildinfo.version={{ .Version }}`
- `grep -n 'contract' internal/buildinfo/buildinfo.go` → a line containing `part of the release`
- `grep -n 'checks this exact output' internal/cli/cli.go` → one match
- `grep -n 'SchemaVersion = ' brainconfig/config.go` → `SchemaVersion = 1`
- `grep -n 'const extensionSchemaVersion' cmd/workctl/main.go` → `= 1`
- `grep -n '^go ' go.mod` → `go 1.26.0`
- `grep -n 'uses: cerebrai-app/actions' .github/workflows/ci.yml .github/workflows/release.yml` → one `go-ci.yml@` and one `go-release.yml@` line
- `grep -n '^## ' README.md` → includes `## Writing an extension`, `## Supported Go versions`, `## Development` and `## Releasing`
- `ls CLAUDE.md AGENTS.md 2>&1` → both "No such file or directory"

**Verify**: every result matches. If any doesn't, STOP.

### Step 2: Write `CLAUDE.md`

Create `CLAUDE.md` at the repository root with exactly this content. If Step 1
showed a different version number, update only that number.

````markdown
# go-cerebrai

`brainctl` and `workctl` (`cmd/`), their shared code (`internal/`), and `brainconfig`, the one
public package, which third-party extensions import. `README.md` is the user documentation:
keep it in step with any behavior change, in the same commit.

## Checks

Run before every commit, from the repository root:

```sh
gofmt -l .                # must print nothing
go vet ./...
go test -race ./...
go mod tidy -diff         # must print nothing
go run ./cmd/brainctl version
```

CI (`.github/workflows/ci.yml`) also runs golangci-lint with its default settings, govulncheck,
a cross-build and `go vet` for every release platform, and the tests on the `go` version in
`go.mod`, through the shared `go-ci.yml` from cerebrai-app/actions. Change those checks there,
not here.

## Contracts other repositories depend on

- **Version.** `.goreleaser.yaml` sets `internal/buildinfo.version` with `-ldflags -X`, and
  the release workflow requires `<binary> version` to print exactly `<binary> <version>`
  (`internal/cli/cli.go`). Don't rename the variable or change that output.
- **Releases.** `.goreleaser.yaml` must keep `release` and `changelog` disabled and build
  reproducibly (`mod_timestamp`, `-trimpath`, fixed archive mtimes). The release workflow
  rebuilds the archives to push the Homebrew casks and Scoop manifests, and fails if they
  differ. Those files in cerebrai-app/homebrew-tap and cerebrai-app/scoop-bucket are
  generated: change `.goreleaser.yaml` instead. See `README.md`, "Releasing".
- **The config file.** `.brain/config.yaml` is shared with extensions in any language. Its
  rules (the `.config.yaml.lock` file lock, replacing the file through a temporary file and a
  rename, unknown keys kept, each extension writing only its own key) are in `README.md`,
  "Writing an extension", and in the package doc in `brainconfig/config.go`. Changing them
  breaks extensions you can't see. `brainconfig`'s exported API follows the module's version.
- **Schema versions.** Raise `brainconfig.SchemaVersion` (`brainconfig/config.go`) or
  `extensionSchemaVersion` (`cmd/workctl/main.go`) only when older releases would misread
  the file, never for an addition they can ignore.
- **Supported Go.** The `go` line in `go.mod` is the oldest supported Go, and CI tests it with
  `GOTOOLCHAIN=local`. Raise it only when the code needs a newer release.

## Conventions

- Doc comments are full sentences that start with the identifier.
- Wrap errors with `%w` and name the file (`fmt.Errorf("%s: %w", path, err)`); messages
  for users say what to do next ("run brainctl init first").
- Tests replace package-level variables as seams, e.g. `install` in
  `internal/cli/cli.go` and `lookPath` in `internal/cli/extensions.go`; follow that pattern.
- The config file is not for secrets: it is committed or backed up with the brain's data.
- Commit subjects are plain imperative sentences without a prefix, e.g. "Keep YAML anchors
  working; write .brain/.gitignore in init".
````

The ```` ```sh ```` block is part of the file's content. The file starts at
`# go-cerebrai` and ends with the "Commit subjects ..." bullet. The outer
four-backtick ```` ````markdown ```` fence is not part of the file.

**Verify**: `head -1 CLAUDE.md` → `# go-cerebrai`, and `tail -1 CLAUDE.md` →
`  working; write .brain/.gitignore in init".`

### Step 3: Check every path it names exists

```sh
grep -oE '`[A-Za-z0-9_./-]+\.(go|yaml|yml|md|mod)`' CLAUDE.md | tr -d '`' | sort -u \
  | grep -vx -e '.brain/config.yaml' -e 'go-ci.yml' \
  | while read -r p; do [ -e "$p" ] || echo "missing: $p"; done
```

**Verify**: no output. It checks `.github/workflows/ci.yml`, `.goreleaser.yaml`,
`README.md`, `brainconfig/config.go`, `cmd/workctl/main.go`, `go.mod`,
`internal/cli/cli.go` and `internal/cli/extensions.go`. Two names are skipped on
purpose because they aren't files in this repository: `.brain/config.yaml` is
in the user's brain home, and `go-ci.yml` is in cerebrai-app/actions.

### Step 4: Check its size and that nothing else changed

**Verify**:
- `wc -l < CLAUDE.md` → 70 or fewer
- `git status --porcelain` → `?? CLAUDE.md` and nothing else, apart from `plans/README.md` if you've already updated it
- `go test ./...` → still `ok` (nothing in the code changed)

## Test plan

No code changes, so no new Go tests. Steps 1 and 3 are the tests: Step 1 checks
the facts, and Step 3 checks that every file `CLAUDE.md` names exists.

## Done criteria

ALL must hold:

- [ ] `CLAUDE.md` exists at the repository root, starting with `# go-cerebrai`
- [ ] The Step 3 path check prints nothing
- [ ] `wc -l < CLAUDE.md` is 70 or fewer
- [ ] `grep -c 'buildinfo.version' CLAUDE.md` prints `1`, and `grep -c 'Writing an extension' CLAUDE.md` prints `1`
- [ ] `git status --porcelain` shows only `CLAUDE.md` (plus `plans/README.md`)
- [ ] `plans/README.md` status row for 003 updated

## STOP conditions

Stop and report back (do not improvise) if:

- Any Step 1 check doesn't match. The contract has changed, and `CLAUDE.md`
  would document something false.
- `CLAUDE.md` or `AGENTS.md` already exists. Merge, don't overwrite, and let the
  operator decide how.
- Writing it seems to need a change to `README.md`, code, workflows or
  `.goreleaser.yaml`.

## Maintenance notes

- `CLAUDE.md` names contracts and points to where each is documented. Keep the
  detail in `README.md` and the code comments, and update `CLAUDE.md` only when
  a contract is added, removed or moved. A PR that touches `.goreleaser.yaml`,
  `internal/buildinfo`, the `version` command, the lock or replace code in
  `brainconfig`, or a schema version should be checked against it.
- For agents that read `AGENTS.md` instead, add an `AGENTS.md` containing the
  single line `See CLAUDE.md.`, or rename the file and make `CLAUDE.md` contain
  `@AGENTS.md`. That's deferred until another agent actually works in this
  repository.
- The same kind of file would help `cerebrai-app/actions`, whose reusable
  workflows hold the other half of the version and release contracts. That's
  out of scope here.
