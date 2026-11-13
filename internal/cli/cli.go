// Package cli is the command line shared by brainctl and workctl, which
// differ only in their name.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

// install is python.Install; tests replace it.
var install = python.Install

// Main runs the program called name with args (without the program name) and
// returns its exit code: 0 on success, 1 on failure, 2 on a usage error.
func Main(name string, args []string, stdout, stderr io.Writer) int {
	usage := func() {
		_, _ = fmt.Fprintf(stderr, `usage: %[1]s <command> [flags]

commands:
  version   print the %[1]s version
  init      set up the local environment (downloads a python-build-standalone distribution)

Run "%[1]s init -h" for the flags of init.
`, name)
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			usage()
			return 2
		}
		// The release workflow checks this exact output.
		if _, err := fmt.Fprintf(stdout, "%s %s\n", name, buildinfo.Version()); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "init":
		return runInit(name, args[1:], stderr)
	case "help", "-h", "-help", "--help":
		usage()
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "%s: unknown command %q\n", name, args[0])
	usage()
	return 2
}

func runInit(name string, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet(name+" init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts := python.Options{Log: stderr}
	fs.StringVar(&opts.PythonVersion, "python-version", python.DefaultPythonVersion,
		`Python version to install: "3", "3.14" or exact like "3.14.15". Picks the newest version
that has the preferred build; a note is printed if a newer one exists only as another build`)
	fs.StringVar(&opts.Variant, "variant", "",
		`exact build options to require, e.g. "pgo+lto" or "freethreaded+pgo+lto"
(default `+python.DefaultVariant+`, falling back to the best available build)`)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "usage: %s init [flags]\n\n"+
			"Downloads a python-build-standalone distribution into the data directory.\n\nflags:\n", name)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "%s init: unexpected arguments %q\n", name, fs.Args())
		fs.Usage()
		return 2
	}
	if _, err := install(opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
