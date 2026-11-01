import { useState } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { useApi } from '../../hooks'
import type { AdminChannel, NetworkChannels } from '../../types'
import { ChannelLogo, channelNo } from '../LiveGuide'

type NetworksData = { networks: NetworkChannels[]; known: string[] }

const label = (c: AdminChannel) => `${channelNo(c.number)} · ${c.name}`

/**
 * Admin → Live TV → Networks: which channels carry each TV network, so the
 * Sports page lists a CBS game on your local CBS station (WDBJ 7) rather than
 * whichever channel's guide names the teams.
 */
export function Networks() {
  const { data: channels } = useApi<AdminChannel[]>('/api/admin/live/channels')
  const { data, reload } = useApi<NetworksData>('/api/admin/live/networks')
  const [list, setList] = useState<NetworkChannels[] | null>(null)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState('')
  const networks = list ?? data?.networks ?? []
  const byId = new Map((channels ?? []).map((c) => [c.id, c]))
  const unmapped = (data?.known ?? []).filter((n) => !networks.some((m) => m.network.toLowerCase() === n.toLowerCase()))

  const save = async (next: NetworkChannels[]) => {
    setList(next)
    setError('')
    try {
      const res = await api.put<NetworksData>('/api/admin/live/networks', next.filter((n) => n.channels.length > 0))
      // Keep networks just added with no channel yet on screen.
      setList([...res.networks, ...next.filter((n) => n.channels.length === 0)])
      reload()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const addNetwork = (name: string) => {
    name = name.trim()
    if (!name || networks.some((n) => n.network.toLowerCase() === name.toLowerCase())) return
    setList([...networks, { network: name, channels: [] }])
    setNewName('')
  }
  const setChannels = (network: string, ids: number[]) => save(networks.map((n) => (n.network === network ? { ...n, channels: ids } : n)))
  const remove = (network: string) => save(networks.filter((n) => n.network !== network))

  return (
    <>
      <p className="muted">
        Pick the channels that carry each network. When a game is on a network listed here, the Sports page, the score bug and game alerts use
        these channels, unless the guide shows the channel airing something else at kick-off (another region's game, say). Other channels of the
        same network, such as another city's CBS, are then left out. Games on networks not listed here are still found by their guide titles.
      </p>
      {error && <div className="form-error">{error}</div>}
      <div className="actions" style={{ marginBottom: 16 }}>
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && addNetwork(newName)}
          list="bb-networks"
          placeholder="Network, as ESPN lists it (CBS, NBC, ESPN…)"
          style={{ width: 320 }}
        />
        <datalist id="bb-networks">
          {unmapped.map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
        <button className="btn btn-primary btn-sm" disabled={!newName.trim()} onClick={() => addNetwork(newName)}>
          Add network
        </button>
      </div>
      <div className="panel list">
        {networks.map((n) => (
          <NetworkRow
            key={n.network}
            network={n}
            channels={channels ?? []}
            byId={byId}
            onChange={(ids) => setChannels(n.network, ids)}
            onRemove={() => remove(n.network)}
          />
        ))}
        {data && networks.length === 0 && <div className="list-row muted">No networks yet. Add CBS, NBC, ABC and FOX with your local stations.</div>}
      </div>
    </>
  )
}

function NetworkRow({
  network,
  channels,
  byId,
  onChange,
  onRemove,
}: {
  network: NetworkChannels
  channels: AdminChannel[]
  byId: Map<number, AdminChannel>
  onChange: (ids: number[]) => void
  onRemove: () => void
}) {
  const [q, setQ] = useState('')
  const listId = `bb-net-${network.network.replace(/\W/g, '_')}`
  const pick = (value: string) => {
    const c = channels.find((c) => label(c) === value)
    if (!c) {
      setQ(value)
      return
    }
    setQ('')
    if (!network.channels.includes(c.id)) onChange([...network.channels, c.id])
  }
  return (
    <div className="list-row" style={{ alignItems: 'flex-start', flexWrap: 'wrap', gap: 12 }}>
      <div style={{ minWidth: 140, fontWeight: 600, paddingTop: 6 }}>{network.network}</div>
      <div className="grow" style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
        {network.channels.map((id) => {
          const c = byId.get(id)
          return (
            <span key={id} className="chip" style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
              {c && <ChannelLogo channel={{ name: c.name, logo: c.logoUrl }} size={20} />}
              {c ? label(c) : `Channel ${id}`}
              <button type="button" className="icon-btn" title="Remove" onClick={() => onChange(network.channels.filter((x) => x !== id))}>
                <Icon name="x" size={14} />
              </button>
            </span>
          )
        })}
        <input value={q} onChange={(e) => pick(e.target.value)} list={listId} placeholder="Add a channel…" style={{ width: 200 }} />
        <datalist id={listId}>
          {channels
            .filter((c) => !network.channels.includes(c.id))
            .map((c) => (
              <option key={c.id} value={label(c)} />
            ))}
        </datalist>
      </div>
      <button type="button" className="btn btn-glass btn-sm" onClick={onRemove}>
        Remove
      </button>
    </div>
  )
}
