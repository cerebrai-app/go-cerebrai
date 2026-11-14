// Command workctl is the workctl command line interface. Run "workctl help" for its
// commands.
package main

import (
	"os"

	"github.com/cerebrai-app/go-cerebrai/internal/cli"
)

// extensionID is workctl's key under extensions in the brain config file.
const extensionID = "app.cerebrai.workctl"

// extensionSchemaVersion is the newest format of workctl's settings in the
// brain config file (their schema_version) that this release
// writes and reads. Raise it when a change would make older releases misread
// them.
const extensionSchemaVersion = 1

func main() {
	os.Exit(cli.Main(cli.Program{
		Name:                   "workctl",
		ExtensionID:            extensionID,
		ExtensionSchemaVersion: extensionSchemaVersion,
	}, os.Args[1:], os.Stdout, os.Stderr))
}
