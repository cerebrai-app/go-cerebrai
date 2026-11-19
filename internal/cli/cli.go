// Package cli is the command line shared by brainctl and workctl. workctl is
// an extension of brainctl: both use the same .brain/config.yaml in the brain
// directory ($BRAIN_HOME, or the current directory), where workctl's init adds
// its settings under extensions.app.cerebrai.workctl. Each program describes
// itself with a Program, so this package keeps no list of extensions.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
	"github.com/cerebrai-app/go-cerebrai/internal/uv"
)

// brainconfigFile is the config file's path inside the brain home.
var brainconfigFile = filepath.Join(brainconfig.DirName, brainconfig.FileName)

// setUp is uv.SetUp; tests replace it.
var setUp = uv.SetUp

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

// Main runs prog with args (without the program name) and returns its exit
// code: 0 on success, 1 on failure, 2 on a usage error.
func Main(prog Program, args []string, stdout, stderr io.Writer) int {
	name := prog.Name
	usage := func() {
		extensions := "  config      get, set and unset values in the settings for extension " + prog.ExtensionID + "\n"
		if prog.ExtensionID == "" {
			extensions = "  config      get, set and unset values in the brain config\n" +
				"  extensions  list the extensions in the brain config and their executables\n"
		}
		_, _ = fmt.Fprintf(stderr, `usage: %[1]s <command> [flags]

commands:
  version     print the %[1]s version
  init        set up %[2]s in the brain home, and install uv and
              Python into the brainctl directory
%[4]s
The brain home is $%[3]s if set, otherwise the current directory.
Run "%[1]s <command> -h" for more about a command.
`, name, brainconfigFile, brainconfig.EnvVar, extensions)
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
	case "extensions":
		// brainctl's alone: its extensions have no extensions of their own.
		if prog.ExtensionID == "" {
			return runExtensions(name, args[1:], stdout, stderr)
		}
	case "config":
		return runConfig(prog, args[1:], stdout, stderr)
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
	opts := uv.Options{Log: stderr}
	fs.StringVar(&opts.Python, "python-version", uv.DefaultPython,
		`Python to install with uv: a version such as "3.14", "3.13.9", or "3.14t" for the
free-threaded build`)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "usage: %s init [flags]\n\n"+
			"%s, then installs uv %s, and Python with it, into the\n"+
			"brainctl directory (~/.local/opt/brainctl, or %%LOCALAPPDATA%%\\brainctl on Windows),\n"+
			"which nothing outside it uses.\n\nflags:\n",
			name, initWrites(prog), uv.Version)
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
	dir, err := brainconfig.Home()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := initConfig(prog, brainconfig.Path(dir), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if _, err := setUp(opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "\n%s is ready in %s\n", name, dir)
	if os.Getenv(brainconfig.EnvVar) == "" {
		_, _ = fmt.Fprintf(stderr, "\nNext, set $%s so %s finds this brain from any directory.\n", brainconfig.EnvVar, name)
		switch profile, line := shellHint(runtime.GOOS, os.Getenv("SHELL"), dir); {
		case line == "":
			_, _ = fmt.Fprintf(stderr, "Set it to %s in your shell's startup file.\n", dir)
		case profile == "":
			_, _ = fmt.Fprintf(stderr, "Run this once, then open a new terminal:\n\n    %s\n", line)
		default:
			_, _ = fmt.Fprintf(stderr, "Add this line to %s, then open a new terminal:\n\n    %s\n", profile, line)
		}
	}
	return 0
}

// shellHint returns the shell profile to edit and the line that sets the
// brain home to dir there, for the user's OS and $SHELL. The profile is empty
// for a command to run once instead (Windows), and the line is empty for a
// shell it doesn't know the syntax of.
func shellHint(goos, shell, dir string) (profile, line string) {
	env := brainconfig.EnvVar
	if goos == "windows" {
		// A trailing \ would escape the closing quote; \\" is a \ then the quote.
		if strings.HasSuffix(dir, `\`) {
			dir += `\`
		}
		// setx stores it for the user; there is no profile to edit.
		return "", `setx ` + env + ` "` + dir + `"`
	}
	export := "export " + env + "='" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
	switch filepath.Base(shell) {
	case "fish":
		// In fish single quotes, \ and ' are escaped with a backslash.
		quoted := strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(dir)
		return "~/.config/fish/config.fish", "set -gx " + env + " '" + quoted + "'"
	case "zsh":
		return "~/.zshrc", export
	case "bash":
		if goos == "darwin" {
			return "~/.bash_profile", export
		}
		return "~/.bashrc", export
	case "sh", "dash", "ksh", "mksh":
		return "~/.profile", export
	}
	return "", ""
}

// initWrites says what init does to the config file for prog, for its usage.
func initWrites(prog Program) string {
	path := brainconfigFile
	if prog.ExtensionID == "" {
		return "Writes " + path + " in the brain home ($" + brainconfig.EnvVar + ",\n" +
			"or the current directory) if it is missing"
	}
	return "Adds settings for extension " + prog.ExtensionID + ", with name and bin " + prog.Name + ",\n" +
		"in " + path + " " +
		"in the brain home ($" + brainconfig.EnvVar + ", or the current directory),\n" +
		"which brainctl init must have created"
}

// initConfig makes sure the config file at path exists and, for an extension
// of brainctl, holds the extension's settings, including its name and its
// executable, by name, as bin. Only brainctl creates the file:
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
	// The program's name and bin are its own name, not where it is
	// installed, so the file stays right on every machine it is backed up or
	// committed to. A name or bin the user set, say an absolute path, is kept.
	// AddExtension checks the existing settings, under the lock, before it
	// writes anything. (When workctl gains settings of its own, check them
	// with DecodeExtension too.)
	added, err := brainconfig.AddExtension(path, id, brainconfig.ExtensionBase{
		SchemaVersion: prog.ExtensionSchemaVersion,
		Name:          prog.Name,
		Bin:           prog.Name,
	})
	if err != nil {
		return err
	}
	switch {
	case slices.Contains(added, "schema_version"):
		_, _ = fmt.Fprintf(log, "Added settings for extension %s to %s\n", id, path)
	case len(added) > 0:
		_, _ = fmt.Fprintf(log, "Added %s to the existing settings for extension %s in %s\n",
			strings.Join(added, " and "), id, path)
	default:
		_, _ = fmt.Fprintf(log, "Using existing settings for extension %s in %s\n", id, path)
	}
	return nil
}
