package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Root      string
	Database  string
	Logs      string
	Downloads string
	// Executors is the root folder each installed CLI gets its own
	// subdirectory under (executors/<slug>/{bin,config,runtime}), so every
	// CLI SessionHub installs is fully self-contained inside the app's own
	// data folder instead of depending on a machine-wide install.
	Executors string
}

func ResolvePaths() (Paths, error) {
	root := os.Getenv("SESSIONHUB_DATA_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home directory: %w", err)
		}
		// A dot-folder in the user's home, matching the convention CLIs like
		// ~/.npm, ~/.codex or ~/.claude use — not the OS's app-config
		// directory, so it's the same place on every platform and matches
		// where users expect an npm-installed CLI's data to live.
		root = filepath.Join(home, ".sessionhub")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data directory: %w", err)
	}
	p := Paths{
		Root:      root,
		Database:  filepath.Join(root, "sessionhub.db"),
		Logs:      filepath.Join(root, "logs"),
		Downloads: filepath.Join(root, "updates"),
		Executors: filepath.Join(root, "executors"),
	}
	for _, dir := range []string{p.Root, p.Logs, p.Downloads, p.Executors} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}
