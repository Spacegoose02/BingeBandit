import { useRef, useState } from 'react'
import { api, ApiError } from '../api'
import { useApi } from '../hooks'
import type { ChannelLogo } from '../types'
import { Icon } from './Icon'

/** A channel's logo field value for a library logo. */
export const libraryRef = (l: ChannelLogo) => `library:${l.file}`

/** Where to show a logo field value from: library logos are served by the server, URLs as they are. */
export const logoSrc = (value: string) => (value.startsWith('library:') ? `/api/live/logos/${value.slice(8)}` : value)

export interface UploadResult {
  added: number
  matched: number
  failed?: string[]
}

/** Adds images to the logo library; with match, each also goes to channels whose names match its file name. */
export async function uploadLogos(files: FileList | File[], match: boolean): Promise<UploadResult> {
  const body = new FormData()
  for (const f of Array.from(files)) body.append('file', f)
  if (match) body.append('match', '1')
  const res = await fetch('/api/admin/live/logos', {
    method: 'POST',
    body,
    credentials: 'same-origin',
    headers: { 'X-Requested-With': 'BingeBandit' },
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText)
  return data
}

export const LOGO_TYPES = 'image/png,image/jpeg,image/svg+xml,image/webp,image/gif'

/** The logo input in channel editors: a URL, or a pick from the logo library (with upload). */
export function LogoField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [open, setOpen] = useState(false)
  const { data, setData } = useApi<ChannelLogo[]>(open ? '/api/admin/live/logos' : null)
  const [q, setQ] = useState('')
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const isLibrary = value.startsWith('library:')

  const upload = async (files: FileList) => {
    setError('')
    try {
      const res = await uploadLogos(files, false)
      if (res.failed?.length) setError(res.failed.join('; '))
      const before = new Set((data ?? []).map((l) => l.id))
      const list = await api.get<ChannelLogo[]>('/api/admin/live/logos')
      setData(list)
      const added = list.find((l) => !before.has(l.id))
      if (added && files.length === 1) onChange(libraryRef(added))
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const shown = (data ?? []).filter((l) => !q || l.name.toLowerCase().includes(q.toLowerCase()))

  return (
    <div className="logo-field">
      <div className="url-row">
        <div className="art-thumb logo logo-preview">{value ? <img src={logoSrc(value)} alt="" /> : <span className="muted small">None</span>}</div>
        {isLibrary ? (
          <span className="chip strong grow">From the logo library</span>
        ) : (
          <input value={value} onChange={(e) => onChange(e.target.value)} placeholder="Logo URL" />
        )}
        <button type="button" className="btn btn-glass btn-sm" onClick={() => setOpen(!open)}>
          <Icon name="image" size={16} /> Library
        </button>
        {value && (
          <button type="button" className="icon-btn" onClick={() => onChange('')} aria-label="Remove logo">
            <Icon name="x" size={16} />
          </button>
        )}
      </div>
      {open && (
        <div className="logo-picker">
          <div className="actions">
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a logo" style={{ width: 200 }} />
            <button type="button" className="btn btn-glass btn-sm" onClick={() => fileRef.current?.click()}>
              <Icon name="upload" size={16} /> Upload
            </button>
            <input ref={fileRef} type="file" accept={LOGO_TYPES} multiple hidden onChange={(e) => e.target.files?.length && upload(e.target.files)} />
          </div>
          {error && <div className="form-error small">{error}</div>}
          <div className="art-grid logo compact">
            {shown.map((l) => (
              <button
                type="button"
                key={l.id}
                className={`art-thumb logo ${value === libraryRef(l) ? 'chosen' : ''}`}
                title={l.name}
                onClick={() => {
                  onChange(libraryRef(l))
                  setOpen(false)
                }}
              >
                <img src={l.url} alt={l.name} loading="lazy" />
              </button>
            ))}
          </div>
          {data?.length === 0 && <div className="muted small">The logo library is empty. Upload images here or under Live TV → Logos.</div>}
        </div>
      )}
    </div>
  )
}
