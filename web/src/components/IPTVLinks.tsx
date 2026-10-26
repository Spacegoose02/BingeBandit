import { useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'

/**
 * The signed-in user's M3U playlist and XMLTV guide URLs for IPTV apps, with
 * a button that makes an API key to put in them. The playlist lists only the
 * channels this user can watch. Plex gets an HDHomeRun-style tuner address
 * (Plex only takes tuners, not M3U) and a guide URL with the key in the path.
 */
export function IPTVLinks({ onCreated }: { onCreated?: () => void }) {
  const [key, setKey] = useState<string | null>(null)
  const origin = window.location.origin
  const create = async () => {
    try {
      setKey((await api.post<{ key: string }>('/api/me/keys', { name: 'IPTV app' })).key)
      onCreated?.()
    } catch (err) {
      alert((err as Error).message)
    }
  }
  const tuner = `${origin}/api/live/tuner/${key ?? '…'}`
  const urls = [
    ['M3U playlist', `${origin}/api/live/playlist.m3u?token=${key ?? '…'}`],
    ['XMLTV guide', `${origin}/api/live/epg.xml?token=${key ?? '…'}`],
    ['Plex tuner', tuner],
    ['Plex guide', `${tuner}/epg.xml`],
  ]
  return (
    <div className="panel facts">
      {urls.map(([label, url]) => (
        <div key={label} style={{ display: 'contents' }}>
          <dt>{label}</dt>
          <dd className="mono" style={{ wordBreak: 'break-all' }}>
            {url}
            {key && (
              <button type="button" className="btn btn-glass btn-sm" style={{ marginLeft: 8 }} onClick={() => navigator.clipboard?.writeText(url)}>
                Copy
              </button>
            )}
          </dd>
        </div>
      ))}
      <dd className="muted small" style={{ gridColumn: '1 / -1' }}>
        {key
          ? "Copy these into TiviMate, VLC or another IPTV app now; the key won't be shown again. For Plex, go to Live TV & DVR, add the Plex tuner address as an HDHomeRun device, then give it the Plex guide URL as an XMLTV guide. Delete the key under API keys to cut the app off."
          : 'Apps like TiviMate, VLC or Plex get the shared, fail-over streams. Create an API key for the app so it never needs your password.'}
      </dd>
      {!key && (
        <dd style={{ gridColumn: '1 / -1' }}>
          <button type="button" className="btn btn-glass btn-sm" onClick={create}>
            <Icon name="key" size={16} /> Create a key for an IPTV app
          </button>
        </dd>
      )}
    </div>
  )
}
