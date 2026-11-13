# go-cerebrai

Shared Go code for the cerebrai-app command line programs: the Go counterpart of
[cerebrai-crates](https://github.com/cerebrai-app/cerebrai-crates).

```sh
go get github.com/cerebrai-app/go-cerebrai@latest
```

## Packages

| Package | What it does |
| --- | --- |
| [`buildinfo`](buildinfo) | `buildinfo.Version()` returns the program's version: the one the release workflow links in from the tag, else the module version from `go install <module>@<version>`, else `dev` |

A program's `version` command prints `<name> <version>` using `buildinfo.Version()`. The
release workflow in [cerebrai-app/actions](https://github.com/cerebrai-app/actions) checks
that output before it publishes anything.

## Supported Go versions

The `go` line in `go.mod` is the oldest supported Go. CI tests with that version and with the
latest stable Go. Raise it only when the code needs a newer release, and keep it within the
[releases Go supports](https://go.dev/doc/devel/release#policy).

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
go mod tidy -diff
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs these through the shared
`go-ci.yml` workflow, along with golangci-lint and govulncheck.

## Releasing

Go modules are released by tag. There is nothing to build:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Before v1.0.0, a minor version may break the API. Programs pick up a new version with
`go get github.com/cerebrai-app/go-cerebrai@v0.1.0`, or through Renovate.
