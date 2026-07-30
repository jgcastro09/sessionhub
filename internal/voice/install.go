// Package voice provides local, offline voice-to-text dictation: recording
// the default microphone (see recorder_windows.go/recorder_other.go) and
// transcribing it with a self-installed copy of whisper.cpp (see
// install.go/server.go) — no cloud API, and no assumption that the machine
// already has ffmpeg, Python, or Whisper installed. Everything it needs is
// downloaded once into the app's own data directory, the same philosophy
// SessionHub already uses for installed CLI executors.
package voice

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Pinned so a future whisper.cpp/model release can't silently change what
// gets downloaded and verified — bumping these is a deliberate code change.
const (
	whisperVersion     = "v1.9.1"
	whisperAssetName   = "whisper-bin-x64.zip"
	whisperAssetSHA256 = "7d8be46ecd31828e1eb7a2ecdd0d6b314feafd82163038ab6092594b0a063539"
	whisperDownloadURL = "https://github.com/ggml-org/whisper.cpp/releases/download/" + whisperVersion + "/" + whisperAssetName

	// ggml-base.bin (not ggml-base.en.bin) — multilingual, since dictation
	// isn't limited to English speakers.
	modelName   = "ggml-base.bin"
	modelSHA256 = "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe"
	modelURL    = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + modelName
)

const (
	maxArchiveBytes = 64 << 20  // whisper-bin-x64.zip is ~8MB
	maxModelBytes   = 512 << 20 // ggml-base.bin is ~141MB
)

// Installed reports the on-disk layout for one pinned whisper.cpp version:
// everything under toolsRoot/whisper/<version>/, sibling to how executors
// get their own isolated folder under executors/<slug>/.
type Installed struct {
	ServerExe string
	ModelPath string
}

// EnsureInstalled downloads and verifies whisper.cpp + its model on first
// use, or returns immediately if a previous run already did so.
func EnsureInstalled(ctx context.Context, toolsRoot string) (Installed, error) {
	dir := filepath.Join(toolsRoot, "whisper", whisperVersion)
	installed := Installed{
		ServerExe: filepath.Join(dir, "whisper-server.exe"),
		ModelPath: filepath.Join(dir, modelName),
	}
	serverInfo, serverErr := os.Stat(installed.ServerExe)
	modelInfo, modelErr := os.Stat(installed.ModelPath)
	if serverErr == nil && !serverInfo.IsDir() && modelErr == nil && !modelInfo.IsDir() {
		return installed, nil
	}

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

	model, err := downloadVerified(ctx, modelURL, modelSHA256, maxModelBytes)
	if err != nil {
		return Installed{}, fmt.Errorf("download whisper model: %w", err)
	}
	if err := os.WriteFile(installed.ModelPath, model, 0o600); err != nil {
		return Installed{}, fmt.Errorf("write whisper model: %w", err)
	}

	if _, err := os.Stat(installed.ServerExe); err != nil {
		return Installed{}, fmt.Errorf("whisper-server.exe missing after extract: %w", err)
	}
	return installed, nil
}

func downloadVerified(ctx context.Context, url, expectedSHA256 string, maximum int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "sessionhub-voice-installer")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: server returned %s", url, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("download %s exceeded %d bytes", url, maximum)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != expectedSHA256 {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expectedSHA256, actual)
	}
	return data, nil
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
