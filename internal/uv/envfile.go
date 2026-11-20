package uv

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvFileName is the name of the environment file in the brainctl directory.
const EnvFileName = "env"

// envFileHeader starts a new environment file.
const envFileHeader = `# The environment brainctl and workctl run uv, and the tools uv installs, in.
# brainctl init writes it. Each line is NAME=value, with nothing quoted; the
# value runs to the end of the line. Lines starting with # are comments.
#
# init sets the variables that keep uv in this directory back to its own
# values every time it runs. Variables you add are kept and passed on too.
`

// EnvFile is the path of the environment file.
func (d Dirs) EnvFile() string { return filepath.Join(d.Root, EnvFileName) }

// required are the variables WriteEnvFile always sets, with their values: they
// keep uv in the brainctl directory.
func (d Dirs) required() [][2]string {
	return [][2]string{
		{"UV_PYTHON_INSTALL_DIR", d.Python},
		{"UV_PYTHON_BIN_DIR", d.Bin},
		{"UV_TOOL_DIR", d.Tools},
		{"UV_TOOL_BIN_DIR", d.Bin},
		{"UV_CACHE_DIR", d.Cache},
		// Only Pythons in the brainctl directory, never the system's or the
		// user's uv's.
		{"UV_MANAGED_PYTHON", "1"},
		// Ignore the user's uv.toml and pyproject.toml settings.
		{"UV_NO_CONFIG", "1"},
	}
}

// WriteEnvFile creates or updates the environment file: the variables in
// required get their values, other variables and comments in it are kept.
// The file is replaced through a temporary file and a rename, so it is never
// half written.
func WriteEnvFile(d Dirs) error {
	path := d.EnvFile()
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(bytes.TrimSpace(old)) == 0 {
		old = []byte(envFileHeader)
	}
	// Check it first: a broken file is the user's to fix, not to overwrite.
	if _, err := parseEnvFile(path, old); err != nil {
		return err
	}

	required := d.required()
	set := map[string]bool{}
	var out strings.Builder
	for line := range strings.Lines(string(old)) {
		line = strings.TrimRight(line, "\r\n")
		if name, _, ok := envLine(line); ok {
			if i := indexFold(required, name); i >= 0 {
				if set[required[i][0]] {
					continue // a duplicate: the first one holds the value
				}
				set[required[i][0]] = true
				line = required[i][0] + "=" + required[i][1]
			}
		}
		out.WriteString(line + "\n")
	}
	for _, kv := range required {
		if !set[kv[0]] {
			out.WriteString(kv[0] + "=" + kv[1] + "\n")
		}
	}
	for _, kv := range required {
		if strings.ContainsAny(kv[1], "\r\n") {
			return fmt.Errorf("%s: %s has a line break, which the file cannot hold", path, kv[0])
		}
	}

	if err := os.MkdirAll(d.Root, 0o755); err != nil {
		return err
	}
	tmp := path + "." + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// readEnvFile returns the variables in the environment file as NAME=value,
// in order. It fails if the file is missing, malformed or lacks one of the
// required variables, since uv would then not stay in the brainctl directory.
func (d Dirs) readEnvFile() ([]string, error) {
	path := d.EnvFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing: run brainctl init first", path)
	}
	if err != nil {
		return nil, err
	}
	vars, err := parseEnvFile(path, data)
	if err != nil {
		return nil, err
	}
	for _, kv := range d.required() {
		if !hasVar(vars, kv[0]) {
			return nil, fmt.Errorf("%s has no %s: run brainctl init to add it", path, kv[0])
		}
	}
	return vars, nil
}

// parseEnvFile parses data, the contents of the environment file at path.
func parseEnvFile(path string, data []byte) ([]string, error) {
	var vars []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, ok := envLine(line)
		if !ok {
			return nil, fmt.Errorf("%s:%d: want NAME=value or a # comment, got %q", path, n, line)
		}
		vars = append(vars, name+"="+value)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return vars, nil
}

// envLine splits a NAME=value line. The name must be a valid variable name.
func envLine(line string) (name, value string, ok bool) {
	name, value, ok = strings.Cut(line, "=")
	if !ok || name == "" {
		return "", "", false
	}
	for i, r := range name {
		letter := r == '_' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z'
		digit := '0' <= r && r <= '9'
		if !letter && (!digit || i == 0) {
			return "", "", false
		}
	}
	return name, value, true
}

// indexFold is the index of the pair in kvs named name, ignoring case, or -1.
func indexFold(kvs [][2]string, name string) int {
	for i, kv := range kvs {
		if strings.EqualFold(kv[0], name) {
			return i
		}
	}
	return -1
}

// hasVar reports whether vars, NAME=value pairs, sets name, ignoring case.
func hasVar(vars []string, name string) bool {
	for _, kv := range vars {
		if n, _, _ := strings.Cut(kv, "="); strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
