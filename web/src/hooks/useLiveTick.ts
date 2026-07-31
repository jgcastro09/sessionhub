import { useEffect, useState } from 'react'

// Opens one long-lived EventSource against /api/events and counts the
// "tick" pushes the server sends (see internal/webserver/events.go).
// Views pass this counter into usePoll's deps so a server-pushed tick
// triggers an immediate refetch instead of waiting for their own interval —
// the interval becomes a fallback for when the browser drops the SSE
// connection (EventSource reconnects on its own, but not instantly).
export function useLiveTick(): { tick: number; connected: boolean } {
  const [tick, setTick] = useState(0)
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    const source = new EventSource('/api/events', { withCredentials: true })
    source.addEventListener('open', () => setConnected(true))
    source.addEventListener('error', () => setConnected(false))
    source.addEventListener('tick', () => setTick((n) => n + 1))
    return () => source.close()
  }, [])

  return { tick, connected }
}
