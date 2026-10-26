import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { Avatar } from '../../components/Avatar'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { Library, User } from '../../types'

export default function AdminUsers() {
  useDocumentTitle('Users')
  const { user: me } = useAuth()
  const { data: users, reload } = useApi<User[]>('/api/admin/users')
  const [adding, setAdding] = useState(false)
  const [resetting, setResetting] = useState<User | null>(null)
  const [access, setAccess] = useState<User | null>(null)
  const [linking, setLinking] = useState<User | null>(null)
  const [unlinking, setUnlinking] = useState<User | null>(null)
  const [emailing, setEmailing] = useState<User | null>(null)
  const [naming, setNaming] = useState<User | null>(null)

  const remove = async (u: User) => {
    if (!confirm(`Delete ${u.username}? Their watch history is removed too.`)) return
    await api.del(`/api/admin/users/${u.id}`).catch((e) => alert(e.message))
    reload()
  }
  const clearPasscode = async (u: User) => {
    if (!confirm(`Remove ${u.username}'s settings passcode? They can set a new one under Account.`)) return
    await api.del(`/api/admin/users/${u.id}/passcode`).catch((e) => alert(e.message))
    reload()
  }
  const removePasskeys = async (u: User) => {
    if (!confirm(`Remove all of ${u.username}'s passkeys? They sign in with their password until they add new ones.`)) return
    await api.del(`/api/admin/users/${u.id}/passkeys`).catch((e) => alert(e.message))
    reload()
  }
  const setLiveTV = async (u: User, on: boolean) => {
    if (!on && !confirm(`Turn off Live TV for ${u.username}${u.ownerId ? '' : ' and their profiles'}? Anything they're watching stops, and Live TV and Recordings disappear for them.`)) return
    await api.put(`/api/admin/users/${u.id}/live-tv`, { on }).catch((e) => alert(e.message))
    reload()
  }
  const setKid = async (u: User, on: boolean) => {
    await api.put(`/api/admin/users/${u.id}/kid`, { on }).catch((e) => alert(e.message))
    reload()
  }
  const reset2FA = async (u: User) => {
    if (!confirm(`Turn off two-step sign-in for ${u.username}? They can sign in with just their password until they set it up again.`)) return
    await api.del(`/api/admin/users/${u.id}/2fa`).catch((e) => alert(e.message))
    reload()
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>Users</h1>
        <button className="btn btn-primary" onClick={() => setAdding(true)}>
          <Icon name="plus" size={18} /> Add user
        </button>
      </header>
      <div className="panel list">
        {users?.map((u) => (
          <div key={u.id} className="list-row">
            <Avatar name={u.profileName || u.username} src={u.avatar} />
            <div className="grow">
              <div>
                {!u.ownerId && u.profileName ? (
                  <>
                    {u.profileName} <span className="muted small">({u.username})</span>
                  </>
                ) : (
                  u.username
                )}{' '}
                {u.id === me?.id && <span className="muted small">(you)</span>}{' '}
                {u.ownerId ? (
                  <span className="pill" title="A profile under another account; it has no password of its own">
                    Profile of {users?.find((o) => o.id === u.ownerId)?.username ?? 'an account'}
                  </span>
                ) : (
                  !u.hasPassword && <span className="pill" title="Made by single sign-on; no password yet">SSO only</span>
                )}
              </div>
              <div className="muted small">
                Joined {new Date(u.createdAt * 1000).toLocaleDateString()}
                {u.email && <> · {u.email}</>}
              </div>
            </div>
            {u.isAdmin ? (
              <span className="pill ok">Admin · all libraries</span>
            ) : (
              <button className="btn btn-glass btn-sm" onClick={() => setAccess(u)}>
                <Icon name="library" size={16} /> {u.allLibraries ? 'All libraries' : `${u.libraryIds?.length ?? 0} libraries`}
              </button>
            )}
            {!u.isAdmin && (
              <button
                className={`btn btn-glass btn-sm${u.liveTV ? '' : ' danger'}`}
                onClick={() => setLiveTV(u, !u.liveTV)}
                title={u.liveTV ? 'Turn off Live TV and recordings for this user' : 'Let this user watch Live TV again'}
              >
                <Icon name="broadcast" size={16} /> {u.liveTV ? 'Live TV on' : 'Live TV off'}
              </button>
            )}
            {!u.isAdmin && u.liveTV && (
              <button
                className={`btn btn-glass btn-sm${u.kid ? ' active' : ''}`}
                onClick={() => setKid(u, !u.kid)}
                title={u.kid ? 'Only sees channels approved for kids (Live TV → Kids). Click to show every channel.' : 'Only show this user the channels approved for kids'}
              >
                <Icon name="user" size={16} /> {u.kid ? 'Kid' : 'Not a kid'}
              </button>
            )}
            {u.hasPasscode && (
              <button className="btn btn-glass btn-sm" onClick={() => clearPasscode(u)} title="For someone who forgot their settings passcode">
                <Icon name="unlock" size={16} /> Clear passcode
              </button>
            )}
            {u.totpEnabled && (
              <button className="btn btn-glass btn-sm" onClick={() => reset2FA(u)} title="For someone who lost their phone and recovery codes">
                Turn off 2-step
              </button>
            )}
            {!!u.passkeys && (
              <button className="btn btn-glass btn-sm" onClick={() => removePasskeys(u)} title="For a lost or shared device">
                Remove {u.passkeys === 1 ? 'passkey' : `${u.passkeys} passkeys`}
              </button>
            )}
            <button className="btn btn-glass btn-sm" onClick={() => setNaming(u)} title="The name everyone else sees; sign-in keeps the username">
              <Icon name="edit" size={16} /> Name
            </button>
            <button className="btn btn-glass btn-sm" onClick={() => setEmailing(u)} title="Single sign-on with this email signs in as this user">
              {u.email ? 'Email' : 'Add email'}
            </button>
            {u.ownerId ? (
              <button className="btn btn-glass btn-sm" onClick={() => setUnlinking(u)} title="Give this profile its own sign-in again">
                Unlink
              </button>
            ) : (
              !u.isAdmin &&
              u.id !== me?.id && (
                <button className="btn btn-glass btn-sm" onClick={() => setLinking(u)} title="Move this user under another account as a profile">
                  <Icon name="link" size={16} /> Link to account
                </button>
              )
            )}
            {!u.ownerId && (
              <button className="btn btn-glass btn-sm" onClick={() => setResetting(u)}>
                <Icon name="key" size={16} /> Reset password
              </button>
            )}
            {u.id !== me?.id && (
              <button className="btn btn-glass btn-sm danger" onClick={() => remove(u)} aria-label={`Delete ${u.username}`}>
                <Icon name="trash" size={16} />
              </button>
            )}
          </div>
        ))}
      </div>
      {adding && <AddUser onClose={() => setAdding(false)} onSaved={() => { setAdding(false); reload() }} />}
      {resetting && <ResetPassword user={resetting} onClose={() => setResetting(null)} />}
      {access && <LibraryAccess user={access} onClose={() => setAccess(null)} onSaved={() => { setAccess(null); reload() }} />}
      {linking && users && (
        <LinkUser user={linking} users={users} onClose={() => setLinking(null)} onSaved={() => { setLinking(null); reload() }} />
      )}
      {unlinking && <UnlinkUser user={unlinking} onClose={() => setUnlinking(null)} onSaved={() => { setUnlinking(null); reload() }} />}
      {naming && <DisplayName user={naming} onClose={() => setNaming(null)} onSaved={() => { setNaming(null); reload() }} />}
      {emailing && <UserEmail user={emailing} onClose={() => setEmailing(null)} onSaved={() => { setEmailing(null); reload() }} />}
    </div>
  )
}

