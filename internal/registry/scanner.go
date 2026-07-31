package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// discovered is one file found by scan, before it is reconciled against
// stored records.
type discovered struct {
	RelPath  string
	Hash     string
	Size     int64
	Lines    int
	Language string
	Category string
	Symbols  []string
}

// scan walks every configured root under project root, applying containment
// (project.ResolvePath-equivalent symlink safety), default exclusions,
// Config.Exclude/Include globs, the extension allowlist, the size limit, and
// a text-content sniff for files with an unrecognized extension. Root must
// already be an absolute, symlink-resolved project root.
func scan(root string, cfg Config) ([]discovered, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []discovered
	for _, r := range cfg.roots() {
		start := filepath.Join(resolvedRoot, filepath.Clean(r))
		err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
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
			if d.IsDir() {
				if isExcludedDir(d.Name()) {
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
			if len(cfg.Exclude) > 0 && matchesAny(cfg.Exclude, rel) {
				return nil
			}
			if len(cfg.Include) > 0 && !matchesAny(cfg.Include, rel) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(rel))
			if !cfg.allowsExtension(ext) {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil {
				return statErr
			}
			if info.Size() > cfg.maxFileBytes() {
				return nil
			}
			entry, ok, readErr := inspectFile(path, rel, cfg)
			if readErr != nil {
				return readErr
			}
			if !ok {
				return nil
			}
			seen[rel] = true
			out = append(out, entry)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func inspectFile(absPath, relPath string, cfg Config) (discovered, bool, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return discovered{}, false, err
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	_, knownExt := languageByExt[ext]
	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	if !knownExt && !buildFileNames[filepath.Base(relPath)] && bytes.IndexByte(data[:sniffLen], 0) >= 0 {
		return discovered{}, false, nil // looks binary and isn't a recognized source extension
	}

	hash := sha256.Sum256(data)
	language, category := classify(relPath, cfg.Categories)
	symbols := detectSymbols(string(data), language)
	return discovered{
		RelPath: relPath, Hash: hex.EncodeToString(hash[:]), Size: int64(len(data)),
		Lines:    bytes.Count(data, []byte("\n")) + boolToInt(len(data) > 0 && data[len(data)-1] != '\n'),
		Language: language, Category: category, Symbols: symbols,
	}, true, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
