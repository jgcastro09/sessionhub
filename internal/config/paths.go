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
	// Tools holds self-contained helper tools SessionHub downloads for its
	// own features (e.g. the local Whisper transcription server), separate
	// from Executors since these aren't user-registered CLIs.
	Tools string
	// Projects contains only Session Hub's local project catalogue and runtime
	// mappings. Portable project definitions live in each root's .shproject.
	Projects string
	// NetworkSettings persists the small Remote Mode preference set. LAN
	// discovery always remains enabled; this file only controls whether
	// Tailscale peer discovery is used as an additional interface.
	NetworkSettings string
	// WebSettings persists the web panel's enabled/bind-mode/port preferences.
	WebSettings string
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
		Root:            root,
		Database:        filepath.Join(root, "sessionhub.db"),
		Logs:            filepath.Join(root, "logs"),
		Downloads:       filepath.Join(root, "updates"),
		Executors:       filepath.Join(root, "executors"),
		Tools:           filepath.Join(root, "tools"),
		Projects:        filepath.Join(root, "projects"),
		NetworkSettings: filepath.Join(root, "network.json"),
		WebSettings:     filepath.Join(root, "web.json"),
	}
	for _, dir := range []string{p.Root, p.Logs, p.Downloads, p.Executors, p.Tools, p.Projects} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}
