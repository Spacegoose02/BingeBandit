import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import type { Library, Member } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

/** Account section: the profiles under this account. */
export function Profiles() {
  const { user } = useAuth()
  const { data: members, reload } = useApi<Member[]>('/api/household')
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<Member | null>(null)
  const [error, setError] = useState('')
  if (!user) return null
  if (user.ownerId) {
    return (
      <div className="panel form" style={{ marginBottom: 20 }}>
        <h3>Profile</h3>
        <p className="muted small">
          This is a profile of another account. <Link to="/who">Switch profile</Link> to manage profiles from the account.
        </p>
      </div>
    )
  }
  const profiles = members?.filter((m) => !m.isOwner) ?? []

  const add = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await api.post('/api/household', { name })
      setName('')
      reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Profiles</h3>
      <p className="muted small">
        Give each person in your home their own profile: separate Continue Watching, watch history, watchlist and playback settings,
        all under this sign-in. Pick who's watching after you sign in, or from your name in the menu. A PIN keeps a profile private; the
        PIN for this main profile is your <strong>settings passcode</strong> below.
      </p>
      {profiles.length > 0 && (
        <div className="list">
          {profiles.map((p) => (
            <div key={p.id} className="list-row">
              <span className="avatar">{p.name.slice(0, 1).toUpperCase()}</span>
              <div className="grow">
                <div>
                  {p.name} {p.hasPin && <Icon name="lock" size={13} className="muted" />}
                </div>
                <div className="muted small">{p.allLibraries ? 'All your libraries' : `${p.libraryIds?.length ?? 0} libraries`}</div>
              </div>
              <button type="button" className="btn btn-glass btn-sm" onClick={() => setEditing(p)}>
                <Icon name="edit" size={16} /> Edit
              </button>
            </div>
          ))}
        </div>
      )}
      <form onSubmit={add} className="field-row">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name, e.g. Kids" maxLength={30} required />
        <button className="btn btn-primary">
          <Icon name="plus" size={16} /> Add profile
        </button>
      </form>
      {error && <div className="form-error">{error}</div>}
      {editing && <EditProfile profile={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />}
    </div>
  )
}

function EditProfile({ profile, onClose, onSaved }: { profile: Member; onClose: () => void; onSaved: () => void }) {
  const { data: libraries } = useApi<Library[]>('/api/libraries')
  const [name, setName] = useState(profile.name)
  const [all, setAll] = useState(profile.allLibraries)
  const [ids, setIds] = useState<Set<number>>(new Set(profile.libraryIds ?? []))
  const [pin, setPin] = useState('')
  const [error, setError] = useState('')

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      await api.put(`/api/household/${profile.id}`, { name, allLibraries: all, libraryIds: [...ids] })
      if (pin) await api.put(`/api/household/${profile.id}/pin`, { pin })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  const removePin = async () => {
    await api.put(`/api/household/${profile.id}/pin`, { pin: '' }).catch((e) => setError(e.message))
    onSaved()
  }
  const remove = async () => {
    if (!confirm(`Delete the profile "${profile.name}"? Its watch history, watchlist and playlists are deleted too.`)) return
    await api.del(`/api/household/${profile.id}`).catch((e) => setError(e.message))
    onSaved()
  }
  const toggle = (id: number) => {
    const next = new Set(ids)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setIds(next)
  }

  return (
    <Modal title={`Edit ${profile.name}`} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={30} required />
        </label>
        <label className="check">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
          All my libraries, including ones added later
        </label>
        {!all && (
          <div className="check-list">
            {libraries?.map((l) => (
              <label key={l.id} className="check">
                <input type="checkbox" checked={ids.has(l.id)} onChange={() => toggle(l.id)} />
                {l.name}
              </label>
            ))}
          </div>
        )}
        <label>
          {profile.hasPin ? 'New PIN (leave empty to keep it)' : 'PIN (optional, 4 to 8 digits)'}
          <input
            className="passcode-input"
            type="password"
            inputMode="numeric"
            pattern="[0-9]{4,8}"
            maxLength={8}
            value={pin}
            onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
            autoComplete="off"
          />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button className="btn btn-primary">Save</button>
          {profile.hasPin && (
            <button type="button" className="btn btn-glass" onClick={removePin}>
              Remove PIN
            </button>
          )}
          <button type="button" className="btn btn-glass danger" onClick={remove}>
            <Icon name="trash" size={16} /> Delete profile
          </button>
        </div>
      </form>
    </Modal>
  )
}
