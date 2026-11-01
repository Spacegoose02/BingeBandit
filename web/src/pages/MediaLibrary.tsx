import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { PosterCard } from '../components/Cards'
import { useAudio } from '../components/AudioPlayer'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { libraryKinds, mediaSubtitle } from '../libraryKinds'
import type { Item, Library, Track } from '../types'

const PAGE = 120

interface Page {
  library: Library
  items: Item[]
  total: number
}

const sorts = [
  ['title', 'Title'],
  ['added', 'Date added'],
  ['year', 'Year'],
] as const

interface FilterOptions {
  genres: string[]
  decades: number[]
}

/**
 * A music, audiobook or photo library: a grid of albums (or artists),
 * audiobooks or photo albums. Movies and shows use the Library page with
 * its rows and collections instead.
 */
export function MediaLibraryView({ library }: { library: Library }) {
  useDocumentTitle(library.name)
  const [params, setParams] = useSearchParams()
  const kind = libraryKinds[library.kind]
  const view = library.kind === 'music' && params.get('view') === 'artists' ? 'artists' : ''
  const genre = params.get('genre') ?? ''
  const [sort, setSort] = useState(() => localStorage.getItem(`bb.sort.${library.id}`) ?? (library.kind === 'photos' ? 'year' : 'title'))
  const [items, setItems] = useState<Item[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const loadingRef = useRef(false)
  const generation = useRef(0)
  const sentinel = useRef<HTMLDivElement>(null)
  const filters = useApi<FilterOptions>(library.kind === 'photos' ? null : `/api/libraries/${library.id}/filters`).data
  const { play } = useAudio()

  const query = `sort=${sort}${view ? `&view=${view}` : ''}${genre && !view ? `&genre=${encodeURIComponent(genre)}` : ''}`
  const load = useCallback(
    async (offset: number) => {
      if (offset === 0) generation.current++
      else if (loadingRef.current) return
      const gen = generation.current
      loadingRef.current = true
      setLoading(true)
      try {
        const page = await api.get<Page>(`/api/libraries/${library.id}/items?${query}&offset=${offset}&limit=${PAGE}`)
        if (gen !== generation.current) return
        setTotal(page.total)
        setItems((prev) => (offset === 0 ? page.items : [...prev, ...page.items.filter((it) => !prev.some((p) => p.id === it.id))]))
      } finally {
        if (gen === generation.current) {
          loadingRef.current = false
          setLoading(false)
        }
      }
    },
    [library.id, query],
  )
  useEffect(() => {
    setItems([])
    load(0)
  }, [load])
  useEffect(() => {
    const el = sentinel.current
    if (!el) return
    const obs = new IntersectionObserver((e) => e[0].isIntersecting && items.length < total && load(items.length), { rootMargin: '800px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [items.length, total, load])

  const setParam = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next, { replace: true })
  }
  const changeSort = (s: string) => {
    localStorage.setItem(`bb.sort.${library.id}`, s)
    setSort(s)
  }
  // Shuffles every song in the shown albums (up to the first 40 albums).
  const shuffleAll = async () => {
    const albums = items.slice(0, 40)
    const lists = await Promise.all(albums.map((a) => api.get<Track[]>(`/api/items/${a.id}/tracks`).catch(() => [] as Track[])))
    play(lists.flat(), 0, { shuffle: true })
  }

  const noun = view ? (total === 1 ? 'artist' : 'artists') : total === 1 ? kind.one : kind.many
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{library.name}</h1>
          <div className="muted">
            {total} {noun}
          </div>
        </div>
        {library.kind === 'music' && (
          <div className="segmented">
            <button className={!view ? 'active' : ''} onClick={() => setParam('view', '')}>Albums</button>
            <button className={view ? 'active' : ''} onClick={() => setParam('view', 'artists')}>Artists</button>
          </div>
        )}
        <div className="media-tools">
          {library.kind === 'music' && !view && items.length > 0 && (
            <button className="btn btn-glass btn-sm" onClick={shuffleAll}>
              <Icon name="shuffle" size={16} /> Shuffle
            </button>
          )}
          {!view && filters && filters.genres.length > 0 && (
            <select aria-label="Genre" value={genre} onChange={(e) => setParam('genre', e.target.value)}>
              <option value="">All genres</option>
              {filters.genres.map((g) => (
                <option key={g} value={g}>{g}</option>
              ))}
            </select>
          )}
          <label className="select-label">
            Sort
            <select value={sort} onChange={(e) => changeSort(e.target.value)}>
              {sorts.map(([v, label]) => (
                <option key={v} value={v}>{label}</option>
              ))}
            </select>
          </label>
        </div>
      </header>
      {!loading && items.length === 0 && (
        <div className="empty">{genre ? 'Nothing here in this genre.' : 'This library is empty. Scans can take a few minutes for big folders.'}</div>
      )}
      <div className="poster-grid square-grid">
        {items.map((it) => (
          <PosterCard key={it.id} item={it} subtitle={mediaSubtitle(it)} />
        ))}
      </div>
      <div ref={sentinel} style={{ height: 1 }} />
    </div>
  )
}
