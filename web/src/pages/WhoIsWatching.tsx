import { useEffect, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { Avatar } from '../components/Avatar'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { Member, User } from '../types'
import { donePicking, stayingAs } from '../profilePick'

export default function WhoIsWatching() {
  useDocumentTitle("Who's watching?")
  const { signedIn } = useAuth()
  const navigate = useNavigate()
  const state = useLocation().state as { from?: string; opening?: boolean } | null
  const next = state?.from && state.from !== '/who' ? state.from : '/'
  const { data: members, error: loadError } = useApi<Member[]>('/api/household')
  const [asking, setAsking] = useState<Member | null>(null)
  const [pin, setPin] = useState('')
  const [error, setError] = useState('')
  // While opening, the account's chosen member (if any) is tried before showing the picker.
  const [deciding, setDeciding] = useState(true)

  useEffect(() => {
    // The household couldn't load (an API key, the server restarting): carry on as who this is.
    if (loadError && !members) {
      donePicking()
      navigate(next, { replace: true })
    }
  }, [loadError]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!members) return
    const leave = () => {
      donePicking()
      navigate(next, { replace: true })
    }
    // One person in the household: nothing to pick.
    if (members.length < 2) return leave()
    // This browser opens as whoever chose to stay signed in here.
    if (state?.opening && members.some((m) => m.current && m.id === stayingAs())) return leave()
    const auto = state?.opening ? members.find((m) => m.auto) : undefined
    if (auto?.current) return leave()
    if (auto && !auto.hasPin) {
      api.post<{ user: User }>(`/api/household/${auto.id}/switch`, {}).then(
        (res) => {
          signedIn(res.user)
          leave()
        },
        () => setDeciding(false),
      )
      return
    }
    setDeciding(false)
  }, [members]) // eslint-disable-line react-hooks/exhaustive-deps

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
      navigate(next, { replace: true })
    } catch (e) {
      setError((e as Error).message)
      setPin('')
    }
  }
  const submitPin = (e: FormEvent) => {
    e.preventDefault()
    if (asking) choose(asking, pin)
  }

  // Nothing to show until it's clear there is a choice to make.
  if (!members || deciding) return <div className="auth-page" />

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
                <Avatar name={m.name} src={m.avatar} className="who-avatar" />
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
