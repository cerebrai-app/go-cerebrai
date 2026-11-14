package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
)

// configUsage is the usage of an extension's config command.
const configUsage = `usage: %[1]s config get <key path>
       %[1]s config set <key path> <value>

commands:
  get   print the value at <key path> in the settings for extension %[2]s
        in %[3]s: a value as it is, a mapping or list as YAML
  set   set the value at <key path>, as a string, creating the mappings on
        the way that are missing

A key path is keys separated by "/": env/client_id is the client_id key in
the env mapping. set never replaces a mapping or list, and schema_version
cannot be set.

The brain home is $%[4]s if set, otherwise the current directory.
`

// runConfig runs an extension's config command with args (after "config").
func runConfig(prog Program, args []string, stdout, stderr io.Writer) int {
	name := prog.Name
	usage := func() {
		_, _ = fmt.Fprintf(stderr, configUsage, name, prog.ExtensionID, brainconfigFile, brainconfig.EnvVar)
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	want := map[string]int{"get": 2, "set": 3}[args[0]]
	switch {
	case args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help":
		usage()
		return 0
	case want == 0:
		_, _ = fmt.Fprintf(stderr, "%s config: unknown command %q\n", name, args[0])
		usage()
		return 2
	case len(args) != want:
		_, _ = fmt.Fprintf(stderr, "%s config %s: want %d argument(s), got %d\n", name, args[0], want-1, len(args)-1)
		usage()
		return 2
	}
	var err error
	if args[0] == "get" {
		err = configGet(prog, args[1], stdout)
	} else {
		err = configSet(prog, args[1], args[2])
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// configGet writes the value at keyPath in prog's settings to stdout.
func configGet(prog Program, keyPath string, stdout io.Writer) error {
	dir, err := brainconfig.Home()
	if err != nil {
		return err
	}
	path := brainconfig.Path(dir)
	cfg, err := brainconfig.Load(path)
	if err != nil {
		return err
	}
	value, err := cfg.ExtensionValue(prog.ExtensionID, prog.ExtensionSchemaVersion, keyPath)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	// A mapping or list is YAML that already ends in a newline.
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	_, err = io.WriteString(stdout, value)
	return err
}

// configSet sets the value at keyPath in prog's settings.
func configSet(prog Program, keyPath, value string) error {
	dir, err := brainconfig.Home()
	if err != nil {
		return err
	}
	path := brainconfig.Path(dir)
	// Check the settings in full first, as init does, so a bad file is left
	// as it is.
	cfg, err := brainconfig.Load(path)
	if err != nil {
		return err
	}
	ok, err := cfg.DecodeExtension(prog.ExtensionID, prog.ExtensionSchemaVersion, new(extensionSettings))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !ok {
		return fmt.Errorf("%s: no settings for extension %s: run %s init first", path, prog.ExtensionID, prog.Name)
	}
	_, err = brainconfig.SetExtensionValue(path, prog.ExtensionID, prog.ExtensionSchemaVersion, keyPath, value)
	return err
}
