import { Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import { formatBytes } from '../format'
import type { Recording, RecordingRule } from '../types'
import { clock } from './LiveGuide'

interface DVR {
  recordings: Recording[]
  rules: RecordingRule[]
  folder: string
  canRecord: boolean
}

const keepChoices = [0, 1, 2, 3, 5, 7, 10, 14, 20, 30]

const day = (unix: number) => new Date(unix * 1000).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })

const statusPill: Record<string, [string, string]> = {
  pending: ['warn', 'Needs approval'],
  scheduled: ['', 'Scheduled'],
  recording: ['danger', 'Recording'],
  completed: ['ok', 'Recorded'],
  failed: ['danger', 'Failed'],
  cancelled: ['', 'Cancelled'],
}

export default function RecordingsPage() {
  useDocumentTitle('Recordings')
  const { user } = useAuth()
  const { data, reload } = useApi<DVR>('/api/dvr', 10000)
  if (!data) return <div className="page-loading" />
  const upcoming = data.recordings.filter((r) => ['pending', 'scheduled', 'recording'].includes(r.status))
  const done = data.recordings.filter((r) => !['pending', 'scheduled', 'recording'].includes(r.status))

  const remove = async (r: Recording, withFile = false) => {
    const what = r.status === 'recording' ? 'Stop this recording? What was recorded so far is kept.' : withFile ? `Delete "${r.title}" and its file?` : null
    if (what && !confirm(what)) return
    await api.del(`/api/dvr/recordings/${r.id}${withFile ? '?file=1' : ''}`).catch((e) => alert(e.message))
    reload()
  }
  const approve = async (path: string) => {
    await api.post(path).catch((e) => alert(e.message))
    reload()
  }

  const row = (r: Recording) => {
    const [cls, label] = statusPill[r.status]
    return (
      <div key={r.id} className="list-row rec-row">
        <div className="rec-when">
          <div>{day(r.programStart)}</div>
          <div className="muted small">{clock(r.programStart)}–{clock(r.programStop)}</div>
        </div>
        <div className="grow">
          <div>
            {r.status === 'recording' && <span className="rec-dot live" />} {r.title}
            {r.subtitle && <span className="muted"> · {r.subtitle}</span>}
          </div>
          <div className="muted small">
            {[r.channelName, r.ruleId ? 'series' : '', user?.isAdmin && r.username, r.size ? formatBytes(r.size) : '', r.error].filter(Boolean).join(' · ')}
          </div>
          {r.conflict && <div className="small rec-conflict">May not record: {r.conflict}</div>}
        </div>
        {r.conflict && r.status !== 'recording' ? <span className="pill warn">Conflict</span> : <span className={`pill ${cls}`}>{label}</span>}
        {r.status === 'pending' && user?.isAdmin && (
          <button className="btn btn-primary btn-sm" onClick={() => approve(`/api/admin/dvr/recordings/${r.id}/approve`)}>Approve</button>
        )}
        {r.status === 'completed' && r.itemId ? (
          <Link className="btn btn-glass btn-sm" to={`/play/${r.itemId}`}><Icon name="play" fill size={14} /> Watch</Link>
        ) : null}
        {['pending', 'scheduled', 'recording'].includes(r.status) ? (
          <button className="btn btn-glass btn-sm danger" onClick={() => remove(r)}>
            {r.status === 'recording' ? 'Stop' : 'Cancel'}
          </button>
        ) : (
          <button className="btn btn-glass btn-sm danger" onClick={() => remove(r, true)} aria-label="Delete recording"><Icon name="trash" size={16} /></button>
        )}
      </div>
    )
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Recordings</h1>
          <div className="muted">Record from the Live TV guide: pick a program, then Record or Record series.</div>
        </div>
        <Link to="/live" className="btn btn-glass"><Icon name="broadcast" size={18} /> Open the guide</Link>
      </header>
      {!data.canRecord && <div className="notice">Recording is turned off for your account. Ask your server admin.</div>}

      <h2 className="section-title">Upcoming <span className="count">{upcoming.length}</span></h2>
      <div className="panel list">
        {upcoming.map(row)}
        {upcoming.length === 0 && <div className="list-row muted">Nothing scheduled.</div>}
      </div>

      <h2 className="section-title">Series <span className="count">{data.rules.length}</span></h2>
      <div className="panel list">
        {data.rules.map((rule) => (
          <div key={rule.id} className="list-row">
            <Icon name="list" />
            <div className="grow">
              <div>{rule.title}</div>
              <div className="muted small">
                {[rule.channelName || 'any channel', `${rule.upcoming} upcoming`, user?.isAdmin && rule.username].filter(Boolean).join(' · ')}
              </div>
            </div>
            <label className="rec-keep">
              <span className="muted small">Keep</span>
              <select
                value={rule.keepLast}
                onChange={async (e) => {
                  const keep = Number(e.target.value)
                  if (keep > 0 && !confirm(`Keep only the newest ${keep} recording${keep === 1 ? '' : 's'} of "${rule.title}"? Older ones and their files are deleted.`)) return
                  await api.patch(`/api/dvr/rules/${rule.id}`, { keepLast: keep }).catch((err) => alert(err.message))
                  reload()
                }}
              >
                {[...new Set([...keepChoices, rule.keepLast])].sort((a, b) => a - b).map((n) => (
                  <option key={n} value={n}>{n === 0 ? 'All' : `Newest ${n}`}</option>
                ))}
              </select>
            </label>
            {rule.status === 'pending' && <span className="pill warn">Needs approval</span>}
            {rule.status === 'pending' && user?.isAdmin && (
              <button className="btn btn-primary btn-sm" onClick={() => approve(`/api/admin/dvr/rules/${rule.id}/approve`)}>Approve</button>
            )}
            <button
              className="btn btn-glass btn-sm danger"
              onClick={async () => {
                if (!confirm(`Stop recording "${rule.title}"? Scheduled episodes are cancelled; finished ones are kept.`)) return
                await api.del(`/api/dvr/rules/${rule.id}`)
                reload()
              }}
            >
              Stop series
            </button>
          </div>
        ))}
        {data.rules.length === 0 && <div className="list-row muted">No series recordings.</div>}
      </div>

      <h2 className="section-title">Recorded <span className="count">{done.length}</span></h2>
      <div className="panel list">
        {done.map(row)}
        {done.length === 0 && <div className="list-row muted">No recordings yet.</div>}
      </div>
      {user?.isAdmin && (
        <p className="muted small" style={{ marginTop: 14 }}>
          Recordings are saved to <span className="mono">{data.folder}</span>. Add that folder as a TV Shows library to browse and watch them with
          artwork; Watch buttons appear here once the library has picked them up.
        </p>
      )}
    </div>
  )
}
