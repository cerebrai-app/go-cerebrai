package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cerebrai-app/go-cerebrai/brainconfig"
)

// runWorkctl runs workctl with args and returns its exit code, stdout and
// stderr.
func runWorkctl(args ...string) (int, string, string) {
	var out, errOut strings.Builder
	code := Main(workctl, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestConfigSetAndGet(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	for _, kv := range [][2]string{
		{"env/client_id", "abc123"}, {"env/debug", "true"}, {"port", "8080"}, {"env/empty", ""},
		{"note", "a: b # not a comment"}, {"name", "My Work"},
	} {
		if code, stdout, stderr := runWorkctl("config", "set", kv[0], kv[1]); code != 0 || stdout != "" || stderr != "" {
			t.Fatalf("set %s: got %d, %q, %q", kv[0], code, stdout, stderr)
		}
		if code, stdout, stderr := runWorkctl("config", "get", kv[0]); code != 0 || stdout != kv[1]+"\n" || stderr != "" {
			t.Errorf("get %s: got %d, %q, %q; want stdout %q", kv[0], code, stdout, stderr, kv[1]+"\n")
		}
	}
	const want = "schema_version: 1\nextensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n" +
		"    name: My Work\n    bin: workctl\n" +
		"    env:\n      client_id: abc123\n      debug: \"true\"\n      empty: \"\"\n" +
		"    port: \"8080\"\n    note: 'a: b # not a comment'\n"
	if got := brainConfig(t); got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	// A mapping prints as YAML.
	if code, stdout, _ := runWorkctl("config", "get", "env"); code != 0 ||
		stdout != "client_id: abc123\ndebug: \"true\"\nempty: \"\"\n" {
		t.Errorf("get env: got %d, %q", code, stdout)
	}
	// Setting the same value again leaves the file alone.
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runWorkctl("config", "set", "env/client_id", "abc123"); code != 0 {
		t.Errorf("set again: got %d", code)
	}
	if again, err := os.Stat(configPath); err != nil || !os.SameFile(info, again) {
		t.Errorf("set of the same value replaced the file: %v", err)
	}
}

func TestConfigGetErrors(t *testing.T) {
	for _, tt := range []struct {
		name, content, keyPath, wantErr string // content "" means no config file
	}{
		{"not set", withWorkctl, "env/client_id", "env/client_id in the settings for extension app.cerebrai.workctl: not set"},
		{"through a value", withWorkctl, "name/x", "name is not a mapping, so it has no x"},
		{"bad key path", withWorkctl, "/x", "invalid key path"},
		{"no settings", brainOnly, "x", "not set"},
		{"no config", "", "x", "run brainctl init first"},
		{"newer settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2}\n", "x", "upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			project(t)
			if tt.content != "" {
				writeBrainConfig(t, tt.content)
			}
			code, stdout, stderr := runWorkctl("config", "get", tt.keyPath)
			if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q, %q; want an error containing %q", code, stdout, stderr, tt.wantErr)
			}
		})
	}
}

func TestConfigSetErrors(t *testing.T) {
	const withEnv = withWorkctl + "    env:\n      client_id: abc\n"
	for _, tt := range []struct {
		name, content, keyPath, value, wantErr string // content "" means no config file
	}{
		{"replacing a mapping", withEnv, "env", "x", "env holds a mapping"},
		{"through a value", withEnv, "env/client_id/x", "y", "env/client_id is not a mapping"},
		{"schema_version", withWorkctl, "schema_version", "2", "schema_version cannot be changed"},
		{"bad bin", withWorkctl, "bin", "bin/workctl", "invalid bin"},
		{"bad name", withWorkctl, "name", "", "invalid name: empty"},
		{"bad key path", withWorkctl, "a//b", "x", "invalid key path"},
		{"no settings", brainOnly, "x", "y", "no settings for extension app.cerebrai.workctl: run workctl init first"},
		{"no config", "", "x", "y", "run brainctl init first"},
		{"newer settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2}\n", "x", "y", "upgrade"},
		{"bad settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 1, name: ''}\n", "x", "y", "invalid name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			project(t)
			if tt.content != "" {
				writeBrainConfig(t, tt.content)
			}
			code, stdout, stderr := runWorkctl("config", "set", tt.keyPath, tt.value)
			if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q, %q; want an error containing %q", code, stdout, stderr, tt.wantErr)
			}
			want := tt.content
			if want == "" {
				want = "<missing>"
			}
			if got := brainConfig(t); got != want {
				t.Errorf("config changed to %q", got)
			}
		})
	}
}

