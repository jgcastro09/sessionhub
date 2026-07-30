// Package voice provides local, offline voice-to-text dictation: recording
// the default microphone (see recorder_windows.go/recorder_darwin.go/
// recorder_other.go) and transcribing it with a self-installed copy of
// whisper.cpp (see install.go/install_windows.go/install_darwin.go/
// server.go) — no cloud API, and no assumption that the machine already has
// ffmpeg, Python, or Whisper installed. Everything it needs is downloaded
// once into the app's own data directory, the same philosophy SessionHub
// already uses for installed CLI executors.
//
// Windows records in-process via WASAPI (github.com/moutend/go-wca, pure
// Go) and downloads whisper.cpp's own official prebuilt zip. macOS has no
// equivalent pure-Go CoreAudio library and whisper.cpp publishes no macOS
// binary at all, so both are built from source in
// .github/workflows/release.yml's macos-voice-tools job and shipped as a
// SessionHub release asset instead; recorder_darwin.go shells out to the
// resulting small native helper the same way both platforms shell out to
// whisper-server.
package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// ggml-base.bin (not ggml-base.en.bin) — multilingual, since dictation
// isn't limited to English speakers. Shared across every platform.
const (
	modelName     = "ggml-base.bin"
	modelSHA256   = "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe"
	modelURL      = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + modelName
	maxModelBytes = 512 << 20 // ggml-base.bin is ~141MB
)

// Installed reports the on-disk layout of the whisper.cpp server, its
// model, and — macOS only — the native recorder helper (empty/unused on
// Windows, which records in-process instead of shelling out).
type Installed struct {
	ServerExe   string
	ModelPath   string
	RecorderExe string
}

// downloadVerified GETs url, enforces a byte cap, and checks the result
// against expectedSHA256 before returning it — the same shape as
// internal/update/update.go's self-update downloader, just generic over any
// URL instead of a GitHub release's own assets.
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

// ensureModel downloads and verifies the shared multilingual Whisper model
// into dir if it isn't already there. Called by every platform's
// EnsureInstalled after it's made sure the whisper.cpp server itself
// exists, so a missing model alone doesn't force re-downloading the
// (much larger, platform-specific) server too.
func ensureModel(ctx context.Context, dir string) (string, error) {
	modelPath := filepath.Join(dir, modelName)
	if info, err := os.Stat(modelPath); err == nil && !info.IsDir() {
		return modelPath, nil
	}
	model, err := downloadVerified(ctx, modelURL, modelSHA256, maxModelBytes)
	if err != nil {
		return "", fmt.Errorf("download whisper model: %w", err)
	}
	if err := os.WriteFile(modelPath, model, 0o600); err != nil {
		return "", fmt.Errorf("write whisper model: %w", err)
	}
	return modelPath, nil
}
