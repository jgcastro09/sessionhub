package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// discovered is one eligible file found by scan, before it is reconciled
// against stored records. It carries every automatically-derived fact about
// the file — Area/Role are deliberately not here, since those depend on the
// entry's (human-owned) Description via taxonomy text-term matching and are
// computed downstream, after merging, in service.go.
type discovered struct {
	RelPath   string
	Filename  string
	Extension string
	Language  string
	Kind      Kind
	Category  string
	Module    string

	Symbols      map[string][]SymbolRef
	Includes     []string
	Imports      []string
	Exports      []string
	Dependencies []string

	Hash           string
	SizeBytes      int64
	LineCount      int
	LastModifiedAt time.Time
}

// scanResult is the full output of one filesystem walk: eligible files plus
// text-looking files that failed the eligibility whitelist (the Pending
// Classification queue), plus the fingerprint cache to persist for the next
// incremental scan and the metrics describing what this scan actually did.
type scanResult struct {
	Files        []discovered
	Pending      []PendingFile
	Fingerprints map[string]FileFingerprint
	Metrics      ScanMetrics
}

// scan walks every configured root under project root, applying symlink
// containment, the declarative exclusion rules (directories are pruned
// entirely, never descended into), the eligibility whitelist, and the size
// cap. Root must already be an absolute, symlink-resolved project root.
//
// cache is the fingerprint cache from the previous scan (possibly empty).
// When full is false, a file whose size+mtime still match its cached
// fingerprint is never re-read or re-hashed — its discovered record is
// rebuilt from the cache plus freshly (and cheaply) recomputed path-derived
// fields. When full is true the cache is consulted only to populate
// FallbackReasons/metrics, never to skip a read — every eligible file is
// re-read and re-hashed, for periodic audit.
func scan(root string, cfg Config, cache map[string]FileFingerprint, full bool) (scanResult, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return scanResult{}, err
	}
	seen := map[string]bool{}
	result := scanResult{
		Fingerprints: make(map[string]FileFingerprint, len(cache)),
		Metrics:      ScanMetrics{StartedAt: time.Now().UTC(), Full: full, FallbackReasons: map[string]int{}},
	}
	for _, r := range cfg.roots() {
		start := filepath.Join(resolvedRoot, filepath.Clean(r))
		walkErr := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			rel, relErr := filepath.Rel(resolvedRoot, path)
			if relErr != nil {
				return relErr
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if cfg.Eligibility.ExcludedDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			// Never follow a symlink out of the project root.
			if d.Type()&fs.ModeSymlink != 0 {
				target, evalErr := filepath.EvalSymlinks(path)
				if evalErr != nil {
					return nil
				}
				within, relErr := filepath.Rel(resolvedRoot, target)
				if relErr != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
					return nil
				}
			}
			if seen[rel] {
				return nil
			}
			if cfg.Eligibility.ExcludedPath(rel) {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil {
				return statErr
			}
			if info.Size() > cfg.maxFileBytes() {
				return nil
			}
			if !cfg.Eligibility.Eligible(rel) {
				if pending, ok := inspectPending(path, rel, info); ok {
					result.Pending = append(result.Pending, pending)
				}
				return nil
			}
			entry, fp, reused, reason, ok, readErr := inspectFile(path, rel, cfg, info, cache, full)
			if readErr != nil {
				return readErr
			}
			if !ok {
				return nil
			}
			seen[rel] = true
			result.Files = append(result.Files, entry)
			result.Fingerprints[rel] = fp
			result.Metrics.FilesSeen++
			if reused {
				result.Metrics.FilesReused++
			} else {
				result.Metrics.FilesReanalyzed++
				result.Metrics.BytesRead += entry.SizeBytes
				result.Metrics.FallbackReasons[reason]++
			}
			return nil
		})
		if walkErr != nil {
			return scanResult{}, walkErr
		}
	}
	result.Metrics.FinishedAt = time.Now().UTC()
	result.Metrics.DurationMS = result.Metrics.FinishedAt.Sub(result.Metrics.StartedAt).Milliseconds()
	return result, nil
}

