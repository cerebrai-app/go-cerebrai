package uv

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// runUV runs d's uv with args, sending its output to log, and returns its
// standard output if capture is true. Tests replace it.
var runUV = func(d Dirs, log io.Writer, capture bool, args ...string) (string, error) {
	cmd, err := d.Command(log, d.UV(), args...)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if capture {
		cmd.Stdout = &out
	}
	err = cmd.Run()
	return out.String(), err
}

// InstallPython installs opts.Python with d's uv, which Install installed, and
// returns the interpreter's path. uv only downloads it if it is not installed
// in d.Python yet. On Windows it is not registered in the registry, where
// other programs would find it.
func InstallPython(d Dirs, opts Options) (string, error) {
	log := opts.log()
	request := opts.python()
	// It is passed to uv as an argument, which must not read as a flag.
	if strings.HasPrefix(request, "-") || strings.TrimSpace(request) != request {
		return "", fmt.Errorf("invalid Python version %q: want a version such as 3.14, 3.14t or 3.13.9", request)
	}
	_, _ = fmt.Fprintf(log, "installing Python %s with uv\n", request)
	if _, err := runUV(d, log, false, "python", "install", "--no-registry", request); err != nil {
		return "", fmt.Errorf("uv python install %s: %w", request, err)
	}
	out, err := runUV(d, log, true, "python", "find", request)
	if err != nil {
		return "", fmt.Errorf("uv python find %s: %w", request, err)
	}
	python := strings.TrimSpace(out)
	if python == "" {
		return "", fmt.Errorf("uv python find %s printed nothing", request)
	}
	_, _ = fmt.Fprintf(log, "Python %s: %s\n", request, python)
	return python, nil
}
