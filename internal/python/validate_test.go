package python

import (
	"strings"
	"testing"
)

func TestValidatePythonVersion(t *testing.T) {
	for _, ok := range []string{"3", "3.14", "3.14.15", "3.9", "3.9.1", "10.0"} {
		if err := validatePythonVersion(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"20261111", // a release date, not a Python version
		"", "3.", ".3", "3..1", "3.14.15.1", "3.x", "v3.14", "3.14.15rc1",
		"-1", "3 ", "1000", "3.14.1000", "+3",
	} {
		err := validatePythonVersion(bad)
		if err == nil || !strings.Contains(err.Error(), "invalid Python version") {
			t.Errorf("%q: got %v, want an invalid Python version error", bad, err)
		}
	}
}

func TestValidateTag(t *testing.T) {
	for _, ok := range []string{"20261026", "v1.2-rc_1"} {
		if err := validateTag(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "..", ".hidden", "../x", "a/b", `a\b`, "a b", "a?b", "a#b", "é"} {
		if validateTag(bad) == nil {
			t.Errorf("%q: accepted", bad)
		}
	}
}
