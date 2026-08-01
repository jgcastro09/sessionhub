import { useState } from 'react'
import { api } from '../../api'
import { Link } from '../../router'
import { RegistryOverviewView } from './RegistryOverviewView'
import { RegistryAllFilesView } from './RegistryAllFilesView'
import { RegistryArchitectureView } from './RegistryArchitectureView'
import { RegistryAreasView } from './RegistryAreasView'
import { RegistryRelationshipsView } from './RegistryRelationshipsView'
import { RegistryReaderView } from './RegistryReaderView'
import { RegistryGlobalSearchView } from './RegistryGlobalSearchView'
import { RegistryGitAuditView } from './RegistryGitAuditView'
import { RegistryReviewQueueView } from './RegistryReviewQueueView'
import { RegistryPendingView } from './RegistryPendingView'
import { RegistryConfigView } from './RegistryConfigView'
import { RegistryInspectorPanel } from './RegistryInspectorPanel'

type Section =
  | 'overview'
  | 'all-files'
  | 'architecture'
  | 'areas'
  | 'relationships'
  | 'reader'
  | 'search'
  | 'git-audit'
  | 'review-queue'
  | 'pending'
  | 'config'

const SECTIONS: { id: Section; label: string }[] = [
  { id: 'overview', label: 'Visão geral' },
  { id: 'all-files', label: 'Todos os arquivos' },
  { id: 'architecture', label: 'Arquitetura' },
  { id: 'areas', label: 'Product Areas' },
  { id: 'relationships', label: 'Relacionamentos' },
  { id: 'reader', label: 'Leitor' },
  { id: 'search', label: 'Busca' },
  { id: 'git-audit', label: 'Auditoria Git' },
  { id: 'review-queue', label: 'Fila de revisão' },
  { id: 'pending', label: 'Pendentes' },
  { id: 'config', label: 'Configuração' },
]

// The full-featured, independent Code Registry panel — replaces the old
// single-page RegistryView/RegistryEntryView with a sub-tabbed shell that
// owns the universal Inspector panel, opened from any section's file click.
export function RegistryLayout({
  projectId,
  entryId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  entryId?: string
  tick: number
  onUnauthorized: () => void
}) {
  const [section, setSection] = useState<Section>('overview')
  const [inspectorEntryId, setInspectorEntryId] = useState<string | undefined>(entryId)
  const [relationshipsSeed, setRelationshipsSeed] = useState<string>()
  const [readerEntryId, setReaderEntryId] = useState<string>()
  const [scanning, setScanning] = useState(false)
  const [scanError, setScanError] = useState<string>()

  async function scan() {
    setScanning(true)
    setScanError(undefined)
    try {
      await api.registryScan(projectId)
    } catch (err) {
      setScanError(err instanceof Error ? err.message : String(err))
    } finally {
      setScanning(false)
    }
  }

  const openEntry = (id: string) => setInspectorEntryId(id)

  return (
    <>
      <Link to="/registry" className="back-link">
        ← registry
      </Link>
      <div className="registry-subtabs">
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            type="button"
            className={`registry-subtab ${section === s.id ? 'active' : ''}`}
            onClick={() => setSection(s.id)}
          >
            {s.label}
          </button>
        ))}
      </div>
      {scanError && <div className="error-banner">{scanError}</div>}

      {section === 'overview' && (
        <RegistryOverviewView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onScan={scan} scanning={scanning} />
      )}
      {section === 'all-files' && <RegistryAllFilesView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />}
      {section === 'architecture' && (
        <RegistryArchitectureView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />
      )}
      {section === 'areas' && <RegistryAreasView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />}
      {section === 'relationships' && (
        <RegistryRelationshipsView
          projectId={projectId}
          tick={tick}
          onUnauthorized={onUnauthorized}
          onOpenEntry={openEntry}
          initialSeedEntryId={relationshipsSeed}
        />
      )}
      {section === 'reader' && (
        <RegistryReaderView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} initialEntryId={readerEntryId} />
      )}
      {section === 'search' && <RegistryGlobalSearchView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />}
      {section === 'git-audit' && <RegistryGitAuditView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />}
      {section === 'review-queue' && (
        <RegistryReviewQueueView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} onOpenEntry={openEntry} />
      )}
      {section === 'pending' && <RegistryPendingView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} />}
      {section === 'config' && <RegistryConfigView projectId={projectId} tick={tick} onUnauthorized={onUnauthorized} />}

      {inspectorEntryId && (
        <RegistryInspectorPanel
          projectId={projectId}
          entryId={inspectorEntryId}
          tick={tick}
          onUnauthorized={onUnauthorized}
          onClose={() => setInspectorEntryId(undefined)}
          onNavigateEntry={(id) => setInspectorEntryId(id)}
          onOpenReader={(id) => {
            setReaderEntryId(id)
            setSection('reader')
            setInspectorEntryId(undefined)
          }}
          onOpenRelationships={(id) => {
            setRelationshipsSeed(id)
            setSection('relationships')
            setInspectorEntryId(undefined)
          }}
        />
      )}
    </>
  )
}
