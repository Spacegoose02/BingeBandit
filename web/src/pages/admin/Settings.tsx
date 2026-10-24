import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { useApi, useDocumentTitle } from '../../hooks'

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

        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary">Save settings</button>
        </div>
      </form>
    </div>
  )
}
