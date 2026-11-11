import type { RequestableTitle } from './types'

/** What a title someone could request says under its poster. */
export function requestLabel(r: RequestableTitle): string {
  const req = r.request
  if (!req || req.status === 'declined') return r.kind === 'show' ? 'Show · press to request' : 'Movie · press to request'
  if (!req.mine && req.status !== 'available') return `${req.people} asked · press for you too`
  switch (req.status) {
    case 'parent':
      return 'Asked · waiting for a grown-up'
    case 'pending':
      return 'Requested · waiting for approval'
    case 'approved':
      return 'Approved · on its way'
    case 'sent':
      if (req.stage === 'downloading') {
        const mins = req.eta ? Math.max(1, Math.round((req.eta - Date.now() / 1000) / 60)) : 0
        return `Downloading ${Math.round((req.progress ?? 0) * 100)}%${mins ? ` · ${mins < 120 ? `${mins} min` : `${Math.round(mins / 60)} h`}` : ''}`
      }
      if (req.stage === 'importing') return 'Almost ready'
      if (req.stage === 'waiting') return 'Requested · not out yet'
      return 'Approved · searching'
    case 'failed':
      return 'Approved · waiting for the admin'
    case 'available':
      return 'On the server'
  }
  return ''
}
