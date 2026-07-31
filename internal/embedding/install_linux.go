//go:build linux

package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jgcastro09/sessionhub/internal/download"
)

var linuxAssets = map[string]struct {
	name   string
	sha256 string
}{
	"amd64": {"llama-b10212-bin-ubuntu-x64.tar.gz", "c97844fdc158f30c2ed3eb2ca32c1a6ede55ca8891b97e2fbfd02895f6e9cd01"},
	"arm64": {"llama-b10212-bin-ubuntu-arm64.tar.gz", "ee14d51f719c1da71bd4fc3165b1aecb1fde762ebee233ed3e6ed71c9e6b47d6"},
}

// EnsureInstalled downloads and verifies llama.cpp's official prebuilt
// Ubuntu binary (glibc-based; matches the CGO_ENABLED=0 release binaries
// SessionHub itself ships on Linux) plus the embedding model.
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	asset, ok := linuxAssets[runtime.GOARCH]
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
