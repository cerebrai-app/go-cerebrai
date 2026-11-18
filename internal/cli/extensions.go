package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
)

// lookPath is exec.LookPath; tests replace it.
var lookPath = exec.LookPath

// extensionsUsage is the usage of brainctl's extensions command.
const extensionsUsage = `usage: %[1]s extensions <command>

commands:
  l, ls, list   list the extensions with settings in %[2]s, each with
                its name, its bin, and where that is found: on PATH for a
                bare name, or at its absolute path

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

// listExtensions writes a table of the extensions in the brain config to
// stdout: each one's ID, name, bin, and where the bin is found, or why not.
// An extension whose bin is missing, bad or not installed is reported in its
// row, not as an error: the list is the same file on every machine, not every
// machine has every extension, and one broken extension must not hide the
// others.
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
	ids := cfg.ExtensionIDs()
	if len(ids) == 0 {
		_, _ = fmt.Fprintf(stderr, "No extensions have settings in %s\n", path)
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "EXTENSION\tNAME\tBIN\tPATH")
	for _, id := range ids {
		ext, _, err := cfg.Extension(id)
		switch {
		case err != nil:
			// One line, so a multi-line YAML error keeps the table's rows.
			_, _ = fmt.Fprintf(w, "%s\t-\t-\t%s\n", cell(id), cell(strings.Join(strings.Fields(err.Error()), " ")))
		case ext.Bin == "":
			_, _ = fmt.Fprintf(w, "%s\t%s\t-\tno bin set\n", cell(id), cell(orDash(ext.Name)))
		default:
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", cell(id), cell(orDash(ext.Name)), cell(ext.Bin), cell(where(ext.Bin)))
		}
	}
	return w.Flush()
}

// cell is s for a table cell, with any control character written as an
// escape (\x1b, \t): the config can come from a cloned repository, and its
// text must not reach the terminal as escape codes that rewrite the screen,
// or as tabs that break the table.
func cell(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// orDash is s, or "-" if s is empty, for a table cell.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// where says where an extension's bin is found: the executable's
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
