import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, img } from '../api'
import { timeAgo } from '../format'
import { useApi } from '../hooks'
import { Icon } from './Icon'

type Notification = {
  id: number
  kind: 'episodes' | 'recording' | 'sports'
  title: string
  body?: string
  link?: string
  image?: string
  createdAt: number
  readAt?: number
}

type Inbox = { notifications: Notification[]; unread: number }

// NotificationBell sits next to the user chip and opens the newest
// notifications: new episodes of shows you follow and your recordings.
export function NotificationBell() {
  const { data, reload, setData } = useApi<Inbox>('/api/notifications', 60_000)
  const [open, setOpen] = useState(false)
  const loc = useLocation()
  const nav = useNavigate()
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => setOpen(false), [loc.pathname])
  useEffect(() => {
    if (!open) return
    reload()
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open, reload])

  const unread = data?.unread ?? 0
  const markRead = async (ids?: number[]) => {
    if (!data) return
    const now = Math.floor(Date.now() / 1000)
    const hit = (n: Notification) => !n.readAt && (!ids || ids.includes(n.id))
    setData({
      unread: Math.max(0, data.unread - data.notifications.filter(hit).length),
      notifications: data.notifications.map((n) => (hit(n) ? { ...n, readAt: now } : n)),
    })
    await api.post('/api/notifications/read', ids ? { ids } : {}).catch(() => reload())
  }
  const clear = async () => {
    setData({ notifications: [], unread: 0 })
    await api.del('/api/notifications').catch(() => reload())
  }
  const openOne = (n: Notification) => {
    if (!n.readAt) markRead([n.id])
    setOpen(false)
    if (n.link) nav(n.link)
  }

  return (
    <div className="notify" ref={box}>
      <button
        className="icon-btn notify-bell"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-label={unread ? `Notifications, ${unread} unread` : 'Notifications'}
        title="Notifications"
      >
        <Icon name="bell" />
        {unread > 0 && <span className="notify-badge">{unread > 99 ? '99+' : unread}</span>}
      </button>
      {open && (
        <div className="menu notify-panel" role="dialog" aria-label="Notifications">
          <div className="notify-head">
            <strong>Notifications</strong>
            {unread > 0 && (
              <button className="link-btn" onClick={() => markRead()}>
                Mark all read
              </button>
            )}
            {unread === 0 && (data?.notifications.length ?? 0) > 0 && (
              <button className="link-btn" onClick={clear}>
                Clear all
              </button>
            )}
          </div>
          {!data?.notifications.length && (
            <p className="muted small notify-empty">
              Nothing yet. You'll hear about new episodes of shows you watch and how your recordings went. Turn these off in Account.
            </p>
          )}
          <div className="notify-list">
            {data?.notifications.map((n) => (
              <button key={n.id} className={`notify-item${n.readAt ? '' : ' unread'}`} onClick={() => openOne(n)}>
                {n.image ? (
                  <img src={img(n.image, 'w92')} alt="" className={`notify-img-${n.kind}`} />
                ) : (
                  <span className={`notify-icon notify-${n.kind}`}>
                    {n.kind === 'recording' ? <span className="nav-rec" /> : <Icon name={n.kind === 'sports' ? 'trophy' : 'tv'} size={18} />}
                  </span>
                )}
                <span className="notify-text">
                  <span className="notify-title">{n.title}</span>
                  {n.body && <span className="notify-body">{n.body}</span>}
                  <span className="notify-time">{timeAgo(n.createdAt)}</span>
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
