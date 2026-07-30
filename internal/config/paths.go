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
}

func ResolvePaths() (Paths, error) {
	root := os.Getenv("SESSIONHUB_DATA_DIR")
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user configuration directory: %w", err)
		}
		root = filepath.Join(base, "sessionhub")
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
	}
	for _, dir := range []string{p.Root, p.Logs, p.Downloads} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}
