import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { Item, Library } from '../../types'

type Settings = Record<string, string>

interface Overlay {
  id: number
  name: string
  image: string
  position: string
  size: number
  margin: number
  condition: string
  kind: '' | 'movie' | 'show'
  libraryId: number
  enabled: boolean
}

interface Condition {
  id: string
  label: string
  value?: string
}

const badgeKeys: [string, string, string][] = [
  ['overlay_resolution', 'resolution', 'Resolution (4K, 1080P, 720P, SD)'],
  ['overlay_hdr', 'hdr', 'Dolby Vision and HDR'],
  ['overlay_audio', 'audioBadge', 'Best audio (Atmos, DTS:X, TrueHD, DTS-HD MA, 7.1, 5.1)'],
  ['overlay_ratings', 'ratings', 'Scores (IMDb, Rotten Tomatoes, TMDB)'],
]

const defaultKeys: [string, string][] = [
  ['default_collections_studios', 'Studios (networks in TV libraries)'],
  ['default_collections_decades', 'Decades (1980s, 1990s…)'],
  ['default_collections_genres', 'Genres'],
  ['default_collections_awards', 'Award winners (Oscars, Golden Globes, BAFTA, Cannes, Venice, Berlin, Emmys)'],
]

const positionLabels: Record<string, string> = {
  'top-left': 'Top left',
  top: 'Top',
  'top-right': 'Top right',
  left: 'Left',
  center: 'Centre',
  right: 'Right',
  'bottom-left': 'Bottom left',
  bottom: 'Bottom',
  'bottom-right': 'Bottom right',
  full: 'Whole poster (frames)',
}

