import type { State } from '../types'

const STATE_STYLE: Record<string, { color: string; label: string }> = {
  running: { color: 'var(--state-running)', label: 'rodando' },
  pending: { color: 'var(--state-idle)', label: 'pendente' },
  queued: { color: 'var(--state-queued)', label: 'na fila' },
  waiting_approval: { color: 'var(--state-queued)', label: 'aguardando' },
  succeeded: { color: 'var(--state-running)', label: 'concluído' },
  failed: { color: 'var(--state-failed)', label: 'falhou' },
  canceled: { color: 'var(--state-idle)', label: 'cancelado' },
}

export function StatusBadge({ state }: { state: State }) {
  const style = STATE_STYLE[state] ?? { color: 'var(--state-idle)', label: state }
  return (
    <span className="status-badge">
      <span className="dot" style={{ background: style.color }} />
      {style.label}
    </span>
  )
}
