import { useEffect, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { IPTVLinks } from '../../components/IPTVLinks'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import { formatBytes, formatClock, timeAgo } from '../../format'
import type { AdminChannel, LiveSource, LiveStream, RelayStat } from '../../types'
import { ChannelLogo, channelNo } from '../LiveGuide'
import { CustomChannelEditor } from './CustomChannel'
import { DispatcharrImport } from './DispatcharrImport'
import { Logos } from './Logos'
import { LogoField } from '../../components/ChannelLogos'

type Tab = 'channels' | 'logos' | 'sources' | 'activity'

export default function AdminLiveTV() {
  useDocumentTitle('Live TV')
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'activity'
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Live TV</h1>
          <div className="muted">Playlists and guides from your IPTV providers, the channels built from them, and who's watching.</div>
        </div>
        <div className="segmented">
          {(['activity', 'channels', 'logos', 'sources'] as Tab[]).map((t) => (
            <button key={t} className={tab === t ? 'active' : ''} onClick={() => setParams({ tab: t }, { replace: true })}>
              {({ activity: 'Activity', channels: 'Channels', logos: 'Logos', sources: 'Sources' } as Record<Tab, string>)[t]}
            </button>
          ))}
        </div>
      </header>
      {tab === 'sources' && <Sources />}
      {tab === 'channels' && <Channels />}
      {tab === 'logos' && <Logos />}
      {tab === 'activity' && <Activity />}
    </div>
  )
}

// ---- Sources ---------------------------------------------------------------

function Sources() {
  const { data, reload } = useApi<LiveSource[]>('/api/admin/live/sources', 5000)
  const [editing, setEditing] = useState<Partial<LiveSource> | null>(null)
  const [filtering, setFiltering] = useState<LiveSource | null>(null)
  const [importing, setImporting] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)
  const refresh = async (s: LiveSource) => {
    setBusy(s.id)
    await api.post(`/api/admin/live/sources/${s.id}/refresh`).catch((e) => alert(e.message))
    setBusy(null)
    reload()
  }
  const remove = async (s: LiveSource) => {
    if (!confirm(`Remove "${s.name}"? ${s.kind === 'm3u' ? 'Channels lose the streams it provided.' : 'Its guide data is deleted.'}`)) return
    await api.del(`/api/admin/live/sources/${s.id}`)
    reload()
  }

  const section = (kind: 'm3u' | 'epg', title: string, blurb: string) => (
    <section>
      <div className="section-title" style={{ justifyContent: 'space-between' }}>
        <span>{title}</span>
        <button className="btn btn-glass btn-sm" onClick={() => setEditing({ kind, refreshHours: kind === 'm3u' ? 24 : 12, enabled: true, maxStreams: 0 })}>
          <Icon name="plus" size={16} /> Add
        </button>
      </div>
      <p className="muted small" style={{ marginTop: -6 }}>{blurb}</p>
      <div className="panel list">
        {data?.filter((s) => s.kind === kind).map((s) => (
          <div key={s.id} className="list-row">
            <Icon name={kind === 'm3u' ? 'tv' : 'list'} />
            <div className="grow">
              <div>
                {s.name} {!s.enabled && <span className="pill">Disabled</span>}
              </div>
              <div className="muted small">
                {s.entryCount.toLocaleString()} {kind === 'm3u' ? 'streams' : 'programs'} · refreshed {timeAgo(s.lastRefresh)} · every {s.refreshHours}h
                {kind === 'm3u' && s.maxStreams > 0 && ` · max ${s.maxStreams} connections`}
              </div>
              {s.lastError && <div className="form-error small">Last refresh failed: {s.lastError}</div>}
            </div>
            <button className="btn btn-glass btn-sm" disabled={busy === s.id} onClick={() => refresh(s)}>
              <Icon name="refresh" size={16} className={busy === s.id ? 'spin' : ''} /> Refresh
            </button>
            {kind === 'm3u' && <button className="btn btn-glass btn-sm" onClick={() => setFiltering(s)}>Groups</button>}
            <button className="btn btn-glass btn-sm" onClick={() => setEditing(s)}><Icon name="edit" size={16} /></button>
            <button className="btn btn-glass btn-sm danger" onClick={() => remove(s)} aria-label="Remove source"><Icon name="trash" size={16} /></button>
          </div>
        ))}
        {data?.filter((s) => s.kind === kind).length === 0 && <div className="list-row muted">None yet.</div>}
      </div>
    </section>
  )

  return (
    <>
      <div className="panel list-row" style={{ marginBottom: 16 }}>
        <Icon name="download" />
        <div className="grow">
          <div>Moving from Dispatcharr?</div>
          <div className="muted small">Import its provider accounts, guides and channels in one go.</div>
        </div>
        <button className="btn btn-glass btn-sm" onClick={() => setImporting(true)}>Import from Dispatcharr</button>
      </div>
      {section('m3u', 'M3U playlists', 'Your IPTV provider accounts. Set the connection limit to what your plan allows; BingeBandit shares one connection per channel between viewers and skips a provider when it is full.')}
      {section('epg', 'Guides (XMLTV)', 'Program guide data. Channels match guide entries by their tvg-id; you can change the match per channel.')}
      <h2 className="section-title">Use these channels in other apps</h2>
      <IPTVLinks />
      {editing && <SourceEditor source={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />}
      {importing && <DispatcharrImport onClose={() => setImporting(false)} onChanged={reload} />}
      {filtering && <GroupFilter source={filtering} onClose={() => setFiltering(null)} onSaved={() => { setFiltering(null); reload() }} />}
    </>
  )
}

