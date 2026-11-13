// Command workctl is the workctl command line interface. Run "workctl help" for its
// commands.
package main

import (
	"os"

	"github.com/cerebrai-app/go-cerebrai/internal/cli"
)

func main() {
	os.Exit(cli.Main("workctl", os.Args[1:], os.Stdout, os.Stderr))
}
