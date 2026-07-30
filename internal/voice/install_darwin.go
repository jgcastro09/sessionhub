//go:build darwin

package voice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ggml-org/whisper.cpp publishes no macOS binary (only Windows/Linux + an
// iOS xcframework), and there's no pure-Go CoreAudio capture library the
// way go-wca covers WASAPI on Windows. So for macOS, SessionHub's own
// release pipeline builds whisper.cpp from source plus a small native
// recorder helper (native/macos/recorder.m) — see the macos-voice-tools job
// in .github/workflows/release.yml — and this downloads that asset from
// SessionHub's own GitHub release instead of ggml-org's.
//
// Pinned to the release that first published this asset; bump both the tag
// and the checksum together only when a new sessionhub-voice-darwin.tar.gz
// is intentionally published (e.g. recorder.m changes, or the pinned
// whisper.cpp version in the CI job changes).
const (
	macosVoiceToolsTag  = "v0.3.1"
	macosVoiceAssetName = "sessionhub-voice-darwin.tar.gz"
	macosVoiceURL       = "https://github.com/jgcastro09/sessionhub/releases/download/" + macosVoiceToolsTag + "/" + macosVoiceAssetName
	// Filled in once the macos-voice-tools CI job has actually run for this
	// tag and printed the real checksum (see that job's "Package" step) —
	// left blank beforehand so EnsureInstalled fails loudly with a clear
	// reason instead of silently skipping verification.
	macosVoiceAssetSHA256 = "2ab0b426232cde1d4f2d42ec64cf12c96cc5eef49f2886b9f1abf1e350f820d4"

	maxArchiveBytes = 64 << 20
)

// EnsureInstalled downloads and verifies SessionHub's self-built macOS
// whisper.cpp + recorder bundle, plus the shared model, on first use.
func EnsureInstalled(ctx context.Context, toolsRoot string) (Installed, error) {
	if macosVoiceAssetSHA256 == "" {
		return Installed{}, fmt.Errorf(
			"macOS voice tools checksum isn't pinned yet — run the macos-voice-tools " +
				"release job once, then set macosVoiceAssetSHA256 in internal/voice/install_darwin.go")
	}

	dir := filepath.Join(toolsRoot, "whisper-macos", macosVoiceToolsTag)
	installed := Installed{
		ServerExe:   filepath.Join(dir, "whisper-server"),
		RecorderExe: filepath.Join(dir, "sessionhub-voice-recorder"),
	}

	if info, err := os.Stat(installed.ServerExe); err != nil || info.IsDir() {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Installed{}, fmt.Errorf("create whisper tools dir: %w", err)
		}
		archive, err := downloadVerified(ctx, macosVoiceURL, macosVoiceAssetSHA256, maxArchiveBytes)
		if err != nil {
			return Installed{}, fmt.Errorf("download macOS voice tools: %w", err)
		}
		if err := extractTarGz(archive, dir); err != nil {
			return Installed{}, fmt.Errorf("extract macOS voice tools: %w", err)
		}
		for _, exe := range []string{installed.ServerExe, installed.RecorderExe} {
			if err := os.Chmod(exe, 0o755); err != nil {
				return Installed{}, fmt.Errorf("make %s executable: %w", exe, err)
			}
		}
		if _, err := os.Stat(installed.ServerExe); err != nil {
			return Installed{}, fmt.Errorf("whisper-server missing after extract: %w", err)
		}
		if _, err := os.Stat(installed.RecorderExe); err != nil {
			return Installed{}, fmt.Errorf("sessionhub-voice-recorder missing after extract: %w", err)
		}
	}

	modelPath, err := ensureModel(ctx, dir)
	if err != nil {
		return Installed{}, err
	}
	installed.ModelPath = modelPath
	return installed, nil
}

// extractTarGz flattens every regular file in the archive directly into
// destDir (the release asset has no subdirectories — see the "Package" step
// in .github/workflows/release.yml's macos-voice-tools job).
func extractTarGz(archive []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		targetPath := filepath.Join(destDir, filepath.Base(header.Name))
		if err := extractTarFile(tr, targetPath); err != nil {
			return fmt.Errorf("extract %s: %w", header.Name, err)
		}
	}
}

func extractTarFile(tr *tar.Reader, targetPath string) error {
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, tr)
	return err
}