type SourceGroups = { newGroupsEnabled: boolean; groups: { name: string; enabled: boolean; streams: number }[] }

// GroupFilter picks which of a playlist's groups are imported. Streams in a
// disabled group are dropped on the next refresh, unless a channel uses them.
function GroupFilter({ source, onClose, onSaved }: { source: LiveSource; onClose: () => void; onSaved: () => void }) {
  const { data } = useApi<SourceGroups>(`/api/admin/live/sources/${source.id}/groups`)
  const [enabled, setEnabled] = useState<Map<string, boolean> | null>(null)
  const [newGroups, setNewGroups] = useState(true)
  const [q, setQ] = useState('')
  const [show, setShow] = useState<'all' | 'on' | 'off'>('all')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    if (!data || enabled) return
    setEnabled(new Map(data.groups.map((g) => [g.name, g.enabled])))
    setNewGroups(data.newGroupsEnabled)
  }, [data, enabled])
  const on = (name: string) => enabled?.get(name) ?? true
  const needle = q.trim().toLowerCase()
  const visible = (data?.groups ?? []).filter(
    (g) => (!needle || (g.name || '(no group)').toLowerCase().includes(needle)) && (show === 'all' || (show === 'on') === on(g.name)),
  )
  const setAll = (names: string[], value: boolean) =>
    setEnabled((m) => {
      const n = new Map(m)
      names.forEach((name) => n.set(name, value))
      return n
    })
  const onCount = data?.groups.filter((g) => on(g.name)).length ?? 0
  const onStreams = data?.groups.reduce((sum, g) => sum + (on(g.name) ? g.streams : 0), 0) ?? 0
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      await api.put(`/api/admin/live/sources/${source.id}/groups`, {
        newGroupsEnabled: newGroups,
        groups: [...(enabled ?? new Map())].map(([name, value]) => ({ name, enabled: value })),
      })
      await api.post(`/api/admin/live/sources/${source.id}/refresh`)
      onSaved()
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <Modal title={`Groups in ${source.name}`} onClose={onClose} wide>
      <div className="form">
        <p className="muted small" style={{ margin: 0 }}>
          Only streams in enabled groups are imported, so Add channels has less to search. Turning a group off never
          breaks a channel: streams your channels already use are kept.
        </p>
        <label className="check">
          <input type="checkbox" checked={newGroups} onChange={(e) => setNewGroups(e.target.checked)} /> Enable groups that appear in future refreshes
        </label>
        <div className="actions">
          <input className="grow" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter groups…" style={{ minWidth: 180 }} />
          <div className="segmented">
            {(['all', 'on', 'off'] as const).map((v) => (
              <button key={v} className={show === v ? 'active' : ''} onClick={() => setShow(v)}>
                {v === 'all' ? 'All' : v === 'on' ? 'Enabled' : 'Disabled'}
              </button>
            ))}
          </div>
          <button className="btn btn-glass btn-sm" onClick={() => setAll(visible.map((g) => g.name), true)}>Enable shown</button>
          <button className="btn btn-glass btn-sm" onClick={() => setAll(visible.map((g) => g.name), false)}>Disable shown</button>
        </div>
        <div className="group-grid">
          {visible.map((g) => (
            <button key={g.name} type="button" className={`group-toggle ${on(g.name) ? 'on' : ''}`} onClick={() => setAll([g.name], !on(g.name))}>
              <Icon name={on(g.name) ? 'check' : 'x'} size={14} />
              <span className="grow" title={g.name}>{g.name || '(no group)'}</span>
              <span className="small">{g.streams.toLocaleString()}</span>
            </button>
          ))}
          {data && visible.length === 0 && <div className="muted small">{data.groups.length ? 'No groups match.' : 'No groups yet. Refresh the playlist first.'}</div>}
        </div>
        <div className="actions end">
          <span className="muted small grow">
            {onCount} of {data?.groups.length ?? 0} groups on · {onStreams.toLocaleString()} streams
          </span>
          {error && <span className="form-error small">{error}</span>}
          <button className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={!enabled || busy} onClick={save}>{busy ? 'Refreshing…' : 'Save and refresh'}</button>
        </div>
      </div>
    </Modal>
  )
}

