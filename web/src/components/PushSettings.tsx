import { useEffect, useState } from 'react'
import { api } from '../api'
import { useAuth } from '../auth'
import { useApi } from '../hooks'
import { currentSubscription, pushUnavailable, turnOffPush, turnOnPush, type PushDevice, type PushStatus } from '../push'

const kindNames: Record<string, string> = {
  request: 'Requests: when something you asked for is ready (and new requests, for admins)',
  sports: 'Games of your teams starting, and final scores',
  together: 'Invitations to watch together',
  live: 'Live TV shows you follow starting',
  episodes: 'New episodes of your shows',
  recording: 'Your recordings finishing or failing',
  server: 'Server alerts (admins)',
}

function when(ts?: number) {
  return ts ? new Date(ts * 1000).toLocaleString() : 'never'
}

/** Push notifications: turn them on for this browser and pick what each device gets. */
export function PushSettings() {
  const { data, reload, setData } = useApi<PushStatus>('/api/push')
  const [endpoint, setEndpoint] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const unavailable = pushUnavailable()
  const { user } = useAuth()
  const kinds = (data?.kinds ?? []).filter((k) => k !== 'server' || user?.isAdmin)

  useEffect(() => {
    currentSubscription().then((s) => setEndpoint(s?.endpoint ?? null), () => setEndpoint(null))
  }, [data])

  const here = data?.devices.find((d) => d.endpoint === endpoint)

  const run = async (f: () => Promise<unknown>, ok?: string) => {
    setBusy(true)
    setMsg(null)
    try {
      await f()
      if (ok) setMsg({ ok: true, text: ok })
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    } finally {
      setBusy(false)
      reload()
    }
  }

  const test = () =>
    run(async () => {
      const res = await api.post<{ results: Record<string, string> }>('/api/push/test')
      const failed = Object.entries(res.results).filter(([, r]) => r !== 'sent')
      if (!Object.keys(res.results).length) throw new Error('No device has push turned on.')
      if (failed.length) {
        const name = (id: string) => data?.devices.find((d) => String(d.id) === id)?.device ?? 'A device'
        throw new Error(failed.map(([id, r]) => `${name(id)}: ${r}`).join('\n'))
      }
    }, 'Sent. It should pop up on each device in a few seconds.')

  const setKind = async (d: PushDevice, kind: string, on: boolean) => {
    const kindsOff = on ? d.kindsOff.filter((k) => k !== kind) : [...d.kindsOff, kind]
    if (data) setData({ ...data, devices: data.devices.map((x) => (x.id === d.id ? { ...x, kindsOff } : x)) })
    await api.put(`/api/push/devices/${d.id}`, { kindsOff }).catch((e) => setMsg({ ok: false, text: e.message }))
  }

  const remove = (d: PushDevice) =>
    run(async () => {
      if (d.endpoint === endpoint) await turnOffPush(data?.devices)
      else await api.del(`/api/push/devices/${d.id}`)
    })

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Push notifications</h3>
      <p className="muted small">
        Get your notifications on your phone or computer even when BingeBandit is closed: requests that are ready, your teams' games
        starting, watch together invitations and anything else under the bell. Choose which ones above; pick per device below.
      </p>
      {unavailable && !here ? (
        <p className="notice">{unavailable}</p>
      ) : (
        <div className="actions">
          {here ? (
            <button type="button" className="btn btn-glass" disabled={busy} onClick={() => remove(here)}>
              Turn off for this browser
            </button>
          ) : (
            <button
              type="button"
              className="btn btn-primary"
              disabled={busy || !data}
              onClick={() => run(() => turnOnPush(data!.publicKey), 'Push notifications are on for this browser.')}
            >
              Turn on for this browser
            </button>
          )}
        </div>
      )}
      {!!data?.devices.length && (
        <>
          <div className="list">
            {data.devices.map((d) => (
              <div key={d.id} className="list-row" style={{ alignItems: 'flex-start' }}>
                <div className="grow">
                  <div>
                    {d.device} {d.endpoint === endpoint && <span className="pill ok">This browser</span>}
                  </div>
                  <div className="muted small">
                    Turned on {when(d.createdAt)} · last sent {when(d.lastUsed)}
                  </div>
                  <details style={{ marginTop: 6 }}>
                    <summary className="small">What this device gets</summary>
                    {kinds.map((k) => (
                      <label key={k} className="check small">
                        <input type="checkbox" checked={!d.kindsOff.includes(k)} onChange={(e) => setKind(d, k, e.target.checked)} />
                        {kindNames[k] ?? k}
                      </label>
                    ))}
                  </details>
                </div>
                <button type="button" className="btn btn-glass btn-sm" disabled={busy} onClick={() => remove(d)}>
                  Remove
                </button>
              </div>
            ))}
          </div>
          <div className="actions">
            <button type="button" className="btn btn-glass" disabled={busy} onClick={test}>
              Send a test
            </button>
          </div>
        </>
      )}
      {msg && (
        <div className={msg.ok ? 'form-ok' : 'form-error'} style={{ whiteSpace: 'pre-line' }}>
          {msg.text}
        </div>
      )}
    </div>
  )
}
