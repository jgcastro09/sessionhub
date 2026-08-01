package registry

import (
	"os"
)

// registryGitignoreContents is the entire, fixed content of
// .shproject/registry/.gitignore. .shproject itself is portable and meant
// to be committed (docs/configuration.md), but index.sqlite3 is a rebuildable
// derived cache (Phase 7) that would otherwise churn/conflict in a team's
// Git history for no benefit — it is never source of truth for anything.
const registryGitignoreContents = "index.sqlite3\nindex.sqlite3-wal\nindex.sqlite3-shm\n"

// ensureRegistryGitignore writes .shproject/registry/.gitignore if it is
// missing or has drifted from the fixed contents above. The file is fully
// owned by this package (nothing else writes into registry/), so it is safe
// to overwrite outright rather than parse-and-merge.
func ensureRegistryGitignore(root string) error {
	path := registryGitignorePath(root)
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == registryGitignoreContents {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if mkErr := os.MkdirAll(registryDir(root), 0o755); mkErr != nil {
		return mkErr
	}
	return os.WriteFile(path, []byte(registryGitignoreContents), 0o644)
}
