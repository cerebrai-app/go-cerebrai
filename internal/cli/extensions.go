package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"text/tabwriter"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
)

// lookPath is exec.LookPath; tests replace it.
var lookPath = exec.LookPath

// extensionsUsage is the usage of brainctl's extensions command.
const extensionsUsage = `usage: %[1]s extensions <command>

commands:
  l, ls, list   list the extension executables in extension_bins in %[2]s,
                with where each is found: on PATH for a bare name, or at
                its absolute path

The brain home is $%[3]s if set, otherwise the current directory.
`

// runExtensions runs brainctl's extensions command with args (after
// "extensions").
func runExtensions(name string, args []string, stdout, stderr io.Writer) int {
	usage := func() {
		_, _ = fmt.Fprintf(stderr, extensionsUsage, name, brainconfigFile, brainconfig.EnvVar)
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "l", "ls", "list":
		if len(args) != 1 {
			_, _ = fmt.Fprintf(stderr, "%s extensions %s: unexpected arguments %q\n", name, args[0], args[1:])
			usage()
			return 2
		}
		if err := listExtensions(stdout, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "-help", "--help":
		usage()
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "%s extensions: unknown command %q\n", name, args[0])
	usage()
	return 2
}

// listExtensions writes a table of the extension executables in the brain
// config to stdout: each entry and where it is found, or "not found". An
// entry that is not installed is reported, not an error: the list is the
// same file on every machine, and not every machine has every extension.
func listExtensions(stdout, stderr io.Writer) error {
	dir, err := brainconfig.Home()
	if err != nil {
		return err
	}
	path := brainconfig.Path(dir)
	cfg, err := brainconfig.Load(path)
	if err != nil {
		return err
	}
	bins := cfg.ExtensionBins()
	if len(bins) == 0 {
		_, _ = fmt.Fprintf(stderr, "No extensions are listed in extension_bins in %s\n", path)
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPATH")
	for _, bin := range bins {
		_, _ = fmt.Fprintf(w, "%s\t%s\n", bin, where(bin))
	}
	return w.Flush()
}

// where says where the extension_bins entry bin is found: the executable's
// path, "not found", or why it cannot be used.
func where(bin string) string {
	found, err := lookPath(bin)
	switch {
	case err == nil:
		return found
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return "not found"
	case errors.Is(err, exec.ErrDot):
		// Found only through a relative PATH entry, which Go refuses to run.
		return "not found (only in the current directory, through a relative PATH entry)"
	case errors.Is(err, fs.ErrPermission):
		return "not executable"
	}
	return fmt.Sprintf("cannot look up: %v", err)
}
