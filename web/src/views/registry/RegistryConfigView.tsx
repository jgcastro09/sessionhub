import { useEffect, useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import type { RegistryConfig, RegistryReasons } from '../../types'

function ReasonsEditor({
  title,
  hint,
  reasons,
  onChange,
}: {
  title: string
  hint: string
  reasons: RegistryReasons
  onChange: (next: RegistryReasons) => void
}) {
  const [newKey, setNewKey] = useState('')
  const [newReason, setNewReason] = useState('')
  const entries = Object.entries(reasons)

  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <div className="card-title">{title}</div>
      <div className="card-subtitle" style={{ marginBottom: '0.5rem' }}>
        {hint}
      </div>
      {entries.map(([key, reason]) => (
        <div className="registry-reasons-row" key={key}>
          <code>{key}</code>
          <input
            className="text-input"
            value={reason}
            onChange={(e) => onChange({ ...reasons, [key]: e.target.value })}
            placeholder="justificativa (obrigatória)"
          />
          <button
            type="button"
            className="btn-link"
            onClick={() => {
              const next = { ...reasons }
              delete next[key]
              onChange(next)
            }}
          >
            remover
          </button>
        </div>
      ))}
      <div className="registry-reasons-row">
        <input className="text-input" placeholder="chave (ex: .ext, nome-de-arquivo, prefixo/)" value={newKey} onChange={(e) => setNewKey(e.target.value)} />
        <input className="text-input" placeholder="justificativa" value={newReason} onChange={(e) => setNewReason(e.target.value)} />
        <button
          type="button"
          className="btn-link"
          disabled={!newKey.trim() || !newReason.trim()}
          onClick={() => {
            onChange({ ...reasons, [newKey.trim()]: newReason.trim() })
            setNewKey('')
            setNewReason('')
          }}
        >
          adicionar
        </button>
      </div>
    </div>
  )
}

export function RegistryConfigView({
  projectId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
}) {
  const { data: loaded, error } = usePoll(() => api.registryConfig(projectId), 30000, onUnauthorized, [projectId, tick])
  const [cfg, setCfg] = useState<RegistryConfig>()
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string>()
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (loaded && !cfg) setCfg(loaded)
  }, [loaded, cfg])

  async function save() {
    if (!cfg) return
    setSaving(true)
    setSaveError(undefined)
    setSaved(false)
    try {
      const updated = await api.saveRegistryConfig(projectId, cfg)
      setCfg(updated)
      setSaved(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (error) return <div className="error-banner">{error}</div>
  if (!cfg) return <div className="empty-state">carregando…</div>

  return (
    <>
      <div className="view-title-row">
        <div className="view-title">Configuração do Registry</div>
        <button type="button" className="btn-primary" onClick={save} disabled={saving}>
          {saving ? 'salvando…' : 'salvar configuração'}
        </button>
      </div>
      {saveError && <div className="error-banner">{saveError}</div>}
      {saved && !saveError && <div className="registry-health-banner healthy">configuração salva.</div>}

      <p className="card-subtitle" style={{ marginBottom: '1rem' }}>
        Toda regra de elegibilidade exige uma justificativa — o servidor rejeita a gravação se qualquer uma estiver vazia.
        Exclusão sempre vence: um arquivo excluído nunca é indexado, mesmo que também combine com uma regra de inclusão.
      </p>

      <ReasonsEditor
        title="Extensões elegíveis"
        hint="Extensão (com ponto, minúscula) → por que ela é indexada."
        reasons={cfg.eligibility.extension_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, extension_reasons: next } })}
      />
      <ReasonsEditor
        title="Nomes de arquivo exatos elegíveis"
        hint="Ex.: Makefile, Dockerfile — arquivos sem extensão útil."
        reasons={cfg.eligibility.exact_filename_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, exact_filename_reasons: next } })}
      />
      <ReasonsEditor
        title="Inclusões explícitas"
        hint="Caminho relativo exato → por que este arquivo específico deve entrar mesmo fora da whitelist geral."
        reasons={cfg.eligibility.explicit_include_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, explicit_include_reasons: next } })}
      />
      <ReasonsEditor
        title="Diretórios excluídos"
        hint="Nome do diretório (não caminho) → por que ele é totalmente ignorado, nunca percorrido."
        reasons={cfg.eligibility.exclude_directory_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, exclude_directory_reasons: next } })}
      />
      <ReasonsEditor
        title="Prefixos de caminho excluídos"
        hint="Prefixo de caminho relativo → por que tudo sob ele é ignorado."
        reasons={cfg.eligibility.exclude_path_prefix_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, exclude_path_prefix_reasons: next } })}
      />
      <ReasonsEditor
        title="Arquivos excluídos"
        hint="Caminho relativo exato → por que este arquivo específico é ignorado."
        reasons={cfg.eligibility.exclude_file_reasons}
        onChange={(next) => setCfg({ ...cfg, eligibility: { ...cfg.eligibility, exclude_file_reasons: next } })}
      />

      <div className="card">
        <div className="card-title">Regras de classificação (somente leitura)</div>
        <div className="card-subtitle" style={{ marginBottom: '0.5rem' }}>
          Categoria e tipo são atribuídos por essas regras ordenadas — a primeira que combinar vence.
        </div>
        <ul>
          {cfg.classification.category_rules.map((rule, i) => (
            <li key={i}>
              {rule.fallback
                ? `(padrão) → ${rule.category}`
                : `${[...(rule.path_prefixes ?? []), ...(rule.extensions ?? []), ...(rule.exact_filenames ?? [])].join(', ')} → ${rule.category}`}
            </li>
          ))}
        </ul>
      </div>
    </>
  )
}
