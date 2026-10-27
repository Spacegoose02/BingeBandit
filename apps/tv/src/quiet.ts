import { api, cached } from './api'

// The profile's "quiet while watching" preference, refreshed now and then so
// the pop-ups that check it don't each ask the server.
let quiet = !!cached<{ quietWhilePlaying?: boolean }>('/api/me/prefs')?.quietWhilePlaying
let checked = 0

/** Whether pop-ups should wait: the person asked for quiet and something is playing. */
export function quietNow(playing: boolean): boolean {
  if (Date.now() - checked > 60000) {
    checked = Date.now()
    api.get<{ quietWhilePlaying?: boolean }>('/api/me/prefs').then((p) => (quiet = !!p.quietWhilePlaying), () => undefined)
  }
  return playing && quiet
}
