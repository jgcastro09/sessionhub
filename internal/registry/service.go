package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/id"
	"github.com/jgcastro09/sessionhub/internal/project"
)

// ErrNotFound is returned when an entry_id does not exist under a project.
var ErrNotFound = errors.New("registry entry not found")

func configPath(root string) string {
	return filepath.Join(root, project.Directory, "registry", "config.json")
}
func recordsDir(root string) string {
	return filepath.Join(root, project.Directory, "registry", "records")
}

// Service is the internal/registry entry point: config, scan, sync,
// coverage validation, and lexical search, all reading and writing JSON
// records under <project>/.shproject/registry. Like internal/tasks.Service,
// it holds no project-specific state beyond a per-project write lock.
type Service struct {
	catalog *project.Catalog
	bus     *events.Bus

	embedder Embedder

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds a Code Registry service. bus may be nil in tests that do not
// care about event delivery; without a wired Embedder (see SetEmbedder),
// SemanticSearch reports ErrSemanticUnavailable and callers fall back to
// lexical search, which is always available.
func New(catalog *project.Catalog, bus *events.Bus) *Service {
	return &Service{catalog: catalog, bus: bus, locks: map[string]*sync.Mutex{}}
}

func (s *Service) lockFor(projectID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[projectID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[projectID] = l
	}
	return l
}

func (s *Service) root(projectID string) (string, error) {
	p, err := s.catalog.Get(projectID)
	if err != nil {
		return "", err
	}
	return p.Root, nil
}

func (s *Service) publish(projectID, kind string, payload any) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(projectID, kind, payload)
}

// LoadConfig reads registry/config.json, returning defaultConfig() if it
// does not exist yet.
func (s *Service) LoadConfig(projectID string) (Config, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Config{}, err
	}
	return loadConfig(root)
}

func loadConfig(root string) (Config, error) {
	data, err := os.ReadFile(configPath(root))
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode registry config: %w", err)
	}
	return cfg, nil
}

// SaveConfig writes registry/config.json atomically.
func (s *Service) SaveConfig(projectID string, cfg Config) error {
	root, err := s.root(projectID)
	if err != nil {
		return err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()
	return atomicfile.WriteJSON(configPath(root), cfg)
}

// loadAllEntries reads every records/*.json file.
func loadAllEntries(root string) ([]Entry, error) {
	dir := recordsDir(root)
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}
		var rf recordFile
		if err := json.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("decode %s: %w", f.Name(), err)
		}
		entries = append(entries, rf.Entries...)
	}
	return entries, nil
}

// saveAllEntries regroups entries by category and rewrites every category
// file, removing category files that no longer have any entries.
func saveAllEntries(root string, entries []Entry) error {
	dir := recordsDir(root)
	existing, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	byCategory := map[string][]Entry{}
	for _, e := range entries {
		cat := e.Category
		if cat == "" {
			cat = "uncategorized"
		}
		byCategory[cat] = append(byCategory[cat], e)
	}
	written := map[string]bool{}
	for cat, list := range byCategory {
		path := filepath.Join(dir, cat+".json")
		if err := atomicfile.WriteJSON(path, recordFile{Entries: list}); err != nil {
			return err
		}
		written[cat+".json"] = true
	}
	for _, f := range existing {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") && !written[f.Name()] {
			_ = os.Remove(filepath.Join(dir, f.Name()))
		}
	}
	return nil
}

// List returns every entry in a project, optionally including missing ones.
func (s *Service) List(projectID string, includeMissing bool) ([]Entry, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	entries, err := loadAllEntries(root)
	if err != nil {
		return nil, err
	}
	if includeMissing {
		return entries, nil
	}
	out := entries[:0:0]
	for _, e := range entries {
		if e.Status == StatusActive {
			out = append(out, e)
		}
	}
	return out, nil
}

