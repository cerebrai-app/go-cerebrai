package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
	"golang.org/x/term"
)

// configUsage is the usage of the config command. %[2]s says where key paths
// start, and %[5]s which keys set and unset cannot change.
const configUsage = `usage: %[1]s config get <key path>
       %[1]s config set <key path> <value>
       %[1]s config unset <key path>

commands:
  get     print the value at <key path> in %[2]s
          in %[3]s: a value as it is, a mapping or list as YAML
  set     set the value at <key path>, as a string, creating the mappings
          on the way that are missing; a value of - is read from standard
          input, without echo at a terminal, for a long value or one to
          keep out of the shell history; not for secrets, as the file is
          committed or backed up with the brain (to store a literal -,
          pipe it in: printf - | %[1]s config set <key path> -)
  unset   remove the key at <key path>, with its value; nothing to do if
          it is not set

A key path is keys separated by "/": env/client_id is the client_id key in
the env mapping. set never replaces a mapping or list, and set and unset
cannot change %[5]s.

The brain home is $%[4]s if set, otherwise the current directory.
`

// configArgs is the number of arguments each config command takes, after
// its name.
var configArgs = map[string]int{"get": 1, "set": 2, "unset": 1}

// runConfig runs the config command with args (after "config"): for
// brainctl on the file's top level, for an extension on its own settings.
func runConfig(prog Program, args []string, stdout, stderr io.Writer) int {
	name := prog.Name
	usage := func() {
		where, fixed := "the top level of the brain config", "schema_version or extensions"
		if prog.ExtensionID != "" {
			where, fixed = "the settings for extension "+prog.ExtensionID, "schema_version"
		}
		_, _ = fmt.Fprintf(stderr, configUsage, name, where, brainconfigFile, brainconfig.EnvVar, fixed)
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	want, known := configArgs[args[0]]
	switch {
	case args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help":
		usage()
		return 0
	case !known:
		_, _ = fmt.Fprintf(stderr, "%s config: unknown command %q\n", name, args[0])
		usage()
		return 2
	case len(args)-1 != want:
		_, _ = fmt.Fprintf(stderr, "%s config %s: want %d argument(s), got %d\n", name, args[0], want, len(args)-1)
		usage()
		return 2
	}
	if err := config(prog, args, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// config runs the config command args, which runConfig has checked.
func config(prog Program, args []string, stdout, stderr io.Writer) error {
	dir, err := brainconfig.Home()
	if err != nil {
		return err
	}
	path := brainconfig.Path(dir)
	cfg, err := brainconfig.Load(path)
	if err != nil {
		return err
	}
	id, version := prog.ExtensionID, prog.ExtensionSchemaVersion
	// brainctl works on the top level, an extension on its own settings.
	scope := brainconfig.TopLevel
	if id != "" {
		scope = brainconfig.ExtensionSettings(id, version)
	}
	if args[0] == "get" {
		value, err := cfg.Get(scope, args[1])
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		// A mapping or list is YAML that already ends in a newline.
		value = strings.TrimSuffix(value, "\n")
		if isTerminal(stdout) {
			// As in extensions ls: the config can come from a cloned
			// repository, so its text must not reach a terminal as escape
			// codes. Piped output stays exact. Line breaks are kept.
			lines := strings.Split(value, "\n")
			for i, line := range lines {
				lines[i] = cell(line)
			}
			value = strings.Join(lines, "\n")
		}
		_, err = io.WriteString(stdout, value+"\n")
		return err
	}

	if id != "" {
		// Set and Unset check the settings themselves, under the lock; this is
		// only for a better message when there are none.
		if _, ok, _ := cfg.Extension(id); !ok {
			return fmt.Errorf("%s: no settings for extension %s: run %s init first", path, id, prog.Name)
		}
	}
	if args[0] == "set" {
		value := args[2]
		if value == "-" {
			if value, err = readValue(args[1], stderr); err != nil {
				return err
			}
		}
		_, err = brainconfig.Set(path, scope, args[1], value)
	} else {
		_, err = brainconfig.Unset(path, scope, args[1])
	}
	return err
}

// isTerminal reports whether w is a terminal; tests replace it.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Where config set - reads its value; tests replace them.
var (
	// stdin is the standard input.
	stdin io.Reader = os.Stdin
	// stdinIsTerminal reports whether the standard input is a terminal.
	stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	// readHidden reads a line from the terminal on the standard input
	// without echoing it.
	readHidden = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

// readValue reads the value for config set <keyPath> -: at a terminal, after
// a prompt on stderr and without echo, so it is not shown on screen;
// otherwise all of the standard input, less one trailing newline, so a file
// or the output of echo can be redirected or piped in.
func readValue(keyPath string, stderr io.Writer) (string, error) {
	if stdinIsTerminal() {
		_, _ = fmt.Fprintf(stderr, "Value for %s: ", keyPath)
		b, err := readHidden()
		_, _ = fmt.Fprintln(stderr)
		if err != nil {
			return "", fmt.Errorf("reading the value for %s: %w", keyPath, err)
		}
		return string(b), nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("reading the value for %s from standard input: %w", keyPath, err)
	}
	value := strings.TrimSuffix(string(b), "\n")
	return strings.TrimSuffix(value, "\r"), nil
}
