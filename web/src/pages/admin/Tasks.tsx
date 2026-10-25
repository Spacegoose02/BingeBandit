import { useEffect, useState } from 'react'
import { api, uploadBackup } from '../../api'
import { Icon } from '../../components/Icon'
import { useApi, useDocumentTitle } from '../../hooks'
import { formatBytes, timeAgo } from '../../format'

interface Task {
  id: string
  name: string
  description: string
  maintenance: boolean
  running: boolean
  nextRun: number
  enabled: boolean
  intervalHours: number
  lastRun: number
  lastDurationMs: number
  lastResult?: string
  lastError?: string
}

interface Backup {
  name: string
  size: number
  modified: number
}

// [setting, label, placeholder, full width]
const remoteFields: [string, string, string, boolean][] = [
  ['backup_s3_endpoint', 'Endpoint', 'https://s3.us-west-002.backblazeb2.com', true],
  ['backup_s3_bucket', 'Bucket', 'my-backups', false],
  ['backup_s3_region', 'Region', 'us-west-002 (blank = us-east-1)', false],
  ['backup_s3_prefix', 'Folder in the bucket', 'bingebandit', false],
  ['backup_s3_access_key', 'Access key ID (B2: key ID)', '', false],
  ['backup_s3_secret_key', 'Secret access key (B2: application key)', '', true],
]

const intervals: [number, string][] = [
  [1, 'Every hour'],
  [3, 'Every 3 hours'],
  [6, 'Every 6 hours'],
  [12, 'Every 12 hours'],
  [24, 'Daily'],
  [72, 'Every 3 days'],
  [168, 'Weekly'],
  [336, 'Every 2 weeks'],
  [720, 'Monthly'],
]

const hours = Array.from({ length: 24 }, (_, h) => h)
const hourLabel = (h: number) => new Date(2000, 0, 1, h).toLocaleTimeString([], { hour: 'numeric' })

function duration(ms: number) {
  if (ms < 1000) return `${ms} ms`
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
}

function when(unix: number) {
  const diff = unix - Date.now() / 1000
  if (diff < 60) return 'soon'
  if (diff < 3600) return `in ${Math.round(diff / 60)}m`
  if (diff < 86400) return `in ${Math.round(diff / 3600)}h`
  return `in ${Math.round(diff / 86400)}d`
}

