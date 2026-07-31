package registry

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

// ErrSemanticUnavailable is returned by SemanticSearch whenever no Embedder
// is wired in, or the embedding engine could not start. Callers must fall
// back to lexical search rather than surface this as a hard failure — busca
// lexical continua sempre disponível como caminho determinístico (plan 5.4).
var ErrSemanticUnavailable = errors.New("semantic search is not available")

// Embedder is the only integration point internal/registry has with
// internal/embedding's local llama.cpp engine, so internal/registry never
// depends on how embeddings are produced, downloaded, or verified.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// SetEmbedder wires in the embedding engine. Safe to call once at startup.
func (s *Service) SetEmbedder(e Embedder) { s.embedder = e }

// indexedText is the text an entry is embedded from: path and symbols carry
// the most identifying signal for source code, then human-reviewed context.
func indexedText(e Entry) string {
	parts := []string{e.Path, e.Module, strings.Join(e.Symbols, " "), e.Description, strings.Join(e.Responsibilities, " ")}
	return strings.Join(parts, "\n")
}

// EnsureSemanticIndex embeds every active entry whose EmbeddingHash doesn't
// match its current Hash (new entries and entries whose content changed
// since the last embedding), then persists the updated vectors. Safe to call
// often — an already-current index costs nothing but the initial file read.
func (s *Service) EnsureSemanticIndex(ctx context.Context, projectID string) error {
	if s.embedder == nil {
		return ErrSemanticUnavailable
	}
	root, err := s.root(projectID)
	if err != nil {
		return err
	}
	lock := s.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	entries, err := loadAllEntries(root)
	if err != nil {
		return err
	}
	dirty := false
	now := time.Now().UTC()
	for i := range entries {
		e := &entries[i]
		if e.Status != StatusActive || e.EmbeddingHash == e.Hash {
			continue
		}
		vector, err := s.embedder.Embed(ctx, indexedText(*e))
		if err != nil {
			return err
		}
		e.Embedding = vector
		e.EmbeddingHash = e.Hash
		e.UpdatedAt = now
		dirty = true
	}
	if dirty {
		return saveAllEntries(root, entries)
	}
	return nil
}

// SemanticSearch embeds query and ranks active, already-indexed entries by
// cosine similarity. It never returns a file that is not a currently active
// Registry entry (unindexed/missing entries are simply skipped).
func (s *Service) SemanticSearch(ctx context.Context, projectID, query string, limit int) ([]SearchResult, error) {
	if s.embedder == nil {
		return nil, ErrSemanticUnavailable
	}
	if err := s.EnsureSemanticIndex(ctx, projectID); err != nil {
		return nil, err
	}
	entries, err := s.List(projectID, false)
	if err != nil {
		return nil, err
	}
	queryVector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	for _, e := range entries {
		if len(e.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(queryVector, e.Embedding)
		results = append(results, SearchResult{Entry: e, Score: int(score * 1000)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
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