// inspectPending sniffs a file that failed the eligibility whitelist to
// decide whether it belongs in the Pending Classification queue (looks like
// text, unrecognized type — a human should decide) or should be silently
// skipped (looks binary — never worth surfacing). This sniff is purely a UX
// signal: eligibility itself is decided entirely by the declarative
// whitelist in policy.go, never by content.
func inspectPending(absPath, relPath string, info fs.FileInfo) (PendingFile, bool) {
	if info.Size() == 0 {
		return PendingFile{}, false
	}
	f, err := os.Open(absPath)
	if err != nil {
		return PendingFile{}, false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if bytes.IndexByte(buf[:n], 0) >= 0 {
		return PendingFile{}, false // looks binary
	}
	return PendingFile{
		Path:        relPath,
		Extension:   strings.ToLower(filepath.Ext(relPath)),
		SizeBytes:   info.Size(),
		DetectedAt:  time.Now().UTC(),
		SuggestedBy: "text content, extension not on the eligibility whitelist",
	}, true
}

// inspectFile builds one discovered record. When full is false and cache
// holds a fingerprint for relPath whose size+mtime still match info, the
// file's content-derived facts (hash, line count, symbols, imports/exports)
// are reused from the cache instead of re-read and re-analyzed — reused
// reports this happened, for scan metrics. Path-derived facts (language,
// category, module, kind) are always recomputed regardless, since they can
// change from a config edit alone, without the file itself changing.
func inspectFile(absPath, relPath string, cfg Config, info fs.FileInfo, cache map[string]FileFingerprint, full bool) (discovered, FileFingerprint, bool, string, bool, error) {
	language := languageForPath(relPath)
	category, kind := cfg.Classification.Classify(relPath)
	module := moduleForPath(relPath, cfg.ModuleOverrides)
	modTime := info.ModTime().UTC()

	if !full {
		if fp, ok := cache[relPath]; ok && fp.matches(info.Size(), modTime) {
			d := discovered{
				RelPath: relPath, Filename: filepath.Base(relPath), Extension: strings.ToLower(filepath.Ext(relPath)),
				Language: language, Kind: kind, Category: category, Module: module,
				Symbols: fp.Symbols, Includes: fp.Includes, Imports: fp.Imports, Exports: fp.Exports,
				Dependencies:   union(fp.Includes, fp.Imports),
				Hash:           fp.Hash,
				SizeBytes:      fp.Size,
				LineCount:      fp.LineCount,
				LastModifiedAt: modTime,
			}
			return d, fp, true, "", true, nil
		}
	}

	reason := "fingerprint-mismatch"
	if _, ok := cache[relPath]; !ok {
		reason = "new"
	}
	if full {
		reason = "full-scan"
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return discovered{}, FileFingerprint{}, false, "", false, err
	}
	symbols := detectSymbols(string(data), language)
	includes, imports, exports := extractDependencies(string(data), language)
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])
	lineCount := bytes.Count(data, []byte("\n")) + boolToInt(len(data) > 0 && data[len(data)-1] != '\n')
	fp := FileFingerprint{
		Size: int64(len(data)), ModTime: modTime, Hash: hashHex, LineCount: lineCount,
		Symbols: symbols, Includes: includes, Imports: imports, Exports: exports,
	}
	d := discovered{
		RelPath: relPath, Filename: filepath.Base(relPath), Extension: strings.ToLower(filepath.Ext(relPath)),
		Language: language, Kind: kind, Category: category, Module: module,
		Symbols: symbols, Includes: includes, Imports: imports, Exports: exports,
		Dependencies:   union(includes, imports),
		Hash:           hashHex,
		SizeBytes:      int64(len(data)),
		LineCount:      lineCount,
		LastModifiedAt: modTime,
	}
	return d, fp, false, reason, true, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