export default function AdminTasks() {
  useDocumentTitle('Scheduled Tasks')
  const { data: tasks, reload } = useApi<Task[]>('/api/admin/tasks', 3000)
  const { data: settings } = useApi<Record<string, string>>('/api/admin/settings')
  const [win, setWin] = useState({ start: '2', end: '5', dir: '', keep: '7' })
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [remote, setRemote] = useState<Record<string, string>>({})
  const [testing, setTesting] = useState(false)

  useEffect(() => {
    if (!settings) return
    setWin({ start: settings.maintenance_start, end: settings.maintenance_end, dir: settings.backup_dir, keep: settings.backup_keep })
    setRemote(Object.fromEntries(remoteFields.map(([k]) => [k, settings[k] ?? ''])))
  }, [settings])

  const configure = async (t: Task, enabled: boolean, intervalHours: number) => {
    await api.put(`/api/admin/tasks/${t.id}`, { enabled, intervalHours }).catch((e) => alert(e.message))
    reload()
  }
  const run = async (t: Task) => {
    await api.post(`/api/admin/tasks/${t.id}/run`).catch((e) => alert(e.message))
    reload()
  }
  const saveSettings = async () => {
    try {
      await api.put('/api/admin/settings', {
        maintenance_start: win.start,
        maintenance_end: win.end,
        backup_dir: win.dir,
        backup_keep: win.keep,
        ...remote,
      })
      setMsg({ ok: true, text: 'Saved.' })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
  }

  const testRemote = async () => {
    setTesting(true)
    setMsg(null)
    try {
      const res = await api.post<{ backups: number }>('/api/admin/backups/remote/test', remote)
      setMsg({ ok: true, text: `Connected: BingeBandit can upload, list and delete there (${res.backups} backups found). Save to use it.` })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
    setTesting(false)
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Scheduled Tasks</h1>
          <div className="muted">Background jobs that keep the server tidy. Times use the server's clock.</div>
        </div>
      </header>

      <div className="panel list">
        {tasks?.map((t) => (
          <div key={t.id} className="task-row">
            <label className="switch" title={t.enabled ? 'Enabled' : 'Disabled'}>
              <input type="checkbox" checked={t.enabled} onChange={(e) => configure(t, e.target.checked, t.intervalHours)} />
              <span />
            </label>
            <div className="grow">
              <div className="task-name">
                {t.name}
                {t.maintenance && <span className="pill">Maintenance window</span>}
              </div>
              <div className="muted small">{t.description}</div>
              <div className="task-meta small">
                {t.running ? (
                  <span className="accent"><Icon name="refresh" size={12} className="spin" /> Running…</span>
                ) : t.lastRun ? (
                  <span className={t.lastError ? 'task-error' : 'muted'}>
                    Last run {timeAgo(t.lastRun)} · {duration(t.lastDurationMs)} · {t.lastError || t.lastResult}
                  </span>
                ) : (
                  <span className="muted">Never run</span>
                )}
                {t.enabled && !t.running && <span className="muted"> · next {t.maintenance ? 'in the window, ' : ''}{when(t.nextRun)}</span>}
              </div>
            </div>
            <select value={t.intervalHours} disabled={!t.enabled} onChange={(e) => configure(t, t.enabled, Number(e.target.value))}>
              {!intervals.some(([h]) => h === t.intervalHours) && <option value={t.intervalHours}>Every {t.intervalHours} hours</option>}
              {intervals.map(([h, label]) => (
                <option key={h} value={h}>{label}</option>
              ))}
            </select>
            <button className="btn btn-glass btn-sm" disabled={t.running} onClick={() => run(t)}>
              <Icon name="play" fill size={14} /> Run now
            </button>
          </div>
        ))}
      </div>

      <h2 className="section-title">Maintenance window and backups</h2>
      <div className="panel form" style={{ maxWidth: 760 }}>
        <div className="field-grid">
          <label>
            Window starts
            <select value={win.start} onChange={(e) => setWin({ ...win, start: e.target.value })}>
              {hours.map((h) => <option key={h} value={h}>{hourLabel(h)}</option>)}
            </select>
          </label>
          <label>
            Window ends
            <select value={win.end} onChange={(e) => setWin({ ...win, end: e.target.value })}>
              {hours.map((h) => <option key={h} value={h}>{hourLabel(h)}</option>)}
            </select>
          </label>
          <label className="span-2">
            Backup folder (use a NAS share or another drive for off-machine backups)
            <input className="mono" value={win.dir} onChange={(e) => setWin({ ...win, dir: e.target.value })} placeholder="Default: backups inside the data folder" />
          </label>
          <label>
            Backups to keep
            <input type="number" min={1} max={365} value={win.keep} onChange={(e) => setWin({ ...win, keep: e.target.value })} />
          </label>
        </div>
        <p className="muted small">Maintenance tasks only start inside the window. Set the same start and end hour to allow any time.</p>
        <h3>Also upload to cloud storage</h3>
        <p className="muted small">
          Any S3-compatible service: Amazon S3, Backblaze B2, Wasabi, Cloudflare R2 or your own MinIO. Each backup is uploaded after it's
          saved, and the same number of backups is kept there. Leave the endpoint empty to turn this off.
        </p>
        <div className="field-grid">
          {remoteFields.map(([key, label, placeholder, span]) => (
            <label key={key} className={span ? 'span-2' : undefined}>
              {label}
              <input
                className="mono"
                type={key === 'backup_s3_secret_key' ? 'password' : 'text'}
                autoComplete="off"
                value={remote[key] ?? ''}
                placeholder={placeholder}
                onChange={(e) => setRemote({ ...remote, [key]: e.target.value })}
              />
            </label>
          ))}
        </div>
        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary" onClick={saveSettings}>Save</button>
          {remote.backup_s3_endpoint && (
            <button className="btn btn-glass" disabled={testing} onClick={testRemote}>
              {testing ? 'Testing…' : 'Test cloud storage'}
            </button>
          )}
        </div>
      </div>

      <Restore />
    </div>
  )
}

function Restore() {
  const { data, reload } = useApi<{ dir: string; backups: Backup[]; bucket?: string; remote?: Backup[]; remoteError?: string }>(
    '/api/admin/backups',
  )
  const [busy, setBusy] = useState('')
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)

  const restore = async (b: Backup) => {
    const when = new Date(b.modified * 1000).toLocaleString()
    if (!confirm(`Restore the backup from ${when}? Users, watch history, settings and edits go back to how they were then. The current database is backed up first.`)) return
    setBusy(b.name)
    setMsg(null)
    try {
      const res = await api.post<{ restored: string; previous: string }>(`/api/admin/backups/${b.name}/restore`)
      setMsg({
        ok: true,
        text: `Restored. The data from before is saved as ${res.previous}. Restart the server so folder watching and Live TV pick up the restored settings.`,
      })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
    setBusy('')
    reload()
  }

  const restoreRemote = async (b: Backup) => {
    const when = new Date(b.modified * 1000).toLocaleString()
    if (!confirm(`Download and restore the cloud backup from ${when}? Users, watch history, settings and edits go back to how they were then. The current database is backed up first.`)) return
    setBusy('remote:' + b.name)
    setMsg(null)
    try {
      const res = await api.post<{ restored: string; previous: string }>(`/api/admin/backups/remote/${b.name}/restore`)
      setMsg({ ok: true, text: `Restored ${res.restored}. The data from before is saved as ${res.previous}. Restart the server so folder watching and Live TV pick up the restored settings.` })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
    setBusy('')
    reload()
  }

  const upload = async (file: File) => {
    if (!confirm(`Restore from ${file.name}? Users, watch history, settings and edits are replaced by the ones in the file. The current database is backed up first.`)) return
    setBusy('upload')
    setMsg(null)
    try {
      const res = await uploadBackup('/api/admin/backups/upload', file)
      setMsg({
        ok: true,
        text: `Restored from ${file.name}. The data from before is saved as ${res.previous}. If the backup came from another server, check the library folders in Settings, then restart the server.`,
      })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
    setBusy('')
    reload()
  }

  return (
    <>
      <h2 className="section-title">Restore a backup</h2>
      <div className="panel list" style={{ maxWidth: 760 }}>
        {data && data.backups.length === 0 && (
          <div className="task-row muted">No backups yet. Run "Back up database" above to make one.</div>
        )}
        {data?.backups.map((b) => (
          <div key={b.name} className="task-row">
            <div className="grow">
              <div className="task-name">{new Date(b.modified * 1000).toLocaleString()}</div>
              <div className="muted small">
                {b.name} · {formatBytes(b.size)} · {timeAgo(b.modified)}
              </div>
            </div>
            <a className="btn btn-glass btn-sm" href={`/api/admin/backups/${b.name}/download`} download title="Download, to keep elsewhere or move to a new server">
              <Icon name="download" size={14} /> Download
            </a>
            <button className="btn btn-glass btn-sm danger" disabled={!!busy} onClick={() => restore(b)}>
              <Icon name="refresh" size={14} className={busy === b.name ? 'spin' : ''} /> Restore
            </button>
          </div>
        ))}
        <div className="task-row">
          <div className="grow">
            <div className="task-name">Restore from a file</div>
            <div className="muted small">A backup downloaded from this or another BingeBandit server (a .db file)</div>
          </div>
          <label className={`btn btn-glass btn-sm danger${busy ? ' disabled' : ''}`}>
            <Icon name="upload" size={14} className={busy === 'upload' ? 'spin' : ''} /> {busy === 'upload' ? 'Uploading…' : 'Upload'}
            <input
              type="file"
              accept=".db,application/vnd.sqlite3,application/x-sqlite3"
              hidden
              disabled={!!busy}
              onChange={(e) => {
                const f = e.target.files?.[0]
                e.target.value = ''
                if (f) upload(f)
              }}
            />
          </label>
        </div>
      </div>
      {data && <p className="muted small">Backups in <span className="mono">{data.dir}</span></p>}
      {data?.bucket && (
        <>
          <h3>In cloud storage ({data.bucket})</h3>
          <div className="panel list" style={{ maxWidth: 760 }}>
            {data.remoteError && <div className="task-row form-error">Couldn't list the bucket: {data.remoteError}</div>}
            {!data.remoteError && data.remote?.length === 0 && <div className="task-row muted">Nothing uploaded yet.</div>}
            {data.remote?.map((b) => (
              <div key={b.name} className="task-row">
                <div className="grow">
                  <div className="task-name">{new Date(b.modified * 1000).toLocaleString()}</div>
                  <div className="muted small">
                    {b.name} · {formatBytes(b.size)} · {timeAgo(b.modified)}
                  </div>
                </div>
                <button className="btn btn-glass btn-sm danger" disabled={!!busy} onClick={() => restoreRemote(b)}>
                  <Icon name="refresh" size={14} className={busy === 'remote:' + b.name ? 'spin' : ''} /> Restore
                </button>
              </div>
            ))}
          </div>
        </>
      )}
      {msg && <div className={msg.ok ? 'form-ok' : 'form-error'} style={{ maxWidth: 760 }}>{msg.text}</div>}
    </>
  )
}
