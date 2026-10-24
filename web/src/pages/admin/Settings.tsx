import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { Webhook } from '../../types'

type Settings = Record<string, string>

const encoders = [
  ['none', 'Software (CPU, libx264)'],
  ['nvenc', 'NVIDIA NVENC'],
  ['qsv', 'Intel Quick Sync'],
  ['vaapi', 'VAAPI (Linux / Docker / Unraid)'],
  ['amf', 'AMD AMF (Windows)'],
  ['videotoolbox', 'Apple VideoToolbox'],
]

export default function AdminSettings() {
  useDocumentTitle('Settings')
  const { refresh } = useAuth()
  const { data } = useApi<Settings>('/api/admin/settings')
  const [s, setS] = useState<Settings>({})
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)

  useEffect(() => {
    if (data) setS(data)
  }, [data])

  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const v = e.target instanceof HTMLInputElement && e.target.type === 'checkbox' ? String(e.target.checked) : e.target.value
    setS({ ...s, [k]: v })
  }

  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      setS(await api.put<Settings>('/api/admin/settings', s))
      setMsg({ ok: true, text: 'Settings saved.' })
      refresh()
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }

  if (!data) return <div className="page-loading" />

  return (
    <div className="page narrow">
      <h1>Settings</h1>
      <form className="form" onSubmit={save}>
        <section className="panel form">
          <h3>General</h3>
          <label>
            Server name
            <input value={s.server_name ?? ''} onChange={set('server_name')} />
          </label>
        </section>

        <section className="panel form">
          <h3>Metadata</h3>
          <label>
            TMDB API key or read access token
            <input value={s.tmdb_api_key ?? ''} onChange={set('tmdb_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
          </label>
          <p className="muted small">
            Free at{' '}
            <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer">
              themoviedb.org/settings/api
            </a>
            . After adding it, use “Refresh metadata” on each library.
          </p>
          <label>
            Metadata language
            <input value={s.metadata_language ?? ''} onChange={set('metadata_language')} placeholder="en-US" />
          </label>
        </section>

        <section className="panel form">
          <h3>Transcoding</h3>
          <label>
            Hardware acceleration
            <select value={s.hw_encoder ?? 'none'} onChange={set('hw_encoder')}>
              {encoders.map(([v, label]) => (
                <option key={v} value={v}>{label}</option>
              ))}
            </select>
          </label>
          <label>
            Temporary transcode folder
            <input value={s.transcode_dir ?? ''} onChange={set('transcode_dir')} placeholder="Default: inside the data folder" className="mono" />
          </label>
          <label className="check">
            <input type="checkbox" checked={s.tonemap_enabled === 'true'} onChange={set('tonemap_enabled')} />
            Tone map HDR to SDR when transcoding
          </label>
          <label className="check">
            <input type="checkbox" checked={s.transcode_throttle === 'true'} onChange={set('transcode_throttle')} />
            Throttle transcodes (stay about 2× ahead of playback instead of converting the whole file at full speed)
          </label>
        </section>

        <section className="panel form">
          <h3>Library</h3>
          <label className="check">
            <input type="checkbox" checked={s.watch_folders === 'true'} onChange={set('watch_folders')} />
            Watch library folders and add new files automatically
          </label>
          <p className="muted small">
            Full rescans, backups and cleanup are under <Link to="/admin/tasks">Scheduled Tasks</Link>. Some network shares don't report changes;
            the scheduled scan still finds those.
          </p>
          <label className="check">
            <input type="checkbox" checked={s.allow_downloads === 'true'} onChange={set('allow_downloads')} />
            Let users download original files
          </label>
        </section>

        <section className="panel form">
          <h3>Live TV recording (DVR)</h3>
          <label className="check">
            <input type="checkbox" checked={s.dvr_allow_users === 'true'} onChange={set('dvr_allow_users')} />
            Let users (not just admins) record
          </label>
          <label className="check">
            <input type="checkbox" checked={s.dvr_require_approval === 'true'} onChange={set('dvr_require_approval')} />
            Ask an admin to approve users' recordings first
          </label>
          <label>
            Recordings folder
            <input className="mono" value={s.dvr_folder ?? ''} onChange={set('dvr_folder')} placeholder="Default: recordings inside the data folder" />
          </label>
          <label>
            Save recordings
            <select value={s.dvr_naming ?? 'library'} onChange={set('dvr_naming')}>
              <option value="library">Named like a TV library (Show/Season 01/Show - S01E02.ts)</option>
              <option value="folder">All in one folder (Show - date.ts)</option>
            </select>
          </label>
          <div className="field-grid">
            <label>
              Start early (minutes)
              <input type="number" min={0} max={60} value={s.dvr_padding_before ?? '1'} onChange={set('dvr_padding_before')} />
            </label>
            <label>
              Keep going after (minutes)
              <input type="number" min={0} max={120} value={s.dvr_padding_after ?? '3'} onChange={set('dvr_padding_after')} />
            </label>
          </div>
        </section>

        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary">Save settings</button>
        </div>
      </form>
      <Webhooks />
    </div>
  )
}

const eventLabels: Record<string, string> = {
  'playback.start': 'Playback started',
  'playback.stop': 'Playback stopped',
  'playback.finished': 'Finished watching',
  'recording.started': 'Recording started',
  'recording.completed': 'Recording completed',
  'recording.failed': 'Recording failed',
}

/** Webhooks POST signed JSON to other services (Home Assistant, Discord bots, n8n…) when things happen. */
function Webhooks() {
  const { data, reload } = useApi<{ webhooks: Webhook[]; events: string[] }>('/api/admin/webhooks')
  const [editing, setEditing] = useState<Partial<Webhook> | null>(null)
  const [results, setResults] = useState<Record<number, string>>({})

  const test = async (h: Webhook) => {
    setResults((r) => ({ ...r, [h.id]: 'Sending…' }))
    try {
      const res = await api.post<{ status: string }>(`/api/admin/webhooks/${h.id}/test`)
      setResults((r) => ({ ...r, [h.id]: res.status }))
    } catch (err) {
      setResults((r) => ({ ...r, [h.id]: (err as Error).message }))
    }
    reload()
  }
  const remove = async (h: Webhook) => {
    if (!confirm(`Delete the webhook "${h.name}"?`)) return
    await api.del(`/api/admin/webhooks/${h.id}`).catch((e) => alert(e.message))
    reload()
  }
  const toggle = async (h: Webhook, enabled: boolean) => {
    await api.put(`/api/admin/webhooks/${h.id}`, { ...h, enabled }).catch((e) => alert(e.message))
    reload()
  }

  return (
    <section className="panel" style={{ marginTop: 20 }}>
      <header className="panel-head">
        <h3>Webhooks</h3>
        <button className="btn btn-glass btn-sm" onClick={() => setEditing({ enabled: true, events: [] })}>
          <Icon name="plus" size={16} /> Add webhook
        </button>
      </header>
      <p className="muted small">
        BingeBandit sends a JSON POST to each URL when a chosen event happens. With a secret set, the body is signed in the{' '}
        <code>X-BingeBandit-Signature</code> header (HMAC-SHA256).
      </p>
      <div className="list">
        {data?.webhooks.map((h) => (
          <div key={h.id} className="list-row">
            <label className="switch" title={h.enabled ? 'On' : 'Off'}>
              <input type="checkbox" checked={h.enabled} onChange={(e) => toggle(h, e.target.checked)} aria-label={`${h.name} enabled`} />
              <span />
            </label>
            <div className="grow">
              <div>{h.name}</div>
              <div className="muted small mono" style={{ wordBreak: 'break-all' }}>{h.url}</div>
              <div className="muted small">
                {h.events.length ? h.events.map((e) => eventLabels[e] ?? e).join(', ') : 'No events chosen'}
                {(results[h.id] || h.lastStatus) && <> · last response: {results[h.id] || h.lastStatus}</>}
              </div>
            </div>
            <button className="btn btn-glass btn-sm" onClick={() => test(h)}>Send test</button>
            <button className="btn btn-glass btn-sm" onClick={() => setEditing(h)}>Edit</button>
            <button className="btn btn-glass btn-sm danger" onClick={() => remove(h)} aria-label={`Delete ${h.name}`}>
              <Icon name="trash" size={16} />
            </button>
          </div>
        ))}
      </div>
      {editing && data && (
        <WebhookEditor
          hook={editing}
          events={data.events}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
    </section>
  )
}

function WebhookEditor({ hook, events, onClose, onSaved }: { hook: Partial<Webhook>; events: string[]; onClose: () => void; onSaved: () => void }) {
  const [h, setH] = useState<Partial<Webhook>>(hook)
  const [error, setError] = useState('')
  const toggle = (e: string) => {
    const cur = new Set(h.events ?? [])
    if (cur.has(e)) cur.delete(e)
    else cur.add(e)
    setH({ ...h, events: events.filter((x) => cur.has(x)) })
  }
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      if (h.id) await api.put(`/api/admin/webhooks/${h.id}`, h)
      else await api.post('/api/admin/webhooks', h)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={h.id ? 'Edit webhook' : 'Add webhook'} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={h.name ?? ''} onChange={(e) => setH({ ...h, name: e.target.value })} required autoFocus placeholder="Home Assistant" />
        </label>
        <label>
          URL
          <input className="mono" type="url" value={h.url ?? ''} onChange={(e) => setH({ ...h, url: e.target.value })} required placeholder="https://…" />
        </label>
        <label>
          Secret (optional, for signing)
          <input className="mono" value={h.secret ?? ''} onChange={(e) => setH({ ...h, secret: e.target.value })} autoComplete="off" />
        </label>
        <fieldset className="webhook-events">
          {events.map((ev) => (
            <label key={ev} className="check">
              <input type="checkbox" checked={h.events?.includes(ev) ?? false} onChange={() => toggle(ev)} />
              {eventLabels[ev] ?? ev}
            </label>
          ))}
        </fieldset>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}
