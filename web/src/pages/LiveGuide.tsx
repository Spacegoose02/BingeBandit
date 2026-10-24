import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { GuideProgram, LiveChannel } from '../types'

const HOURS = 4 // guide window width
const SLOT = 30 * 60 // ruler step, seconds

export const clock = (unix: number) => new Date(unix * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
export const channelNo = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1))

interface Guide {
  from: number
  to: number
  channels: LiveChannel[]
  programs: Record<string, GuideProgram[]>
}

export function ChannelLogo({ channel, size = 40 }: { channel: Pick<LiveChannel, 'logo' | 'name'>; size?: number }) {
  const [broken, setBroken] = useState(false)
  if (!channel.logo || broken) return <span className="ch-logo-fallback" style={{ width: size * 1.5, height: size }}>{channel.name.slice(0, 3).toUpperCase()}</span>
  return <img className="ch-logo" src={channel.logo} alt="" style={{ height: size, maxWidth: size * 1.6 }} onError={() => setBroken(true)} loading="lazy" />
}

export default function LiveGuide() {
  useDocumentTitle('Live TV')
  const nav = useNavigate()
  const [from, setFrom] = useState(() => Math.floor(Date.now() / 1000 / SLOT) * SLOT)
  const to = from + HOURS * 3600
  const { data, reload } = useApi<Guide>(`/api/live/guide?from=${from}&to=${to}`, 60000)
  const [filter, setFilter] = useState(() => localStorage.getItem('bb.guideFilter') ?? 'all')
  const [selected, setSelected] = useState<{ channel: LiveChannel; program?: GuideProgram } | null>(null)
  const [now, setNow] = useState(Date.now() / 1000)
  const grid = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(900)

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now() / 1000), 30000)
    return () => window.clearInterval(t)
  }, [])
  useEffect(() => {
    const el = grid.current
    if (!el) return
    const ro = new ResizeObserver(() => setWidth(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [data])
  useEffect(() => localStorage.setItem('bb.guideFilter', filter), [filter])

  const groups = useMemo(() => [...new Set((data?.channels ?? []).map((c) => c.group).filter(Boolean))] as string[], [data])
  const channels = (data?.channels ?? []).filter((c) => filter === 'all' || (filter === 'fav' ? c.favorite : c.group === filter))
  const labelW = width < 560 ? 120 : Math.min(270, Math.max(210, width * 0.26))
  const pxPerSec = (width - labelW) / (to - from)
  const x = (t: number) => (Math.max(Math.min(t, to), from) - from) * pxPerSec
  const hero = selected ?? (channels[0] ? { channel: channels[0], program: channels[0].now } : null)

  const toggleFav = async (c: LiveChannel) => {
    await api[c.favorite ? 'del' : 'post'](`/api/live/channels/${c.id}/favorite`)
    reload()
  }

  if (data && data.channels.length === 0) {
    return (
      <div className="page">
        <div className="empty-hero">
          <Icon name="tv" size={64} />
          <h1>No live channels yet</h1>
          <p>An admin can add an M3U playlist and an XMLTV guide under Server → Live TV.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="page live-guide">
      {hero && (
        <section className="guide-hero">
          <div className="guide-hero-text">
            <div className="muted small">
              {channelNo(hero.channel.number)} · {hero.channel.name}
              {hero.program && ` · ${clock(hero.program.start)}–${clock(hero.program.stop)}`}
            </div>
            <h1>{hero.program?.title ?? hero.channel.name}</h1>
            {hero.program?.subtitle && <div className="guide-sub">{hero.program.subtitle}</div>}
            {hero.program?.description && <p className="guide-desc">{hero.program.description}</p>}
            <div className="actions">
              <button className="btn btn-primary" onClick={() => nav(`/live/${hero.channel.id}`)}>
                <Icon name="play" fill size={18} /> Watch {hero.channel.name}
              </button>
            </div>
          </div>
          <ChannelLogo channel={hero.channel} size={72} />
        </section>
      )}

      <div className="guide-toolbar">
        <select value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter channels">
          <option value="all">All channels</option>
          <option value="fav">Favorites</option>
          {groups.map((g) => (
            <option key={g} value={g}>{g}</option>
          ))}
        </select>
        <div className="actions">
          <button className="btn btn-glass btn-sm" onClick={() => setFrom(from - 2 * 3600)} aria-label="Earlier"><Icon name="back" size={16} /></button>
          <button className="btn btn-glass btn-sm" onClick={() => setFrom(Math.floor(Date.now() / 1000 / SLOT) * SLOT)}>Now</button>
          <button className="btn btn-glass btn-sm" onClick={() => setFrom(from + 2 * 3600)} aria-label="Later"><Icon name="next" size={16} /></button>
        </div>
        <span className="muted small">
          {new Date(from * 1000).toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' })}
        </span>
      </div>

      <div className="guide-grid" ref={grid}>
        <div className="guide-ruler" style={{ paddingLeft: labelW }}>
          {Array.from({ length: (HOURS * 3600) / SLOT }, (_, i) => from + i * SLOT).map((t) => (
            <span key={t} style={{ left: labelW + x(t) }}>{clock(t)}</span>
          ))}
          {now >= from && now < to && <span className="guide-now-label" style={{ left: labelW + x(now) }}>Now</span>}
        </div>
        {channels.length === 0 && <div className="empty">No channels match this filter.</div>}
        {channels.map((c) => {
          const progs = (c.epgId && data?.programs[c.epgId]) || []
          return (
            <div key={c.id} className="guide-row">
              <div className="guide-channel" style={{ width: labelW }}>
                <button className={`fav-star ${c.favorite ? 'on' : ''}`} onClick={() => toggleFav(c)} aria-label={c.favorite ? 'Remove favorite' : 'Add favorite'}>
                  <Icon name="star" fill={c.favorite} size={14} />
                </button>
                <button className="guide-channel-btn" onClick={() => nav(`/live/${c.id}`)} title={`Watch ${c.name}`}>
                  <span className="ch-num">{channelNo(c.number)}</span>
                  <ChannelLogo channel={c} size={28} />
                  <span className="ch-name">{c.name}</span>
                </button>
              </div>
              <div className="guide-track">
                {progs.length === 0 && (
                  <button className="guide-prog" style={{ left: 0, width: width - labelW - 4 }} onClick={() => setSelected({ channel: c })}>
                    <span className="gp-title">{c.name}</span>
                    <span className="gp-time">No guide information</span>
                  </button>
                )}
                {progs.map((p) => {
                  const left = x(p.start)
                  const w = x(p.stop) - left
                  if (w <= 2) return null
                  const live = p.start <= now && p.stop > now
                  const isSel = selected?.program?.id === p.id
                  return (
                    <button
                      key={p.id}
                      className={`guide-prog ${live ? 'live' : ''} ${p.stop <= now ? 'past' : ''} ${isSel ? 'selected' : ''}`}
                      style={{ left, width: w - 4 }}
                      onClick={() => (isSel && live ? nav(`/live/${c.id}`) : setSelected({ channel: c, program: p }))}
                      onDoubleClick={() => live && nav(`/live/${c.id}`)}
                      title={`${p.title} · ${clock(p.start)}–${clock(p.stop)}`}
                    >
                      {p.start < from && <Icon name="back" size={12} className="gp-cont" />}
                      <span className="gp-title">{p.title}</span>
                      {w > 120 && <span className="gp-time">{clock(p.start)}–{clock(p.stop)}</span>}
                    </button>
                  )
                })}
                {now >= from && now < to && <span className="guide-now-line" style={{ left: x(now) }} />}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
