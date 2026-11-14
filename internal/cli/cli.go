// Package cli is the command line shared by brainctl and workctl. workctl is
// an extension of brainctl: both use the same .brain/brainconfig.yaml, where
// workctl's init adds its settings under extensions.app.cerebrai.workctl. Each program describes itself with
// a Program, so this package keeps no list of extensions.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/python"
)

// install is python.Install; tests replace it.
var install = python.Install

// Program describes the program Main runs.
type Program struct {
	// Name is the program's name, as used in messages.
	Name string
	// ExtensionID is, for an extension of brainctl, its key under extensions
	// in the config file: a reverse domain name followed by its name, such as
	// app.cerebrai.workctl (see brainconfig.CheckID). Empty for brainctl.
	ExtensionID string
	// ExtensionSchemaVersion is, for an extension, the newest format of its
	// settings in the config file (the schema_version under its ID) that it
	// writes and reads. Raise it when a change would make older releases misread
	// the settings. Unused for brainctl.
	ExtensionSchemaVersion int
}

// extensionSettings is the settings an extension built on this package keeps
// in the config file.
type extensionSettings struct {
	SchemaVersion int `yaml:"schema_version"`
}

// Main runs prog with args (without the program name) and returns its exit
// code: 0 on success, 1 on failure, 2 on a usage error.
func Main(prog Program, args []string, stdout, stderr io.Writer) int {
	name := prog.Name
	usage := func() {
		_, _ = fmt.Fprintf(stderr, `usage: %[1]s <command> [flags]

commands:
  version   print the %[1]s version
  init      set up %[2]s in the current directory and download a
            python-build-standalone distribution

Run "%[1]s init -h" for the flags of init.
`, name, filepath.Join(brainconfig.DirName, brainconfig.FileName))
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
		return runInit(prog, args[1:], stderr)
	case "help", "-h", "-help", "--help":
		usage()
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "%s: unknown command %q\n", name, args[0])
	usage()
	return 2
}

func runInit(prog Program, args []string, stderr io.Writer) int {
	name := prog.Name
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
			name, initWrites(prog))
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
	if err := initConfig(prog, brainconfig.Path(dir), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if _, err := install(opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// initWrites says what init does to the config file for prog, for its usage.
func initWrites(prog Program) string {
	path := filepath.Join(brainconfig.DirName, brainconfig.FileName)
	if prog.ExtensionID == "" {
		return "Writes " + path + " in the current directory if it is missing"
	}
	return "Adds settings for extension " + prog.ExtensionID + " to " + path + "\n" +
		"in the current directory, which brainctl init must have created"
}

// initConfig makes sure the config file at path exists and, for an extension
// of brainctl, holds the extension's settings. Only brainctl creates the file:
// an extension fails if brainctl init has not run yet.
func initConfig(prog Program, path string, log io.Writer) error {
	id := prog.ExtensionID
	if id == "" {
		created, err := brainconfig.Init(path)
		if err != nil {
			return err
		}
		if created {
			_, _ = fmt.Fprintf(log, "Wrote %s\n", path)
		} else {
			_, _ = fmt.Fprintf(log, "Using existing %s\n", path)
		}
		return nil
	}
	added, err := brainconfig.AddExtension(path, id, prog.ExtensionSchemaVersion)
	if err != nil {
		return err
	}
	if !added {
		// AddExtension checked only the version; check the rest too.
		cfg, err := brainconfig.Load(path)
		if err != nil {
			return err
		}
		if _, err := cfg.DecodeExtension(id, prog.ExtensionSchemaVersion, new(extensionSettings)); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if added {
		_, _ = fmt.Fprintf(log, "Added settings for extension %s to %s\n", id, path)
	} else {
		_, _ = fmt.Fprintf(log, "Using existing settings for extension %s in %s\n", id, path)
	}
	return nil
}
