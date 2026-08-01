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
// Classification queue).
type scanResult struct {
	Files   []discovered
	Pending []PendingFile
}

// scan walks every configured root under project root, applying symlink
// containment, the declarative exclusion rules (directories are pruned
// entirely, never descended into), the eligibility whitelist, and the size
// cap. Root must already be an absolute, symlink-resolved project root.
func scan(root string, cfg Config) (scanResult, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return scanResult{}, err
	}
	seen := map[string]bool{}
	var result scanResult
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
			entry, ok, readErr := inspectFile(path, rel, cfg, info)
			if readErr != nil {
				return readErr
			}
			if !ok {
				return nil
			}
			seen[rel] = true
			result.Files = append(result.Files, entry)
			return nil
		})
		if walkErr != nil {
			return scanResult{}, walkErr
		}
	}
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

func inspectFile(absPath, relPath string, cfg Config, info fs.FileInfo) (discovered, bool, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return discovered{}, false, err
	}
	language := languageForPath(relPath)
	category, kind := cfg.Classification.Classify(relPath)
	module := moduleForPath(relPath, cfg.ModuleOverrides)
	symbols := detectSymbols(string(data), language)
	includes, imports, exports := extractDependencies(string(data), language)
	hash := sha256.Sum256(data)
	lineCount := bytes.Count(data, []byte("\n")) + boolToInt(len(data) > 0 && data[len(data)-1] != '\n')
	return discovered{
		RelPath: relPath, Filename: filepath.Base(relPath), Extension: strings.ToLower(filepath.Ext(relPath)),
		Language: language, Kind: kind, Category: category, Module: module,
		Symbols: symbols, Includes: includes, Imports: imports, Exports: exports,
		Dependencies:   union(includes, imports),
		Hash:           hex.EncodeToString(hash[:]),
		SizeBytes:      int64(len(data)),
		LineCount:      lineCount,
		LastModifiedAt: info.ModTime().UTC(),
	}, true, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