function LibraryAccess({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const { data: libraries } = useApi<Library[]>('/api/admin/libraries')
  const [all, setAll] = useState(user.allLibraries)
  const [ids, setIds] = useState<Set<number>>(new Set(user.libraryIds ?? []))
  const [error, setError] = useState('')
  const toggle = (id: number) =>
    setIds((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/libraries`, { all, libraryIds: [...ids] })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Libraries for ${user.username}`} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label className="check">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
          All libraries, including ones added later
        </label>
        <div className={`check-list ${all ? 'disabled' : ''}`}>
          {libraries?.map((l) => (
            <label key={l.id} className="check">
              <input type="checkbox" disabled={all} checked={all || ids.has(l.id)} onChange={() => toggle(l.id)} />
              <Icon name={l.kind === 'movies' ? 'film' : 'tv'} size={16} /> {l.name}
            </label>
          ))}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}

function AddUser({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.post('/api/admin/users', { username, password, isAdmin })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title="Add user" onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} required minLength={2} autoFocus />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        <label className="check">
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
          Admin (can manage libraries, users and settings)
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Add user</button>
        </div>
      </form>
    </Modal>
  )
}

function ResetPassword({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/password`, { password })
      onClose()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Reset password for ${user.username}`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <label>
          New password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoFocus autoComplete="new-password" />
        </label>
        <p className="muted small">They will be signed out on all devices.</p>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Reset password</button>
        </div>
      </form>
    </Modal>
  )
}

function LinkUser({ user, users, onClose, onSaved }: { user: User; users: User[]; onClose: () => void; onSaved: () => void }) {
  const accounts = users.filter((u) => !u.ownerId && u.id !== user.id)
  const profiles = users.filter((u) => u.ownerId === user.id)
  const [accountId, setAccountId] = useState(accounts[0]?.id ?? 0)
  const [name, setName] = useState(user.username.replace(/[()]/g, '').slice(0, 30))
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.post(`/api/admin/users/${user.id}/link`, { accountId, name })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Link ${user.username} to an account`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <p className="muted small">
          {user.username} becomes a profile in that account's "Who's watching?". Their watch history, ratings, lists and settings
          stay. Their password, two-step sign-in and passkeys are removed and they're signed out everywhere; single sign-on and
          their settings passcode (now their profile PIN) keep working.
          {profiles.length > 0 && ` Their ${profiles.length === 1 ? 'profile moves' : `${profiles.length} profiles move`} along with them.`}
        </p>
        <label>
          Account
          <select value={accountId} onChange={(e) => setAccountId(Number(e.target.value))} required>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.username}
              </option>
            ))}
          </select>
        </label>
        <label>
          Profile name
          <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={30} />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={!accountId}>Link</button>
        </div>
      </form>
    </Modal>
  )
}

function UnlinkUser({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const [username, setUsername] = useState(user.profileName ?? '')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.post(`/api/admin/users/${user.id}/unlink`, { username, password })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Unlink ${user.profileName ?? user.username}`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <p className="muted small">
          This profile becomes its own account, with its watch history and settings. It leaves "Who's watching?" and is signed out
          on its devices.
        </p>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} required minLength={2} autoFocus />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Unlink</button>
        </div>
      </form>
    </Modal>
  )
}

function UserEmail({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const [email, setEmail] = useState(user.email ?? '')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/email`, { email })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Email for ${user.profileName || user.username}`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <p className="muted small">
          The first time someone signs in with single sign-on using this email, they land in this user{user.ownerId ? ' (this profile)' : ''}.
          The provider has to have confirmed the email. Leave it empty to remove it.
        </p>
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}

function DisplayName({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(user.profileName ?? '')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/users/${user.id}/display-name`, { name })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`Name for ${user.profileName || user.username}`} onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <p className="muted small">
          {user.ownerId
            ? "The profile's name in Who's watching and everywhere others see it."
            : `What everyone else sees, in Watch Together, activity, recommendations and Who's watching. Sign-in keeps the username ${user.username}. Leave it empty to show the username.`}
        </p>
        <label>
          Display name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder={user.username} maxLength={30} required={!!user.ownerId} autoFocus />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}
