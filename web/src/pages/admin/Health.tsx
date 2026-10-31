import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Icon } from '../../components/Icon'
import { useApi, useDocumentTitle } from '../../hooks'
import { formatBytes, timeAgo } from '../../format'
import type { ServerRelease } from '../../types'

interface Folder {
  label: string
  kind: 'data' | 'library' | 'recordings' | 'backups' | 'transcode'
  path: string
  online: boolean
  error?: string
  total: number
  free: number
  low: boolean
}

interface HealthTask {
  id: string
  name: string
  enabled: boolean
  running: boolean
  lastRun: number
  lastError?: string
}

interface SentAlert {
  kind: string
  title: string
  body?: string
  problem: boolean
  time: number
  results?: Record<string, string>
}

interface Health {
  update: { current: string; checking: boolean; latest?: ServerRelease; available?: boolean; error?: string }
  folders: Folder[]
  lowPercent: number
  system: {
    version: string
    os: string
    started: number
    cpus: number
    memory: number
    goroutines: number
    database: number
    streams: number
    transcodes: number
  }
  tasks: HealthTask[]
  alerts: SentAlert[]
  channels: string[]
}

const kindLabels: Record<Folder['kind'], string> = {
  data: 'Data',
  library: 'Library',
  recordings: 'Recordings',
  backups: 'Backups',
  transcode: 'Transcoding',
}

const channelLabels: Record<string, string> = { email: 'Email', discord: 'Discord', ntfy: 'ntfy' }

