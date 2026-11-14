// Package cli is the command line shared by brainctl and workctl, which
// differ only in their name. workctl is an extension of brainctl: both use
// the same .brain/config.yaml, where workctl's init adds extensions.workctl.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/config"
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
  init      set up %[2]s in the current directory and download a
            python-build-standalone distribution

Run "%[1]s init -h" for the flags of init.
`, name, filepath.Join(config.DirName, config.FileName))
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
			"%s, then downloads a python-build-standalone\n"+
			"distribution into the data directory.\n\nflags:\n",
			name, initWrites(name))
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
	// The config comes first: it is quick and needs no network, so a directory
	// that cannot be written fails before the download.
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := initConfig(name, config.Path(dir), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if _, err := install(opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// initWrites says what init does to the config file for the program called
// name, for its usage.
func initWrites(name string) string {
	path := filepath.Join(config.DirName, config.FileName)
	if name == config.Base {
		return "Writes " + path + " in the current directory if it is missing"
	}
	return "Adds extensions." + name + " to " + path + " in the current directory\n" +
		"(creating the file if it is missing)"
}

// extensionSchemaVersions is, for each extension of brainctl, the newest
// format of its settings in the config file (extensions.<name>.schema_version)
// that this release writes and reads. Raise one when a change would make older
// releases of that extension misread its settings.
var extensionSchemaVersions = map[string]int{
	"workctl": 1,
}

// initConfig makes sure the config file at path exists and, for an extension
// of brainctl, holds the extension's key. An extension creates the file too if
// brainctl's init has not run yet.
func initConfig(name, path string, log io.Writer) error {
	created, err := config.Init(path)
	if err != nil {
		return err
	}
	if created {
		_, _ = fmt.Fprintf(log, "Wrote %s\n", path)
	}
	if name == config.Base {
		if !created {
			_, _ = fmt.Fprintf(log, "Using existing %s\n", path)
		}
		return nil
	}
	version, ok := extensionSchemaVersions[name]
	if !ok {
		return fmt.Errorf("%s is not a known extension of %s", name, config.Base)
	}
	added, err := config.AddExtension(path, name, version)
	if err != nil {
		return err
	}
	if added {
		_, _ = fmt.Fprintf(log, "Added extensions.%s to %s\n", name, path)
	} else {
		_, _ = fmt.Fprintf(log, "Using existing extensions.%s in %s\n", name, path)
	}
	return nil
}
