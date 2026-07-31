package registry

// ContextPack is a limited, ready-to-paste bundle of one entry plus the
// entries most related to it, per plan 5.3 ("gerar ... context packs
// limitados"). It exists so a task/editor/agent can pull in "what surrounds
// this file" without ever reading raw filesystem paths.
type ContextPack struct {
	Entry   Entry          `json:"entry"`
	Related []SearchResult `json:"related"`
}

// SearchResults is a named slice so callers get a Filter helper without
// reaching into internal/registry internals.
type SearchResults []SearchResult

// Filter drops a result matching excludeEntryID (typically the entry the
// caller already has, so it does not show up as "related to itself").
func (r SearchResults) Filter(excludeEntryID string) SearchResults {
	out := make(SearchResults, 0, len(r))
	for _, item := range r {
		if item.Entry.EntryID != excludeEntryID {
			out = append(out, item)
		}
	}
	return out
}
