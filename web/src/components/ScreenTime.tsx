import { useEffect, useState } from 'react'
import { api } from '../api'

/** A profile's screen time as the server reports it. */
export interface ScreenTimeState {
  rules: { weekdayMinutes: number; weekendMinutes: number; bedtime: string; wake: string }
  usedMinutes: number
  leftMinutes: number // -1: no limit today
  blocked?: string
}

const ALLOWANCES = [0, 30, 60, 90, 120, 180, 240]
const minutesLabel = (m: number) => (m === 0 ? 'No limit' : m < 60 ? `${m} minutes` : `${m / 60} hour${m === 60 ? '' : 's'}`.replace('.5 hours', '½ hours'))

/**
 * Screen time for a profile: minutes a day on weekdays and at weekends,
 * and a bedtime. url is /api/household/{id}/screen-time (the account's own
 * profiles) or /api/admin/users/{id}/screen-time. Saves by itself.
 */
export function ScreenTimeSettings({ url }: { url: string }) {
  const [state, setState] = useState<ScreenTimeState | null>(null)
  const [weekday, setWeekday] = useState(0)
  const [weekend, setWeekend] = useState(0)
  const [bedtimeOn, setBedtimeOn] = useState(false)
  const [bedtime, setBedtime] = useState('20:00')
  const [wake, setWake] = useState('07:00')
  const [note, setNote] = useState('')

  const show = (s: ScreenTimeState) => {
    setState(s)
    setWeekday(s.rules?.weekdayMinutes ?? 0)
    setWeekend(s.rules?.weekendMinutes ?? 0)
    setBedtimeOn(!!s.rules?.bedtime)
    if (s.rules?.bedtime) setBedtime(s.rules.bedtime)
    if (s.rules?.wake) setWake(s.rules.wake)
  }
  useEffect(() => {
    api.get<ScreenTimeState>(url).then(show, () => setState(null))
  }, [url])

  const save = async (reset = false) => {
    setNote('')
    try {
      const s = await api.put<ScreenTimeState>(url, {
        weekdayMinutes: weekday,
        weekendMinutes: weekend,
        bedtime: bedtimeOn ? bedtime : '',
        wake: bedtimeOn ? wake : '',
        reset,
      })
      show(s)
      setNote(reset ? 'Today starts over.' : 'Saved.')
    } catch (e) {
      setNote((e as Error).message)
    }
  }

  if (!state) return null
  const select = (value: number, set: (n: number) => void) => (
    <select value={value} onChange={(e) => set(Number(e.target.value))}>
      {[...new Set([...ALLOWANCES, value])].sort((a, b) => a - b).map((m) => (
        <option key={m} value={m}>
          {minutesLabel(m)}
        </option>
      ))}
    </select>
  )
  return (
    <fieldset className="screen-time">
      <legend>Screen time</legend>
      <div className="screen-time-row">
        <label>
          Monday to Friday
          {select(weekday, setWeekday)}
        </label>
        <label>
          Saturday and Sunday
          {select(weekend, setWeekend)}
        </label>
      </div>
      <label className="check">
        <input type="checkbox" checked={bedtimeOn} onChange={(e) => setBedtimeOn(e.target.checked)} />
        Bedtime
      </label>
      {bedtimeOn && (
        <div className="screen-time-row">
          <label>
            No watching from
            <input type="time" value={bedtime} onChange={(e) => setBedtime(e.target.value)} required />
          </label>
          <label>
            until
            <input type="time" value={wake} onChange={(e) => setWake(e.target.value)} required />
          </label>
        </div>
      )}
      <p className="muted small">
        {state.leftMinutes >= 0
          ? `Today: ${state.usedMinutes} minutes watched, ${state.leftMinutes} left.`
          : state.usedMinutes
            ? `Today: ${state.usedMinutes} minutes watched.`
            : 'Movies, shows and Live TV count while they play. When the time is up, or at bedtime, playback stops with a message.'}
        {state.blocked ? ` ${state.blocked}` : ''}
      </p>
      <div className="actions">
        <button type="button" className="btn btn-glass" onClick={() => save()}>
          Save screen time
        </button>
        {(state.blocked || state.usedMinutes > 0) && (
          <button type="button" className="btn btn-glass" onClick={() => save(true)}>
            Start today over
          </button>
        )}
        {note && <span className="muted small">{note}</span>}
      </div>
    </fieldset>
  )
}
