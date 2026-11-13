# go-cerebrai

The cerebrai-app command line programs in Go, `brainctl` and `workctl`, and the code they share.
They replace the Rust [brainctl](https://github.com/cerebrai-app/brainctl) and
[workctl](https://github.com/cerebrai-app/workctl), whose shared code is in
[cerebrai-crates](https://github.com/cerebrai-app/cerebrai-crates).

## Layout

| Path | What it is |
| --- | --- |
| [`cmd/brainctl`](cmd/brainctl) | the `brainctl` program |
| [`cmd/workctl`](cmd/workctl) | the `workctl` program |
| [`internal/buildinfo`](internal/buildinfo) | `buildinfo.Version()`: the version the release links in from the tag, else the module version from `go install`, else `dev` |

Shared code lives under `internal/`, so only these programs can import it and it has no API of
its own to version.

## Install

### Prebuilt binaries

Tagged releases publish both programs on the
[Releases page](https://github.com/cerebrai-app/go-cerebrai/releases), as
`<program>-v<version>-<os>-<arch>.tar.gz` (`.zip` for Windows) with a `.sha256` file each, for
`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64`. The Linux
binaries are static and run on any distribution.

```sh
gh release download --repo cerebrai-app/go-cerebrai --pattern 'brainctl-*-linux-amd64.tar.gz*'
sha256sum -c brainctl-*.tar.gz.sha256
tar xzf brainctl-*.tar.gz
install -m 755 brainctl-*/brainctl ~/.local/bin/    # any directory on your PATH
```

On macOS, use `shasum -a 256 -c` in place of `sha256sum -c`.

### With Go

```sh
go install github.com/cerebrai-app/go-cerebrai/cmd/brainctl@latest
go install github.com/cerebrai-app/go-cerebrai/cmd/workctl@latest
```

Check the install with `brainctl version`.

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
every platform, checks that each Linux x86_64 binary reports the tag's version, and publishes one
GitHub Release with all the archives.
