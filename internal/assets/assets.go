package assets

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed logo.png
var LogoPNG []byte

// EnsureLogoExtracted writes logo.png into dataDir/logo.png if not present or updated,
// returning the absolute path to logo.png.
func EnsureLogoExtracted(dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	logoPath := filepath.Join(dataDir, "logo.png")
	if info, err := os.Stat(logoPath); err == nil && info.Size() == int64(len(LogoPNG)) {
		return logoPath, nil
	}
	if err := os.WriteFile(logoPath, LogoPNG, 0o600); err != nil {
		return "", err
	}
	return logoPath, nil
}
