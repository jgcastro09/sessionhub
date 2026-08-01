import { useMemo, useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

interface TreeNode {
  name: string
  path: string
  entryId?: string
  children: Map<string, TreeNode>
}

function buildTree(entries: { path: string; entry_id: string }[]): TreeNode {
  const root: TreeNode = { name: '', path: '', children: new Map() }
  for (const e of entries) {
    const segments = e.path.split('/')
    let node = root
    let acc = ''
    segments.forEach((segment, i) => {
      acc = acc ? `${acc}/${segment}` : segment
      let child = node.children.get(segment)
      if (!child) {
        child = { name: segment, path: acc, children: new Map() }
        node.children.set(segment, child)
      }
      if (i === segments.length - 1) child.entryId = e.entry_id
      node = child
    })
  }
  return root
}

function FileTree({ node, depth, selectedPath, onSelect }: { node: TreeNode; depth: number; selectedPath?: string; onSelect: (entryId: string, path: string) => void }) {
  const children = Array.from(node.children.values()).sort((a, b) => {
    const aDir = a.children.size > 0 ? 0 : 1
    const bDir = b.children.size > 0 ? 0 : 1
    return aDir !== bDir ? aDir - bDir : a.name.localeCompare(b.name)
  })
  return (
    <>
      {children.map((child) => (
        <div key={child.path}>
          <div
            className={`registry-tree-row ${selectedPath === child.path ? 'active' : ''}`}
            style={{ paddingLeft: `${depth * 1.1}rem` }}
            onClick={() => child.entryId && onSelect(child.entryId, child.path)}
          >
            {child.children.size > 0 ? '📁' : '📄'} {child.name}
          </div>
          {child.children.size > 0 && <FileTree node={child} depth={depth + 1} selectedPath={selectedPath} onSelect={onSelect} />}
        </div>
      ))}
    </>
  )
}

export function RegistryReaderView({
  projectId,
  tick,
  onUnauthorized,
  initialEntryId,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  initialEntryId?: string
}) {
  const [selected, setSelected] = useState<{ entryId: string; path: string } | undefined>(
    initialEntryId ? { entryId: initialEntryId, path: '' } : undefined,
  )
  const [ref, setRef] = useState<string>('')

  const { data: allEntries } = usePoll(
    () => api.registryEntries(projectId, { limit: 5000 }),
    30000,
    onUnauthorized,
    [projectId, tick],
  )
  const tree = useMemo(() => buildTree((allEntries?.results ?? []).map((r) => r.entry)), [allEntries])

  const { data: source, error: sourceError } = usePoll(
    () =>
      selected
        ? ref
          ? api.registrySourceAtRevision(projectId, selected.entryId, ref)
          : api.registrySource(projectId, selected.entryId)
        : Promise.resolve(undefined),
    30000,
    onUnauthorized,
    [projectId, selected?.entryId, ref, tick],
  )
  const { data: history } = usePoll(
    () => (selected ? api.registrySourceHistory(projectId, selected.entryId) : Promise.resolve(undefined)),
    30000,
    onUnauthorized,
    [projectId, selected?.entryId, tick],
  )

  const lines = source?.content.split('\n') ?? []

  return (
    <>
      <div className="view-title">Leitor de código</div>
      <div className="registry-split">
        <div className="registry-split-side registry-file-tree">
          <FileTree node={tree} depth={0} selectedPath={selected?.path} onSelect={(entryId, path) => {
            setSelected({ entryId, path })
            setRef('')
          }} />
        </div>
        <div className="registry-split-main">
          {!selected && <div className="empty-state">selecione um arquivo na árvore.</div>}
          {selected && (
            <>
              <div className="view-title-row">
                <div className="card-title">{selected.path || selected.entryId}</div>
                {history && history.length > 0 && (
                  <select className="text-input" value={ref} onChange={(e) => setRef(e.target.value)}>
                    <option value="">árvore de trabalho</option>
                    {history.map((rev) => (
                      <option key={rev.hash} value={rev.hash}>
                        {rev.hash.slice(0, 8)} · {rev.subject.slice(0, 40)}
                      </option>
                    ))}
                  </select>
                )}
              </div>
              {sourceError && <div className="error-banner">{sourceError}</div>}
              {source && (
                <div className="registry-reader-pane">
                  <pre className="registry-reader-code">
                    {lines.map((line, i) => (
                      <div className="registry-reader-line" key={i}>
                        <span className="registry-reader-line-number">{i + 1}</span>
                        <span>{line}</span>
                      </div>
                    ))}
                  </pre>
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </>
  )
}
