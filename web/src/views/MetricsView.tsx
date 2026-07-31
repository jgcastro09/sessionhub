import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { formatCost, formatTokens } from '../format'
import type { Session } from '../types'

export function MetricsView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
  const { data: totals, error } = usePoll(() => api.metrics(), 20000, onUnauthorized, [tick])
  const { data: sessions } = usePoll(() => api.sessions(), 20000, onUnauthorized, [tick])
  const { data: perSession } = usePoll(
    () => fetchPerSessionMetrics(sessions ?? []),
    20000,
    onUnauthorized,
    [tick, sessions?.map((s) => s.id).join(',')],
  )

  return (
    <>
      <div className="view-title">Métricas</div>
      {error && <div className="error-banner">{error}</div>}
      {totals && (
        <div className="stat-grid">
          <div className="stat-tile">
            <div className="label">Custo total</div>
            <div className="value">{formatCost(totals.cost_micros_usd)}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Tokens entrada</div>
            <div className="value">{formatTokens(totals.input_tokens)}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Tokens saída</div>
            <div className="value">{formatTokens(totals.output_tokens)}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Cache lido</div>
            <div className="value">{formatTokens(totals.cache_read)}</div>
          </div>
        </div>
      )}
      {perSession && perSession.length > 0 && (
        <div className="card-list">
          {perSession.map(({ session, metric }) => (
            <div className="card" key={session.id}>
              <div className="card-row">
                <div className="card-title">{session.name}</div>
                <b>{formatCost(metric.cost_micros_usd)}</b>
              </div>
              <div className="card-meta">
                <span>entrada <b>{formatTokens(metric.input_tokens)}</b></span>
                <span>saída <b>{formatTokens(metric.output_tokens)}</b></span>
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

async function fetchPerSessionMetrics(sessions: Session[]) {
  const results = await Promise.all(
    sessions.map(async (session) => ({ session, metric: await api.metrics(session.id) })),
  )
  return results
}
