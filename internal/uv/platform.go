package uv

import (
	"debug/elf"
	"errors"
	"path/filepath"
	"strings"
)

// hostTriple is the platform name of uv's release archives for goos/goarch.
// On Linux, linuxLibc says whether the system C library is "gnu" or "musl".
func hostTriple(goos, goarch string, linuxLibc func() string) (string, error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if arch == "" {
		return "", errors.New("uv has no release for " + goos + "/" + goarch)
	}
	switch goos {
	case "linux":
		return arch + "-unknown-linux-" + linuxLibc(), nil
	case "darwin":
		return arch + "-apple-darwin", nil
	case "windows":
		return arch + "-pc-windows-msvc", nil
	}
	return "", errors.New("uv has no release for " + goos + "/" + goarch)
}

// systemLibc reports the C library of this Linux system: "musl" if /bin/sh is
// linked against musl (Alpine and the like), otherwise "gnu". Rust settles
// this at compile time; these Go binaries are static, so it is a runtime check.
func systemLibc() string {
	f, err := elf.Open("/bin/sh")
	if err != nil {
		return "gnu"
	}
	defer func() { _ = f.Close() }()
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		buf := make([]byte, p.Filesz)
		if _, err := p.ReadAt(buf, 0); err != nil {
			return "gnu"
		}
		return libcFromInterpreter(string(buf))
	}
	return "gnu"
}

// libcFromInterpreter maps an ELF interpreter such as /lib/ld-musl-x86_64.so.1
// or /lib64/ld-linux-x86-64.so.2 to "musl" or "gnu".
func libcFromInterpreter(interp string) string {
	if strings.Contains(filepath.Base(strings.TrimRight(interp, "\x00")), "musl") {
		return "musl"
	}
	return "gnu"
}
