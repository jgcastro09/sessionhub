import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { StatusBadge } from '../components/StatusBadge'
import { formatRelativeTime } from '../format'

export function QueueView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
  const { data: queue, error: queueError } = usePoll(() => api.queue(), 20000, onUnauthorized, [tick])
  const { data: schedules, error: schedulesError } = usePoll(() => api.schedules(), 20000, onUnauthorized, [tick])
  const { data: pipelines, error: pipelinesError } = usePoll(() => api.pipelines(), 20000, onUnauthorized, [tick])
  const error = queueError ?? schedulesError ?? pipelinesError

  const nothing = queue?.length === 0 && schedules?.length === 0 && pipelines?.length === 0

  return (
    <>
      <div className="view-title">Fila</div>
      {error && <div className="error-banner">{error}</div>}
      {nothing && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Sem itens na fila, schedules ou pipelines agora.
        </div>
      )}

      {!!pipelines?.length && (
        <>
          <div className="view-title">Pipelines</div>
          <div className="card-list" style={{ marginBottom: 20 }}>
            {pipelines.map((pipeline) => (
              <div className="card" key={pipeline.id}>
                <div className="card-row">
                  <div className="card-title">{pipeline.name}</div>
                  <StatusBadge state={pipeline.state} />
                </div>
                <div className="card-meta">
                  <span>criada <b>{formatRelativeTime(pipeline.created_at)}</b></span>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {!!queue?.length && (
        <>
          <div className="view-title">Prompts na fila</div>
          <div className="card-list" style={{ marginBottom: 20 }}>
            {queue.map((item) => (
              <div className="card" key={item.id}>
                <div className="card-row">
                  <div className="card-title">{item.prompt.slice(0, 60) || '(sem prompt)'}</div>
                  <StatusBadge state={item.state} />
                </div>
                <div className="card-meta">
                  <span>prioridade <b>{item.priority}</b></span>
                  <span>criado <b>{formatRelativeTime(item.created_at)}</b></span>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {!!schedules?.length && (
        <>
          <div className="view-title">Schedules</div>
          <div className="card-list">
            {schedules.map((schedule) => (
              <div className="card" key={schedule.id}>
                <div className="card-row">
                  <div className="card-title">{schedule.name}</div>
                  <span className="status-badge">
                    <span className="dot" style={{ background: schedule.enabled ? 'var(--state-running)' : 'var(--state-idle)' }} />
                    {schedule.enabled ? 'ativo' : 'pausado'}
                  </span>
                </div>
                <div className="card-meta">
                  <span>{schedule.kind}</span>
                  {schedule.next_run && <span>próxima <b>{formatRelativeTime(schedule.next_run)}</b></span>}
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </>
  )
}
