# Plan 001: A quoted `"<<"` key no longer blocks config edits

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 8928536..HEAD -- brainconfig/config.go brainconfig/values.go brainconfig/values_test.go internal/cli/config_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `8928536`, 2026-11-18

## Why this matters

`brainctl config set '<<' x` succeeds and writes `"<<": x` (correctly quoted)
to `.brain/config.yaml`. But after that, every later `brainctl config set` or
`unset` at the top level fails with "the top of its scope uses a YAML merge key
(<<), so changing it here could hide what it inherits: edit the file by hand".
`workctl config` behaves the same inside its own settings. A file where
someone wrote a quoted `"<<"` (or `'<<'`) key by hand gets the same treatment.

The cause: the code finds YAML's merge key by comparing a key's **text** with
`<<`. A quoted `"<<"` has the same text but is an ordinary string key. YAML (and
the `go.yaml.in/yaml/v3` parser) tells them apart by **tag**: a plain `<<` key
node has tag `!!merge`, and a quoted `"<<"` or `'<<'` has tag `!!str`. After
this plan, only a real merge key counts as one, and a quoted `"<<"` is a key like
any other.

## Current state

Package `brainconfig` (`brainconfig/`) reads and writes the config file. It
works on `yaml.Node` trees from `go.yaml.in/yaml/v3`. A mapping node's `Content`
alternates key node, value node, key node, value node, and so on.

These are the four places that find a key by its text. All four need to tell a
merge key apart:

`brainconfig/config.go:574-582`: the general key lookup, used everywhere:

```go
// mappingValue is the value of key in the mapping node m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
```

`brainconfig/values.go:71-79` in `mergedValue`, which looks up a key the
way YAML reads it, including keys inherited through a merge key:

```go
func mergedValue(m *yaml.Node, key string) *yaml.Node {
	if v := mappingValue(m, key); v != nil {
		return v
	}
	merge := mappingValue(m, "<<")
	if merge == nil {
		return nil
	}
```

`brainconfig/values.go:100-103` in `checkNoMerge`, which refuses edits to a
mapping that has a merge key. This is the function that wrongly refuses:

```go
func checkNoMerge(m *yaml.Node, keys []string) error {
	if m.Kind != yaml.MappingNode || mappingValue(m, "<<") == nil {
		return nil
	}
```

`brainconfig/values.go:398-399` in `unsetIn`, which scans the keys directly:

```go
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == keys[last] {
```

How yaml.v3 tags these keys. The advisor checked this with yaml.v3 v3.0.6,
the version in `go.mod`:

| YAML written | key node `.Value` | key node `.ShortTag()` |
|---|---|---|
| `<<: *b` | `<<` | `!!merge` |
| `!!merge <<: {x: 1}` | `<<` | `!!merge` |
| `"<<": x` | `<<` | `!!str` |
| `'<<': x` | `<<` | `!!str` |

When this package creates a key, it uses `scalar(key)` (config.go:585-587,
tag `!!str`), and the encoder writes a `!!str` key `<<` as `"<<"`. So setting a
key named `<<` already produces a plain string key. Only the reading side is
wrong.

**Conventions to match** (see `brainconfig/values.go`): every function has a doc
comment in full sentences that starts with its name. Unexported helpers sit next
to the code that uses them. Tests are table-driven or step lists using the
package's helpers `writeConfig`, `readFile`, `withSettings`, `workctlID` and
`ptr` (in `brainconfig/config_test.go` and `brainconfig/values_test.go`). The
existing merge-key test, `TestMergeKeys` (`brainconfig/values_test.go:472-524`),
must keep passing unchanged. It proves real merge keys are still handled.

## Commands you will need

Run from the repository root.

| Purpose | Command | Expected on success |
|---|---|---|
| Format check | `gofmt -l .` | no output |
| Vet | `go vet ./...` | exit 0, no output |
| Focused tests | `go test ./brainconfig ./internal/cli -run 'QuotedMergeKey\|MergeKeyIsNotAKey\|TestMergeKeys'` | `ok` for both packages |
| All tests | `go test -race ./...` | `ok` for brainconfig, internal/buildinfo, internal/cli, internal/python |
| Tidy | `go mod tidy -diff` | exit 0, no output |

## Scope

**In scope** (the only files you should modify):
- `brainconfig/config.go`: only the `mappingValue` function
- `brainconfig/values.go`: add `isMergeKey` and `mergeValue`, and change
  `mergedValue`, `checkNoMerge` and `unsetIn` to use them
- `brainconfig/values_test.go`: append two tests
- `internal/cli/config_test.go`: append one test