function SourceEditor({ source, onClose, onSaved }: { source: Partial<LiveSource>; onClose: () => void; onSaved: () => void }) {
  const [s, setS] = useState(source)
  const [error, setError] = useState('')
  const isM3U = s.kind === 'm3u'
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      if (s.id) await api.put(`/api/admin/live/sources/${s.id}`, s)
      else await api.post('/api/admin/live/sources', s)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`${s.id ? 'Edit' : 'Add'} ${isM3U ? 'M3U playlist' : 'guide'}`} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={s.name ?? ''} onChange={(e) => setS({ ...s, name: e.target.value })} required placeholder={isM3U ? 'My IPTV provider' : 'Provider guide'} />
        </label>
        <label>
          URL or file path
          <input className="mono" value={s.url ?? ''} onChange={(e) => setS({ ...s, url: e.target.value })} required placeholder={isM3U ? 'http://provider/get.php?username=…&type=m3u_plus' : 'http://provider/xmltv.php or .xml.gz'} />
        </label>
        <div className="field-grid">
          {isM3U && (
            <label>
              Connection limit (0 = unlimited)
              <input type="number" min={0} value={s.maxStreams ?? 0} onChange={(e) => setS({ ...s, maxStreams: Number(e.target.value) })} />
            </label>
          )}
          <label>
            Refresh every (hours)
            <input type="number" min={1} value={s.refreshHours ?? 24} onChange={(e) => setS({ ...s, refreshHours: Number(e.target.value) })} />
          </label>
        </div>
        {isM3U && (
          <label>
            User agent (optional)
            <input value={s.userAgent ?? ''} onChange={(e) => setS({ ...s, userAgent: e.target.value })} placeholder="Default: VLC" />
          </label>
        )}
        <label className="check">
          <input type="checkbox" checked={s.enabled ?? true} onChange={(e) => setS({ ...s, enabled: e.target.checked })} /> Enabled
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">{s.id ? 'Save' : 'Add and import'}</button>
        </div>
      </form>
    </Modal>
  )
}

// ---- Channels --------------------------------------------------------------

// A provider channel that lost a stream, or has none left to play.
const needsAttention = (c: AdminChannel) => !c.custom && (!!c.lost?.length || !c.streams?.length)

