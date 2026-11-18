# Plan 002: The README's Homebrew and Scoop instructions match the tap and bucket repositories

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 8928536..HEAD -- README.md`
> If `README.md` changed since this plan was written, compare the "Current
> state" excerpt against the live file before proceeding; on a mismatch, treat
> it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: docs
- **Planned at**: commit `8928536`, 2026-11-18

## Why this matters

`brainctl` and `workctl` are installed through two other repositories:
[cerebrai-app/homebrew-tap](https://github.com/cerebrai-app/homebrew-tap) and
[cerebrai-app/scoop-bucket](https://github.com/cerebrai-app/scoop-bucket). Both
had their READMEs rewritten on 2026-11-18 ("Document how to add, trust and
install from this repository"). This repository's README still has the older
instructions, and they disagree. For Homebrew it skips `brew tap`, `--cask` and
the Tap Trust step. For Scoop it uses a different local bucket name
(`cerebrai-app` instead of `cerebrai`). A user following this README gets
commands that differ from the tap's own docs, and may hit Homebrew's
untrusted-tap refusal with no explanation. After this plan, this README gives
the same commands as the tap and bucket READMEs and links to them for the
details.

## Current state

`README.md:24-40`, the two subsections to replace:

````markdown
### Homebrew (macOS and Linux)

```sh
brew install cerebrai-app/tap/brainctl
brew install cerebrai-app/tap/workctl
```

`brew upgrade` picks up new releases. The binaries are not signed by Apple, so the cask clears
macOS's quarantine flag on install; without that, macOS would refuse to run them.

### Scoop (Windows)

```powershell
scoop bucket add cerebrai-app https://github.com/cerebrai-app/scoop-bucket
scoop install cerebrai-app/brainctl
scoop install cerebrai-app/workctl
```
````

The next line (41) is blank, and line 42 is `### Prebuilt binaries`. Keep that
and everything after it unchanged.

The source of truth, quoted from `cerebrai-app/homebrew-tap` `README.md` (main,
2026-11-18):

