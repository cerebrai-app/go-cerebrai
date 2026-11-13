// Command brainctl is the brainctl command line interface. Run "brainctl help" for its
// commands.
package main

import (
	"os"

	"github.com/cerebrai-app/go-cerebrai/internal/cli"
)

func main() {
	os.Exit(cli.Main("brainctl", os.Args[1:], os.Stdout, os.Stderr))
}