function Channels() {
  const { data, reload } = useApi<AdminChannel[]>('/api/admin/live/channels')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<AdminChannel | null>(null)
  const [custom, setCustom] = useState<AdminChannel | 'new' | null>(null)
  const [q, setQ] = useState('')
  const [onlyAttention, setOnlyAttention] = useState(false)
  const attention = data?.filter(needsAttention).length ?? 0
  const nextNumber = Math.floor(Math.max(0, ...(data ?? []).map((c) => c.number))) + 1

  const toggle = async (c: AdminChannel) => {
    await api.put(`/api/admin/live/channels/${c.id}`, { ...c, enabled: !c.enabled, streamIds: null })
    reload()
  }
  const remove = async (c: AdminChannel) => {
    if (!confirm(`Delete channel ${channelNo(c.number)} ${c.name}?`)) return
    await api.del(`/api/admin/live/channels/${c.id}`)
    reload()
  }
  const shown = data?.filter(
    (c) => (!q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q)) && (!onlyAttention || attention === 0 || needsAttention(c)),
  )

  return (
    <>
      <div className="actions" style={{ marginBottom: 16 }}>
        <button className="btn btn-primary" onClick={() => setAdding(true)}>
          <Icon name="plus" size={18} /> Add channels
        </button>
        <button className="btn btn-glass" onClick={() => setCustom('new')}>
          <Icon name="film" size={18} /> New custom channel
        </button>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a channel" style={{ width: 220 }} />
        <span className="muted small">{data?.length ?? 0} channels</span>
        {attention > 0 && (
          <button className={`btn btn-sm ${onlyAttention ? 'btn-primary' : 'btn-glass'}`} onClick={() => setOnlyAttention(!onlyAttention)}>
            <span className="pill warn">{attention}</span> need{attention === 1 ? 's' : ''} a stream replaced
          </button>
        )}
      </div>
      <div className="panel list">
        {shown?.map((c) => (
          <div key={c.id} className={`list-row channel-row ${c.enabled ? '' : 'disabled'}`}>
            <span className="ch-num big">{channelNo(c.number)}</span>
            <ChannelLogo channel={{ name: c.name, logo: c.logoUrl }} size={30} />
            <div className="grow">
              <div>{c.name}</div>
              <div className="muted small">
                {c.custom
                  ? [c.group, 'custom channel from your library'].filter(Boolean).join(' · ')
                  : [c.group, c.epgId ? `guide: ${c.epgId}` : 'no guide match', `${c.streams?.length ?? 0} stream${c.streams?.length === 1 ? '' : 's'}`]
                      .filter(Boolean)
                      .join(' · ')}
                {!c.custom && !c.streams?.length && <span className="pill danger">No streams</span>}
                {!!c.lost?.length && (
                  <span className="pill warn" title={`No longer offered by the provider: ${c.lost.join(', ')}`}>
                    Lost {c.lost.length === 1 ? `"${c.lost[0]}"` : `${c.lost.length} streams`}
                  </span>
                )}
              </div>
            </div>
            <label className="switch" title={c.enabled ? 'Enabled' : 'Hidden from viewers'}>
              <input type="checkbox" checked={c.enabled} onChange={() => toggle(c)} />
              <span />
            </label>
            <button className="btn btn-glass btn-sm" onClick={() => (c.custom ? setCustom(c) : setEditing(c))}><Icon name="edit" size={16} /> Edit</button>
            <button className="btn btn-glass btn-sm danger" onClick={() => remove(c)} aria-label="Delete channel"><Icon name="trash" size={16} /></button>
          </div>
        ))}
        {data?.length === 0 && <div className="list-row muted">No channels yet. Add an M3U playlist under Sources, then use Add channels.</div>}
      </div>
      {adding && <AddChannels onClose={() => setAdding(false)} onDone={() => { setAdding(false); reload() }} nextNumber={nextNumber} />}
      {custom && (
        <CustomChannelEditor
          channel={custom === 'new' ? undefined : custom}
          nextNumber={nextNumber}
          onClose={() => setCustom(null)}
          onSaved={reload}
        />
      )}
      {editing && <ChannelEditor channel={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />}
    </>
  )
}

