//go:build windows

package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/download"
)

// windowsAssetName/SHA256 are amd64-only: llama.cpp's CPU-only Windows
// release only publishes an x64 build, matching one of the five platforms
// SessionHub itself releases for.
const (
	windowsAssetName   = "llama-b10212-bin-win-cpu-x64.zip"
	windowsAssetSHA256 = "b83210d228a34f39dadac3112839ef2d1efa843665ea305807421ad9363f293a"
)

// EnsureInstalled downloads and verifies llama.cpp's official prebuilt
// Windows (CPU) binary plus the embedding model.
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	dir := filepath.Join(toolsRoot, "llama", llamaCppTag, "amd64")
	installed := Installed{ServerExe: filepath.Join(dir, "llama-server.exe")}

	if info, err := os.Stat(installed.ServerExe); err != nil || info.IsDir() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Installed{}, fmt.Errorf("create embedding tools dir: %w", err)
		}
		url := "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaCppTag + "/" + windowsAssetName
		archive, err := download.Verified(ctx, url, windowsAssetSHA256, maxArchiveBytes, "Embedding engine", report)
		if err != nil {
			return Installed{}, fmt.Errorf("download llama.cpp: %w", err)
		}
		reportProgress(report, Progress{Stage: "Installing embedding engine"})
		if err := download.ExtractZip(archive, dir); err != nil {
			return Installed{}, fmt.Errorf("extract llama.cpp: %w", err)
		}
		if _, err := os.Stat(installed.ServerExe); err != nil {
			return Installed{}, fmt.Errorf("llama-server.exe missing after extract: %w", err)
		}
	}

	modelPath, err := ensureModel(ctx, toolsRoot, report)
	if err != nil {
		return Installed{}, err
	}
	installed.ModelPath = modelPath
	return installed, nil
}
