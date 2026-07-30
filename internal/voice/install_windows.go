//go:build windows

package voice

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
func EnsureInstalled(ctx context.Context, toolsRoot string) (Installed, error) {
	dir := filepath.Join(toolsRoot, "whisper", whisperVersion)
	installed := Installed{
		ServerExe: filepath.Join(dir, "whisper-server.exe"),
	}

	if info, err := os.Stat(installed.ServerExe); err != nil || info.IsDir() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Installed{}, fmt.Errorf("create whisper tools dir: %w", err)
		}
		archive, err := downloadVerified(ctx, whisperDownloadURL, whisperAssetSHA256, maxArchiveBytes)
		if err != nil {
			return Installed{}, fmt.Errorf("download whisper.cpp: %w", err)
		}
		if err := extractZip(archive, dir); err != nil {
			return Installed{}, fmt.Errorf("extract whisper.cpp: %w", err)
		}
		if _, err := os.Stat(installed.ServerExe); err != nil {
			return Installed{}, fmt.Errorf("whisper-server.exe missing after extract: %w", err)
		}
	}

	modelPath, err := ensureModel(ctx, dir)
	if err != nil {
		return Installed{}, err
	}
	installed.ModelPath = modelPath
	return installed, nil
}

// extractZip flattens the archive's top-level "Release/" folder (whisper.cpp's
// Windows release layout) directly into destDir, since whisper-server.exe
// needs its sibling ggml*.dll files alongside it to run.
func extractZip(archive []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		name := file.Name
		if idx := strings.IndexByte(name, '/'); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		targetPath := filepath.Join(destDir, filepath.Base(name))
		if err := extractZipFile(file, targetPath); err != nil {
			return fmt.Errorf("extract %s: %w", file.Name, err)
		}
	}
	return nil
}

func extractZipFile(file *zip.File, targetPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}