function AddChannels({ onClose, onDone, nextNumber }: { onClose: () => void; onDone: () => void; nextNumber: number }) {
  const { data: sources } = useApi<LiveSource[]>('/api/admin/live/sources')
  const providers = sources?.filter((s) => s.kind === 'm3u') ?? []
  const [source, setSource] = useState(0)
  const { data: groups } = useApi<{ name: string; count: number }[]>(`/api/admin/live/groups?source=${source}`)
  const [group, setGroup] = useState('')
  const [q, setQ] = useState('')
  const { data } = useApi<{ streams: LiveStream[]; total: number }>(`/api/admin/live/streams?limit=200&source=${source}&group=${encodeURIComponent(group)}&q=${encodeURIComponent(q)}`)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [start, setStart] = useState(nextNumber)
  const [error, setError] = useState('')
  const toggle = (id: number) =>
    setPicked((s) => {
      const n = new Set(s)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  const unused = data?.streams.filter((s) => !s.channelIds?.length) ?? []
  const create = async () => {
    try {
      // Keep the playlist's order so numbering follows it.
      const ids = (data?.streams ?? []).filter((s) => picked.has(s.id)).map((s) => s.id)
      await api.post('/api/admin/live/channels', { streamIds: ids, startNumber: start })
      onDone()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Modal title="Add channels" onClose={onClose} wide>
      <div className="form">
        <div className="field-grid">
          {providers.length > 1 && (
            <label className="span-2">
              Provider
              <select value={source} onChange={(e) => { setSource(Number(e.target.value)); setGroup('') }}>
                <option value={0}>All providers</option>
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>{p.name} ({p.entryCount.toLocaleString()})</option>
                ))}
              </select>
            </label>
          )}
          <label>
            Group
            <select value={group} onChange={(e) => setGroup(e.target.value)}>
              <option value="">All groups</option>
              {groups?.map((g) => (
                <option key={g.name} value={g.name}>{g.name || '(no group)'} ({g.count})</option>
              ))}
            </select>
          </label>
          <label>
            Search
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Channel name" />
          </label>
        </div>
        <div className="actions">
          <button className="btn btn-glass btn-sm" onClick={() => setPicked(new Set([...picked, ...unused.map((s) => s.id)]))}>
            Select all {unused.length} not yet added
          </button>
          <button className="btn btn-glass btn-sm" onClick={() => setPicked(new Set())}>Clear</button>
          <span className="muted small">{data?.total ?? 0} streams{(data?.total ?? 0) > 200 ? ' (showing 200; narrow with a group or search)' : ''}</span>
        </div>
        <div className="stream-pick">
          {data?.streams.map((s) => (
            <label key={s.id} className={`stream-row ${s.channelIds?.length ? 'used' : ''}`}>
              <input type="checkbox" checked={picked.has(s.id)} onChange={() => toggle(s.id)} />
              <ChannelLogo channel={s} size={22} />
              <span className="grow">{s.name}</span>
              <span className="muted small">{providers.length > 1 && !source ? [s.group, s.sourceName].filter(Boolean).join(' · ') : s.group}</span>
              {s.channelIds?.length ? <span className="pill">in use</span> : null}
            </label>
          ))}
        </div>
        <div className="actions end">
          <label className="select-label">
            Number from
            <input type="number" min={1} value={start} onChange={(e) => setStart(Number(e.target.value))} style={{ width: 90 }} />
          </label>
          <span className="muted small grow">Streams with a tvg-chno keep that number when it's free.</span>
          {error && <span className="form-error small">{error}</span>}
          <button className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={picked.size === 0} onClick={create}>Create {picked.size} channels</button>
        </div>
      </div>
    </Modal>
  )
}

