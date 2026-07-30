package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadVerifiedReportsProgress(t *testing.T) {
	payload := []byte("offline voice model")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "19")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	var updates []Progress
	got, err := downloadVerified(context.Background(), server.URL, hex.EncodeToString(sum[:]), 1024, "Whisper model", func(progress Progress) {
		updates = append(updates, progress)
	})
	if err != nil {
		t.Fatalf("downloadVerified: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded %q, want %q", got, payload)
	}
	if len(updates) < 3 {
		t.Fatalf("received %d progress updates, want at least 3", len(updates))
	}
	last := updates[len(updates)-1]
	if last.Stage != "Verifying Whisper model" || last.Current != int64(len(payload)) || last.Total != int64(len(payload)) {
		t.Fatalf("final progress = %#v, want verification at %d bytes", last, len(payload))
	}
}

func TestEnsureModelReusesLegacyModelWithoutNetwork(t *testing.T) {
	toolsRoot := t.TempDir()
	legacyDir := filepath.Join(toolsRoot, "whisper-macos", "v0.3.7")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, modelName)
	if err := os.WriteFile(legacyPath, []byte("previously verified model"), 0o600); err != nil {
		t.Fatal(err)
	}

	modelPath, err := ensureModel(context.Background(), toolsRoot, legacyDir, nil)
	if err != nil {
		t.Fatalf("ensureModel: %v", err)
	}
	if want := filepath.Join(toolsRoot, "whisper-models", modelName); modelPath != want {
		t.Fatalf("model path = %q, want %q", modelPath, want)
	}
	got, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previously verified model" {
		t.Fatalf("reused model = %q", got)
	}
}
