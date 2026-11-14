package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirName(t *testing.T) {
	for name, want := range map[string]string{"brainctl": ".brain", "workctl": ".work"} {
		if got := DirName(name); got != want {
			t.Errorf("DirName(%q) = %q, want %q", name, got, want)
		}
	}
	if got, want := Path("/p", "brainctl"), filepath.Join("/p", ".brain", "config.yaml"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestInitCreatesFile(t *testing.T) {
	path := Path(t.TempDir(), "brainctl")
	cfg, created, err := Init(path)
	if err != nil || !created || cfg != Default() {
		t.Fatalf("Init: %+v, %v, %v", cfg, created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "schema_version: 1\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestInitKeepsExistingFile(t *testing.T) {
	path := Path(t.TempDir(), "brainctl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const content = "# mine\nschema_version: 1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, created, err := Init(path)
	if err != nil || created || cfg.SchemaVersion != 1 {
		t.Fatalf("Init: %+v, %v, %v", cfg, created, err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf("file changed to %q", data)
	}
}

func TestInitRejectsBadExistingFile(t *testing.T) {
	path := Path(t.TempDir(), "brainctl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("schema_version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(path); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("Init: %v", err)
	}
}

func TestUnmarshal(t *testing.T) {
	if cfg, err := Unmarshal([]byte("schema_version: 1\n")); err != nil || cfg != Default() {
		t.Errorf("valid: %+v, %v", cfg, err)
	}
	for in, wantErr := range map[string]string{
		"":                                 "empty",
		"# only a comment\n":               "empty",
		"other: 1\n":                       "invalid config file",
		"schema_version: 1\nextra: true\n": "invalid config file",
		"schema_version: one\n":            "invalid config file",
		"schema_version: 0\n":              "no schema_version",
		"schema_version: -1\n":             "invalid schema_version",
		"schema_version: 2\n":              "reads only up to 1",
		"[1, 2]\n":                         "invalid config file",
	} {
		if _, err := Unmarshal([]byte(in)); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want error containing %q", in, err, wantErr)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	data, err := Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := Unmarshal(data); err != nil || cfg != Default() {
		t.Errorf("%+v, %v", cfg, err)
	}
}
