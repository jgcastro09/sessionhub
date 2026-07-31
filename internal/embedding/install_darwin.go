//go:build darwin

package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jgcastro09/sessionhub/internal/download"
)

var darwinAssets = map[string]struct {
	name   string
	sha256 string
}{
	"arm64": {"llama-b10212-bin-macos-arm64.tar.gz", "4ae7eb11dd47657373acd4d0f719f8324f9c63d1623793516b84a4429aaafaf4"},
	"amd64": {"llama-b10212-bin-macos-x64.tar.gz", "8d2598cca0786b30d57c5d6a93d986af9be9864cf578d561c62358b04ccb3f21"},
}

// EnsureInstalled downloads and verifies llama.cpp's official prebuilt macOS
// binary (arm64 or amd64, matching this process) plus the embedding model,
// or returns immediately if a previous run already did so.
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	asset, ok := darwinAssets[runtime.GOARCH]
	if !ok {
		return Installed{}, ErrUnsupportedPlatform
	}
	dir := filepath.Join(toolsRoot, "llama", llamaCppTag, runtime.GOARCH)
	installed := Installed{ServerExe: filepath.Join(dir, "llama-server")}

	if info, err := os.Stat(installed.ServerExe); err != nil || info.IsDir() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Installed{}, fmt.Errorf("create embedding tools dir: %w", err)
		}
		url := "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaCppTag + "/" + asset.name
		archive, err := download.Verified(ctx, url, asset.sha256, maxArchiveBytes, "Embedding engine", report)
		if err != nil {
			return Installed{}, fmt.Errorf("download llama.cpp: %w", err)
		}
		reportProgress(report, Progress{Stage: "Installing embedding engine"})
		if err := download.ExtractTarGz(archive, dir); err != nil {
			return Installed{}, fmt.Errorf("extract llama.cpp: %w", err)
		}
		if err := os.Chmod(installed.ServerExe, 0o755); err != nil {
			return Installed{}, fmt.Errorf("make llama-server executable: %w", err)
		}
		if _, err := os.Stat(installed.ServerExe); err != nil {
			return Installed{}, fmt.Errorf("llama-server missing after extract: %w", err)
		}
	}

	modelPath, err := ensureModel(ctx, toolsRoot, report)
	if err != nil {
		return Installed{}, err
	}
	installed.ModelPath = modelPath
	return installed, nil
}
