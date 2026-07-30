package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// lookupInDirs checks whether command exists as an executable file directly
// inside any of dirs (checked in order), trying Windows' PATHEXT suffixes
// when relevant. It never reads or writes the process-wide PATH, so it's
// safe to call from anywhere without racing other lookups, and it never
// falls back to a machine-wide install — an executor is only "found" if
// it's inside its own managed folder.
func lookupInDirs(command string, dirs ...string) (string, bool) {
	if command == "" || filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		return "", false
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		if raw := os.Getenv("PATHEXT"); raw != "" {
			exts = strings.Split(raw, ";")
		} else {
			exts = []string{".COM", ".EXE", ".BAT", ".CMD"}
		}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			candidate := filepath.Join(dir, command+ext)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
	}
	return "", false
}

// FindInExecutorDir reports whether command is already installed inside an
// executor's own managed folder. It checks, in order: bin/ (the documented
// layout), node_modules/.bin (npm install without -g, deterministic
// regardless of npm's global-prefix config), the root itself (some
// installers, notably npm -g on Windows, place the shim directly in the
// install prefix), and finally extraDirs — a provider's own conventional
// install location, for CLIs whose official installer doesn't support a
// custom directory. It never touches the OS PATH: an executor installed
// through SessionHub is only ever resolved from one of these known folders.
func FindInExecutorDir(command, executorRoot string, extraDirs ...string) (string, bool) {
	dirs := append([]string{
		filepath.Join(executorRoot, "bin"),
		filepath.Join(executorRoot, "node_modules", ".bin"),
		executorRoot,
	}, extraDirs...)
	return lookupInDirs(command, dirs...)
}
