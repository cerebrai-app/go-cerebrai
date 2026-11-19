# go-cerebrai

The cerebrai-app command line programs in Go, `brainctl` and `workctl`, and the code they share.
They replace the Rust [brainctl](https://github.com/cerebrai-app/brainctl) and
[workctl](https://github.com/cerebrai-app/workctl), whose shared code is in
[cerebrai-crates](https://github.com/cerebrai-app/cerebrai-crates).

## Layout

| Path | What it is |
| --- | --- |
| [`cmd/brainctl`](cmd/brainctl), [`cmd/workctl`](cmd/workctl) | the two programs; each only calls `cli.Main` with its name (and `workctl` with its extension ID and the schema version of its settings) |
| [`internal/cli`](internal/cli) | the command line both programs share: `version`, `init` and `config`, and `brainctl`'s `extensions` |
| [`brainconfig`](brainconfig) | reads and writes the brain config file, `.brain/config.yaml`, for `brainctl` and its extensions, including [third-party ones](#writing-an-extension) |
| [`internal/python`](internal/python) | downloads, verifies and unpacks [python-build-standalone](https://github.com/astral-sh/python-build-standalone) distributions (the port of the `cerebrai-python` crate) |
| [`internal/buildinfo`](internal/buildinfo) | `buildinfo.Version()`: the version the release links in from the tag, else the module version from `go install`, else `dev` |

Shared code lives under `internal/`, so only these programs can import it and it has no API of
its own to version. The exception is `brainconfig`, which other programs import to be extensions of
`brainctl`; its API follows the module's version.

## Install

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

### Prebuilt binaries

Tagged releases publish both programs on the
[Releases page](https://github.com/cerebrai-app/go-cerebrai/releases), as
`<program>-v<version>-<os>-<arch>.tar.gz` (`.zip` for Windows) with one `checksums.txt`, for
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64` and `windows/arm64`. The Linux
binaries are static and run on any distribution.

```sh
gh release download --repo cerebrai-app/go-cerebrai --pattern 'brainctl-*-linux-amd64.tar.gz' --pattern checksums.txt
sha256sum --ignore-missing -c checksums.txt
gh attestation verify brainctl-*-linux-amd64.tar.gz --owner cerebrai-app   # built by this repository's release workflow
tar xzf brainctl-*.tar.gz
install -m 755 brainctl-*/brainctl ~/.local/bin/    # any directory on your PATH
```

On macOS, use `shasum -a 256 --ignore-missing -c` in place of `sha256sum --ignore-missing -c`.
On Windows, download the `.zip`, extract it, and put `brainctl.exe` in a directory on your `PATH`.

### With Go

```sh
go install github.com/cerebrai-app/go-cerebrai/cmd/brainctl@latest
go install github.com/cerebrai-app/go-cerebrai/cmd/workctl@latest
```

Check the install with `brainctl version`.

## Usage

`brainctl` and `workctl` have the same `version`, `init` and `config` commands; `extensions` is
`brainctl`'s alone.

```sh
brainctl version
brainctl init                              # Python 3.14, freethreaded+pgo+lto
brainctl init --python-version 3           # newest Python 3.x
brainctl init --python-version 3.13.9      # exact version
brainctl init --variant pgo+lto            # exact build options, no fallback
brainctl extensions ls                     # the extensions brainctl can discover (also l, list)
workctl config set env/client_id abc123    # set a value in workctl's settings
workctl config get env/client_id           # print it
workctl config unset env/client_id         # remove it
brainctl config set owner me               # the same for the brain config's top level
```

### `init`

First, `brainctl init` writes a config file to `.brain/config.yaml` in the brain home,
creating `.brain` if needed. The brain home is `$BRAIN_HOME` if set, otherwise the current
directory:

```sh
cd ~/notes && brainctl init               # writes ~/notes/.brain/config.yaml
export BRAIN_HOME=~/notes                 # then, from anywhere:
brainctl init && workctl init             # both use ~/notes/.brain/config.yaml
```

The config lives next to the brain's data so it is backed up or committed with it. `$BRAIN_HOME`
must name an existing directory (a relative path is taken from the current directory): a typo is an
error rather than a new, empty brain somewhere else.

The new file holds:

```yaml
schema_version: 1
```

`schema_version` is the file's format. If the file already exists, `init` leaves it unchanged but
checks that it is valid YAML with a `schema_version` this release supports. A file from a newer
release (a higher `schema_version`) is an error, so `init` stops before downloading anything.
Top-level keys `brainctl` doesn't know, whether from a newer release or your own, are ignored,
and kept when an extension edits the file.

**`config.yaml` is not for secrets.** It is meant to be committed or backed up with the brain's
data, so anyone who can read the repository or the backup can read it. Keep API keys, tokens and
passwords in environment variables or a password manager, and have extensions read them from
there.

`brainctl init` makes `.brain` and `config.yaml` readable by you alone (`0700` and `0600`). Loosen
them by hand to share the file; later edits keep whatever permissions it has. That is on Linux and
macOS: on Windows these permissions only set the read-only flag, and who can read the file depends
on the access rules `.brain` inherits from its parent folder.

Every edit replaces the file whole, through a temporary file, so a failed write never leaves it
half-written, and a new file only appears once it is complete. The config can be a symlink, say
into a dotfiles repository: edits go to the file it points to, and the link stays. An empty
file, which a crash in another program can leave, is filled in by the next `brainctl init`.

Programs that change the file at the same moment, say several extensions' `init` in one setup
script, take turns: each holds a lock on `.config.yaml.lock`, beside the config, from reading the
file until it has replaced it. The lock is the operating system's (`flock`, or `LockFileEx` on
Windows), so a crashed program never leaves it held; a change that waits over 10 seconds for one
gives up with an error. The lock file stays in `.brain`, so `brainctl init` writes
`.brain/.gitignore` listing `.config.yaml.lock` and `.config.yaml.*.tmp`, if there is no
`.brain/.gitignore` yet, so committing `.brain` leaves them out. It never changes one you have.

`workctl` is an extension of `brainctl` and shares its config file. `workctl init` adds its
settings under `extensions`, keyed by its ID `app.cerebrai.workctl`. Run `brainctl init` first:
`workctl init` never creates the file, and fails before downloading anything if it is missing.

```yaml
schema_version: 1
extensions:
  app.cerebrai.workctl:
    schema_version: 1
    name: workctl
    bin: workctl
```

Three keys are standard in every extension's settings:

- `schema_version` is the format of the extension's own settings, numbered independently of the
  file's `schema_version`.
- `name` is what to call the extension: one line of text, such as `workctl` or `Example Tool`.
- `bin` is the extension's executable, which `brainctl` can discover: a bare name, looked up on
  `PATH`, or an absolute path for one that isn't on `PATH`. A relative path with a directory in it
  (`bin/workctl`) is an error, since it would depend on the current directory. Extensions set
  themselves by name, so the file works on every machine it is backed up or committed to.

`workctl init` never changes existing settings for `app.cerebrai.workctl`, except to add `name`
or `bin` if one is missing: a `name` or `bin` you set yourself, say `/opt/tools/bin/workctl`, is
kept. It checks the settings first, though, and changes nothing if they are wrong: a valid `name`
and `bin`, values of the right type, and a `schema_version` this release of `workctl` supports.
Keys in its settings that `workctl` doesn't know, from a newer release or your own, are ignored
and kept. Adding the key rewrites the file: comments and key order are kept, but blank lines are
dropped and indentation becomes two spaces.

`brainctl` never looks inside `extensions`, beyond checking that it is a mapping, so it works with
any extension, including ones it has never heard of, and a broken extension's settings can't stop
it.

Then it downloads a [python-build-standalone](https://github.com/astral-sh/python-build-standalone)
distribution to `<data dir>/python-build-standalone/<version>+<build options>/` (for example
`3.14.10+freethreaded+pgo+lto`). The data dir is `$XDG_DATA_HOME` if set to an absolute path,
otherwise `%LOCALAPPDATA%` on Windows and `~/.local/share` elsewhere. It does nothing if the
install directory already exists.

`init` looks up the latest release on every run, so it needs network access even when everything
is already installed. It uses `HTTPS_PROXY` and the system's trusted certificates. A newer Python
patch version in a newer release installs into a new directory next to the old one. Older
directories are never removed (each is a few hundred MB): delete the ones you no longer need by
hand. A later release that rebuilds the *same* Python version is not picked up: `init` finds the
existing directory and does nothing, so delete it if you want the newer build.

`--python-version` takes a Python version (`3`, `3.14` or `3.14.15`), never a
python-build-standalone release date. The preferred build comes first, then the newest matching
version that has it: if a newer Python exists only as another build (say 3.15 without a
free-threaded build), `init` prints a note and installs the older one with the preferred build.
Pass `--variant` to require a specific build.

The default is the newest Python 3.14 `freethreaded+pgo+lto` build. If that build isn't published
for your platform (e.g. Windows has no LTO builds) it falls back to the next best build and says
so. Downloads are checked against the release's `SHA256SUMS`, which catches corrupt or truncated
downloads. The checksums come from the same GitHub release as the archive, so this does not
protect against a compromised release. Unpacking cannot write outside the install directory,
whatever paths or links the archive holds.

On Linux, `init` picks the glibc or musl build by checking which C library `/bin/sh` uses, since
the binaries themselves are static.

### `extensions`

`brainctl extensions ls` (or `l`, or `list`) lists the extensions with settings in the brain
home's config file, with each one's `name`, its `bin`, and where that is found: on `PATH` for a
bare name, or at its absolute path.

```console
$ brainctl extensions ls
EXTENSION             NAME          BIN                    PATH
app.cerebrai.workctl  workctl       workctl                /usr/local/bin/workctl
com.example.tool      Example Tool  /opt/example/bin/tool  not found
```

An extension that isn't installed shows as `not found` (or `not executable`) rather than making
the command fail: the config is the same file on every machine it is backed up or committed to,
and not every machine has every extension. An extension with no `name` shows `-`, one with no
`bin` shows `no bin set`, and one
whose settings are broken shows why, in its own row, without hiding the others. A missing or
invalid config file is an error. The table goes to stdout; with no extensions, stdout is empty
and a note goes to stderr.

### `config`

`config get`, `config set` and `config unset` read and write values in the brain home's config
file. A key path is keys separated by `/`: `env/client_id` is the `client_id` key in the `env`
mapping.

- `workctl config` works on `workctl`'s own settings, under `extensions.app.cerebrai.workctl`.
- `brainctl config` works on the file's top level, where your own keys go (`brainctl` ignores
  keys it doesn't know). It can read anything, but can't change `schema_version`, which it
  manages, or `extensions`, which holds the extensions' settings: change those with each
  extension's own `config` command.

```console
$ workctl config set env/client_id abc123
$ workctl config set env/debug true
$ workctl config set prompts/system - < system-prompt.txt
$ workctl config get env/client_id
abc123
$ workctl config get env
client_id: abc123
debug: "true"
$ workctl config unset env/debug
$ brainctl config set owner me
```

```yaml
schema_version: 1
extensions:
  app.cerebrai.workctl:
    schema_version: 1
    name: workctl
    bin: workctl
    env:
      client_id: abc123
owner: me
```

- `get` prints a value as it is, and a mapping or list as YAML. A key path that isn't set is an
  error (exit status 1), so scripts can tell it apart from an empty value.
- `set` creates the mappings on the way that are missing, and stores the value as a string: one
  that would read as another type, like `true` or `8080`, is quoted so it reads back the same.
  Setting the value a key already has leaves the file alone.
- A value of `-` is read from standard input: handy for a long or multi-line value, or one you'd
  rather keep out of your shell history and the process list. At a terminal, `set` prompts for it
  and doesn't echo what you type; otherwise it reads all of standard input, less one trailing
  newline, so you can redirect a file or pipe a command into it:
  `workctl config set prompts/system - < system-prompt.txt`. To set a literal `-`, pipe it in the
  same way (`printf - | workctl config set key -`). The value still goes into `config.yaml`, so
  this doesn't make it a place for secrets.
- `set` won't lose data: it never replaces a mapping or a list with a value, and a key path can't
  go through a value that isn't a mapping. `workctl` can't change `schema_version`, which it
  manages, and `name` and `bin` must be valid.
- `unset` removes the key and its value: a mapping or list goes with everything in it, and a
  mapping it leaves empty is kept, as `{}`. Unsetting a key that isn't set does nothing and isn't
  an error, so it is safe to repeat.
- YAML anchors you write by hand keep working: `set` keeps an anchor (`a: &x old`) on the value it
  replaces, so keys that refer to it (`b: *x`) follow the new value, and `unset` won't remove an
  anchor, or anything holding one, since what refers to it would then refer to nothing.
- Merge keys (`<<: *defaults`) are read as YAML reads them: `get` finds inherited keys. But `set`
  and `unset` won't change a mapping that has a merge key, since a new key there could hide what
  it inherits: edit such a mapping by hand.
- `workctl config` checks its settings first, as `init` does, and fails with "run workctl init
  first" if there are none yet. A failed `set` or `unset` changes nothing. Comments are kept,
  including one on a value replaced; a removed key's own comments go with it.

## Writing an extension

Any program can be an extension of `brainctl`, without changes to `brainctl`: it adds its own key
under `extensions` in `.brain/config.yaml` in the brain home, holding its settings and its
executable. The rules:

- **ID.** The key is the extension's ID: a reverse domain name for a domain its author controls,
  followed by the extension's name, so two authors never pick the same key. `workctl`, from
  `cerebrai.app`, is `app.cerebrai.workctl`; a tool from `example.com` would be
  `com.example.tool`, and one without a domain can use its code host, as in
  `io.github.some-user.tool`. An ID has at least three labels separated by dots, each of lower case
  letters and digits with single hyphens between them.
- **Shape.** The value is a mapping with a `schema_version`: a positive integer for the format of
  the extension's settings. The extension numbers its own versions, starting at 1, independently
  of the file's `schema_version` and of other extensions.
- **Set your `name` and `bin`.** Add them to your settings if they aren't there: a one-line `name`
  for people, and as `bin` your executable's name if it is installed on `PATH`, otherwise its
  absolute path. Never replace a `name` or `bin` the user set.
- **Find the file the same way.** The brain home is `$BRAIN_HOME` if set (an existing
  directory; a relative path is taken from the current directory), otherwise the current directory.
- **No secrets in your settings.** The file is committed or backed up with the brain's data. Read
  API keys, tokens and passwords from environment variables or the system's credential store
  instead.
- **Write only your own key.** Don't change the top-level keys or other extensions' settings, and
  keep content you don't understand, including keys you don't know, at the top level or in your
  own settings: ignore them rather than rejecting the file. Never create the file: if it is
  missing, fail and ask the user to run `brainctl init`.
- **Take the lock.** Hold an exclusive lock on `.config.yaml.lock`, beside the config file (beside
  the file a symlinked config points to), from reading the file until you have replaced it:
  `flock(LOCK_EX)` on Linux and macOS, `LockFileEx` on its first byte on Windows. Replace the
  file by writing a temporary file beside it and renaming it into place, so readers never see it
  half-written. A program that skips the lock, or an edit by hand while a command is changing the
  file, can lose a change without any error.
- **Check the versions.** Refuse a file whose top-level `schema_version` is newer than you know
  (currently `1`), and settings whose `schema_version` is newer than your release reads, rather
  than misreading them.

```yaml
schema_version: 1
extensions:
  app.cerebrai.workctl:
    schema_version: 1
    name: workctl
    bin: workctl
  com.example.tool:
    schema_version: 3
    name: Example Tool
    bin: /opt/example/bin/tool
    color: blue
```

Extensions written in Go can import [`brainconfig`](brainconfig), which follows these rules:

```go
import "github.com/cerebrai-app/go-cerebrai/brainconfig"

type settings struct {
	brainconfig.ExtensionBase `yaml:",inline"` // schema_version, name and bin
	Color                     string           `yaml:"color"`
}

dir, err := brainconfig.Home() // $BRAIN_HOME, or the current directory
if err != nil {
	return err
}
path := brainconfig.Path(dir)
// Read and check your settings first, so a bad file is left as it is; this
// rejects newer versions and values of the wrong type, and ignores keys
// settings has no field for. A missing file is an error
// matching brainconfig.ErrNoConfig, which asks the user to run brainctl init.
cfg, err := brainconfig.Load(path)
if err != nil {
	return err
}
var s settings
if _, err := cfg.DecodeExtension("com.example.tool", 3, &s); err != nil {
	return err
}
// Add your settings, or fill in a missing name or bin, keeping what the user set.
if _, err := brainconfig.AddExtension(path, "com.example.tool", brainconfig.ExtensionBase{
	SchemaVersion: 3,
	Name:          "Example Tool",
	Bin:           "tool",
}); err != nil {
	return err
}
```

For a `config` command like `workctl`'s, `cfg.Get(scope, keyPath)` reads a value by key path,
and `brainconfig.Set(path, scope, keyPath, value)` and `brainconfig.Unset(path, scope, keyPath)`
change one, with the same rules, where `scope` is `brainconfig.ExtensionSettings(id,
schemaVersion)` for your settings (`brainconfig.TopLevel` is the file's top level, for
`brainctl`). Every change through `brainconfig` takes the lock; `workctl`'s command is a thin layer over them in
[`internal/cli`](internal/cli).

Extensions in other languages edit the YAML directly. Use a library that keeps comments if you
can, since people may annotate the file.

## Supported Go versions

The `go` line in `go.mod` is the oldest supported Go. CI tests with that version and with the
latest stable Go, on Linux, macOS and Windows; releases are built with the latest stable Go. Raise it only when the code needs
a newer release, and keep it within the [releases Go supports](https://go.dev/doc/devel/release#policy).

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
go mod tidy -diff
go run ./cmd/brainctl version
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs these through the Go actions
from [cerebrai-app/actions](https://github.com/cerebrai-app/actions) (`go-lint`, `go-test`,
`go-cross-build` and `govulncheck`), along with golangci-lint, govulncheck, a build and `go vet`
for every release platform, and the tests on Linux, macOS and Windows, each also on the `go`
version from go.mod. Windows runs the tests without `-race`.

## Releasing

Both programs share one version, taken from the tag. Run [`release.sh`](release.sh) from any
branch with a clean working tree:

```sh
./release.sh patch        # or minor, major, or an exact version such as v1.0.0-rc1
./release.sh -n minor     # dry run: check everything, tag nothing
```

It switches to `main` and fast-forwards it to `origin/main`, then checks that the version is new
and higher than every earlier tag, and runs the checks from [`CLAUDE.md`](CLAUDE.md). Then it
shows the commits since the last release, asks for confirmation, and pushes an annotated tag. To
tag by hand instead:

```sh
git tag -a v0.1.0 -m v0.1.0 && git push origin v0.1.0
```

The `Release` workflow checks that the tag is on `main`, runs the tests, builds both programs for
every platform with [GoReleaser](https://goreleaser.com) (configured in
[`.goreleaser.yaml`](.goreleaser.yaml)), checks that each Linux x86_64 binary reports the tag's
version, and publishes one GitHub Release with all the archives, `checksums.txt` and a build
attestation for each archive. It then updates the Homebrew casks in
[homebrew-tap](https://github.com/cerebrai-app/homebrew-tap) and the Scoop manifests in
[scoop-bucket](https://github.com/cerebrai-app/scoop-bucket), using the GitHub App whose
credentials are the `RELEASE_APP_CLIENT_ID` and `RELEASE_APP_PRIVATE_KEY` secrets. Prerelease tags
(`v1.0.0-rc1`) skip the casks and manifests. To try a
change to the config locally:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish   # writes dist/
```
