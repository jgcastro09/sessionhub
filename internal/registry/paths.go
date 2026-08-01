package registry

import (
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/project"
)

// registryDir is <project>/.shproject/registry, the root every other path
// in this file is relative to.
func registryDir(root string) string {
	return filepath.Join(root, project.Directory, "registry")
}

func configPath(root string) string {
	return filepath.Join(registryDir(root), "config.json")
}

func taxonomyPath(root string) string {
	return filepath.Join(registryDir(root), "taxonomy.json")
}

func recordsDir(root string) string {
	return filepath.Join(registryDir(root), "records")
}

func pendingPath(root string) string {
	return filepath.Join(registryDir(root), "pending.json")
}

func indexDBPath(root string) string {
	return filepath.Join(registryDir(root), "index.sqlite3")
}

func scanStatePath(root string) string {
	return filepath.Join(registryDir(root), "scanstate.json")
}

func registryGitignorePath(root string) string {
	return filepath.Join(registryDir(root), ".gitignore")
}