**Out of scope** (do NOT touch):
- `README.md`: the documented behavior ("`set` and `unset` won't change a
  mapping that has a merge key") stays true. Only the detection is fixed.
- `splitKeyPath` in `values.go`: do **not** forbid `<<` in key paths. A quoted
  `"<<"` is a legal key, and after this fix it works like any other.
- Any change to how merge keys themselves are handled (refusing edits, reading
  inherited keys). `TestMergeKeys` pins that behavior.
- `go.mod` / `go.sum`: no new dependencies.

## Git workflow

- Branch: `advisor/001-quoted-merge-key`, unless the operator names another.
- One commit is fine. This repo's commit subjects are plain imperative
  sentences with no prefix, e.g. `Keep YAML anchors working; write
  .brain/.gitignore in init`. Use something like `Tell a quoted "<<" key from a
  YAML merge key`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the failing tests

Append this to the end of `brainconfig/values_test.go`. The file already
imports `errors`, `strings` and `testing`.

```go
// A quoted "<<" is an ordinary key, not YAML's merge key: it can be read,
// removed, and does not stop changes to the mapping that holds it.
func TestQuotedMergeKeyIsAPlainKey(t *testing.T) {
	scope := ExtensionSettings(workctlID, 1)
	for _, quoted := range []string{`"<<"`, `'<<'`} {
		path := writeConfig(t, withSettings("    "+quoted+": x\n"))
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := cfg.Get(scope, "<<"); err != nil || got != "x" {
			t.Errorf("%s: get << = %q, %v; want x", quoted, got, err)
		}
		if _, err := Set(path, scope, "color", "blue"); err != nil {
			t.Errorf("%s: set color: %v", quoted, err)
		}
		if _, err := Unset(path, scope, "<<"); err != nil {
			t.Errorf("%s: unset <<: %v", quoted, err)
		}
		if got, want := readFile(t, path), withSettings("    color: blue\n"); got != want {
			t.Errorf("%s: file = %q, want %q", quoted, got, want)
		}
	}

	// Setting a key named << writes it quoted, and later changes still work.
	path := writeConfig(t, "schema_version: 1\n")
	for _, kv := range [][2]string{{"<<", "x"}, {"owner", "me"}} {
		if _, err := Set(path, TopLevel, kv[0], kv[1]); err != nil {
			t.Fatalf("set %s: %v", kv[0], err)
		}
	}
	if got, want := readFile(t, path), "schema_version: 1\n\"<<\": x\nowner: me\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("load: %v", err)
	}
}

// A real merge key (<< written plainly) is not a key of its own: getting <<
// finds nothing, and unsetting it is refused like any change to that mapping.
func TestMergeKeyIsNotAKey(t *testing.T) {
	const body = "    defaults: &d {color: blue}\n    profile:\n      <<: *d\n"
	path := writeConfig(t, withSettings(body))
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := ExtensionSettings(workctlID, 1)
	if _, err := cfg.Get(scope, "profile/<<"); !errors.Is(err, ErrNotSet) {
		t.Errorf("get profile/<<: %v, want ErrNotSet", err)
	}
	if _, err := Unset(path, scope, "profile/<<"); err == nil || !strings.Contains(err.Error(), "uses a YAML merge key (<<)") {
		t.Errorf("unset profile/<<: %v", err)
	}
	if got, want := readFile(t, path), withSettings(body); got != want {
		t.Errorf("file changed to %q", got)
	}
}
```

Append this to the end of `internal/cli/config_test.go`. It uses that
package's existing helpers `project`, `writeBrainConfig`, `runBrainctl` and
`brainConfig`.

```go
// brainctl config can set a key named <<, which is written quoted, and the
// file can still be changed afterwards.
func TestBrainctlConfigQuotedMergeKey(t *testing.T) {
	project(t)
	writeBrainConfig(t, "schema_version: 1\n")
	for _, args := range [][]string{{"config", "set", "<<", "x"}, {"config", "set", "owner", "me"}} {
		if code, _, stderr := runBrainctl(args...); code != 0 || stderr != "" {
			t.Errorf("%q: got %d, %q", args, code, stderr)
		}
	}
	if code, stdout, stderr := runBrainctl("config", "get", "<<"); code != 0 || stdout != "x\n" {
		t.Errorf("get <<: got %d, %q, %q", code, stdout, stderr)
	}
	if got, want := brainConfig(t), "schema_version: 1\n\"<<\": x\nowner: me\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}
```

**Verify**: `go test ./brainconfig ./internal/cli -run 'QuotedMergeKey|MergeKeyIsNotAKey'`
→ **FAIL**, with exactly these three tests failing: `TestQuotedMergeKeyIsAPlainKey`,
`TestMergeKeyIsNotAKey` (message `get profile/<<: <nil>, want ErrNotSet`) and
`TestBrainctlConfigQuotedMergeKey` (the config is missing `owner: me`). If they
fail to **compile**, or any of them passes, STOP.

### Step 2: Add `isMergeKey` and `mergeValue`

In `brainconfig/values.go`, directly above the `// checkNoMerge returns ...` doc
comment (currently line 96), insert:

```go
// isMergeKey reports whether k, a key in a mapping, is YAML's merge key: <<
// written plainly (or tagged !!merge). A quoted "<<" is an ordinary key.
func isMergeKey(k *yaml.Node) bool {
	return k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge"
}

// mergeValue is the value of the merge key in the mapping m (what <<: refers
// to), or nil if m has none.
func mergeValue(m *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if isMergeKey(m.Content[i]) {
			return m.Content[i+1]
		}
	}
	return nil
}
```

Use `ShortTag()`, not `.Tag ==`. `ShortTag()` also normalizes the long form
`tag:yaml.org,2002:merge`.

**Verify**: `go vet ./brainconfig` → exit 0 (the helpers are unused for now, which
vet allows).

### Step 3: Use them in the four lookups

Make exactly these edits:

1. `brainconfig/values.go`, in `mergedValue`:
   `merge := mappingValue(m, "<<")` → `merge := mergeValue(m)`
2. `brainconfig/values.go`, in `checkNoMerge`:
   `if m.Kind != yaml.MappingNode || mappingValue(m, "<<") == nil {` →
   `if m.Kind != yaml.MappingNode || mergeValue(m) == nil {`
3. `brainconfig/values.go`, in `unsetIn`:
   `if parent.Content[i].Value == keys[last] {` →
   `if !isMergeKey(parent.Content[i]) && parent.Content[i].Value == keys[last] {`
4. `brainconfig/config.go`, `mappingValue`: change the doc comment and the
   condition to:

   ```go
   // mappingValue is the value of key in the mapping node m, or nil. A merge
   // key (<<: *base) is not a key of its own: see mergeValue.
   func mappingValue(m *yaml.Node, key string) *yaml.Node {
   	for i := 0; i+1 < len(m.Content); i += 2 {
   		if !isMergeKey(m.Content[i]) && m.Content[i].Value == key {
   ```

**Verify**:
- `grep -n 'mappingValue(m, "<<")' brainconfig/*.go` → no output
- `go test ./brainconfig ./internal/cli -run 'QuotedMergeKey|MergeKeyIsNotAKey|TestMergeKeys'` → `ok` for both packages

### Step 4: Run the full checks

**Verify** (all from the repository root):
- `gofmt -l .` → no output
- `go vet ./...` → exit 0
- `go test -race ./...` → `ok` for all four packages with tests
- `go mod tidy -diff` → exit 0, no output

## Test plan

- `TestQuotedMergeKeyIsAPlainKey` (brainconfig): with a double- or single-quoted
  `"<<"` key in an extension's settings, `Get` reads it, `Set` of a sibling key
  succeeds, and `Unset` removes the quoted key. Setting a top-level `<<` and
  then `owner` writes `"<<": x` and leaves a loadable file. This is the
  regression.
- `TestMergeKeyIsNotAKey` (brainconfig): a real merge key isn't readable as key
  `<<`, and unsetting it is refused without changing the file. This guards the
  new `mappingValue` and `unsetIn` behavior.
- `TestBrainctlConfigQuotedMergeKey` (internal/cli): the end-to-end bug report
  through the `brainctl` command.
- `TestMergeKeys` (existing, unchanged) must still pass. It proves real merge
  keys are still detected and refused.

## Done criteria

ALL must hold:

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` exits 0
- [ ] `go test -race ./...` exits 0, and `go test ./brainconfig ./internal/cli -run 'QuotedMergeKey|MergeKeyIsNotAKey' -v 2>&1 | grep -c '^--- PASS'` prints `3`
- [ ] `grep -n 'mappingValue(m, "<<")' brainconfig/*.go` prints nothing
- [ ] `git diff --stat` lists only `brainconfig/config.go`, `brainconfig/values.go`, `brainconfig/values_test.go`, `internal/cli/config_test.go` (plus `plans/README.md`)
- [ ] `plans/README.md` status row for 001 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts in "Current state" don't match the live code (drift).
- In Step 1 the new tests don't fail as described, or fail for a different
  reason, such as a missing helper.
- `TestMergeKeys` or any other existing test fails after Step 3. That would
  mean real merge keys are no longer detected, so don't adjust the old test to
  make it pass.
- A key node for a plain `<<` turns out not to have `ShortTag() == "!!merge"`,
  for example after a yaml.v3 upgrade in `go.mod`.
- The fix seems to need changes to `README.md`, `splitKeyPath`, or any file not
  listed in Scope.

## Maintenance notes

- Any new code that looks up a key by text should go through `mappingValue`,
  which now skips merge keys, or must check `isMergeKey` itself. A direct
  `.Value == key` scan over `Content` reintroduces this bug.
- A reviewer should check that the `!isMergeKey` guard was added in exactly
  `mappingValue` and `unsetIn`, and that `mergedValue`/`checkNoMerge` now use
  `mergeValue`.
- If the yaml library is ever replaced or upgraded across a major version,
  re-run `TestMergeKeys` and `TestMergeKeyIsNotAKey`. Merge-key detection
  depends on how the parser tags `<<`.
