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
	"fmt"
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/download"
)

// ggml-small.bin (not ggml-small.en.bin) — the multilingual Whisper small
// model is substantially more accurate than the former base default while
// staying practical for local CLI dictation. It is shared across every
// platform and tool version.
const (
	modelName     = "ggml-small.bin"
	modelSHA256   = "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"
	modelURL      = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + modelName
	maxModelBytes = 512 << 20 // ggml-small.bin is ~465MB
)

// Installed reports the on-disk layout of the whisper.cpp server, its
// model, and — macOS only — the native recorder helper (empty/unused on
// Windows, which records in-process instead of shelling out).
type Installed struct {
	ServerExe   string
	ModelPath   string
	RecorderExe string
}

// Progress and ProgressReporter are aliases of internal/download's types:
// every self-installed local tool (Whisper here, llama.cpp in
// internal/embedding) reports progress the same shape, verified through the
// same download.Verified function, so there is exactly one implementation.
type Progress = download.Progress
type ProgressReporter = download.ProgressReporter

func reportProgress(report ProgressReporter, progress Progress) {
	download.Report(report, progress)
}

// downloadVerified is a thin, voice-flavored name for download.Verified,
// kept so the rest of this package (and its tests) don't need to change.
func downloadVerified(ctx context.Context, url, expectedSHA256 string, maximum int64, stage string, report ProgressReporter) ([]byte, error) {
	return download.Verified(ctx, url, expectedSHA256, maximum, stage, report)
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
	// verified model is never downloaded a second time.
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
