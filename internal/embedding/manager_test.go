package embedding

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// TestManagerEnsureAndEmbed is a real, end-to-end check: it downloads the
// pinned llama.cpp binary and embedding model (~35MB total, cached after the
// first run) and starts the actual local server. It is opt-in via
// SESSIONHUB_TEST_NETWORK=1 so the normal test suite never depends on
// network access.
func TestManagerEnsureAndEmbed(t *testing.T) {
	if os.Getenv("SESSIONHUB_TEST_NETWORK") != "1" {
		t.Skip("set SESSIONHUB_TEST_NETWORK=1 to run the real download+embed integration test")
	}
	toolsRoot := t.TempDir()
	manager := NewManager(toolsRoot)
	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := manager.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	vecA, err := manager.Embed(ctx, "func NewProject creates a project")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecA) != Dimensions {
		t.Fatalf("expected %d dimensions, got %d", Dimensions, len(vecA))
	}
	vecB, err := manager.Embed(ctx, "creating a new project instance")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	vecC, err := manager.Embed(ctx, "the quick brown fox jumps over the lazy dog")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	related := cosineSimilarity(vecA, vecB)
	unrelated := cosineSimilarity(vecA, vecC)
	if related <= unrelated {
		t.Fatalf("expected semantically related text to score higher: related=%.4f unrelated=%.4f", related, unrelated)
	}
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
