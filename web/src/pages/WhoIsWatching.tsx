import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { Member, User } from '../types'
import { donePicking } from '../profilePick'

export default function WhoIsWatching() {
  useDocumentTitle("Who's watching?")
  const { signedIn } = useAuth()
  const navigate = useNavigate()
  const { data: members } = useApi<Member[]>('/api/household')
  const [asking, setAsking] = useState<Member | null>(null)
  const [pin, setPin] = useState('')
  const [error, setError] = useState('')

  // One person in the household: nothing to pick.
  useEffect(() => {
    if (members && members.length < 2) {
      donePicking()
      navigate('/', { replace: true })
    }
  }, [members, navigate])

  const choose = async (m: Member, code = '') => {
    if (m.hasPin && !m.current && !code) {
      setAsking(m)
      setPin('')
      setError('')
      return
    }
    try {
      if (!m.current) {
        const res = await api.post<{ user: User }>(`/api/household/${m.id}/switch`, { pin: code })
        signedIn(res.user)
      }
      donePicking()
      navigate('/', { replace: true })
    } catch (e) {
      setError((e as Error).message)
      setPin('')
    }
  }
  const submitPin = (e: FormEvent) => {
    e.preventDefault()
    if (asking) choose(asking, pin)
  }

  return (
    <div className="auth-page">
      <div className="who">
        <h1>{asking ? `Enter ${asking.name}'s PIN` : "Who's watching?"}</h1>
        {asking ? (
          <form className="form who-pin" onSubmit={submitPin}>
            <input
              className="passcode-input"
              type="password"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={8}
              value={pin}
              onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
              autoFocus
              aria-label="PIN"
            />
            {error && <div className="form-error">{error}</div>}
            <div className="actions">
              <button className="btn btn-primary" disabled={pin.length < 4}>Continue</button>
              <button type="button" className="btn btn-glass" onClick={() => setAsking(null)}>Back</button>
            </div>
          </form>
        ) : (
          <div className="who-grid">
            {members?.map((m) => (
              <button key={m.id} className={`who-card ${m.current ? 'current' : ''}`} onClick={() => choose(m)}>
                <span className="avatar who-avatar">{m.name.slice(0, 1).toUpperCase()}</span>
                <span className="who-name">{m.name}</span>
                {m.hasPin && !m.current && <Icon name="lock" size={14} className="muted" />}
              </button>
            ))}
          </div>
        )}
        {!asking && error && <div className="form-error">{error}</div>}
      </div>
    </div>
  )
}
