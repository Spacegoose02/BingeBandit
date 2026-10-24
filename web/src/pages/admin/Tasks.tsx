import { useEffect, useState } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { useApi, useDocumentTitle } from '../../hooks'
import { timeAgo } from '../../format'

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

  useEffect(() => {
    if (settings)
      setWin({ start: settings.maintenance_start, end: settings.maintenance_end, dir: settings.backup_dir, keep: settings.backup_keep })
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
      })
      setMsg({ ok: true, text: 'Saved.' })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    }
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
        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary" onClick={saveSettings}>Save</button>
        </div>
      </div>
    </div>
  )
}