function ChannelEditor({ channel, onClose, onSaved }: { channel: AdminChannel; onClose: () => void; onSaved: () => void }) {
  const [c, setC] = useState(channel)
  const [streams, setStreams] = useState(channel.streams ?? [])
  const [epgQ, setEpgQ] = useState('')
  const [streamQ, setStreamQ] = useState('')
  const { data: epg } = useApi<{ id: string; displayName: string }[]>(epgQ ? `/api/admin/live/epg-channels?q=${encodeURIComponent(epgQ)}` : null)
  const { data: found } = useApi<{ streams: LiveStream[] }>(streamQ ? `/api/admin/live/streams?limit=20&q=${encodeURIComponent(streamQ)}` : null)
  const [error, setError] = useState('')

  const move = (i: number, dir: -1 | 1) => {
    const n = [...streams]
    const j = i + dir
    if (j < 0 || j >= n.length) return
    ;[n[i], n[j]] = [n[j], n[i]]
    setStreams(n)
  }
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/live/channels/${c.id}`, { ...c, streamIds: streams.map((s) => s.streamId) })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <Modal title={`Edit ${channel.name}`} onClose={onClose} wide>
      <form className="form" onSubmit={save}>
        <div className="field-grid">
          <label>
            Number
            <input type="number" step="0.1" min={0.1} value={c.number} onChange={(e) => setC({ ...c, number: Number(e.target.value) })} required />
          </label>
          <label>
            Name
            <input value={c.name} onChange={(e) => setC({ ...c, name: e.target.value })} required />
          </label>
          <label>
            Category
            <input value={c.group ?? ''} onChange={(e) => setC({ ...c, group: e.target.value })} placeholder="News, Sports…" />
          </label>
        </div>
        <div className="label">Logo</div>
        <LogoField value={c.logo ?? ''} onChange={(logo) => setC({ ...c, logo })} />

        <div className="label">Guide match</div>
        <div className="actions">
          <span className="chip strong">{c.epgId || 'none'}</span>
          <input value={epgQ} onChange={(e) => setEpgQ(e.target.value)} placeholder="Search guide channels" style={{ width: 240 }} />
          {c.epgId && <button type="button" className="btn btn-glass btn-sm" onClick={() => setC({ ...c, epgId: '' })}>Clear</button>}
        </div>
        {epgQ && (
          <div className="pick-list">
            {epg?.map((g) => (
              <button type="button" key={g.id} onClick={() => { setC({ ...c, epgId: g.id }); setEpgQ('') }}>
                {g.displayName} <span className="muted small mono">{g.id}</span>
              </button>
            ))}
            {epg?.length === 0 && <div className="muted small menu-pad">No matches</div>}
          </div>
        )}

        <div className="label">Streams, in fallback order</div>
        {!!channel.lost?.length && (
          <p className="small" style={{ margin: 0, color: 'var(--warn)' }}>
            The provider no longer offers {channel.lost.map((n) => `"${n}"`).join(', ')}, so {channel.lost.length === 1 ? 'it was' : 'they were'} removed from this channel.
            Add a replacement below; saving clears this notice.
          </p>
        )}
        <p className="muted small" style={{ margin: 0 }}>The first stream plays; if it fails or stalls, the next one takes over without viewers reconnecting.</p>
        <div className="panel list compact">
          {streams.map((s, i) => (
            <div key={s.streamId} className="list-row">
              <span className="muted">{i + 1}</span>
              <div className="grow">
                <div>{s.name}</div>
                <div className="muted small">{s.sourceName}</div>
              </div>
              <button type="button" className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label="Move up"><Icon name="arrowUp" size={16} /></button>
              <button type="button" className="icon-btn" onClick={() => move(i, 1)} disabled={i === streams.length - 1} aria-label="Move down"><Icon name="arrowDown" size={16} /></button>
              <button type="button" className="icon-btn danger" onClick={() => setStreams(streams.filter((x) => x !== s))} aria-label="Remove stream"><Icon name="x" size={16} /></button>
            </div>
          ))}
          {streams.length === 0 && <div className="list-row muted">No streams: viewers can't watch this channel.</div>}
        </div>
        <input value={streamQ} onChange={(e) => setStreamQ(e.target.value)} placeholder="Add a fallback stream: search all playlists" />
        {streamQ && (
          <div className="pick-list">
            {found?.streams
              .filter((s) => !streams.some((x) => x.streamId === s.id))
              .map((s) => (
                <button type="button" key={s.id} onClick={() => { setStreams([...streams, { streamId: s.id, name: s.name, sourceId: s.sourceId, sourceName: s.sourceName }]); setStreamQ('') }}>
                  {s.name} <span className="muted small">{s.sourceName} · {s.group}</span>
                </button>
              ))}
          </div>
        )}
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}

// ---- Activity --------------------------------------------------------------

const mbps = (bps: number) => `${(bps / 1e6).toFixed(2)} Mbps`

function Activity() {
  const { data, reload } = useApi<RelayStat[]>('/api/admin/live/activity', 3000)
  const [, tick] = useState(0)
  useEffect(() => {
    const t = window.setInterval(() => tick((n) => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [])
  const now = Date.now() / 1000
  const stop = async (r: RelayStat) => {
    await api.del(`/api/admin/live/activity/${r.channelId}`)
    reload()
  }
  const kick = async (id: string) => {
    await api.del(`/api/admin/live/viewers/${id}`)
    reload()
  }
  const logs = (
    <p className="muted small">
      Starts, stream switches and errors are in the <Link to="/admin/logs?only=live">Live TV logs</Link>.
    </p>
  )
  if (data && data.length === 0) return <>{logs}<div className="empty">No live channels are being watched right now.</div></>
  return (
    <>
      {logs}
      <div className="relay-grid">
        {data?.map((r) => (
          <div key={r.channelId} className="panel relay-card">
            <div className="relay-head">
              <ChannelLogo channel={r} size={44} />
              <div className="grow">
                <div className="relay-name"><span className="ch-num">{channelNo(r.number)}</span> {r.name}</div>
                {r.now && <div className="np-state"><Icon name="activity" size={14} /> Now playing: {r.now.title}</div>}
              </div>
              <span className="muted small">{formatClock(now - r.started)}</span>
              <button className="icon-btn danger" onClick={() => stop(r)} title="Stop this channel for everyone"><Icon name="x" size={18} /></button>
            </div>
            <dl className="np-facts">
              <dt>Stream</dt>
              <dd>{r.stream} <span className="muted small">({r.source})</span>{r.failovers > 0 && <span className="pill warn">{r.failovers} fail-over{r.failovers > 1 ? 's' : ''}</span>}</dd>
              {r.video && (<><dt>Web video</dt><dd><span className={`pill ${r.video === 'copy' ? 'ok' : 'warn'}`}>{r.video === 'copy' ? 'Passed through' : `Converted · ${encoderName(r.encoder)}`}</span>{r.video === 'copy' && <span className="muted small"> (already H.264, so no hardware encoder is needed)</span>}</dd></>)}
              {r.hwError && (<><dt>Hardware encoder</dt><dd className="muted small">Failed, so this channel is converted on the CPU: {r.hwError}</dd></>)}
              {!!r.behind && r.behind >= 2 && (<><dt>Pace</dt><dd><span className="pill warn">{Math.round(r.behind)} s behind real time</span></dd></>)}
            {r.lastError && (<><dt>Last error</dt><dd className="muted small">{r.lastError}</dd></>)}
            </dl>
            <div className="relay-stats">
              <span><Icon name="activity" size={14} /> {mbps(r.currentBps)}</span>
              <span>Avg {mbps(r.avgBps)}</span>
              <span><Icon name="download" size={14} /> {formatBytes(r.bytes)}</span>
              <span><Icon name="users" size={14} /> {r.viewers.length}</span>
            </div>
            <table className="viewer-table">
              <thead>
                <tr><th>IP address</th><th>User</th><th>Player</th><th>Connected</th><th></th></tr>
              </thead>
              <tbody>
                {r.viewers.map((v) => (
                  <tr key={v.id}>
                    <td className="mono">{v.ip}</td>
                    <td>{v.username}</td>
                    <td>{v.kind === 'hls' ? 'Web' : 'App (TS)'}</td>
                    <td>{formatClock(now - v.connected)}</td>
                    <td><button className="icon-btn danger" onClick={() => kick(v.id)} aria-label="Disconnect viewer"><Icon name="x" size={14} /></button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ))}
      </div>
    </>
  )
}

function encoderName(enc?: string): string {
  const names: Record<string, string> = { none: 'CPU', nvenc: 'NVIDIA NVENC', qsv: 'Intel Quick Sync', vaapi: 'VAAPI', amf: 'AMD AMF', videotoolbox: 'VideoToolbox' }
  return names[enc ?? ''] ?? 'CPU'
}
