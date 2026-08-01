package registry

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/jgcastro09/sessionhub/internal/safego"
)

const defaultWatchDebounce = 1500 * time.Millisecond

// Watcher is an opt-in, per-project filesystem watcher — Fase 1.5's
// "frescor contínuo." It only ever runs the incremental scanner (never AI
// proposals, never a network call): a burst of create/write/rename/remove
// events under an eligible root is coalesced by a debounce timer into a
// single Scan() once the filesystem goes quiet, so a person saving a file
// five times in two seconds costs one scan, not five.
type Watcher struct {
	svc       *Service
	projectID string
	root      string
	debounce  time.Duration

	fsw *fsnotify.Watcher

	mu     sync.Mutex
	timer  *time.Timer
	closed bool
	done   chan struct{}

	// onReconcile, when set (tests only), is called after every debounced
	// Scan() this watcher triggers, instead of only updating Freshness.
	onReconcile func(entries []Entry, err error)
}

// StartWatcher begins watching projectID's eligible roots for filesystem
// changes, per its stored Config.Watch policy. It is a deliberate no-op —
// (nil, nil) — when that policy is disabled (the default), so a caller can
// invoke this unconditionally on every project attach without checking
// opt-in state itself. Directories excluded by policy.go's declarative
// rules (node_modules, vendor, .git, ...) are never registered with the OS
// watch, exactly like scan()'s own directory pruning.
func (s *Service) StartWatcher(projectID string) (*Watcher, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	if !cfg.Watch.Enabled {
		return nil, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	debounce := defaultWatchDebounce
	if cfg.Watch.DebounceMS > 0 {
		debounce = time.Duration(cfg.Watch.DebounceMS) * time.Millisecond
	}

	w := &Watcher{
		svc: s, projectID: projectID, root: resolvedRoot, debounce: debounce,
		fsw: fsw, done: make(chan struct{}),
	}
	if err := w.addTree(resolvedRoot, cfg); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	go safego.Run("registry.Watcher["+projectID+"]", w.loop)
	return w, nil
}

// addTree registers resolvedRoot and every eligible, non-excluded
// subdirectory under it with the OS watch — fsnotify watches are
// non-recursive, so every directory needs its own explicit Add. A directory
// created later is picked up via handleEvent re-calling this on Create
// events for directories.
func (w *Watcher) addTree(dir string, cfg Config) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a single unreadable subtree must never abort the whole watch
		}
		if !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(w.root, path)
		if relErr == nil && rel != "." && cfg.Eligibility.ExcludedDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if err := w.fsw.Add(path); err != nil {
			log.Printf("sessionhub: registry watcher: add %s: %v", path, err)
		}
		return nil
	})
}

// Close stops the watcher and releases the underlying OS resources. Safe to
// call more than once.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	err := w.fsw.Close()
	<-w.done
	return err
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("sessionhub: registry watcher error (project %s): %v", w.projectID, err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if cfg, err := loadConfig(w.root); err == nil {
				_ = w.addTree(event.Name, cfg)
			}
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, w.reconcile)
}

func (w *Watcher) reconcile() {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	w.svc.setFreshness(w.projectID, FreshnessUpdating, "filesystem change detected")
	entries, err := w.svc.Scan(w.projectID)
	if err != nil {
		w.svc.setFreshness(w.projectID, FreshnessFailed, err.Error())
	} else {
		w.svc.setFreshness(w.projectID, FreshnessFresh, "")
	}
	if w.onReconcile != nil {
		w.onReconcile(entries, err)
	}
}
