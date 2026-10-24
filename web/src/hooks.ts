import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api'

/** Fetches a GET endpoint; pass null to skip. Re-fetches when path changes. */
export function useApi<T>(path: string | null, pollMs?: number) {
  // Results are tagged with their path so a new path never sees the previous
  // path's data, not even for one render (e.g. /play/4 → /play/5).
  const [result, setResult] = useState<{ path: string; data: T | null; error: string | null } | null>(null)
  const pathRef = useRef(path)
  pathRef.current = path

  const reload = useCallback(async () => {
    const p = pathRef.current
    if (!p) return
    try {
      const d = await api.get<T>(p)
      if (pathRef.current === p) setResult({ path: p, data: d, error: null })
    } catch (e) {
      if (pathRef.current === p) setResult((r) => ({ path: p, data: r?.path === p ? r.data : null, error: (e as Error).message }))
    }
  }, [])

  useEffect(() => {
    reload()
    if (!pollMs || !path) return
    const t = window.setInterval(reload, pollMs)
    return () => window.clearInterval(t)
  }, [path, pollMs, reload])

  const current = result && result.path === path ? result : null
  const setData = useCallback((d: T) => path && setResult({ path, data: d, error: null }), [path])
  return { data: current?.data ?? null, error: current?.error ?? null, loading: !!path && !current, reload, setData }
}

export function useDocumentTitle(title: string | undefined) {
  useEffect(() => {
    document.title = title ? `${title} · BingeBandit` : 'BingeBandit'
  }, [title])
}
