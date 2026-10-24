import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api'
import { PosterCard } from '../components/Cards'
import { useDocumentTitle } from '../hooks'
import type { Item, Library } from '../types'

const PAGE = 120
const sorts = [
  ['title', 'Title'],
  ['added', 'Date added'],
  ['release', 'Release date'],
  ['year', 'Year'],
  ['rating', 'Rating'],
] as const

interface Page {
  library: Library
  items: Item[]
  total: number
}

// Keyed by id so switching libraries resets paging and restores that library's sort.
export default function LibraryPage() {
  const { id } = useParams()
  return <LibraryView key={id} id={id!} />
}

function LibraryView({ id }: { id: string }) {
  const [sort, setSort] = useState(() => localStorage.getItem(`bb.sort.${id}`) ?? 'title')
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const sentinel = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(false)
  useDocumentTitle(library?.name)

  const load = useCallback(
    async (offset: number) => {
      if (loadingRef.current) return
      loadingRef.current = true
      setLoading(true)
      try {
        const page = await api.get<Page>(`/api/libraries/${id}/items?sort=${sort}&offset=${offset}&limit=${PAGE}`)
        setLibrary(page.library)
        setTotal(page.total)
        setItems((prev) => (offset === 0 ? page.items : [...prev, ...page.items]))
      } finally {
        loadingRef.current = false
        setLoading(false)
      }
    },
    [id, sort],
  )

  useEffect(() => {
    setItems([])
    load(0)
  }, [load])

  useEffect(() => {
    const el = sentinel.current
    if (!el) return
    const obs = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && items.length < total) load(items.length)
    }, { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [items.length, total, load])

  const changeSort = (s: string) => {
    localStorage.setItem(`bb.sort.${id}`, s)
    setSort(s)
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{library?.name ?? ' '}</h1>
          <div className="muted">{total} {library?.kind === 'shows' ? 'shows' : 'movies'}</div>
        </div>
        <label className="select-label">
          Sort
          <select value={sort} onChange={(e) => changeSort(e.target.value)}>
            {sorts.map(([v, label]) => (
              <option key={v} value={v}>{label}</option>
            ))}
          </select>
        </label>
      </header>
      {!loading && items.length === 0 && <div className="empty">This library is empty. Scans can take a few minutes for big folders.</div>}
      <div className="poster-grid">
        {items.map((it) => (
          <PosterCard key={it.id} item={it} />
        ))}
      </div>
      <div ref={sentinel} style={{ height: 1 }} />
    </div>
  )
}
