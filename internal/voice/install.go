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
	"bytes"
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

// Progress reports an installation step or byte-level download progress.
// Total is zero when the remote server does not provide a content length.
type Progress struct {
	Stage   string
	Current int64
	Total   int64
}

// ProgressReporter is optional so callers outside the UI can retain the
// simple synchronous installation API.
type ProgressReporter func(Progress)

func reportProgress(report ProgressReporter, progress Progress) {
	if report != nil {
		report(progress)
	}
}

// downloadVerified GETs url, enforces a byte cap, and checks the result
// against expectedSHA256 before returning it — the same shape as
// internal/update/update.go's self-update downloader, just generic over any
// URL instead of a GitHub release's own assets.
func downloadVerified(ctx context.Context, url, expectedSHA256 string, maximum int64, stage string, report ProgressReporter) ([]byte, error) {
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
	if response.ContentLength > maximum {
		return nil, fmt.Errorf("download %s exceeded %d bytes", url, maximum)
	}
	total := response.ContentLength
	reportProgress(report, Progress{Stage: stage, Total: total})
	var data bytes.Buffer
	buffer := make([]byte, 64<<10)
	var downloaded int64
	lastPercent := int64(-1)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			downloaded += int64(read)
			if downloaded > maximum {
				return nil, fmt.Errorf("download %s exceeded %d bytes", url, maximum)
			}
			if _, err := data.Write(buffer[:read]); err != nil {
				return nil, err
			}
			if total <= 0 || downloaded*100/total != lastPercent {
				if total > 0 {
					lastPercent = downloaded * 100 / total
				}
				reportProgress(report, Progress{Stage: stage, Current: downloaded, Total: total})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	reportProgress(report, Progress{Stage: "Verifying " + stage, Current: downloaded, Total: total})
	model := data.Bytes()
	sum := sha256.Sum256(model)
	actual := hex.EncodeToString(sum[:])
	if actual != expectedSHA256 {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expectedSHA256, actual)
	}
	return model, nil
}

// ensureModel downloads and verifies the shared multilingual Whisper model
// into dir if it isn't already there. Called by every platform's
// EnsureInstalled after it's made sure the whisper.cpp server itself
// exists, so a missing model alone doesn't force re-downloading the
// (much larger, platform-specific) server too.
func ensureModel(ctx context.Context, toolsRoot, legacyDir string, report ProgressReporter) (string, error) {
	modelDir := filepath.Join(toolsRoot, "whisper-models")
	modelPath := filepath.Join(modelDir, modelName)
	if info, err := os.Stat(modelPath); err == nil && !info.IsDir() {
		return modelPath, nil
	}
	if err := os.MkdirAll(modelDir, 0o700); err != nil {
		return "", fmt.Errorf("create whisper model directory: %w", err)
	}
	// Versions before v0.3.10 kept the model alongside the platform tool
	// binaries. Reuse it with a hard link when upgrading, so an existing
	// verified 141MB model is never downloaded a second time.
	legacyPath := filepath.Join(legacyDir, modelName)
	if info, err := os.Stat(legacyPath); err == nil && !info.IsDir() {
		reportProgress(report, Progress{Stage: "Reusing installed Whisper model"})
		if err := os.Link(legacyPath, modelPath); err == nil {
			return modelPath, nil
		}
		return legacyPath, nil
	}
	model, err := downloadVerified(ctx, modelURL, modelSHA256, maxModelBytes, "Whisper model", report)
	if err != nil {
		return "", fmt.Errorf("download whisper model: %w", err)
	}
	temporary, err := os.CreateTemp(modelDir, modelName+"-*")
	if err != nil {
		return "", fmt.Errorf("create temporary whisper model: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(model); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write whisper model: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("set whisper model permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close whisper model: %w", err)
	}
	if err := os.Rename(temporaryPath, modelPath); err != nil {
		return "", fmt.Errorf("install whisper model: %w", err)
	}
	return modelPath, nil
}
