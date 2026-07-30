package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileEvent struct {
	Workspace string
	Hash      string
	ChangedAt time.Time
}

// WorkspaceHash produces a deterministic metadata-and-content digest. It is
// used by the polling watcher on every supported operating system. Runtime
// data and .git internals are excluded.
func WorkspaceHash(workspace, pattern string) (string, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || relative == ".sessionhub" {
				return filepath.SkipDir
			}
			return nil
		}
		if pattern != "" {
			matched, err := filepath.Match(pattern, filepath.Base(path))
			if err != nil {
				return fmt.Errorf("invalid watcher pattern %q: %w", pattern, err)
			}
			if !matched {
				return nil
			}
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		relative, _ := filepath.Rel(root, path)
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func Watch(
	ctx context.Context,
	workspace, pattern string,
	interval, debounce time.Duration,
	events chan<- FileEvent,
) error {
	if interval <= 0 {
		return fmt.Errorf("watch interval must be positive")
	}
	previous, err := WorkspaceHash(workspace, pattern)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastEmitted time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			current, err := WorkspaceHash(workspace, pattern)
			if err != nil {
				return err
			}
			if current == previous {
				continue
			}
			previous = current
			if now.Sub(lastEmitted) < debounce {
				continue
			}
			lastEmitted = now
			select {
			case events <- FileEvent{Workspace: workspace, Hash: current, ChangedAt: now}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func LogPattern(value, pattern string) (bool, error) {
	if pattern == "" {
		return false, fmt.Errorf("log watcher pattern is required")
	}
	return strings.Contains(value, pattern), nil
}