> Homebrew 6 only loads non-official taps that you have trusted
> ([Tap Trust](https://docs.brew.sh/Tap-Trust)). Installing a cask by its full name trusts
> that one cask:
>
> ```sh
> brew tap cerebrai-app/tap
> brew install --cask cerebrai-app/tap/brainctl
> brew install --cask cerebrai-app/tap/workctl
> ```
>
> To trust the whole tap instead, including casks added to it later, so that you can use
> the short names: `brew tap cerebrai-app/tap`, `brew trust cerebrai-app/tap`,
> `brew install --cask brainctl workctl`.
>
> `brew upgrade` picks up new releases after `brew update`:
> `brew update`, `brew upgrade --cask brainctl workctl`.

And from `cerebrai-app/scoop-bucket` `README.md` (main, 2026-11-18):

> ```powershell
> scoop bucket add cerebrai https://github.com/cerebrai-app/scoop-bucket
> scoop install cerebrai/brainctl
> scoop install cerebrai/workctl
> ```
>
> `cerebrai` is the local name for the bucket, and `cerebrai/` installs from it even if
> another bucket has a manifest with the same name.
>
> Scoop has no separate trust step: adding the bucket is the decision to trust it.

**Conventions to match** (see the rest of `README.md`): lines wrap at about
100 characters. Prose is plain and second person ("you"). Shell blocks are
fenced with `sh` (or `powershell` for Windows) and have no `$` prompts. Links to
sibling repositories use full `https://github.com/cerebrai-app/...` URLs.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Show the section | `sed -n '22,44p' README.md` | the new Homebrew and Scoop subsections, then `### Prebuilt binaries` |
| Old commands gone | the `grep -nE` command in Step 2 (copy it from there) | no output |
| New commands present | `grep -c 'brew install --cask cerebrai-app/tap/' README.md` | `2` |

## Scope

**In scope** (the only file you should modify):
- `README.md`: only lines 24-40 (the `### Homebrew (macOS and Linux)` and
  `### Scoop (Windows)` subsections)

**Out of scope** (do NOT touch):
- The `homebrew-tap` and `scoop-bucket` repositories. They are the source of
  truth here, so don't edit them to match this one.
- `.goreleaser.yaml`. Cask and manifest names, URLs and the quarantine hook stay
  as they are.
- Every other README section, including "Prebuilt binaries", "With Go" and
  "Releasing".

## Git workflow

- Branch: `advisor/002-install-docs`, unless the operator names another.
- One commit. Subjects in this repo are plain imperative sentences, e.g.
  `Say plainly that config.yaml is not for secrets`. Use something like
  `Match the Homebrew and Scoop install steps to the tap and bucket`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Confirm the source of truth hasn't moved

If you can read the two upstream READMEs (a local clone of
`cerebrai-app/homebrew-tap` / `cerebrai-app/scoop-bucket`, or
`https://github.com/cerebrai-app/homebrew-tap/blob/main/README.md` and
`https://github.com/cerebrai-app/scoop-bucket/blob/main/README.md`), check that
their Install commands still match the quotes in "Current state". If you can't
read them, continue with the quotes.

**Verify**: the upstream install commands match the quoted ones exactly, or you
couldn't read them. If they differ, STOP.

### Step 2: Replace the two subsections

Replace `README.md` lines 24-40 (from `### Homebrew (macOS and Linux)` through
the closing fence of the Scoop block) with exactly:

````markdown
### Homebrew (macOS and Linux)

```sh
brew tap cerebrai-app/tap
brew install --cask cerebrai-app/tap/brainctl
brew install --cask cerebrai-app/tap/workctl
```

Homebrew only loads taps you have trusted; installing a cask by its full name, as above, trusts
that one cask. To trust the whole tap and use the short names, see
[homebrew-tap](https://github.com/cerebrai-app/homebrew-tap#install). Run `brew update` and then
`brew upgrade --cask brainctl workctl` to pick up new releases. The binaries are not signed by
Apple, so the cask clears macOS's quarantine flag on install; without that, macOS would refuse to
run them.

### Scoop (Windows)

```powershell
scoop bucket add cerebrai https://github.com/cerebrai-app/scoop-bucket
scoop install cerebrai/brainctl
scoop install cerebrai/workctl
```

`cerebrai` is the local name for the bucket; adding it is the decision to trust it. See
[scoop-bucket](https://github.com/cerebrai-app/scoop-bucket#install) for updating and removing.
````

Leave the blank line before `### Prebuilt binaries` as it is.

**Verify**:
- `grep -nE 'brew install cerebrai-app/tap/|scoop (bucket add|install) cerebrai-app' README.md` → no output
- `grep -c 'brew install --cask cerebrai-app/tap/' README.md` → `2`
- `grep -c 'scoop install cerebrai/' README.md` → `2`
- `grep -n '^### ' README.md | sed -n '1,4p'` → `### Homebrew (macOS and Linux)`, `### Scoop (Windows)`, `### Prebuilt binaries`, `### With Go`, in that order
- `awk 'length > 100' README.md` → no lines from the section you edited (lines that were already long elsewhere can stay)

### Step 3: Check nothing else changed

**Verify**: `git diff --stat` → `README.md | ...` only (plus `plans/README.md`), and
`git diff README.md | grep '^[-+]' | grep -v '^[-+][-+]' | wc -l` is between 15
and 45 (the advisor's dry run gave 22), so the change is just this section.

## Test plan

Docs only, so no Go tests. The `grep` checks in Step 2 are the tests. Optionally,
run `go test ./...` to confirm nothing else was touched. It should pass,
unchanged.

## Done criteria

ALL must hold:

- [ ] `grep -nE 'brew install cerebrai-app/tap/|scoop (bucket add|install) cerebrai-app' README.md` prints nothing
- [ ] `grep -c 'brew install --cask cerebrai-app/tap/' README.md` prints `2`
- [ ] `grep -c 'scoop install cerebrai/' README.md` prints `2`
- [ ] `README.md` links to `https://github.com/cerebrai-app/homebrew-tap#install` and `https://github.com/cerebrai-app/scoop-bucket#install`
- [ ] `git diff --stat` lists only `README.md` (plus `plans/README.md`)
- [ ] `plans/README.md` status row for 002 updated

## STOP conditions

Stop and report back (do not improvise) if:

- `README.md` lines 24-40 don't match the "Current state" excerpt.
- The upstream tap or bucket README (Step 1) gives different install commands
  from the ones quoted here. Then the right text has changed, and the advisor
  should re-plan.
- You find yourself wanting to change `.goreleaser.yaml`, the cask or manifest
  names, or the other repositories.

## Maintenance notes

- The tap and bucket READMEs are the detailed, maintained docs. This README
  only gives the shortest working commands and links there. When those READMEs
  change their install commands, update this section in the same change.
- The cask and manifest names come from `.goreleaser.yaml` (`homebrew_casks`
  and `scoops`, `name: brainctl` / `name: workctl`). Renaming them needs this
  section updated too.
- A reviewer should run the Homebrew commands on a Mac with Homebrew 6 if one
  is at hand. The advisor relied on the tap's README for the Tap Trust behavior
  and didn't check it against Homebrew itself.
