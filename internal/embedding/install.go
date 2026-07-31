// Package embedding provides the Code Registry's local, offline semantic
// search: a self-installed copy of llama.cpp's server binary running in
// --embedding mode, talked to over loopback HTTP, the same "download once,
// verify by hardcoded SHA-256, run as a local subprocess" pattern
// internal/voice already uses for Whisper (see install_darwin.go/
// install_linux.go/install_windows.go/manager.go). There is no Ollama, no
// externally reachable server, and no CGO: llama.cpp ships an official
// prebuilt binary for every platform SessionHub targets, so this needs no
// from-source build the way internal/voice's macOS path does for Whisper.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
	"github.com/jgcastro09/sessionhub/internal/download"
)

// Dimensions is the vector length all-MiniLM-L6-v2 produces. Registry
// records store embeddings computed by this exact model/quantization; if the
// model ever changes, every stored embedding_hash mismatches its entry's
// current Hash and gets recomputed automatically (see internal/registry's
// EnsureSemanticIndex).
const Dimensions = 384

// The embedding model is pinned by content hash, downloaded once into
// ~/.sessionhub/tools/embedding-models/ and cached forever after — never
// re-fetched just because the app restarted.
const (
	modelName     = "all-MiniLM-L6-v2-Q8_0.gguf"
	modelSHA256   = "263215c3cadd6e16740741a7624ab4cbb6c8e777688bd5331ecfbf5681c2f8ed"
	modelURL      = "https://huggingface.co/second-state/All-MiniLM-L6-v2-Embedding-GGUF/resolve/main/" + modelName
	maxModelBytes = 64 << 20 // the Q8_0 file is ~24MB
)

// llamaCppTag pins every platform installer to one specific llama.cpp build
// ("b<number>" release tag) so a new upstream release can never silently
// change what gets downloaded and trusted — bumping it is a deliberate code
// change together with each platform's matching checksum.
const llamaCppTag = "b10212"

// maxArchiveBytes bounds every platform's llama.cpp release archive download
// (the largest CPU-only build here is ~18MB).
const maxArchiveBytes = 64 << 20

// ErrUnsupportedPlatform is returned by EnsureInstalled on a platform with
// no known llama.cpp release asset. Search then stays lexical-only, per plan
// 5.4 ("busca lexical continua sempre disponível").
var ErrUnsupportedPlatform = errors.New("local semantic search is not supported on this platform")

// Installed reports where the llama.cpp server binary and the embedding
// model live on disk.
type Installed struct {
	ServerExe string
	ModelPath string
}

// Progress and ProgressReporter mirror internal/voice's naming so both
// installers read the same way in the TUI/Web progress callback, but are
// really just internal/download's shared types.
type Progress = download.Progress
type ProgressReporter = download.ProgressReporter

func reportProgress(report ProgressReporter, progress Progress) {
	download.Report(report, progress)
}

// ensureModel downloads and verifies the shared embedding model into
// toolsRoot if it isn't already there.
func ensureModel(ctx context.Context, toolsRoot string, report ProgressReporter) (string, error) {
	modelPath := filepath.Join(toolsRoot, "embedding-models", modelName)
	if info, err := os.Stat(modelPath); err == nil && !info.IsDir() {
		return modelPath, nil
	}
	data, err := download.Verified(ctx, modelURL, modelSHA256, maxModelBytes, "Embedding model", report)
	if err != nil {
		return "", fmt.Errorf("download embedding model: %w", err)
	}
	if err := atomicfile.Write(modelPath, data, 0o600); err != nil {
		return "", fmt.Errorf("install embedding model: %w", err)
	}
	return modelPath, nil
}
