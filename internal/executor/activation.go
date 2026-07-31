package executor

import (
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

// HasLoginState reports whether a SessionHub-managed executor has retained
// login/config data in its own profile directory. It never reads or exposes
// the profile contents.
func HasLoginState(cfg domain.ExecutorConfig) bool {
	if cfg.InstallDir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(cfg.InstallDir, "config"))
	return err == nil && len(entries) > 0
}
