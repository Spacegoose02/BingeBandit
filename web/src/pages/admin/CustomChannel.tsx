import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api'
import { LogoField } from '../../components/ChannelLogos'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi } from '../../hooks'
import type { AdminChannel, Block, ContentSource, GuideProgram } from '../../types'
import { clock } from '../LiveGuide'

interface SourceOption {
  type: ContentSource['type']
  id?: number
  label?: string
  name: string
  detail?: string
}

const toTime = (m: number) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`
const fromTime = (s: string) => {
  const [h, m] = s.split(':').map(Number)
  return h * 60 + m
}
const sourceKey = (s: ContentSource) => `${s.type}:${s.id ?? s.label}`

/** Create or edit a custom (library-built) channel and its time blocks. */
export function CustomChannelEditor({ channel, nextNumber, onClose, onSaved }: { channel?: AdminChannel; nextNumber: number; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({ number: channel?.number ?? nextNumber, name: channel?.name ?? '', logo: channel?.logo ?? '', group: channel?.group ?? 'Custom', enabled: channel?.enabled ?? true })
  const [blocks, setBlocks] = useState<Block[]>([{ startMinute: -1, endMinute: -1, mode: 'shuffle', sources: [] }])
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<number | null>(channel?.id ?? null)
  const { data: existing } = useApi<{ blocks: Block[] }>(channel ? `/api/admin/live/custom/${channel.id}` : null)
  const { data: schedule, reload: reloadSchedule } = useApi<GuideProgram[]>(savedId ? `/api/admin/live/custom/${savedId}/schedule` : null)
  const { data: options } = useApi<SourceOption[]>('/api/admin/live/content-sources')

  useEffect(() => {
    if (existing?.blocks.length) setBlocks(existing.blocks)
  }, [existing])

  // Sources are stored without names; look them up for display.
  const nameOf = (s: ContentSource) => s.name ?? options?.find((o) => sourceKey(o) === sourceKey(s))?.name ?? s.label ?? `${s.type} ${s.id}`

  const update = (i: number, patch: Partial<Block>) => setBlocks(blocks.map((b, j) => (j === i ? { ...b, ...patch } : b)))

  const save = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      const body = { ...form, blocks: blocks.map((b) => ({ ...b, sources: b.sources.map((s) => ({ type: s.type, id: s.id, label: s.label, name: nameOf(s) })) })) }
      const res = channel || savedId
        ? await api.put<{ channel: AdminChannel }>(`/api/admin/live/custom/${savedId}`, body)
        : await api.post<{ channel: AdminChannel }>('/api/admin/live/custom', body)
      setSavedId(res.channel.id)
      reloadSchedule()
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal title={channel ? `Edit ${channel.name}` : 'New custom channel'} onClose={onClose} wide>
      <form className="form" onSubmit={save}>
        <p className="muted small" style={{ margin: 0 }}>
          A 24/7 channel made from your own library. It shows up in the guide and plays like any live channel.
        </p>
        <div className="field-grid">
          <label>
            Number
            <input type="number" min={1} step="0.1" value={form.number} onChange={(e) => setForm({ ...form, number: Number(e.target.value) })} required />
          </label>
          <label>
            Name
            <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Cartoon Classics" required />
          </label>
          <label>
            Category
            <input value={form.group} onChange={(e) => setForm({ ...form, group: e.target.value })} />
          </label>
        </div>
        <div className="label">Logo (optional)</div>
        <LogoField value={form.logo} onChange={(logo) => setForm({ ...form, logo })} />

        <div className="label">What plays</div>
        {blocks.map((b, i) => (
          <div key={i} className="panel block-editor">
            <div className="block-head">
              <select
                value={b.startMinute < 0 ? 'all' : 'timed'}
                onChange={(e) => update(i, e.target.value === 'all' ? { startMinute: -1, endMinute: -1 } : { startMinute: 18 * 60, endMinute: 21 * 60 })}
              >
                <option value="all">All day (default)</option>
                <option value="timed">Between…</option>
              </select>
              {b.startMinute >= 0 && (
                <>
                  <input type="time" value={toTime(b.startMinute)} onChange={(e) => update(i, { startMinute: fromTime(e.target.value) })} />
                  <span className="muted">and</span>
                  <input type="time" value={toTime(b.endMinute % 1440)} onChange={(e) => update(i, { endMinute: fromTime(e.target.value) || 1440 })} />
                </>
              )}
              <select value={b.mode} onChange={(e) => update(i, { mode: e.target.value as Block['mode'] })}>
                <option value="shuffle">Shuffle</option>
                <option value="order">In order</option>
              </select>
              <div className="spacer" />
              {blocks.length > 1 && (
                <button type="button" className="icon-btn danger" onClick={() => setBlocks(blocks.filter((_, j) => j !== i))} aria-label="Remove block">
                  <Icon name="trash" size={16} />
                </button>
              )}
            </div>
            <div className="chips">
              {b.sources.map((s) => (
                <span key={sourceKey(s)} className="chip removable">
                  {nameOf(s)} <span className="muted small">{s.type}</span>
                  <button type="button" onClick={() => update(i, { sources: b.sources.filter((x) => sourceKey(x) !== sourceKey(s)) })} aria-label="Remove source">
                    <Icon name="x" size={12} />
                  </button>
                </span>
              ))}
              {b.sources.length === 0 && <span className="muted small">No content yet</span>}
            </div>
            <SourcePicker onPick={(o) => !b.sources.some((s) => sourceKey(s) === sourceKey(o)) && update(i, { sources: [...b.sources, o] })} />
          </div>
        ))}
        <button
          type="button"
          className="btn btn-glass btn-sm"
          style={{ alignSelf: 'flex-start' }}
          onClick={() => setBlocks([...blocks, { startMinute: 6 * 60, endMinute: 9 * 60, mode: 'shuffle', sources: [] }])}
        >
          <Icon name="plus" size={16} /> Add a time block
        </button>
        <p className="muted small" style={{ margin: 0 }}>
          Time blocks override the all-day block, e.g. cartoons from 6–9 AM. Shuffle plays everything once before repeating; In order continues where it left off.
        </p>

        {schedule && schedule.length > 0 && (
          <>
            <div className="label">Coming up</div>
            <div className="schedule-preview">
              {schedule.slice(0, 12).map((p) => (
                <div key={p.id} className="schedule-row">
                  <span className="muted mono">{clock(p.start)}</span>
                  <span>{p.title}</span>
                  {p.subtitle && <span className="muted small">{p.subtitle}</span>}
                </div>
              ))}
            </div>
          </>
        )}

        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <label className="check grow">
            <input type="checkbox" checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} /> Visible to viewers
          </label>
          <button type="button" className="btn btn-glass" onClick={onClose}>Close</button>
          <button className="btn btn-primary" disabled={saving}>{saving ? 'Building schedule…' : savedId ? 'Save' : 'Create channel'}</button>
        </div>
      </form>
    </Modal>
  )
}

function SourcePicker({ onPick }: { onPick: (o: ContentSource) => void }) {
  const [q, setQ] = useState('')
  const { data } = useApi<SourceOption[]>(q.trim() ? `/api/admin/live/content-sources?q=${encodeURIComponent(q.trim())}` : null)
  const needle = q.trim().toLowerCase()
  const matches = data?.filter((o) => o.type === 'show' || o.name.toLowerCase().includes(needle)).slice(0, 12)
  return (
    <div className="source-picker">
      <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Add content: search libraries, shows, collections, playlists, labels" />
      {q.trim() && (
        <div className="pick-list">
          {matches?.map((o) => (
            <button
              type="button"
              key={`${o.type}:${o.id ?? o.label}`}
              onClick={() => {
                onPick({ type: o.type, id: o.id, label: o.label, name: o.name })
                setQ('')
              }}
            >
              {o.name} <span className="muted small">{o.detail}</span>
            </button>
          ))}
          {matches?.length === 0 && <div className="muted small menu-pad">Nothing matches</div>}
        </div>
      )}
    </div>
  )
}
