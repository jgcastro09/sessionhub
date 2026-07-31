package assets

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed logo.png
var LogoPNG []byte

//go:embed AppIcon.icns
var AppIconICNS []byte

// EnsureLogoExtracted writes logo.png and AppIcon.icns into dataDir.
func EnsureLogoExtracted(dataDir string) (string, string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", "", err
	}
	logoPath := filepath.Join(dataDir, "logo.png")
	if info, err := os.Stat(logoPath); err != nil || info.Size() != int64(len(LogoPNG)) {
		_ = os.WriteFile(logoPath, LogoPNG, 0o600)
	}
	icnsPath := filepath.Join(dataDir, "AppIcon.icns")
	if info, err := os.Stat(icnsPath); err != nil || info.Size() != int64(len(AppIconICNS)) {
		_ = os.WriteFile(icnsPath, AppIconICNS, 0o600)
	}
	return logoPath, icnsPath, nil
}
