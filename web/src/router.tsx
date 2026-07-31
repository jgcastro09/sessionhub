import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

// A deliberately minimal client-side router: the rest of this SPA has no
// server-rendered routes, no data loaders, and no framework-mode surface
// (see api.ts's plain fetch() calls) — pulling in a full routing library for
// "match /projects/:id/tasks/:taskID and expose params" would be a lot of
// dependency for very little behavior, so this is ~60 lines instead.

interface RouterContextValue {
  path: string
  navigate: (to: string) => void
}

const RouterContext = createContext<RouterContextValue | null>(null)

export function RouterProvider({ children }: { children: ReactNode }) {
  const [path, setPath] = useState(() => window.location.pathname)

  useEffect(() => {
    const onPopState = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const navigate = (to: string) => {
    if (to === window.location.pathname) return
    window.history.pushState(null, '', to)
    setPath(to)
  }

  return <RouterContext.Provider value={{ path, navigate }}>{children}</RouterContext.Provider>
}

function useRouter(): RouterContextValue {
  const ctx = useContext(RouterContext)
  if (!ctx) throw new Error('useRouter must be used inside RouterProvider')
  return ctx
}

export function usePath(): string {
  return useRouter().path
}

export function useNavigate(): (to: string) => void {
  return useRouter().navigate
}

/** matchPath("/projects/:projectID/tasks/:taskID", "/projects/abc/tasks/TASK-1")
 *    -> { projectID: "abc", taskID: "TASK-1" } */
export function matchPath(pattern: string, path: string): Record<string, string> | null {
  const patternParts = pattern.split('/').filter(Boolean)
  const pathParts = path.split('/').filter(Boolean)
  if (patternParts.length !== pathParts.length) return null
  const params: Record<string, string> = {}
  for (let i = 0; i < patternParts.length; i++) {
    const part = patternParts[i]
    const value = decodeURIComponent(pathParts[i])
    if (part.startsWith(':')) {
      params[part.slice(1)] = value
    } else if (part !== pathParts[i]) {
      return null
    }
  }
  return params
}

export function Link({ to, children, className }: { to: string; children: ReactNode; className?: string }) {
  const navigate = useNavigate()
  return (
    <a
      href={to}
      className={className}
      onClick={(event) => {
        event.preventDefault()
        navigate(to)
      }}
    >
      {children}
    </a>
  )
}
