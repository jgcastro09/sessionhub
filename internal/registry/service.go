package registry

import (
	"database/sql"
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

// Service is the internal/registry entry point: config, taxonomy, scan,
// review, search, health, and the relationship graph, all reading and
// writing JSON records under <project>/.shproject/registry, backed by a
// rebuildable SQLite cache for full-text/semantic search (index.go). Like
// internal/tasks.Service, it holds no project-specific state beyond a
// per-project write lock.
type Service struct {
	catalog *project.Catalog
	bus     *events.Bus

	embedder Embedder

	mu    sync.Mutex
	locks map[string]*sync.Mutex

	dbMu sync.Mutex
	dbs  map[string]*sql.DB // project root -> open index.sqlite3 connection
}

// Close releases every open index.sqlite3 connection this Service has
// cached. Safe to call once at process/test shutdown; not required for
// correctness (the index is a rebuildable cache), only to release file
// handles promptly.
func (s *Service) Close() error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	var firstErr error
	for _, db := range s.dbs {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.dbs = nil
	return firstErr
}

// New builds a Code Registry service. bus may be nil in tests that do not
// care about event delivery; without a wired Embedder (see SetEmbedder),
// semantic search reports ErrSemanticUnavailable and callers fall back to
// lexical/full-text search, which is always available.
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

// SaveConfig validates cfg (every eligibility/exclusion rule must carry a
// justification, every classification rule list must end in a fallback)
// before writing registry/config.json atomically — an invalid config never
// reaches disk.
func (s *Service) SaveConfig(projectID string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	root, err := s.root(projectID)
	if err != nil {
		return err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()
	return atomicfile.WriteJSON(configPath(root), cfg)
}

// LoadTaxonomy reads registry/taxonomy.json, seeding a generic default the
// first time it's called for a project.
func (s *Service) LoadTaxonomy(projectID string) (Taxonomy, error) {
	root, err := s.root(projectID)
	if err != nil {
		return Taxonomy{}, err
	}
	return LoadTaxonomy(root)
}

// SaveTaxonomy writes registry/taxonomy.json atomically. Scan never calls
// this — only an explicit edit (Web Panel Config view, or a direct API
// call) does, so hand-edited roles/areas are never clobbered by a rescan.
func (s *Service) SaveTaxonomy(projectID string, t Taxonomy) error {
	root, err := s.root(projectID)
	if err != nil {
		return err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()
	return SaveTaxonomy(root, t)
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
// file, removing category files that no longer have any entries. A category
// file whose marshaled content is byte-identical to what's already on disk
// is left untouched — a scan that changed nothing performs zero writes,
// keeping the registry's own JSON diff-free and mtimes stable.
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
		if err := writeJSONIfChanged(path, recordFile{Entries: list}); err != nil {
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

// writeJSONIfChanged marshals value the same way atomicfile.WriteJSON does
// and skips the write entirely if the file already holds identical bytes.
func writeJSONIfChanged(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == string(data) {
		return nil
	}
	return atomicfile.Write(path, data, 0o644)
}

func loadPending(root string) ([]PendingFile, error) {
	data, err := os.ReadFile(pendingPath(root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list pendingFileList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("decode pending.json: %w", err)
	}
	return list.Files, nil
}

func savePending(root string, files []PendingFile) error {
	return writeJSONIfChanged(pendingPath(root), pendingFileList{Files: files})
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

// Pending returns the current Pending Classification queue: text-looking
// files that failed the eligibility whitelist on the most recent scan.
func (s *Service) Pending(projectID string) ([]PendingFile, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	return loadPending(root)
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
// Panel's Code Reader. It re-resolves the path through project.ResolvePath
// on every call, so it can never read outside the project root even if a
// record were somehow tampered with.
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

// Scan discovers files under the project's configured roots, reconciles
// them against stored records (recomputing every declarative/structural
// field, detecting renames by content hash, force-invalidating review
// status on content change), and writes the updated records back. It
// returns the full merged entry set.
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
	if err := ensureRegistryGitignore(root); err != nil {
		return nil, err
	}
	taxonomy, err := LoadTaxonomy(root)
	if err != nil {
		return nil, err
	}

	result, err := scan(root, cfg)
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
	merged := make([]Entry, 0, len(result.Files))
	var changed []Entry

	for _, d := range result.Files {
		if idx, ok := byPath[d.RelPath]; ok && !claimed[idx] {
			claimed[idx] = true
			e, didChange := applyDiscovery(existing[idx], d, now, taxonomy)
			merged = append(merged, e)
			if didChange {
				changed = append(changed, e)
			}
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
					if _, alsoFound := indexOfDiscovered(result.Files, existing[idx].Path); alsoFound {
						continue
					}
				}
				claimed[idx] = true
				prior := existing[idx]
				e, _ := applyDiscovery(prior, d, now, taxonomy)
				e.PreviousPaths = appendPreviousPath(e.PreviousPaths, prior.Path)
				merged = append(merged, e)
				changed = append(changed, e)
				renamed = true
				break
			}
			if renamed {
				continue
			}
		}
		e := newEntryFromDiscovery(d, now, taxonomy)
		merged = append(merged, e)
		changed = append(changed, e)
	}
	for i, e := range existing {
		if !claimed[i] {
			if e.Status != StatusMissing {
				e.Status = StatusMissing
				e.UpdatedAt = now
				changed = append(changed, e)
			}
			merged = append(merged, e)
		}
	}

	// Probable relations are derived from the whole merged set, so this
	// pass runs after every file has been reconciled.
	probable := computeProbableRelatedFiles(merged)
	for i := range merged {
		newProbable := probable[merged[i].EntryID]
		if !equalStringSlices(merged[i].ProbableRelatedFiles, newProbable) {
			merged[i].ProbableRelatedFiles = newProbable
			merged[i].UpdatedAt = now
			changed = append(changed, merged[i])
		}
	}

	if err := saveAllEntries(root, merged); err != nil {
		return nil, err
	}
	if err := savePending(root, result.Pending); err != nil {
		return nil, err
	}
	if err := s.syncIndex(root, changed); err != nil {
		return nil, err
	}
	s.publish(projectID, events.KindRegistryScan, map[string]int{"entries": len(merged), "changed": len(changed)})
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

// applyDiscovery merges a fresh scan of an existing entry's file into it.
// Every declarative/structural field (Category, Kind, Module, Area, Role,
// Symbols, Includes/Imports/Exports/Dependencies, size/line/hash/mtime) is
// unconditionally refreshed from d — it can never "freeze" and go stale.
// Human-owned fields (Description, Responsibilities, Criticality,
// RelatedFiles) are never touched here, only by Review.
//
// The one rule that matters most: if the content hash changed, ReviewStatus
// is force-set to NeedsReview, unconditionally, regardless of what it was
// before. This is the fix for the bug where an approved/reviewed file could
// be edited and silently remain "reviewed" forever.
func applyDiscovery(existing Entry, d discovered, now time.Time, taxonomy Taxonomy) (Entry, bool) {
	hashChanged := existing.Hash != d.Hash
	structuralChanged := hashChanged ||
		existing.Path != d.RelPath || existing.Category != d.Category || existing.Kind != d.Kind ||
		existing.Module != d.Module || existing.Language != d.Language ||
		!equalSymbols(existing.Symbols, d.Symbols) ||
		!equalStringSlices(existing.Includes, d.Includes) ||
		!equalStringSlices(existing.Imports, d.Imports) ||
		!equalStringSlices(existing.Exports, d.Exports) ||
		!equalStringSlices(existing.Dependencies, d.Dependencies) ||
		existing.LineCount != d.LineCount || existing.SizeBytes != d.SizeBytes ||
		existing.Status != StatusActive

	existing.Path = d.RelPath
	existing.Filename = d.Filename
	existing.Extension = d.Extension
	existing.Language = d.Language
	existing.Kind = d.Kind
	existing.Category = d.Category
	existing.Module = d.Module
	existing.Symbols = d.Symbols
	existing.Includes = d.Includes
	existing.Imports = d.Imports
	existing.Exports = d.Exports
	existing.Dependencies = d.Dependencies
	existing.LineCount = d.LineCount
	existing.SizeBytes = d.SizeBytes
	existing.LastModifiedAt = d.LastModifiedAt
	existing.Hash = d.Hash
	existing.Status = StatusActive

	// Area/Role depend on Module/Path (always fresh, above) and on
	// human-owned Description/Responsibilities (untouched here) — so this
	// recomputes from whatever mix of the two currently exists on the
	// entry, every scan.
	newArea := ClassifyArea(taxonomy, existing)
	newRole := ClassifyRole(taxonomy, existing)
	if newArea != existing.Area || newRole != existing.Role {
		structuralChanged = true
	}
	existing.Area = newArea
	existing.Role = newRole

	if hashChanged {
		existing.ReviewStatus = ReviewNeedsReview
	}
	if structuralChanged {
		existing.LastScannedAt = now
		existing.UpdatedAt = now
	}
	return existing, structuralChanged
}

func newEntryFromDiscovery(d discovered, now time.Time, taxonomy Taxonomy) Entry {
	e := Entry{
		EntryID: id.New("entry"), Path: d.RelPath, Filename: d.Filename, Extension: d.Extension,
		Language: d.Language, Kind: d.Kind, Category: d.Category, Module: d.Module,
		Symbols: d.Symbols, Includes: d.Includes, Imports: d.Imports, Exports: d.Exports,
		Dependencies:   d.Dependencies,
		LineCount:      d.LineCount,
		SizeBytes:      d.SizeBytes,
		LastModifiedAt: d.LastModifiedAt,
		Hash:           d.Hash,
		Status:         StatusActive,
		ReviewStatus:   ReviewNeedsReview,
		LastScannedAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	e.Area = ClassifyArea(taxonomy, e)
	e.Role = ClassifyRole(taxonomy, e)
	return e
}

// appendPreviousPath records a rename, capping history at 20 entries
// (oldest dropped first) and never duplicating an already-recorded path.
func appendPreviousPath(existing []string, oldPath string) []string {
	for _, p := range existing {
		if p == oldPath {
			return existing
		}
	}
	out := append(existing, oldPath)
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalSymbols(a, b map[string][]SymbolRef) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}

// ReviewInput is the human/agent-authored portion of an entry. Module,
// Category, Kind, Area, and Role are declarative facts the scanner always
// recomputes — they are not accepted here; to correct a wrong module/area
// assignment, add a rule to Config.ModuleOverrides or taxonomy.json instead
// of hand-editing one entry.
type ReviewInput struct {
	Description      string      `json:"description"`
	Responsibilities []string    `json:"responsibilities"`
	Criticality      Criticality `json:"criticality"`
	RelatedFiles     []string    `json:"related_files"` // entry_id, validated against the live entry set
	ReviewedBy       string      `json:"reviewed_by,omitempty"`
}

// Review records a human/agent review of one entry: it is the only method
// that ever writes LastReviewedHash, always setting it to the entry's
// current Hash, and the only method that ever sets ReviewStatus to
// ReviewReviewed. Every declarative/structural field is left untouched —
// only the next Scan ever changes those.
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
	byID := make(map[string]bool, len(entries))
	found := -1
	for i, e := range entries {
		byID[e.EntryID] = true
		if e.EntryID == entryID {
			found = i
		}
	}
	if found == -1 {
		return Entry{}, ErrNotFound
	}
	for _, rel := range input.RelatedFiles {
		if rel == entryID {
			return Entry{}, fmt.Errorf("entry cannot be related to itself")
		}
		if !byID[rel] {
			return Entry{}, fmt.Errorf("related file %q does not exist", rel)
		}
	}

	now := time.Now().UTC()
	e := entries[found]
	e.Description = strings.TrimSpace(input.Description)
	e.Responsibilities = input.Responsibilities
	e.Criticality = input.Criticality
	e.RelatedFiles = input.RelatedFiles
	e.LastReviewedHash = e.Hash
	e.ReviewStatus = ReviewReviewed
	e.ReviewedAt = &now
	e.ReviewedBy = strings.TrimSpace(input.ReviewedBy)
	e.UpdatedAt = now
	entries[found] = e
	if err := saveAllEntries(root, entries); err != nil {
		return Entry{}, err
	}
	if err := s.syncIndex(root, []Entry{e}); err != nil {
		return Entry{}, err
	}
	s.publish(projectID, events.KindRegistryUpdated, map[string]string{"entry_id": e.EntryID})
	return e, nil
}
