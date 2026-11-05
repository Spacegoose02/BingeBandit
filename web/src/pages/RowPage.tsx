import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { api } from '../api'
import { readBack, writeBack } from '../backState'
import { PosterCard } from '../components/Cards'
import { useApi, useDocumentTitle } from '../hooks'
import type { Item } from '../types'

const PAGE = 120

interface RowPageData {
  title: string
  items: Item[]
}

/** "See all" of a built-in row (Recently Added, Top Rated, a genre…) from
 *  Home or a library's Recommended tab: /rows?key=recent&libraries=1,2. Its
 *  titles load a page at a time as you scroll. */
export default function RowPage() {
  const { search, key } = useLocation()
  const query = useMemo(() => {
    const q = new URLSearchParams(search)
    q.delete('offset')
    q.delete('limit')
    return q.toString()
  }, [search])
  // Back to it loads as many as were showing, so the scroll position is there.
  const first = useMemo(() => Math.min(500, Math.max(PAGE, readBack<number>(key, 'loaded') ?? 0)), [query, key]) // eslint-disable-line react-hooks/exhaustive-deps
  const { data, error } = useApi<RowPageData>(`/api/rows?${query}&limit=${first}`)
  const [more, setMore] = useState<Item[]>([])
  const [ended, setEnded] = useState(false)
  const sentinel = useRef<HTMLDivElement>(null)
  const busy = useRef(false)
  useDocumentTitle(data?.title)
  useEffect(() => {
    setMore([])
    setEnded(!!data && data.items.length < first)
  }, [data, first])

  const items = data ? [...data.items, ...more] : []
  useEffect(() => {
    if (items.length) writeBack(key, 'loaded', items.length)
  }, [key, items.length])
  useEffect(() => {
    const el = sentinel.current
    if (!el || !data || ended) return
    const obs = new IntersectionObserver(async (entries) => {
      if (!entries[0].isIntersecting || busy.current) return
      busy.current = true
      try {
        const page = await api.get<RowPageData>(`/api/rows?${query}&offset=${items.length}&limit=${PAGE}`)
        if (page.items.length < PAGE) setEnded(true)
        const have = new Set(items.map((it) => it.id))
        setMore((m) => [...m, ...page.items.filter((it) => !have.has(it.id))])
      } catch {
        setEnded(true)
      } finally {
        busy.current = false
      }
    }, { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [data, ended, items, query])

  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{data.title}</h1>
        </div>
      </header>
      {items.length === 0 && <div className="empty">Nothing here right now.</div>}
      <div className="poster-grid">
        {items.map((it) => (
          <PosterCard key={it.id} item={it} />
        ))}
      </div>
      <div ref={sentinel} style={{ height: 1 }} />
    </div>
  )
}