export default function AdminOverlays() {
  useDocumentTitle('Overlays & collections')
  const { data: settings } = useApi<Settings>('/api/admin/settings')
  const { data, reload } = useApi<{ overlays: Overlay[]; conditions: Condition[]; positions: string[] }>('/api/admin/overlays')
  const { data: libraries } = useApi<Library[]>('/api/admin/libraries')
  const [s, setS] = useState<Settings>({})
  const [msg, setMsg] = useState<{ ok: boolean; text: string; where: string } | null>(null)
  const [editing, setEditing] = useState<Overlay | 'new' | null>(null)
  const [previewItem, setPreviewItem] = useState<Item | null>(null)
  const [showAll, setShowAll] = useState(true)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    if (settings) setS(settings)
  }, [settings])

  const check = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setS({ ...s, [k]: String(e.target.checked) })

  const save = (keys: string[], where: string) => async (e: FormEvent) => {
    e.preventDefault()
    const body: Settings = {}
    for (const k of keys) body[k] = s[k] ?? ''
    try {
      setS(await api.put<Settings>('/api/admin/settings', body))
      setMsg({
        ok: true,
        where,
        text: where === 'collections' ? 'Saved. Default collections are being built now (see Tasks).' : 'Saved. Posters update as they reload.',
      })
      setNonce((n) => n + 1)
    } catch (err) {
      setMsg({ ok: false, where, text: (err as Error).message })
    }
  }

  const preview = new URLSearchParams()
  for (const [k, q] of badgeKeys) preview.set(q, String(s[k] === 'true'))
  if (previewItem) preview.set('item', String(previewItem.id))
  if (showAll) {
    preview.set('res', '4k')
    preview.set('range', 'dv')
    preview.set('audio', 'atmos')
  }
  preview.set('n', String(nonce))

  const conditionText = (c: string) => {
    const [id, ...rest] = c.split(':')
    const def = data?.conditions.find((x) => x.id === c || (x.value && x.id === id))
    if (!def) return c
    return def.value ? `${def.label} ${rest.join(':')}${id === 'new' ? ' days' : id === 'rt' ? '%' : ''}` : def.label
  }

  const move = async (i: number, by: number) => {
    if (!data) return
    const ids = data.overlays.map((o) => o.id)
    const j = i + by
    if (j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    await api.put('/api/admin/overlays/order', { ids }).catch((e) => alert(e.message))
    reload()
    setNonce((n) => n + 1)
  }
  const toggle = async (o: Overlay, enabled: boolean) => {
    await api.put(`/api/admin/overlays/${o.id}`, { ...o, enabled }).catch((e) => alert(e.message))
    reload()
    setNonce((n) => n + 1)
  }
  const remove = async (o: Overlay) => {
    if (!confirm(`Delete the overlay “${o.name}”?`)) return
    await api.del(`/api/admin/overlays/${o.id}`).catch((e) => alert(e.message))
    reload()
    setNonce((n) => n + 1)
  }

  if (!settings || !data) return <div className="page-loading" />
  const on = s.overlays_enabled === 'true'

  return (
    <div className="page narrow">
      <h1>Overlays & collections</h1>
      <p className="muted">
        Kometa-style extras. Overlays are drawn on movie and show posters as they're sent to apps; your artwork is never changed, and turning
        overlays off brings the plain posters straight back.
      </p>

      <form className="panel form" onSubmit={save(['overlays_enabled', ...badgeKeys.map(([k]) => k)], 'overlays')}>
        <h3>Poster overlays</h3>
        <label className="check">
          <input type="checkbox" checked={on} onChange={check('overlays_enabled')} />
          Show overlays on posters
        </label>
        <div className="overlay-settings">
          <div className="grow">
            <div className="muted small" style={{ marginBottom: 6 }}>Built-in badges</div>
            {badgeKeys.map(([k, , label]) => (
              <label key={k} className="check">
                <input type="checkbox" checked={s[k] === 'true'} onChange={check(k)} disabled={!on} />
                {label}
              </label>
            ))}
            <p className="muted small">
              Badges come from each title's best copy (for shows, the best episode). Scores need a ratings provider in Settings → Metadata.
              Files analyzed before this version are looked at again for Dolby Vision and Atmos by the “Detect Dolby Vision and Atmos” task.
              To use your own badge instead of a built-in one, turn the built-in one off and add yours below.
            </p>
            {msg?.where === 'overlays' && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
            <div className="actions">
              <button className="btn btn-primary">Save overlays</button>
            </div>
          </div>
          <div className="overlay-preview">
            <img src={`/api/admin/overlays/preview?${preview}`} alt="Poster preview" />
            <PreviewPicker item={previewItem} onPick={setPreviewItem} />
            <label className="check small">
              <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} />
              Pretend it's 4K Dolby Vision with Atmos
            </label>
          </div>
        </div>
      </form>

      <section className="panel">
        <header className="panel-head">
          <h3>Your overlays</h3>
          <button className="btn btn-glass btn-sm" onClick={() => setEditing('new')}>
            <Icon name="upload" size={14} /> Add overlay
          </button>
        </header>
        <p className="muted small">
          Upload your own badges, ribbons or frames (PNG with a transparent background works best) and choose where they go and which
          titles get them. Later ones are drawn on top. They show while poster overlays are on.
        </p>
        {data.overlays.length === 0 && <p className="muted">No overlays of your own yet.</p>}
        <div className="list">
          {data.overlays.map((o, i) => (
            <div key={o.id} className="task-row">
              <label className="switch" title={o.enabled ? 'On' : 'Off'}>
                <input type="checkbox" checked={o.enabled} onChange={(e) => toggle(o, e.target.checked)} aria-label={`${o.name} on`} />
                <span />
              </label>
              <img className="overlay-thumb" src={o.image} alt="" />
              <div className="grow">
                <div className="task-name">{o.name}</div>
                <div className="muted small">
                  {conditionText(o.condition)} · {positionLabels[o.position] ?? o.position}
                  {o.position !== 'full' && ` · ${o.size}% wide`}
                  {o.kind && ` · ${o.kind === 'movie' ? 'movies' : 'shows'} only`}
                  {o.libraryId > 0 && ` · ${libraries?.find((l) => l.id === o.libraryId)?.name ?? 'one library'}`}
                </div>
              </div>
              <button className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label="Move down (drawn earlier)">
                <span className="rot90"><Icon name="back" size={16} /></span>
              </button>
              <button className="icon-btn" onClick={() => move(i, 1)} disabled={i === data.overlays.length - 1} aria-label="Move up (drawn later)">
                <span className="rot90"><Icon name="next" size={16} /></span>
              </button>
              <button className="btn btn-glass btn-sm" onClick={() => setEditing(o)}>
                <Icon name="edit" size={14} /> Edit
              </button>
              <button className="btn btn-glass btn-sm danger" onClick={() => remove(o)} aria-label={`Delete ${o.name}`}>
                <Icon name="trash" size={16} />
              </button>
            </div>
          ))}
        </div>
      </section>

      <form className="panel form" onSubmit={save([...defaultKeys.map(([k]) => k), 'default_collections_minimum'], 'collections')}>
        <h3>Default collections</h3>
        <p className="muted small">
          Like Kometa's defaults: BingeBandit makes these collections in every movie and show library and keeps them current. Franchises
          (TMDB collections such as “The Lord of the Rings Collection”) are already made automatically. Default collections start with
          Collection Mode “hide”, so the Library tab keeps showing titles; turn any of them into Home rows in the library's rows settings.
          Turning a group off removes the collections it made (never your own).
        </p>
        {defaultKeys.map(([k, label]) => (
          <label key={k} className="check">
            <input type="checkbox" checked={s[k] === 'true'} onChange={check(k)} />
            {label}
          </label>
        ))}
        <label>
          Smallest studio, decade or genre collection (titles)
          <input type="number" min={1} max={1000} value={s.default_collections_minimum ?? '5'} onChange={(e) => setS({ ...s, default_collections_minimum: e.target.value })} />
        </label>
        <p className="muted small">
          Award winners come from Wikidata and are matched by IMDb and TMDB id, so they need matched titles. Up to 40 studios and genres per
          library, biggest first.
        </p>
        {msg?.where === 'collections' && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary">Save collections</button>
        </div>
      </form>

      {editing && (
        <OverlayEditor
          overlay={editing === 'new' ? null : editing}
          conditions={data.conditions}
          positions={data.positions}
          libraries={libraries ?? []}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
            setNonce((n) => n + 1)
          }}
        />
      )}
    </div>
  )
}