function uptime(started: number) {
  const s = Math.max(0, Date.now() / 1000 - started)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`
}

/** Server health: drives and free space, the newest version, failing tasks and recent alerts. */
export default function AdminHealth() {
  useDocumentTitle('Server health')
  const [refresh, setRefresh] = useState(0)
  const { data, error } = useApi<Health>(`/api/admin/health${refresh ? `?refresh=1&n=${refresh}` : ''}`, 60000)

  if (error) return <div className="page"><div className="form-error">{error}</div></div>
  if (!data) return <div className="page-loading" />

  const { update, system } = data
  const offline = data.folders.filter((f) => !f.online)
  const low = data.folders.filter((f) => f.low)
  const failing = data.tasks.filter((t) => t.enabled && t.lastError)

  return (
    <div className="page narrow">
      <header className="page-head">
        <h1>Server health</h1>
        <div className="muted small">
          BingeBandit {system.version} · {system.os} · up {uptime(system.started)}
        </div>
      </header>

      {update.available && update.latest && (
        <div className="notice">
          <strong>BingeBandit {update.latest.version} is available</strong> (this server runs {update.current}).{' '}
          <a href={update.latest.url} target="_blank" rel="noreferrer">What's new and downloads</a>. With Docker or Unraid, pull the new image
          and restart the container; on Windows, download the new build and replace the old one.
          {update.latest.notes && <pre className="release-notes">{update.latest.notes}</pre>}
        </div>
      )}
      {offline.length > 0 && (
        <div className="notice warn">
          {offline.length === 1 ? 'A storage folder is offline' : `${offline.length} storage folders are offline`}. Titles on it stay in the
          library but won't play until it's back.
        </div>
      )}
      {low.length > 0 && <div className="notice warn">A drive has less than {data.lowPercent}% free.</div>}
      {failing.length > 0 && (
        <div className="notice warn">
          {failing.map((t) => t.name).join(', ')} failed the last time {failing.length === 1 ? 'it' : 'they'} ran. See{' '}
          <Link to="/admin/tasks">Tasks</Link>.
        </div>
      )}

      <section className="panel">
        <header className="panel-head">
          <h3>Updates</h3>
          <button className="btn btn-glass btn-sm" onClick={() => setRefresh((n) => n + 1)}>
            <Icon name="refresh" size={16} /> Check now
          </button>
        </header>
        <p className="small">
          {update.available && update.latest
            ? `Version ${update.latest.version} is out; this server runs ${update.current}.`
            : update.latest
              ? `Up to date: the newest release is ${update.latest.version}.`
              : update.checking
                ? 'No release found yet.'
                : 'Update checks are off (Settings → Admin alerts).'}
          {update.error && <span className="muted"> Couldn't check: {update.error}</span>}
        </p>
      </section>

      <h2 className="section-title">Storage</h2>
      <div className="panel list">
        {data.folders.map((f, i) => {
          const used = f.total ? (f.total - f.free) / f.total : 0
          return (
            <div key={`${f.path}-${i}`} className="list-row">
              <Icon name={f.kind === 'library' ? 'library' : f.kind === 'recordings' ? 'broadcast' : 'folder'} />
              <div className="grow">
                <div>
                  {f.label} <span className="muted small">· {kindLabels[f.kind]}</span>
                </div>
                <div className="muted small mono" style={{ wordBreak: 'break-all' }}>{f.path}</div>
                {f.online && f.total > 0 && (
                  <>
                    <div className="progress-track disk-bar" style={{ marginTop: 6 }}>
                      <div className={f.low ? 'low' : undefined} style={{ width: `${used * 100}%` }} />
                    </div>
                    <div className="muted small">
                      {formatBytes(f.free)} free of {formatBytes(f.total)} ({Math.round((1 - used) * 100)}% free)
                    </div>
                  </>
                )}
                {!f.online && <div className="small form-error-text">This folder {f.error}.</div>}
              </div>
              <span className={`pill ${!f.online ? 'danger' : f.low ? 'warn' : 'ok'}`}>{!f.online ? 'Offline' : f.low ? 'Nearly full' : 'Online'}</span>
            </div>
          )
        })}
      </div>

      <h2 className="section-title">System</h2>
      <dl className="panel facts">
        <dt>Running since</dt>
        <dd>{new Date(system.started * 1000).toLocaleString()}</dd>
        <dt>Streams</dt>
        <dd>
          {system.streams} playing · {system.transcodes} converting
        </dd>
        <dt>Database</dt>
        <dd>{formatBytes(system.database)}</dd>
        <dt>Memory</dt>
        <dd>{formatBytes(system.memory)}</dd>
        <dt>Processors</dt>
        <dd>{system.cpus}</dd>
      </dl>

      <h2 className="section-title">Tasks</h2>
      <div className="panel list">
        {data.tasks
          .filter((t) => t.enabled)
          .map((t) => (
            <div key={t.id} className="list-row">
              <div className="grow">
                <div>{t.name}</div>
                <div className="muted small">
                  {t.running ? 'Running now' : `Last ran ${timeAgo(t.lastRun)}`}
                  {t.lastError && <> · {t.lastError}</>}
                </div>
              </div>
              {t.lastRun > 0 && <span className={`pill ${t.lastError ? 'danger' : 'ok'}`}>{t.lastError ? 'Failed' : 'OK'}</span>}
            </div>
          ))}
      </div>

      <h2 className="section-title">Recent alerts</h2>
      <p className="muted small">
        {data.channels.length
          ? `Sent to admins' notifications and ${data.channels.map((c) => channelLabels[c] ?? c).join(', ')}.`
          : "Sent to admins' notifications only."}{' '}
        Set up email, Discord or ntfy in <Link to="/admin/settings">Settings → Admin alerts</Link>.
      </p>
      <div className="panel list">
        {data.alerts.length === 0 && <div className="list-row muted">No alerts since the server started.</div>}
        {data.alerts.map((a, i) => (
          <div key={i} className="list-row">
            <Icon name={a.problem ? 'info' : 'check'} />
            <div className="grow">
              <div>{a.title}</div>
              {a.body && <div className="muted small" style={{ whiteSpace: 'pre-line' }}>{a.body}</div>}
              <div className="muted small">
                {timeAgo(a.time)}
                {Object.entries(a.results ?? {}).map(([ch, res]) => (
                  <span key={ch}>
                    {' · '}
                    {channelLabels[ch] ?? ch}: {res === 'sent' ? 'sent' : <span className="form-error-text">{res}</span>}
                  </span>
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

/** A notice for the Dashboard when the daily check found a newer version. */
export function UpdateNotice({ update }: { update?: ServerRelease }) {
  if (!update) return null
  return (
    <div className="notice">
      BingeBandit {update.version} is available. <Link to="/admin/health">See what's new</Link>
    </div>
  )
}
