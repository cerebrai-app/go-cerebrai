// Package buildinfo reports the version of the running program.
//
// Release builds link the version in (cerebrai-app/actions go-release.yml does
// this from the tag):
//
//	go build -ldflags "-X github.com/cerebrai-app/go-cerebrai/buildinfo.version=1.2.3"
//
// Without that, for example after `go install <module>@v1.2.3`, the version
// comes from the module version Go records in the binary.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// version is set with -ldflags -X. Its name is part of the release workflow's
// contract: renaming it silently drops the version from released binaries.
var version string

// Version returns the program's version without a leading "v", such as
// "1.2.3", or "dev" when no version is known (a plain `go build` or `go run`
// outside a version control checkout).
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	return fromBuildInfo(info, ok)
}

func fromBuildInfo(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return "dev"
	}
	switch v := info.Main.Version; v {
	case "", "(devel)":
		return "dev"
	default:
		return strings.TrimPrefix(v, "v")
	}
}