function PreviewPicker({ item, onPick }: { item: Item | null; onPick: (it: Item | null) => void }) {
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Item[]>([])
  useEffect(() => {
    if (!q.trim()) {
      setResults([])
      return
    }
    const t = setTimeout(() => {
      api
        .get<Item[]>(`/api/search?q=${encodeURIComponent(q)}`)
        .then((r) => setResults((r ?? []).filter((it) => it.kind === 'movie' || it.kind === 'show').slice(0, 6)))
        .catch(() => setResults([]))
    }, 250)
    return () => clearTimeout(t)
  }, [q])
  return (
    <div className="overlay-pick">
      <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={item ? `Previewing ${item.title}` : 'Preview another title…'} />
      {results.length > 0 && (
        <div className="overlay-pick-list">
          {results.map((it) => (
            <button
              type="button"
              key={it.id}
              onClick={() => {
                onPick(it)
                setQ('')
              }}
            >
              {it.title} {it.year ? <span className="muted">({it.year})</span> : null}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function OverlayEditor({
  overlay,
  conditions,
  positions,
  libraries,
  onClose,
  onSaved,
}: {
  overlay: Overlay | null
  conditions: Condition[]
  positions: string[]
  libraries: Library[]
  onClose: () => void
  onSaved: () => void
}) {
  const [o, setO] = useState<Overlay>(
    overlay ?? { id: 0, name: '', image: '', position: 'top-left', size: 25, margin: 3, condition: 'always', kind: '', libraryId: 0, enabled: true },
  )
  const [imageURL, setImageURL] = useState(overlay?.image ?? '')
  const [cond, setCond] = useState(() => o.condition.split(':')[0])
  const [value, setValue] = useState(() => o.condition.split(':').slice(1).join(':'))
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const def = conditions.find((c) => c.id === cond)

  const upload = async (file: File | undefined) => {
    if (!file) return
    const form = new FormData()
    form.append('file', file)
    setBusy(true)
    try {
      const r = await api.post<{ ref: string; url: string }>('/api/admin/overlays/image', form)
      setO((o) => ({ ...o, image: r.ref, name: o.name || file.name.replace(/\.[^.]+$/, '') }))
      setImageURL(r.url)
      setErr('')
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const body = { ...o, condition: def?.value ? `${cond}:${value.trim()}` : cond }
    try {
      if (o.id) await api.put(`/api/admin/overlays/${o.id}`, body)
      else await api.post('/api/admin/overlays', body)
      onSaved()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <Modal title={overlay ? 'Edit overlay' : 'Add overlay'} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <div className="overlay-upload">
          {imageURL ? <img src={imageURL} alt="" /> : <div className="overlay-empty muted small">No image yet</div>}
          <label className="btn btn-glass btn-sm">
            <Icon name="upload" size={14} /> {imageURL ? 'Replace image' : 'Upload image'}
            <input type="file" accept="image/png,image/jpeg,image/webp" hidden disabled={busy} onChange={(e) => upload(e.target.files?.[0])} />
          </label>
        </div>
        <label>
          Name
          <input value={o.name} onChange={(e) => setO({ ...o, name: e.target.value })} placeholder="e.g. 4K badge" />
        </label>
        <label>
          Show it on
          <select value={cond} onChange={(e) => setCond(e.target.value)}>
            {conditions.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
                {c.value ? '…' : ''}
              </option>
            ))}
          </select>
        </label>
        {def?.value && (
          <label>
            {def.label} ({def.value})
            <input value={value} onChange={(e) => setValue(e.target.value)} inputMode={cond === 'label' ? 'text' : 'decimal'} />
          </label>
        )}
        <label>
          Position
          <select value={o.position} onChange={(e) => setO({ ...o, position: e.target.value })}>
            {positions.map((p) => (
              <option key={p} value={p}>
                {positionLabels[p] ?? p}
              </option>
            ))}
          </select>
        </label>
        {o.position !== 'full' && (
          <div className="row-2">
            <label>
              Width (% of the poster)
              <input type="number" min={1} max={100} value={o.size} onChange={(e) => setO({ ...o, size: Number(e.target.value) })} />
            </label>
            <label>
              Gap to the edge (%)
              <input type="number" min={0} max={25} value={o.margin} onChange={(e) => setO({ ...o, margin: Number(e.target.value) })} />
            </label>
          </div>
        )}
        <div className="row-2">
          <label>
            Titles
            <select value={o.kind} onChange={(e) => setO({ ...o, kind: e.target.value as Overlay['kind'] })}>
              <option value="">Movies and shows</option>
              <option value="movie">Movies only</option>
              <option value="show">Shows only</option>
            </select>
          </label>
          <label>
            Library
            <select value={o.libraryId} onChange={(e) => setO({ ...o, libraryId: Number(e.target.value) })}>
              <option value={0}>Every library</option>
              {libraries
                .filter((l) => l.kind === 'movies' || l.kind === 'shows')
                .map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.name}
                  </option>
                ))}
            </select>
          </label>
        </div>
        <label className="check">
          <input type="checkbox" checked={o.enabled} onChange={(e) => setO({ ...o, enabled: e.target.checked })} />
          On
        </label>
        {err && <div className="form-error">{err}</div>}
        <div className="actions">
          <button type="button" className="btn btn-glass" onClick={onClose}>
            Cancel
          </button>
          <button className="btn btn-primary" disabled={busy || !o.image}>
            Save overlay
          </button>
        </div>
      </form>
    </Modal>
  )
}
