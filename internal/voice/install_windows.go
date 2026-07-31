//go:build windows

package voice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/download"
)

// Pinned so a future whisper.cpp release can't silently change what gets
// downloaded and verified — bumping this is a deliberate code change.
const (
	whisperVersion     = "v1.9.1"
	whisperAssetName   = "whisper-bin-x64.zip"
	whisperAssetSHA256 = "7d8be46ecd31828e1eb7a2ecdd0d6b314feafd82163038ab6092594b0a063539"
	whisperDownloadURL = "https://github.com/ggml-org/whisper.cpp/releases/download/" + whisperVersion + "/" + whisperAssetName

	maxArchiveBytes = 64 << 20 // whisper-bin-x64.zip is ~8MB
)

// EnsureInstalled downloads and verifies whisper.cpp's official prebuilt
// Windows binary + its model on first use, or returns immediately if a
// previous run already did so.
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	dir := filepath.Join(toolsRoot, "whisper", whisperVersion)
	installed := Installed{
		ServerExe: filepath.Join(dir, "whisper-server.exe"),
	}

	if info, err := os.Stat(installed.ServerExe); err != nil || info.IsDir() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Installed{}, fmt.Errorf("create whisper tools dir: %w", err)
		}
		archive, err := downloadVerified(ctx, whisperDownloadURL, whisperAssetSHA256, maxArchiveBytes, "Whisper tools", report)
		if err != nil {
			return Installed{}, fmt.Errorf("download whisper.cpp: %w", err)
		}
		reportProgress(report, Progress{Stage: "Installing Whisper tools"})
		if err := download.ExtractZip(archive, dir); err != nil {
			return Installed{}, fmt.Errorf("extract whisper.cpp: %w", err)
		}
		if _, err := os.Stat(installed.ServerExe); err != nil {
			return Installed{}, fmt.Errorf("whisper-server.exe missing after extract: %w", err)
		}
	}

	modelPath, err := ensureModel(ctx, toolsRoot, dir, report)
	if err != nil {
		return Installed{}, err
	}
	installed.ModelPath = modelPath
	return installed, nil
}
