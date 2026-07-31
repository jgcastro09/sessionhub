import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { formatCost, formatTokens } from '../format'
import type { Project } from '../types'

export function MetricsView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
	const { data: projects } = usePoll(() => api.projects(), 20000, onUnauthorized, [tick])
	const { data: perProject, error } = usePoll(
    () => fetchPerProjectMetrics(projects ?? []),
    20000,
    onUnauthorized,
		[tick, projects?.map((s) => s.id).join(',')],
	)
	const totals = perProject?.reduce((sum, { metric }) => ({
		input_tokens: sum.input_tokens + metric.input_tokens,
		output_tokens: sum.output_tokens + metric.output_tokens,
		cache_read: sum.cache_read + metric.cache_read,
		cost_micros_usd: sum.cost_micros_usd + metric.cost_micros_usd,
	}), { input_tokens: 0, output_tokens: 0, cache_read: 0, cost_micros_usd: 0 })

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
      {perProject && perProject.length > 0 && (
        <div className="card-list">
          {perProject.map(({ project, metric }) => (
            <div className="card" key={project.id}>
              <div className="card-row">
                <div className="card-title">{project.name}</div>
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

async function fetchPerProjectMetrics(projects: Project[]) {
  const results = await Promise.all(
    projects.map(async (project) => ({ project, metric: await api.metrics(project.id) })),
  )
  return results
}