func TestConfigUsesBrainHome(t *testing.T) {
	project(t)
	brain := brainAt(t)
	if err := os.MkdirAll(filepath.Join(brain, ".brain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brainconfig.Path(brain), []byte(withWorkctl), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runWorkctl("config", "set", "env/client_id", "abc"); code != 0 {
		t.Fatalf("set: got %d, %q", code, stderr)
	}
	if code, stdout, _ := runWorkctl("config", "get", "env/client_id"); code != 0 || stdout != "abc\n" {
		t.Errorf("get: got %d, %q", code, stdout)
	}
	data, err := os.ReadFile(brainconfig.Path(brain))
	if err != nil || !strings.Contains(string(data), "client_id: abc") {
		t.Errorf("config in BRAIN_HOME = %q, %v", data, err)
	}
	cwdIsEmpty(t)
}

func TestConfigUsage(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	for _, args := range [][]string{
		{"config"}, {"config", "bogus"}, {"config", "get"}, {"config", "get", "a", "b"},
		{"config", "set", "a"}, {"config", "set", "a", "b", "c"}, {"config", "unset"}, {"config", "unset", "a", "b"},
	} {
		code, stdout, stderr := runWorkctl(args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: workctl config get <key path>") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"config", "-h"}, {"config", "help"}, {"config", "--help"}} {
		code, stdout, stderr := runWorkctl(args...)
		if code != 0 || stdout != "" || !strings.Contains(stderr, "env/client_id is the client_id key in\nthe env mapping") ||
			!strings.Contains(stderr, "app.cerebrai.workctl") {
			t.Errorf("%q: got %d, %q, %q", args, code, stdout, stderr)
		}
	}
	if _, _, stderr := runWorkctl("help"); !strings.Contains(stderr, "config      get, set and unset values in the settings for extension app.cerebrai.workctl") {
		t.Errorf("workctl help does not mention config:\n%s", stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config changed to %q", got)
	}
}

// brainctl's config works on the file's top level, where the user's own keys
// live, and leaves schema_version and the extensions' settings alone.
func TestBrainctlConfig(t *testing.T) {
	project(t)
	const before = "schema_version: 1 # format\nowner: me # mine\n" + "extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: workctl\n    bin: workctl\n"
	writeBrainConfig(t, before)
	for _, step := range []struct {
		args       []string
		wantCode   int
		wantStdout string
		wantErr    string
	}{
		{[]string{"config", "get", "owner"}, 0, "me\n", ""},
		{[]string{"config", "set", "owner", "you"}, 0, "", ""},
		{[]string{"config", "get", "owner"}, 0, "you\n", ""},
		{[]string{"config", "set", "sync/remote", "origin"}, 0, "", ""},
		{[]string{"config", "get", "sync"}, 0, "remote: origin\n", ""},
		{[]string{"config", "get", "schema_version"}, 0, "1\n", ""},
		{[]string{"config", "get", "extensions/app.cerebrai.workctl/name"}, 0, "workctl\n", ""},
		{[]string{"config", "get", "missing"}, 1, "", "missing in the config file: not set"},
		{[]string{"config", "set", "schema_version", "2"}, 1, "", "schema_version cannot be changed: brainctl manages it"},
		{[]string{"config", "unset", "schema_version"}, 1, "", "schema_version cannot be changed"},
		{[]string{"config", "set", "extensions/app.cerebrai.workctl/name", "x"}, 1, "", "extensions cannot be changed here"},
		{[]string{"config", "unset", "extensions"}, 1, "", "extensions cannot be changed here"},
		{[]string{"config", "set", "owner/x", "y"}, 1, "", "config file: owner is not a mapping, so it has no x"},
		{[]string{"config", "unset", "sync/remote"}, 0, "", ""},
		{[]string{"config", "unset", "sync/remote"}, 0, "", ""}, // already gone: nothing to do
		{[]string{"config", "unset", "owner"}, 0, "", ""},
	} {
		code, stdout, stderr := runBrainctl(step.args...)
		if code != step.wantCode || stdout != step.wantStdout ||
			(step.wantErr == "" && stderr != "") || !strings.Contains(stderr, step.wantErr) {
			t.Errorf("%q: got %d, %q, %q; want %d, %q, an error containing %q",
				step.args, code, stdout, stderr, step.wantCode, step.wantStdout, step.wantErr)
		}
	}
	const want = "schema_version: 1 # format\n" +
		"extensions:\n  app.cerebrai.workctl:\n    schema_version: 1\n    name: workctl\n    bin: workctl\n" +
		"sync: {}\n"
	if got := brainConfig(t); got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	if _, _, stderr := runBrainctl("help"); !strings.Contains(stderr, "config      get, set and unset values in the brain config") {
		t.Errorf("brainctl help does not mention config:\n%s", stderr)
	}
	if _, _, stderr := runBrainctl("config", "-h"); !strings.Contains(stderr, "in the top level of the brain config") ||
		!strings.Contains(stderr, "cannot change schema_version or extensions") {
		t.Errorf("brainctl config -h:\n%s", stderr)
	}
}

// A third-party extension built on this package gets the command for its
// own settings, and never touches workctl's.
func TestConfigForThirdPartyExtension(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl+"  com.example.tool:\n    schema_version: 3\n")
	var out, errOut strings.Builder
	if code := Main(thirdParty, []string{"config", "set", "env/client_id", "theirs"}, &out, &errOut); code != 0 {
		t.Fatalf("set: got %d, %q", code, errOut.String())
	}
	if code, _, stderr := runWorkctl("config", "get", "env/client_id"); code != 1 || !strings.Contains(stderr, "not set") {
		t.Errorf("workctl sees the tool's value: got %d, %q", code, stderr)
	}
	out.Reset()
	if code := Main(thirdParty, []string{"config", "get", "env/client_id"}, &out, &errOut); code != 0 || out.String() != "theirs\n" {
		t.Errorf("get: got %d, %q", code, out.String())
	}
}

func TestConfigGetWriteError(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl)
	var errOut strings.Builder
	if code := Main(workctl, []string{"config", "get", "name"}, failingWriter{}, &errOut); code != 1 || !strings.HasPrefix(errOut.String(), "error: ") {
		t.Errorf("got %d, %q", code, errOut.String())
	}
}

func TestConfigUnset(t *testing.T) {
	project(t)
	writeBrainConfig(t, withWorkctl+"    env:\n      client_id: abc # mine\n      debug: \"true\"\n    port: \"80\"\n")
	for _, step := range []struct {
		args     []string
		wantCode int
		wantErr  string
	}{
		{[]string{"config", "unset", "env/client_id"}, 0, ""},
		{[]string{"config", "unset", "env/client_id"}, 0, ""}, // already gone
		{[]string{"config", "unset", "nothing/here"}, 0, ""},
		{[]string{"config", "unset", "port"}, 0, ""},
		{[]string{"config", "unset", "schema_version"}, 1, "schema_version cannot be changed: the extension manages it"},
		{[]string{"config", "unset", "env/debug/x"}, 1, "env/debug is not a mapping"},
		{[]string{"config", "unset", "a//b"}, 1, "invalid key path"},
	} {
		code, stdout, stderr := runWorkctl(step.args...)
		if code != step.wantCode || stdout != "" || (step.wantErr == "" && stderr != "") || !strings.Contains(stderr, step.wantErr) {
			t.Errorf("%q: got %d, %q, %q; want %d, an error containing %q", step.args, code, stdout, stderr, step.wantCode, step.wantErr)
		}
	}
	if got, want := brainConfig(t), withWorkctl+"    env:\n      debug: \"true\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	if code, _, stderr := runWorkctl("config", "get", "env/client_id"); code != 1 || !strings.Contains(stderr, "not set") {
		t.Errorf("get after unset: got %d, %q", code, stderr)
	}
	// The whole mapping goes too.
	if code, _, stderr := runWorkctl("config", "unset", "env"); code != 0 {
		t.Fatalf("unset env: got %d, %q", code, stderr)
	}
	if got := brainConfig(t); got != withWorkctl {
		t.Errorf("config = %q, want %q", got, withWorkctl)
	}
}

func TestConfigUnsetErrors(t *testing.T) {
	for _, tt := range []struct {
		name, content, wantErr string // content "" means no config file
	}{
		{"no settings", brainOnly, "no settings for extension app.cerebrai.workctl: run workctl init first"},
		{"no config", "", "run brainctl init first"},
		{"newer settings", "schema_version: 1\nextensions:\n  app.cerebrai.workctl: {schema_version: 2, x: y}\n", "upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			project(t)
			if tt.content != "" {
				writeBrainConfig(t, tt.content)
			}
			code, _, stderr := runWorkctl("config", "unset", "x")
			if code != 1 || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("got %d, %q; want an error containing %q", code, stderr, tt.wantErr)
			}
			want := tt.content
			if want == "" {
				want = "<missing>"
			}
			if got := brainConfig(t); got != want {
				t.Errorf("config changed to %q", got)
			}
		})
	}
}
