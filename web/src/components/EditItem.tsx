import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, img } from '../api'
import { useApi } from '../hooks'
import { formatClock } from '../format'
import type { Collection, Item, Marker } from '../types'
import { ChipsInput } from './ChipsInput'
import { Icon } from './Icon'
import { Modal } from './Modal'

type Tab = 'details' | 'tags' | 'artwork' | 'advanced'

interface Form {
  title: string
  sortTitle: string
  originalTitle: string
  edition: string
  releaseDate: string
  contentRating: string
  rating: number
  studio: string
  tagline: string
  overview: string
  genres: string[]
  labels: string[]
  collections: string[]
  episodeOrder: '' | 'desc'
}

/** Admin editor for an item: text fields with locks, tags, artwork. */
export function EditItem({
  item,
  collectionNames,
  markers = [],
  onClose,
  onSaved,
  onChanged,
}: {
  item: Item
  collectionNames: string[]
  markers?: Marker[]
  onClose: () => void
  /** The form was saved; close and reload. */
  onSaved: () => void
  /** Artwork changed immediately; reload but keep the dialog open. */
  onChanged: () => void
}) {
  const [tab, setTab] = useState<Tab>('details')
  const [form, setForm] = useState<Form>({
    title: item.title,
    sortTitle: item.sortTitle,
    originalTitle: item.originalTitle ?? '',
    edition: item.edition ?? '',
    releaseDate: item.releaseDate ?? '',
    contentRating: item.contentRating ?? '',
    rating: item.rating ?? 0,
    studio: item.studio ?? '',
    tagline: item.tagline ?? '',
    overview: item.overview ?? '',
    genres: item.genres ?? [],
    labels: item.labels ?? [],
    collections: collectionNames,
    episodeOrder: item.episodeOrder ?? '',
  })
  const [locked, setLocked] = useState<Set<string>>(new Set(item.locked ?? []))
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const topLevel = item.kind === 'movie' || item.kind === 'show'
  const { data: labelSuggestions } = useApi<string[]>(`/api/admin/libraries/${item.libraryId}/labels`)
  const { data: libCollections } = useApi<Collection[]>(topLevel ? `/api/libraries/${item.libraryId}/collections` : null)

  // Editing a field locks it, like Plex, so the next refresh keeps the edit.
  const set = <K extends keyof Form>(key: K, lockAs: string | null = key) => (v: Form[K]) => {
    setForm((f) => ({ ...f, [key]: v }))
    if (lockAs) setLocked((l) => new Set(l).add(lockAs))
  }
  const toggleLock = (field: string) =>
    setLocked((l) => {
      const n = new Set(l)
      if (n.has(field)) n.delete(field)
      else n.add(field)
      return n
    })

  const save = async (e?: FormEvent) => {
    e?.preventDefault()
    setSaving(true)
    setError('')
    try {
      await api.put(`/api/admin/items/${item.id}`, {
        ...form,
        rating: Number(form.rating) || 0,
        locked: [...locked],
        collections: topLevel ? form.collections : undefined,
      })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const field = (label: string, lockName: string, input: ReactNode) => (
    <div className="field">
      <div className="field-head">
        <span>{label}</span>
        <button
          type="button"
          className={`lock-btn ${locked.has(lockName) ? 'on' : ''}`}
          onClick={() => toggleLock(lockName)}
          title={locked.has(lockName) ? 'Locked: metadata refreshes keep this value' : 'Unlocked: metadata refreshes may change this'}
        >
          <Icon name={locked.has(lockName) ? 'lock' : 'unlock'} size={14} />
        </button>
      </div>
      {input}
    </div>
  )

  const tabs: [Tab, string][] = [
    ['details', 'Details'],
    ['tags', 'Tags'],
    ['artwork', 'Artwork'],
  ]
  if (item.kind !== 'season') tabs.push(['advanced', 'Advanced'])

  return (
    <Modal title={`Edit ${item.title}`} onClose={onClose} wide>
      <div className="tabs">
        {tabs.map(([t, label]) => (
          <button key={t} type="button" className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
            {label}
          </button>
        ))}
      </div>

      {tab === 'artwork' ? (
        <ArtworkTab item={item} onChanged={onChanged} />
      ) : (
        <form className="form" onSubmit={save}>
          {tab === 'details' && (
            <div className="field-grid">
              {field('Title', 'title', <input value={form.title} onChange={(e) => set('title')(e.target.value)} required />)}
              {field('Sort title', 'sortTitle', <input value={form.sortTitle} onChange={(e) => set('sortTitle')(e.target.value)} placeholder="Defaults to the title without “The/A/An”" />)}
              {field('Original title', 'originalTitle', <input value={form.originalTitle} onChange={(e) => set('originalTitle')(e.target.value)} />)}
              {item.kind === 'movie' && (
                <label className="field">
                  <div className="field-head"><span>Edition</span></div>
                  <input value={form.edition} onChange={(e) => set('edition', null)(e.target.value)} placeholder="Director's Cut, Extended…" />
                </label>
              )}
              {field(item.kind === 'show' ? 'First aired' : 'Originally available', 'releaseDate', (
                <input type="date" value={form.releaseDate} onChange={(e) => set('releaseDate')(e.target.value)} />
              ))}
              {field('Content rating', 'contentRating', <input value={form.contentRating} onChange={(e) => set('contentRating')(e.target.value)} placeholder="PG-13, TV-MA…" />)}
              {field('Rating (0–10)', 'rating', (
                <input type="number" min={0} max={10} step={0.1} value={form.rating} onChange={(e) => set('rating')(Number(e.target.value))} />
              ))}
              {field(item.kind === 'show' ? 'Network' : 'Studio', 'studio', <input value={form.studio} onChange={(e) => set('studio')(e.target.value)} />)}
              <div className="span-2">
                {field('Tagline', 'tagline', <input value={form.tagline} onChange={(e) => set('tagline')(e.target.value)} />)}
              </div>
              <div className="span-2">
                {field('Summary', 'overview', <textarea rows={5} value={form.overview} onChange={(e) => set('overview')(e.target.value)} />)}
              </div>
            </div>
          )}

          {tab === 'tags' && (
            <>
              {field('Genres', 'genres', <ChipsInput value={form.genres} onChange={set('genres')} placeholder="Add a genre" />)}
              <div className="field">
                <div className="field-head"><span>Labels</span></div>
                <ChipsInput value={form.labels} onChange={set('labels', null)} suggestions={labelSuggestions ?? []} placeholder="e.g. Disney, Christmas, Kids" />
                <p className="muted small">Smart collections pick up every item with their label.</p>
              </div>
              {topLevel &&
                field('Collections', 'collections', (
                  <ChipsInput
                    value={form.collections}
                    onChange={set('collections')}
                    suggestions={libCollections?.map((c) => c.name) ?? []}
                    placeholder="Type a collection name"
                  />
                ))}
            </>
          )}

          {tab === 'advanced' && item.kind === 'show' && (
            <label>
              Episode order
              <select value={form.episodeOrder} onChange={(e) => set('episodeOrder', null)(e.target.value as Form['episodeOrder'])}>
                <option value="">Oldest first (aired order)</option>
                <option value="desc">Newest first</option>
              </select>
            </label>
          )}
          {tab === 'advanced' && (item.kind === 'movie' || item.kind === 'episode') && <MarkerEditor item={item} markers={markers} onSaved={onChanged} />}

          {error && <div className="form-error">{error}</div>}
          <div className="actions end">
            <span className="muted small grow">
              <Icon name="lock" size={12} /> Locked fields keep your edits when metadata is refreshed.
            </span>
            <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
            <button className="btn btn-primary" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
          </div>
        </form>
      )}
    </Modal>
  )
}

/** "1:23" or "1:02:03" → seconds; "" → null. */
function parseClock(v: string): number | null | undefined {
  if (!v.trim()) return null
  const parts = v.trim().split(':').map(Number)
  if (parts.some((n) => !Number.isFinite(n) || n < 0) || parts.length > 3) return undefined
  return parts.reduce((acc, n) => acc * 60 + n, 0)
}

/** Intro and credits times for one movie or episode, entered as m:ss. */
function MarkerEditor({ item, markers, onSaved }: { item: Item; markers: Marker[]; onSaved: () => void }) {
  const get = (kind: Marker['kind']) => markers.find((m) => m.kind === kind)
  const fmt = (n?: number) => (n === undefined ? '' : formatClock(n))
  const [v, setV] = useState({
    introStart: fmt(get('intro')?.start),
    introEnd: fmt(get('intro')?.end),
    creditsStart: fmt(get('credits')?.start),
    creditsEnd: fmt(get('credits')?.end),
  })
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)

  const save = async () => {
    const out: { kind: string; start: number; end: number }[] = []
    for (const [kind, s, e] of [
      ['intro', v.introStart, v.introEnd],
      ['credits', v.creditsStart, v.creditsEnd],
    ] as const) {
      const start = parseClock(s)
      const end = parseClock(e)
      if (start === undefined || end === undefined) return setMsg({ ok: false, text: 'Use times like 1:05 or 1:02:30' })
      if (start === null && end === null) continue
      if (start === null || end === null) return setMsg({ ok: false, text: `Fill in both ${kind} times, or neither` })
      out.push({ kind, start, end })
    }
    try {
      await api.put(`/api/admin/items/${item.id}/markers`, { markers: out })
      setMsg({ ok: true, text: 'Saved. Detection will not change these.' })
      onSaved()
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }

  const source = (kind: Marker['kind']) => {
    const m = get(kind)
    return m ? (m.source === 'manual' ? 'set by hand' : 'detected') : 'not found'
  }

  return (
    <div className="form">
      <div className="label">Skip markers</div>
      <div className="field-grid">
        <label>
          Intro starts <span className="muted small">({source('intro')})</span>
          <input value={v.introStart} onChange={(e) => setV({ ...v, introStart: e.target.value })} placeholder="0:45" />
        </label>
        <label>
          Intro ends
          <input value={v.introEnd} onChange={(e) => setV({ ...v, introEnd: e.target.value })} placeholder="1:30" />
        </label>
        <label>
          Credits start <span className="muted small">({source('credits')})</span>
          <input value={v.creditsStart} onChange={(e) => setV({ ...v, creditsStart: e.target.value })} placeholder="42:10" />
        </label>
        <label>
          Credits end
          <input value={v.creditsEnd} onChange={(e) => setV({ ...v, creditsEnd: e.target.value })} placeholder="43:20" />
        </label>
      </div>
      <p className="muted small">Leave both times of a section empty to let automatic detection handle it.</p>
      {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
      <div className="actions">
        <button type="button" className="btn btn-glass btn-sm" onClick={save}>Save markers</button>
      </div>
    </div>
  )
}

type ArtKind = 'poster' | 'backdrop' | 'logo'
interface ArtOption {
  ref: string
  url: string
  width: number
  height: number
  lang?: string
  votes: number
}

function ArtworkTab({ item, onChanged }: { item: Item; onChanged: () => void }) {
  const kinds: [ArtKind, string][] = item.kind === 'episode'
    ? [['backdrop', 'Thumbnail']]
    : item.kind === 'season'
      ? [['poster', 'Poster']]
      : [['poster', 'Poster'], ['backdrop', 'Background'], ['logo', 'Logo']]
  const [kind, setKind] = useState<ArtKind>(kinds[0][0])
  const [current, setCurrent] = useState<Record<ArtKind, string | undefined>>({ poster: item.poster, backdrop: item.backdrop, logo: item.logo })
  const { data, loading } = useApi<{ options: ArtOption[]; notice?: string }>(`/api/admin/items/${item.id}/artwork?type=${kind}`)
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => setError(''), [kind])

  const apply = async (fn: () => Promise<{ url: string }>) => {
    setBusy(true)
    setError('')
    try {
      const res = await fn()
      setCurrent((c) => ({ ...c, [kind]: res.url }))
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const upload = (file: File) =>
    apply(async () => {
      const body = new FormData()
      body.append('file', file)
      const res = await fetch(`/api/admin/items/${item.id}/artwork/upload?type=${kind}`, {
        method: 'POST',
        body,
        credentials: 'same-origin',
        headers: { 'X-Requested-With': 'BingeBandit' },
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText)
      return data
    })

  const shape = kind === 'poster' ? 'poster' : kind === 'logo' ? 'logo' : 'wide'

  return (
    <div className="artwork">
      {kinds.length > 1 && (
        <div className="segmented">
          {kinds.map(([k, label]) => (
            <button key={k} className={k === kind ? 'active' : ''} onClick={() => setKind(k)}>{label}</button>
          ))}
        </div>
      )}
      <div className="artwork-current">
        <div className={`art-thumb ${shape} current`}>
          {current[kind] ? <img src={img(current[kind], 'w500')} alt="" /> : <span className="muted small">None</span>}
        </div>
        <div className="artwork-sources">
          <div className="url-row">
            <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="Paste an image URL (ThePosterDB, Fanart.tv…)" />
            <button
              className="btn btn-glass btn-sm"
              disabled={!url || busy}
              onClick={() => apply(() => api.put<{ url: string }>(`/api/admin/items/${item.id}/artwork`, { type: kind, url })).then(() => setUrl(''))}
            >
              <Icon name="link" size={16} /> Use URL
            </button>
          </div>
          <button className="btn btn-glass btn-sm" disabled={busy} onClick={() => fileRef.current?.click()}>
            <Icon name="upload" size={16} /> Upload image
          </button>
          <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
          {error && <div className="form-error small">{error}</div>}
          <p className="muted small">Choosing artwork locks it, so metadata refreshes won't replace it.</p>
        </div>
      </div>

      {kind === 'poster' && <TPDbSearch item={item} busy={busy} onPick={(url) => apply(() => api.put<{ url: string }>(`/api/admin/items/${item.id}/artwork`, { type: kind, url }))} />}

      <div className="label">From TMDB</div>
      {loading && <div className="muted small">Loading…</div>}
      {data?.notice && <div className="muted small">{data.notice}</div>}
      {data && !data.notice && data.options.length === 0 && <div className="muted small">TMDB has no alternatives for this item.</div>}
      <div className={`art-grid ${shape}`}>
        {data?.options.map((o) => (
          <button
            key={o.ref}
            className={`art-thumb ${shape}`}
            disabled={busy}
            onClick={() => apply(() => api.put<{ url: string }>(`/api/admin/items/${item.id}/artwork`, { type: kind, ref: o.ref }))}
            title={`${o.width}×${o.height}${o.lang ? ` · ${o.lang}` : ''}`}
          >
            <img src={img(o.url, kind === 'backdrop' ? 'w500' : 'w342')} alt="" loading="lazy" />
            {o.lang && <span className="art-lang">{o.lang}</span>}
          </button>
        ))}
      </div>
    </div>
  )
}

interface TPDbTitle {
  id: number
  title: string
  url: string
  posters: { id: number; title?: string; uploader?: string; download: string }[]
}

/** Posters from ThePosterDB, searched on demand (it has no API, so the server reads its pages). */
function TPDbSearch({ item, busy, onPick }: { item: Item; busy: boolean; onPick: (url: string) => void }) {
  const [q, setQ] = useState('')
  const [res, setRes] = useState<{ query: string; titles: TPDbTitle[] } | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const search = async (e?: FormEvent) => {
    e?.preventDefault()
    setLoading(true)
    setError('')
    try {
      const r = await api.get<{ query: string; titles: TPDbTitle[] | null }>(`/api/admin/items/${item.id}/tpdb${q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ''}`)
      // Older servers send null for an empty list; keep a bad response from blanking the page.
      setRes({ query: r.query, titles: (r.titles ?? []).map((t) => ({ ...t, posters: t.posters ?? [] })) })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }
  const term = q.trim() || res?.query || item.title
  const posters = res?.titles.reduce((n, t) => n + t.posters.length, 0) ?? 0
  return (
    <>
      <div className="label">From ThePosterDB</div>
      <form className="url-row" onSubmit={search}>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Search for “${item.title}”, or paste a ThePosterDB link`} />
        <button className="btn btn-glass btn-sm" disabled={loading}>
          <Icon name="search" size={16} /> {loading ? 'Searching…' : 'Search'}
        </button>
        <a className="btn btn-glass btn-sm" href={`https://theposterdb.com/search?term=${encodeURIComponent(term)}`} target="_blank" rel="noreferrer">
          <Icon name="link" size={16} /> Open site
        </a>
      </form>
      {error && <div className="form-error small">{error}</div>}
      {res && posters === 0 && <div className="muted small">No posters found for “{res.query}”. Try another title, or paste a link from their site.</div>}
      {res?.titles.filter((t) => t.posters.length > 0).map((t) => (
        <div key={`${t.id}:${t.url}`} className="tpdb-title">
          <a className="small" href={t.url} target="_blank" rel="noreferrer">{t.title || t.url}</a>
          <div className="art-grid poster">
            {t.posters.map((p) => (
              <button key={p.id} className="art-thumb poster" disabled={busy} onClick={() => onPick(p.download)} title={[p.title, p.uploader && `by ${p.uploader}`].filter(Boolean).join(' · ')}>
                <img src={`/api/admin/tpdb/${p.id}/thumb`} alt="" loading="lazy" />
                {p.uploader && <span className="art-lang">{p.uploader}</span>}
              </button>
            ))}
          </div>
        </div>
      ))}
    </>
  )
}
