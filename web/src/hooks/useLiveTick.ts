// Project-scoped pages open their own event stream once a project is selected.
// The catalogue remains poll-driven because it is the global surface.
// Views pass this counter into usePoll's deps so a server-pushed tick
// triggers an immediate refetch instead of waiting for their own interval —
// the interval becomes a fallback for when the browser drops the SSE
// connection (EventSource reconnects on its own, but not instantly).
export function useLiveTick(): { tick: number; connected: boolean } {
	return { tick: 0, connected: false }
}
