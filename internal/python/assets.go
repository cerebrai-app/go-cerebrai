package python

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// fallbackVariants are tried in order, after DefaultVariant, when
// Options.Variant is empty and the default is not available for this platform
// (e.g. Windows has no LTO builds). Compiler optimizations are preferred over
// free-threading.
var fallbackVariants = []string{
	"pgo+lto",
	"freethreaded+pgo",
	"pgo",
	"freethreaded+lto",
	"lto",
	"freethreaded+noopt",
	"noopt",
}

// asset is one downloadable distribution listed in a release's SHA256SUMS.
type asset struct {
	fileName     string
	sha256       string
	version      []int
	buildOptions string
}

// dirName is the install directory name, e.g. 3.14.10+freethreaded+pgo+lto.
func (a asset) dirName() string {
	return versionString(a.version) + "+" + a.buildOptions
}

func versionString(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// defaultVariants is the default build followed by the fallbacks, in
// preference order.
func defaultVariants() []string {
	return append([]string{DefaultVariant}, fallbackVariants...)
}

// parseSums parses a release SHA256SUMS file, keeping the full archives for
// triple with a plain numeric CPython version (pre-releases are skipped).
func parseSums(sums, triple string) []asset {
	var assets []asset
	for line := range strings.Lines(sums) {
		if a, ok := parseLine(line, triple); ok {
			assets = append(assets, a)
		}
	}
	return assets
}

func parseLine(line, triple string) (asset, bool) {
	line = strings.TrimSpace(line)
	i := strings.IndexFunc(line, unicode.IsSpace)
	if i < 0 {
		return asset{}, false
	}
	hash := line[:i]
	name := strings.TrimLeft(strings.TrimSpace(line[i:]), "*")
	// Names come from a remote file and end up in paths and URLs.
	if strings.ContainsAny(name, `/\`) {
		return asset{}, false
	}
	// cpython-<version>+<tag>-<triple>-<build options>-full.tar.zst
	rest, ok := strings.CutPrefix(name, "cpython-")
	if !ok {
		return asset{}, false
	}
	version, rest, ok := strings.Cut(rest, "+")
	if !ok {
		return asset{}, false
	}
	if _, rest, ok = strings.Cut(rest, "-"); !ok {
		return asset{}, false
	}
	if rest, ok = strings.CutPrefix(rest, triple+"-"); !ok {
		return asset{}, false
	}
	buildOptions, ok := strings.CutSuffix(rest, "-full.tar.zst")
	if !ok {
		return asset{}, false
	}
	v, ok := parseVersion(version)
	if !ok {
		return asset{}, false
	}
	return asset{fileName: name, sha256: hash, version: v, buildOptions: buildOptions}, true
}

// parseVersion turns "3.14.10" into [3 14 10]. It fails if any part is not a
// plain number.
func parseVersion(s string) ([]int, bool) {
	parts := strings.Split(s, ".")
	v := make([]int, len(parts))
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v[i] = n
	}
	return v, true
}

// versionMatches reports whether version starts with the parts of want, so
// "3.14" matches 3.14.10 but "3.1" does not.
func versionMatches(version []int, want string) bool {
	w, ok := parseVersion(want)
	return ok && len(w) <= len(version) && slices.Equal(version[:len(w)], w)
}

// newerVersionNote explains a skipped version. The preferred build wins over a
// newer version (see selectAsset); when that means skipping a newer matching
// version that only exists as another build, say so, so asking for Python 3
// never quietly gives an older Python. It returns "" when nothing was skipped.
func newerVersionNote(assets []asset, want string, chosen asset) string {
	var newest []int
	for _, a := range assets {
		if versionMatches(a.version, want) && slices.Compare(a.version, newest) > 0 {
			newest = a.version
		}
	}
	if newest == nil || slices.Compare(newest, chosen.version) <= 0 {
		return ""
	}
	var builds []string
	for _, a := range assets {
		if slices.Equal(a.version, newest) {
			builds = append(builds, a.buildOptions)
		}
	}
	slices.Sort(builds)
	return fmt.Sprintf("note: Python %s exists only as %s; using %s with %s",
		versionString(newest), strings.Join(builds, ", "),
		versionString(chosen.version), chosen.buildOptions)
}

// selectAsset picks the first variant, in preference order, with any matching
// asset, and within it the highest version.
func selectAsset(assets []asset, want string, variants []string) (asset, bool) {
	for _, v := range variants {
		var best asset
		found := false
		for _, a := range assets {
			if a.buildOptions == v && versionMatches(a.version, want) &&
				(!found || slices.Compare(a.version, best.version) > 0) {
				best, found = a, true
			}
		}
		if found {
			return best, true
		}
	}
	return asset{}, false
}

// available lists the build options offered for want, for error messages.
func available(assets []asset, want string) string {
	var builds []string
	for _, a := range assets {
		if versionMatches(a.version, want) {
			builds = append(builds, a.buildOptions)
		}
	}
	slices.Sort(builds)
	builds = slices.Compact(builds)
	if len(builds) == 0 {
		return "none (is the Python version right? pre-releases such as 3.15.0a1 are not offered)"
	}
	return strings.Join(builds, ", ")
}
