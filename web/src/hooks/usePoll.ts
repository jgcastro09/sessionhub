import { useEffect, useRef, useState } from 'react'
import { UnauthorizedError } from '../api'

interface PollResult<T> {
  data: T | undefined
  error: string | undefined
  loading: boolean
}

// Project-scoped SSE lives at /api/v2/projects/{projectID}/events; polling
// remains a fallback for the global catalogue.
// polling keeps the monitoring views useful in the meantime and SSE can
// simply replace the interval later without changing any view component.
export function usePoll<T>(fetcher: () => Promise<T>, intervalMs: number, onUnauthorized: () => void, deps: unknown[] = []): PollResult<T> {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<string>()
  const [loading, setLoading] = useState(true)
  const onUnauthorizedRef = useRef(onUnauthorized)
  onUnauthorizedRef.current = onUnauthorized

  useEffect(() => {
    let cancelled = false
    setLoading(true)

    const tick = () => {
      fetcher()
        .then((result) => {
          if (cancelled) return
          setData(result)
          setError(undefined)
        })
        .catch((err) => {
          if (cancelled) return
          if (err instanceof UnauthorizedError) {
            onUnauthorizedRef.current()
            return
          }
          setError(err instanceof Error ? err.message : String(err))
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }

    tick()
    const id = window.setInterval(tick, intervalMs)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, error, loading }
}
