import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

function BarList({ data, total }: { data: Record<string, number>; total: number }) {
  const entries = Object.entries(data)
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 10)
  if (entries.length === 0) return <div className="card-subtitle">sem dados</div>
  return (
    <div className="registry-bar-list">
      {entries.map(([label, count]) => (
        <div className="registry-bar-row" key={label}>
          <div className="registry-bar-label">{label || '(sem categoria)'}</div>
          <div className="registry-bar-track">
            <div className="registry-bar-fill" style={{ width: `${total > 0 ? (count / total) * 100 : 0}%` }} />
          </div>
          <div className="registry-bar-value">{count}</div>
        </div>
      ))}
    </div>
  )
}

export function RegistryOverviewView({
  projectId,
  tick,
  onUnauthorized,
  onScan,
  scanning,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  onScan: () => void
  scanning: boolean
}) {
  const { data: health, error: healthError } = usePoll(() => api.registryHealth(projectId), 15000, onUnauthorized, [projectId, tick])
  const { data: stats, error: statsError } = usePoll(() => api.registryStats(projectId), 15000, onUnauthorized, [projectId, tick])

  return (
    <>
      <div className="view-title-row">
        <div className="view-title">Visão geral</div>
        <button type="button" className="btn-primary" onClick={onScan} disabled={scanning}>
          {scanning ? 'escaneando…' : 'escanear'}
        </button>
      </div>
      {healthError && <div className="error-banner">{healthError}</div>}
      {statsError && <div className="error-banner">{statsError}</div>}

      {health && (
        <div className={`registry-health-banner ${health.healthy ? 'healthy' : 'unhealthy'}`}>
          {health.healthy ? 'registry saudável' : 'registry com pendências'}
        </div>
      )}

      {stats && (
        <div className="stat-grid">
          <div className="stat-tile">
            <div className="label">Arquivos</div>
            <div className="value">{stats.total_files}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Linhas</div>
            <div className="value">{stats.total_lines}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Módulos</div>
            <div className="value">{Object.keys(stats.by_module).length}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Áreas</div>
            <div className="value">{Object.keys(stats.by_area).length}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Precisam revisão</div>
            <div className="value">{stats.by_review_status['needs_review'] ?? 0}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Pendentes de classificação</div>
            <div className="value">{health?.pending_classification_count ?? 0}</div>
          </div>
        </div>
      )}

      {health && !health.healthy && (
        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-title">Pendências</div>
          <ul>
            {health.missing_paths.length > 0 && <li>{health.missing_paths.length} arquivo(s) descobertos sem registro</li>}
            {health.orphaned_entries.length > 0 && <li>{health.orphaned_entries.length} entrada(s) órfã(s) (arquivo removido)</li>}
            {health.stale_reviews.length > 0 && <li>{health.stale_reviews.length} entrada(s) com revisão desatualizada</li>}
            {health.schema_issues.length > 0 && <li>{health.schema_issues.length} problema(s) de metadados em entradas revisadas</li>}
            {health.classification_issues.length > 0 && <li>{health.classification_issues.length} entrada(s) sem classificação</li>}
            {health.dangling_relationships.length > 0 && <li>{health.dangling_relationships.length} relação(ões) apontando para entradas inexistentes</li>}
            {health.dependency_issues.length > 0 && <li>{health.dependency_issues.length} dependência(s) ambígua(s)</li>}
            {health.pending_classification_count > 0 && <li>{health.pending_classification_count} arquivo(s) pendentes de classificação</li>}
          </ul>
        </div>
      )}

      {stats && (
        <div className="registry-charts-grid">
          <div className="card">
            <div className="card-title">Por linguagem</div>
            <BarList data={stats.by_language} total={stats.total_files} />
          </div>
          <div className="card">
            <div className="card-title">Por categoria</div>
            <BarList data={stats.by_category} total={stats.total_files} />
          </div>
          <div className="card">
            <div className="card-title">Por módulo</div>
            <BarList data={stats.by_module} total={stats.total_files} />
          </div>
          <div className="card">
            <div className="card-title">Por criticidade</div>
            <BarList data={stats.by_criticality} total={stats.total_files} />
          </div>
        </div>
      )}
    </>
  )
}
