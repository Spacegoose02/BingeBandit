import { useRef, useState } from 'react'
import { api } from '../../api'
import { libraryRef, LOGO_TYPES, uploadLogos } from '../../components/ChannelLogos'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi } from '../../hooks'
import type { AdminChannel, ChannelLogo } from '../../types'
import { ChannelLogo as Logo, channelNo } from '../LiveGuide'

/** Admin → Live TV → Logos: your own channel logos, which survive playlist refreshes. */
export function Logos() {
  const { data, reload } = useApi<ChannelLogo[]>('/api/admin/live/logos')
  const [match, setMatch] = useState(() => localStorage.getItem('bb.logoMatch') !== '0')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [assigning, setAssigning] = useState<ChannelLogo | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const upload = async (files: FileList) => {
    setBusy(true)
    setNote('')
    try {
      const res = await uploadLogos(files, match)
      const parts = [`Added ${res.added} logo${res.added === 1 ? '' : 's'}.`]
      if (match) parts.push(`${res.matched} channel${res.matched === 1 ? '' : 's'} now use${res.matched === 1 ? 's' : ''} them.`)
      if (res.failed?.length) parts.push(`Skipped: ${res.failed.join('; ')}`)
      setNote(parts.join(' '))
      reload()
    } catch (e) {
      setNote((e as Error).message)
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }
  const matchAll = async () => {
    const res = await api.post<{ matched: number }>('/api/admin/live/logos/match')
    setNote(res.matched ? `${res.matched} channel${res.matched === 1 ? '' : 's'} got a logo from the library.` : 'Every channel with a matching name already uses its logo.')
    reload()
  }
  const rename = async (l: ChannelLogo) => {
    const name = prompt('Logo name (used to match channel names)', l.name)?.trim()
    if (!name || name === l.name) return
    await api.put(`/api/admin/live/logos/${l.id}`, { name })
    reload()
  }
  const remove = async (l: ChannelLogo) => {
    const used = l.channels ? ` ${l.channels} channel${l.channels === 1 ? '' : 's'} will go back to the provider's logo.` : ''
    if (!confirm(`Delete the logo “${l.name}”?${used}`)) return
    await api.del(`/api/admin/live/logos/${l.id}`)
    reload()
  }
  const shown = (data ?? []).filter((l) => !q || l.name.toLowerCase().includes(q.toLowerCase()))

  return (
    <>
      <div className="actions" style={{ marginBottom: 8 }}>
        <button className="btn btn-primary" disabled={busy} onClick={() => fileRef.current?.click()}>
          <Icon name="upload" size={18} /> {busy ? 'Uploading…' : 'Upload logos'}
        </button>
        <input ref={fileRef} type="file" accept={LOGO_TYPES} multiple hidden onChange={(e) => e.target.files?.length && upload(e.target.files)} />
        <label className="check toolbar-check">
          <input
            type="checkbox"
            checked={match}
            onChange={(e) => {
              setMatch(e.target.checked)
              try {
                localStorage.setItem('bb.logoMatch', e.target.checked ? '1' : '0')
              } catch {
                /* private mode */
              }
            }}
          />
          Give each to channels with the same name
        </label>
        <button className="btn btn-glass" onClick={matchAll} disabled={!data?.length}>
          <Icon name="refresh" size={18} /> Match all to channels
        </button>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a logo" style={{ width: 200 }} />
        <span className="muted small">{data?.length ?? 0} logos</span>
      </div>
      <p className="muted small" style={{ marginTop: 0 }}>
        PNG, JPEG, SVG, WebP or GIF up to 2 MB each. Name files after the channel (“CNN.png”, “TSN 1.svg”): matching ignores case, “HD”, country prefixes like “US:” and text in brackets. Library logos stay when playlists refresh.
      </p>
      {note && <div className="notice small" style={{ marginBottom: 12 }}>{note}</div>}
      <div className="logo-library">
        {shown.map((l) => (
          <div key={l.id} className="logo-card panel">
            <div className="art-thumb logo">
              <img src={l.url} alt="" loading="lazy" />
            </div>
            <button className="logo-name" onClick={() => rename(l)} title="Rename">{l.name}</button>
            <div className="muted small">{l.channels ? `${l.channels} channel${l.channels === 1 ? '' : 's'}` : 'Not used'}</div>
            <div className="actions">
              <button className="btn btn-glass btn-sm" onClick={() => setAssigning(l)}><Icon name="tv" size={16} /> Channels</button>
              <button className="btn btn-glass btn-sm danger" onClick={() => remove(l)} aria-label="Delete logo"><Icon name="trash" size={16} /></button>
            </div>
          </div>
        ))}
      </div>
      {data?.length === 0 && <div className="panel list"><div className="list-row muted">No logos yet. Upload a few, or a whole folder's worth at once.</div></div>}
      {assigning && <AssignLogo logo={assigning} onClose={() => setAssigning(null)} onSaved={() => { setAssigning(null); reload() }} />}
    </>
  )
}

/** Pick which channels use a logo. */
function AssignLogo({ logo, onClose, onSaved }: { logo: ChannelLogo; onClose: () => void; onSaved: () => void }) {
  const { data } = useApi<AdminChannel[]>('/api/admin/live/channels')
  const [picked, setPicked] = useState<Set<number> | null>(null)
  const [q, setQ] = useState('')
  const [error, setError] = useState('')
  const using = new Set((data ?? []).filter((c) => c.logo === libraryRef(logo)).map((c) => c.id))
  const sel = picked ?? using
  const shown = (data ?? []).filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q))
  const toggle = (id: number) => {
    const n = new Set(sel)
    if (n.has(id)) n.delete(id)
    else n.add(id)
    setPicked(n)
  }
  const save = async () => {
    try {
      await api.post(`/api/admin/live/logos/${logo.id}/assign`, { channelIds: [...sel] })
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Modal title={`Channels using “${logo.name}”`} onClose={onClose}>
      <div className="form">
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a channel" autoFocus />
        <div className="panel list compact assign-list">
          {shown.map((c) => (
            <label key={c.id} className="list-row">
              <input type="checkbox" checked={sel.has(c.id)} onChange={() => toggle(c.id)} />
              <span className="ch-num">{channelNo(c.number)}</span>
              <Logo channel={{ name: c.name, logo: c.logoUrl }} size={22} />
              <span className="grow">{c.name}</span>
            </label>
          ))}
        </div>
        <p className="muted small" style={{ margin: 0 }}>Channels you untick go back to their provider's logo.</p>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <span className="muted small grow">{sel.size} selected</span>
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={save}>Save</button>
        </div>
      </div>
    </Modal>
  )
}
