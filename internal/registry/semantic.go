package registry

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ErrSemanticUnavailable is returned by SemanticSearch whenever no Embedder
// is wired in. Callers must fall back to full-text/lexical search rather
// than surface this as a hard failure — that path is always available.
var ErrSemanticUnavailable = errors.New("semantic search is not available")

// Embedder is the only integration point internal/registry has with
// internal/embedding's local llama.cpp engine, so internal/registry never
// depends on how embeddings are produced, downloaded, or verified.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// SetEmbedder wires in the embedding engine. Safe to call once at startup.
func (s *Service) SetEmbedder(e Embedder) { s.embedder = e }

// embeddingModel is a fixed cache key today, since Embedder does not
// currently expose a model identifier/version of its own. The embeddings
// table is keyed by (entry_id, model) specifically so that changing this
// (or wiring a real model name through Embedder) never corrupts vectors
// from a different model — old rows simply age out unused.
const embeddingModel = "default"

// indexedText is the text an entry is embedded from: path and symbols carry
// the most identifying signal for source code, then human-reviewed context.
// Deliberately metadata-only (not raw file content), matching the reference
// system's approach and keeping embedding calls cheap.
func indexedText(e Entry) string {
	parts := []string{e.Path, e.Module, flattenSymbolNames(e.Symbols), e.Description, strings.Join(e.Responsibilities, " ")}
	return strings.Join(parts, "\n")
}

func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(buf []byte) []float32 {
	n := len(buf) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return out
}

// EnsureSemanticIndex embeds every active entry whose stored embedding hash
// doesn't match its current Hash (new entries and entries whose content
// changed since the last embedding) and persists the vectors into
// index.sqlite3's embeddings table. Safe to call often — an already-current
// index costs nothing but one cache read.
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
	db, err := s.indexDB(root)
	if err != nil {
		return err
	}
	indexedHashes := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT entry_id, hash FROM embeddings WHERE model = ?`, embeddingModel)
	if err != nil {
		return fmt.Errorf("read embedding cache: %w", err)
	}
	for rows.Next() {
		var entryID, hash string
		if err := rows.Scan(&entryID, &hash); err != nil {
			rows.Close()
			return err
		}
		indexedHashes[entryID] = hash
	}
	rows.Close()

	for _, e := range entries {
		if e.Status != StatusActive || indexedHashes[e.EntryID] == e.Hash {
			continue
		}
		vector, err := s.embedder.Embed(ctx, indexedText(e))
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO embeddings(entry_id, model, hash, vector) VALUES(?,?,?,?)
ON CONFLICT(entry_id, model) DO UPDATE SET hash=excluded.hash, vector=excluded.vector`,
			e.EntryID, embeddingModel, e.Hash, encodeVector(vector)); err != nil {
			return fmt.Errorf("store embedding: %w", err)
		}
	}
	return nil
}

// SemanticSearch embeds query and ranks active, already-indexed entries by
// cosine similarity. It never returns a file that is not a currently active
// Registry entry.
func (s *Service) SemanticSearch(ctx context.Context, projectID, query string, limit int) ([]SearchResult, error) {
	if s.embedder == nil {
		return nil, ErrSemanticUnavailable
	}
	if err := s.EnsureSemanticIndex(ctx, projectID); err != nil {
		return nil, err
	}
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	entries, err := s.List(projectID, false)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.EntryID] = e
	}
	db, err := s.indexDB(root)
	if err != nil {
		return nil, err
	}
	queryVector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT entry_id, vector FROM embeddings WHERE model = ?`, embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("read embeddings: %w", err)
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var entryID string
		var raw []byte
		if err := rows.Scan(&entryID, &raw); err != nil {
			return nil, err
		}
		e, ok := byID[entryID]
		if !ok {
			continue
		}
		score := cosineSimilarity(queryVector, decodeVector(raw))
		results = append(results, SearchResult{Entry: e, Score: int(score * 1000), MatchReasons: []string{"semantic match"}})
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
