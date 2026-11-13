package python

import (
	"fmt"
	"strings"
)

// validatePythonVersion accepts 3, 3.14 or 3.14.15: up to three numbers of one
// to three digits. That keeps a python-build-standalone release date such as
// 20261111 (which would otherwise parse as a huge major version) from being
// taken as one.
func validatePythonVersion(v string) error {
	parts := strings.Split(v, ".")
	ok := len(parts) <= 3
	for _, p := range parts {
		if len(p) < 1 || len(p) > 3 || strings.Trim(p, "0123456789") != "" {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("invalid Python version %q: use 3, 3.14 or 3.14.15 "+
			"(this is a Python version, not a python-build-standalone release)", v)
	}
	return nil
}

// validateTag checks the release tag from latest-release.json, a date like
// 20261026: only characters that are safe to put in a URL path, and nothing
// that could climb out of it.
func validateTag(tag string) error {
	ok := tag != "" && !strings.HasPrefix(tag, ".")
	for _, c := range tag {
		if !isASCIIAlnum(c) && c != '.' && c != '-' && c != '_' {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("invalid release tag %q: use letters, digits, '.', '-' and '_' only", tag)
	}
	return nil
}

func isASCIIAlnum(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
