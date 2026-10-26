import { useState } from 'react'
import { api } from '../../api'
import { useApi } from '../../hooks'
import type { AdminChannel, KidApprovals } from '../../types'
import { ChannelLogo, channelNo } from '../LiveGuide'

// Kid mode: which Live TV channels profiles marked as kids may see. A whole
// group can be approved (including channels added to it later), or channels
// one by one.
export function KidChannels() {
  const { data: channels } = useApi<AdminChannel[]>('/api/admin/live/channels')
  const { data, reload } = useApi<KidApprovals>('/api/admin/live/kids')
  const [q, setQ] = useState('')
  const [error, setError] = useState('')

  const groups = new Set(data?.groups.filter((g) => g.approved).map((g) => g.name))
  const single = new Set(data?.channelIds)
  const allowed = channels?.filter((c) => single.has(c.id) || (c.group && groups.has(c.group))).length ?? 0
  const shown = channels?.filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q) || c.group?.toLowerCase().includes(q.toLowerCase()))

  const setGroup = async (name: string, on: boolean) => {
    await api.put('/api/admin/live/kids/group', { name, on }).then(reload, (e) => setError(e.message))
  }
  const setChannels = async (ids: number[], on: boolean) => {
    await api.put('/api/admin/live/kids/channels', { channelIds: ids, on }).then(reload, (e) => setError(e.message))
  }

  return (
    <>
      <p className="muted">
        Profiles marked as a kid (under Users, or in Account → Profiles) only see the channels approved here, in the guide, the channel list, their
        M3U playlist and their Plex tuner. {allowed} of {channels?.length ?? 0} channels are approved.
      </p>
      {error && <div className="form-error">{error}</div>}
      <h2>Groups</h2>
      <div className="panel list" style={{ marginBottom: 24 }}>
        {data?.groups.map((g) => (
          <div key={g.name} className="list-row">
            <div className="grow">
              <div>{g.name}</div>
              <div className="muted small">
                {g.channels} channel{g.channels === 1 ? '' : 's'}
                {g.approved ? ', and any added to this group later' : ''}
              </div>
            </div>
            <label className="switch" title={g.approved ? 'Approved for kids' : 'Not for kids'}>
              <input type="checkbox" checked={g.approved} onChange={() => setGroup(g.name, !g.approved)} />
              <span />
            </label>
          </div>
        ))}
        {data?.groups.length === 0 && <div className="list-row muted">No channel groups. Approve channels one by one below.</div>}
      </div>
      <h2>Channels</h2>
      <div className="actions" style={{ marginBottom: 16 }}>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a channel or group" style={{ width: 240 }} />
        {shown && shown.length > 0 && (
          <>
            <button className="btn btn-glass btn-sm" onClick={() => setChannels(shown.map((c) => c.id), true)}>
              Approve {q ? 'these' : 'all'}
            </button>
            <button className="btn btn-glass btn-sm" onClick={() => setChannels(shown.map((c) => c.id), false)}>
              Clear {q ? 'these' : 'all'}
            </button>
          </>
        )}
      </div>
      <div className="panel list">
        {shown?.map((c) => {
          const viaGroup = !!c.group && groups.has(c.group)
          const on = viaGroup || single.has(c.id)
          return (
            <div key={c.id} className={`list-row channel-row ${c.enabled ? '' : 'disabled'}`}>
              <span className="ch-num big">{channelNo(c.number)}</span>
              <ChannelLogo channel={{ name: c.name, logo: c.logoUrl }} size={30} />
              <div className="grow">
                <div>{c.name}</div>
                <div className="muted small">{[c.group, viaGroup ? 'approved with its group' : ''].filter(Boolean).join(' · ')}</div>
              </div>
              <label className="switch" title={viaGroup ? 'Approved with its group' : on ? 'Approved for kids' : 'Not for kids'}>
                <input type="checkbox" checked={on} disabled={viaGroup} onChange={() => setChannels([c.id], !on)} />
                <span />
              </label>
            </div>
          )
        })}
        {channels?.length === 0 && <div className="list-row muted">No channels yet.</div>}
      </div>
    </>
  )
}
