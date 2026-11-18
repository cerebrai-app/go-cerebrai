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
