# go-cerebrai

The cerebrai-app command line programs in Go, `brainctl` and `workctl`, and the code they share.
They replace the Rust [brainctl](https://github.com/cerebrai-app/brainctl) and
[workctl](https://github.com/cerebrai-app/workctl), whose shared code is in
[cerebrai-crates](https://github.com/cerebrai-app/cerebrai-crates).

## Layout

| Path | What it is |
| --- | --- |
| [`cmd/brainctl`](cmd/brainctl), [`cmd/workctl`](cmd/workctl) | the two programs; each only calls `cli.Main` with its name |
| [`internal/cli`](internal/cli) | the command line both programs share: `version` and `init` |
| [`internal/python`](internal/python) | downloads, verifies and unpacks [python-build-standalone](https://github.com/astral-sh/python-build-standalone) distributions (the port of the `cerebrai-python` crate) |
| [`internal/buildinfo`](internal/buildinfo) | `buildinfo.Version()`: the version the release links in from the tag, else the module version from `go install`, else `dev` |

Shared code lives under `internal/`, so only these programs can import it and it has no API of
its own to version.

## Install

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

### Prebuilt binaries

Tagged releases publish both programs on the
[Releases page](https://github.com/cerebrai-app/go-cerebrai/releases), as
`<program>-v<version>-<os>-<arch>.tar.gz` (`.zip` for Windows) with one `checksums.txt`, for
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64`. The Linux
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

`brainctl` and `workctl` have the same commands.

```sh
brainctl version
brainctl init                              # Python 3.14, freethreaded+pgo+lto
brainctl init --python-version 3           # newest Python 3.x
brainctl init --python-version 3.13.9      # exact version
brainctl init --variant pgo+lto            # exact build options, no fallback
```

### `init`

Downloads a [python-build-standalone](https://github.com/astral-sh/python-build-standalone)
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

## Supported Go versions

The `go` line in `go.mod` is the oldest supported Go. CI tests with that version and with the
latest stable Go; releases are built with the latest stable Go. Raise it only when the code needs
a newer release, and keep it within the [releases Go supports](https://go.dev/doc/devel/release#policy).

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
go mod tidy -diff
go run ./cmd/brainctl version
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs these through the shared
`go-ci.yml` workflow from [cerebrai-app/actions](https://github.com/cerebrai-app/actions), along
with golangci-lint, govulncheck, a build and `go vet` for every release platform, and the tests
on the `go` version from go.mod.

## Releasing

Both programs share one version, taken from the tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
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
