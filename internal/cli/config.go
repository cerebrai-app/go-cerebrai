package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
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
          on the way that are missing
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
	if err := config(prog, args, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// config runs the config command args, which runConfig has checked.
func config(prog Program, args []string, stdout io.Writer) error {
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
		if !strings.HasSuffix(value, "\n") {
			value += "\n"
		}
		_, err = io.WriteString(stdout, value)
		return err
	}

	if id != "" {
		// Check an extension's settings in full first, as init does, so a bad
		// file is left as it is.
		ok, err := cfg.DecodeExtension(id, version, new(extensionSettings))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !ok {
			return fmt.Errorf("%s: no settings for extension %s: run %s init first", path, id, prog.Name)
		}
	}
	if args[0] == "set" {
		_, err = brainconfig.Set(path, scope, args[1], args[2])
	} else {
		_, err = brainconfig.Unset(path, scope, args[1])
	}
	return err
}
