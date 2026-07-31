package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SystemGlobalDirs returns common conventional global binary directories for
// the current OS where CLIs are installed outside of PATH or in default locations.
func SystemGlobalDirs() []string {
	var dirs []string
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".deno", "bin"),
			filepath.Join(home, ".claude", "bin"),
			filepath.Join(home, ".codex", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, "bin"),
		)
		// Check NVM Node versions if present
		nvmDir := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvmDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					dirs = append(dirs, filepath.Join(nvmDir, entry.Name(), "bin"))
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			dirs = append(dirs, filepath.Join(appData, "npm"))
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			dirs = append(dirs,
				filepath.Join(localAppData, "bin"),
				filepath.Join(localAppData, "Programs"),
				filepath.Join(localAppData, "agy", "bin"),
			)
		}
	} else {
		dirs = append(dirs,
			"/usr/local/bin",
			"/opt/homebrew/bin",
			"/usr/bin",
			"/bin",
		)
	}
	return dirs
}

// lookupInDirs checks whether command exists as an executable file directly
// inside any of dirs (checked in order), trying Windows' PATHEXT suffixes
// when relevant.
func lookupInDirs(command string, dirs ...string) (string, bool) {
	if command == "" {
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

	// Handle direct absolute or relative file path
	if filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		for _, ext := range exts {
			candidate := command + ext
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
		return "", false
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
// executor's own managed folder or provider extraDirs.
func FindInExecutorDir(command, executorRoot string, extraDirs ...string) (string, bool) {
	dirs := append([]string{
		filepath.Join(executorRoot, "bin"),
		filepath.Join(executorRoot, "node_modules", ".bin"),
		executorRoot,
	}, extraDirs...)
	return lookupInDirs(command, dirs...)
}

// FindExecutable checks for an existing executable binary in order:
// 1. The executor's own managed folder (bin/, node_modules/.bin, executorRoot)
// 2. Direct absolute or relative file path
// 3. System PATH via exec.LookPath
// 4. Standard global CLI install directories (~/.local/bin, /usr/local/bin, /opt/homebrew/bin, %APPDATA%\npm, etc.)
// 5. Provider extraDirs
func FindExecutable(command, executorRoot string, extraDirs ...string) (string, bool) {
	if command == "" {
		return "", false
	}
	// 1. Managed executor directory check
	if resolved, found := FindInExecutorDir(command, executorRoot, extraDirs...); found {
		return resolved, true
	}
	// 2. Direct path check if command contains path separators or is absolute
	if filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		if resolved, found := lookupInDirs(command); found {
			return resolved, true
		}
	}
	// 3. System PATH check
	if path, err := exec.LookPath(command); err == nil && path != "" {
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			return path, true
		}
	}
	// 4. System standard global dirs + provider extraDirs
	globalDirs := append(SystemGlobalDirs(), extraDirs...)
	return lookupInDirs(command, globalDirs...)
}