// Get resolves a single entry by entry_id.
func (s *Service) Get(projectID, entryID string) (Entry, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Entry{}, err
	}
	entries, err := loadAllEntries(root)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.EntryID == entryID {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// ReadSource returns the current file content backing an entry, for the Web
// Panel's Reader (plan 5.5/6.5). It re-resolves the path through
// project.ResolvePath on every call, so it can never read outside the
// project root even if a record were somehow tampered with.
func (s *Service) ReadSource(projectID, entryID string) (string, error) {
	root, err := s.root(projectID)
	if err != nil {
		return "", err
	}
	entry, err := s.Get(projectID, entryID)
	if err != nil {
		return "", err
	}
	if entry.Status != StatusActive {
		return "", fmt.Errorf("entry %s no longer has a file on disk", entryID)
	}
	resolved, err := project.ResolvePath(root, entry.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// EntryExists implements tasks.RegistryChecker.
func (s *Service) EntryExists(projectID, entryID string) (bool, error) {
	_, err := s.Get(projectID, entryID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Search runs lexical search, always available and deterministic. Callers
// that also want semantic results should call SemanticSearch separately and
// merge/present both, per plan 5.4 (lexical is never blocked by the
// embedding model's availability).
func (s *Service) Search(projectID, query string, limit int) ([]SearchResult, error) {
	entries, err := s.List(projectID, false)
	if err != nil {
		return nil, err
	}
	return searchLexical(entries, query, limit), nil
}

// CoverageReport is the result of Validate: every eligible file the scanner
// would discover today must already have a corresponding active entry.
type CoverageReport struct {
	MissingPaths []string `json:"missing_paths"` // discovered files with no entry at all
	StaleHashes  []string `json:"stale_hashes"`  // entries whose hash changed and are unreviewed
}

func (r CoverageReport) OK() bool { return len(r.MissingPaths) == 0 }

// Validate re-runs discovery (without writing anything) and compares it
// against the stored records. A non-empty MissingPaths list must block
// validate/build, per plan 5.3 — coverage never passes silently.
func (s *Service) Validate(projectID string) (CoverageReport, error) {
	root, err := s.root(projectID)
	if err != nil {
		return CoverageReport{}, err
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return CoverageReport{}, err
	}
	found, err := scan(root, cfg)
	if err != nil {
		return CoverageReport{}, err
	}
	entries, err := loadAllEntries(root)
	if err != nil {
		return CoverageReport{}, err
	}
	byPath := map[string]Entry{}
	for _, e := range entries {
		if e.Status == StatusActive {
			byPath[e.Path] = e
		}
	}
	var report CoverageReport
	for _, d := range found {
		entry, ok := byPath[d.RelPath]
		if !ok {
			report.MissingPaths = append(report.MissingPaths, d.RelPath)
			continue
		}
		if entry.Hash != d.Hash && !entry.Reviewed {
			report.StaleHashes = append(report.StaleHashes, d.RelPath)
		}
	}
	return report, nil
}

// Scan discovers files, reconciles them against stored records (preserving
// every human-reviewed field, detecting renames by content hash), and
// writes the updated records back. It returns the full merged entry set.
func (s *Service) Scan(projectID string) ([]Entry, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	cfg, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(configPath(root)); os.IsNotExist(statErr) {
		if err := atomicfile.WriteJSON(configPath(root), cfg); err != nil {
			return nil, err
		}
	}
	found, err := scan(root, cfg)
	if err != nil {
		return nil, err
	}
	existing, err := loadAllEntries(root)
	if err != nil {
		return nil, err
	}

	byPath := map[string]int{}   // path -> index in existing
	byHash := map[string][]int{} // hash -> indices in existing, for rename detection
	for i, e := range existing {
		byPath[e.Path] = i
		byHash[e.Hash] = append(byHash[e.Hash], i)
	}
	claimed := map[int]bool{}
	now := time.Now().UTC()
	merged := make([]Entry, 0, len(found))

	for _, d := range found {
		if idx, ok := byPath[d.RelPath]; ok && !claimed[idx] {
			claimed[idx] = true
			merged = append(merged, applyDiscovery(existing[idx], d, now))
			continue
		}
		if candidates, ok := byHash[d.Hash]; ok {
			renamed := false
			for _, idx := range candidates {
				if claimed[idx] {
					continue
				}
				// Only treat it as a rename if the old path is not itself
				// still present among discovered files (otherwise two
				// unrelated files sharing content would swap identities).
				if _, stillPresent := byPath[existing[idx].Path]; stillPresent && existing[idx].Path != d.RelPath {
					if _, alsoFound := indexOfDiscovered(found, existing[idx].Path); alsoFound {
						continue
					}
				}
				claimed[idx] = true
				merged = append(merged, applyDiscovery(existing[idx], d, now))
				renamed = true
				break
			}
			if renamed {
				continue
			}
		}
		merged = append(merged, Entry{
			EntryID: id.New("entry"), Path: d.RelPath, Category: d.Category, Language: d.Language,
			Hash: d.Hash, Size: d.Size, Lines: d.Lines, Symbols: d.Symbols,
			Status: StatusActive, CreatedAt: now, UpdatedAt: now,
		})
	}
	for i, e := range existing {
		if !claimed[i] {
			e.Status = StatusMissing
			e.UpdatedAt = now
			merged = append(merged, e)
		}
	}

	if err := saveAllEntries(root, merged); err != nil {
		return nil, err
	}
	s.publish(projectID, events.KindRegistryScan, map[string]int{"entries": len(merged)})
	return merged, nil
}

func indexOfDiscovered(found []discovered, path string) (int, bool) {
	for i, d := range found {
		if d.RelPath == path {
			return i, true
		}
	}
	return 0, false
}

// applyDiscovery updates the automatic fields of an existing entry from a
// fresh scan while preserving every human-reviewed field.
func applyDiscovery(existing Entry, d discovered, now time.Time) Entry {
	existing.Path = d.RelPath
	existing.Category = d.Category
	existing.Language = d.Language
	existing.Hash = d.Hash
	existing.Size = d.Size
	existing.Lines = d.Lines
	existing.Symbols = d.Symbols
	existing.Status = StatusActive
	existing.UpdatedAt = now
	if !existing.Reviewed && existing.Module == "" {
		existing.Module = filepath.Dir(d.RelPath)
	}
	return existing
}

// ReviewInput is the human-authored portion of an entry.
type ReviewInput struct {
	Module             string   `json:"module"`
	Description        string   `json:"description"`
	Responsibilities   []string `json:"responsibilities"`
	Criticality        string   `json:"criticality"`
	RelationsConfirmed []string `json:"relations_confirmed"`
	RelationsProbable  []string `json:"relations_probable"`
}

// Review records a human review of one entry. Once reviewed, Scan never
// overwrites these fields again for that entry (plan 5.2).
func (s *Service) Review(projectID, entryID string, input ReviewInput) (Entry, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Entry{}, err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	entries, err := loadAllEntries(root)
	if err != nil {
		return Entry{}, err
	}
	found := -1
	for i, e := range entries {
		if e.EntryID == entryID {
			found = i
			break
		}
	}
	if found == -1 {
		return Entry{}, ErrNotFound
	}
	now := time.Now().UTC()
	e := entries[found]
	e.Module = strings.TrimSpace(input.Module)
	e.Description = strings.TrimSpace(input.Description)
	e.Responsibilities = input.Responsibilities
	e.Criticality = input.Criticality
	e.RelationsConfirmed = input.RelationsConfirmed
	e.RelationsProbable = input.RelationsProbable
	e.Reviewed = true
	e.ReviewedAt = &now
	e.UpdatedAt = now
	entries[found] = e
	if err := saveAllEntries(root, entries); err != nil {
		return Entry{}, err
	}
	s.publish(projectID, events.KindRegistryUpdated, map[string]string{"entry_id": e.EntryID})
	return e, nil
}
