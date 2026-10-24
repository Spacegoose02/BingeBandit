import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi, useDocumentTitle } from '../hooks'
import { commonLanguages, languageName } from '../format'
import type { Prefs } from '../types'

function PlaybackPrefs() {
  const { data } = useApi<Prefs>('/api/me/prefs')
  const [p, setP] = useState<Prefs | null>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  useEffect(() => setP(data), [data])
  if (!p) return null

  const set = <K extends keyof Prefs>(k: K, v: Prefs[K]) => setP({ ...p, [k]: v })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      setP(await api.put<Prefs>('/api/me/prefs', p))
      setMsg({ ok: true, text: 'Playback settings saved.' })
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }
  const langSelect = (value: string, onChange: (v: string) => void, emptyLabel: string) => (
    <select value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{emptyLabel}</option>
      {commonLanguages.map((l) => (
        <option key={l} value={l}>{languageName(l)}</option>
      ))}
    </select>
  )

  return (
    <form className="panel form" onSubmit={save} style={{ marginBottom: 20 }}>
      <h3>Playback</h3>
      <div className="field-grid">
        <label>
          Preferred audio language
          {langSelect(p.audioLanguage, (v) => set('audioLanguage', v), "File's default track")}
        </label>
        <label>
          Subtitle language
          {langSelect(p.subtitleLanguage, (v) => set('subtitleLanguage', v), 'None')}
        </label>
        <label className="span-2">
          Show subtitles
          <select value={p.subtitleMode} onChange={(e) => set('subtitleMode', e.target.value as Prefs['subtitleMode'])}>
            <option value="auto">When the audio is in another language (plus forced subtitles)</option>
            <option value="always">Always</option>
            <option value="forced">Only forced subtitles (foreign dialogue, signs)</option>
            <option value="off">Never</option>
          </select>
        </label>
        <label>
          Subtitle size
          <select value={p.subtitleSize} onChange={(e) => set('subtitleSize', e.target.value as Prefs['subtitleSize'])}>
            <option value="small">Small</option>
            <option value="medium">Medium</option>
            <option value="large">Large</option>
          </select>
        </label>
        <label className="check" style={{ alignSelf: 'end' }}>
          <input type="checkbox" checked={p.subtitleBackground} onChange={(e) => set('subtitleBackground', e.target.checked)} />
          Dark box behind subtitles
        </label>
      </div>
      <div className="field-grid">
        <label>
          Intros
          <select value={p.skipIntro} onChange={(e) => set('skipIntro', e.target.value as Prefs['skipIntro'])}>
            <option value="button">Show a Skip Intro button</option>
            <option value="auto">Skip automatically</option>
            <option value="off">Don't offer to skip</option>
          </select>
        </label>
        <label>
          End credits
          <select value={p.skipCredits} onChange={(e) => set('skipCredits', e.target.value as Prefs['skipCredits'])}>
            <option value="button">Show a Next button</option>
            <option value="auto">Go to the next episode automatically</option>
            <option value="off">Don't offer to skip</option>
          </select>
        </label>
      </div>
      <label className="check">
        <input type="checkbox" checked={p.autoQuality} onChange={(e) => set('autoQuality', e.target.checked)} />
        Lower the quality automatically when playback keeps buffering
      </label>
      <label className="check">
        <input type="checkbox" checked={p.autoPlayNext} onChange={(e) => set('autoPlayNext', e.target.checked)} />
        Play the next episode automatically
      </label>
      {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
      <div className="actions">
        <button className="btn btn-primary">Save playback settings</button>
      </div>
    </form>
  )
}

export default function Account() {
  useDocumentTitle('Account')
  const { user } = useAuth()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put('/api/me/password', { current, new: next })
      setMsg({ ok: true, text: 'Password changed.' })
      setCurrent('')
      setNext('')
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }

  return (
    <div className="page narrow">
      <h1>Account</h1>
      <p className="muted">
        Signed in as <strong>{user?.username}</strong>
        {user?.isAdmin && ' (admin)'}
      </p>
      <PlaybackPrefs />
      <form className="panel form" onSubmit={submit}>
        <h3>Change password</h3>
        <label>
          Current password
          <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
        </label>
        <label>
          New password
          <input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} autoComplete="new-password" />
        </label>
        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <button className="btn btn-primary">Update password</button>
      </form>
    </div>
  )
}
