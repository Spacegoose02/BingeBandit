import { useEffect, useState } from 'react'
import { desktop, type DesktopUpdate } from '../desktop'
import { Icon } from './Icon'

/**
 * In the Windows app: says when a new version of the app has downloaded (or,
 * for the portable app, is out), with Restart now. Later hides it until the
 * next start; the update installs when the app closes anyway.
 */
export function DesktopUpdateNotice() {
  const updates = desktop?.updates
  const [u, setU] = useState<DesktopUpdate | null>(null)
  const [later, setLater] = useState(() => sessionStorage.getItem('bb.updateLater') ?? '')
  useEffect(() => {
    if (!updates) return
    updates.state().then(setU, () => undefined)
    return updates.onChange(setU)
  }, [updates])
  if (!updates || !u || !u.version || later === u.version) return null
  if (u.status !== 'ready' && u.status !== 'available') return null
  const ready = u.status === 'ready'
  return (
    <div className="notice together-invite desktop-update">
      <Icon name="download" size={18} />
      <span className="grow">
        {ready ? (
          <>
            BingeBandit for Windows <strong>{u.version}</strong> is ready. It installs when you close the app, or now.
          </>
        ) : (
          <>
            BingeBandit for Windows <strong>{u.version}</strong> is out. The portable app doesn't update itself: download the new one.
          </>
        )}
      </span>
      <button className="btn btn-primary btn-sm" onClick={() => updates.install()}>
        {ready ? 'Restart now' : 'Download'}
      </button>
      <button
        className="btn btn-glass btn-sm"
        onClick={() => {
          sessionStorage.setItem('bb.updateLater', u.version ?? '')
          setLater(u.version ?? '')
        }}
      >
        Later
      </button>
    </div>
  )
}
