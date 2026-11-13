// Command brainctl is the brainctl command line interface.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
)

const usage = `usage: brainctl <command>

commands:
  version   print the brainctl version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command in args and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		// The release workflow checks this exact output.
		if _, err := fmt.Fprintf(stdout, "brainctl %s\n", buildinfo.Version()); err != nil {
			_, _ = fmt.Fprintln(stderr, "brainctl:", err)
			return 1
		}
		return 0
	}
	_, _ = fmt.Fprint(stderr, usage)
	return 2
}
