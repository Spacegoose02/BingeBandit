import { useEffect, useRef, useState } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { useDocumentTitle } from '../../hooks'

interface Line {
  seq: number
  time: number
  level: 'info' | 'warn' | 'error'
  text: string
}

const MAX_LINES = 3000

export default function AdminLogs() {
  useDocumentTitle('Logs')
  const [lines, setLines] = useState<Line[]>([])
  const [filter, setFilter] = useState('')
  const [level, setLevel] = useState<'all' | 'warn' | 'error'>('all')
  const [follow, setFollow] = useState(true)
  const lastSeq = useRef(0)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let stop = false
    const poll = async () => {
      try {
        const fresh = await api.get<Line[]>(`/api/admin/logs?after=${lastSeq.current}`)
        if (!stop && fresh.length) {
          lastSeq.current = fresh[fresh.length - 1].seq
          setLines((prev) => [...prev, ...fresh].slice(-MAX_LINES))
        }
      } catch {
        // keep polling; the server may be restarting
      }
    }
    poll()
    const t = window.setInterval(poll, 2000)
    return () => {
      stop = true
      window.clearInterval(t)
    }
  }, [])

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [lines, follow, filter, level])

  const needle = filter.toLowerCase()
  const shown = lines.filter(
    (l) => (level === 'all' || l.level === 'error' || (level === 'warn' && l.level === 'warn')) && (!needle || l.text.toLowerCase().includes(needle)),
  )

  return (
    <div className="page logs-page">
      <header className="page-head">
        <h1>Logs</h1>
        <div className="actions">
          <input className="log-filter" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" />
          <div className="segmented">
            {(['all', 'warn', 'error'] as const).map((l) => (
              <button key={l} className={level === l ? 'active' : ''} onClick={() => setLevel(l)}>
                {l === 'all' ? 'All' : l === 'warn' ? 'Warnings+' : 'Errors'}
              </button>
            ))}
          </div>
          <label className="check small">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
          </label>
          <a className="btn btn-glass btn-sm" href="/api/admin/logs/download">
            <Icon name="download" size={16} /> Download
          </a>
        </div>
      </header>
      <div className="log-box" ref={box} onScroll={(e) => {
        const el = e.currentTarget
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        if (atBottom !== follow) setFollow(atBottom)
      }}>
        {shown.map((l) => (
          <div key={l.seq} className={`log-line ${l.level}`}>{l.text}</div>
        ))}
        {shown.length === 0 && <div className="muted">No log lines{filter ? ' match the filter' : ' yet'}.</div>}
      </div>
    </div>
  )
}
