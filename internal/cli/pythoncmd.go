package cli

import (
	"fmt"
	"io"

	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

// installed is python.Installed; tests replace it.
var installed = python.Installed

// pythonUsage is the usage of the python command.
const pythonUsage = `usage: %s python <command>

commands:
  l, ls, list   list the installed python-build-standalone distributions,
                one directory per line, oldest Python version first
`

// runPython runs the python command with args (after "python").
func runPython(name string, args []string, stdout, stderr io.Writer) int {
	usage := func() { _, _ = fmt.Fprintf(stderr, pythonUsage, name) }
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "l", "ls", "list":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s python %s: unexpected arguments %q\n", name, args[0], args[1:])
			usage()
			return 2
		}
		dirs, root, err := installed()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if len(dirs) == 0 {
			_, _ = fmt.Fprintf(stderr, "No Python is installed in %s; run %s init\n", root, name)
			return 0
		}
		for _, d := range dirs {
			if _, err := fmt.Fprintln(stdout, d); err != nil {
				_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
				return 1
			}
		}
		return 0
	case "help", "-h", "-help", "--help":
		usage()
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "%s python: unknown command %q\n", name, args[0])
	usage()
	return 2
}
