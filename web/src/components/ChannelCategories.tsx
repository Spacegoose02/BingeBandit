import { useState } from 'react'
import { api } from '../api'
import type { ChannelCategory, LiveChannel } from '../types'
import { Icon } from './Icon'
import { Modal } from './Modal'

const channelNo = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1))

/** Create, rename, order and fill your own channel categories. */
export function ManageCategories({
  categories,
  channels,
  onClose,
  onChanged,
}: {
  categories: ChannelCategory[]
  channels: LiveChannel[]
  onClose: () => void
  onChanged: () => void
}) {
  const [editing, setEditing] = useState<ChannelCategory | null>(null)
  const [error, setError] = useState('')
  const favorites = channels.filter((c) => c.favorite).map((c) => c.id)

  const run = async (fn: () => Promise<unknown>) => {
    setError('')
    try {
      await fn()
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const create = (fromFavorites: boolean) => {
    const name = prompt(fromFavorites ? 'Name for a category with your favourite channels' : 'Name for the new category')?.trim()
    if (!name) return
    run(() => api.post('/api/live/categories', { name, channelIds: fromFavorites ? favorites : [] }))
  }
  const rename = (c: ChannelCategory) => {
    const name = prompt('Category name', c.name)?.trim()
    if (name && name !== c.name) run(() => api.put(`/api/live/categories/${c.id}`, { ...c, name }))
  }
  const remove = (c: ChannelCategory) => confirm(`Delete the category “${c.name}”? The channels stay.`) && run(() => api.del(`/api/live/categories/${c.id}`))
  const move = (i: number, dir: -1 | 1) => {
    const ids = categories.map((c) => c.id)
    const j = i + dir
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    run(() => api.put('/api/live/categories/order', { ids }))
  }

  if (editing) {
    return (
      <PickChannels
        category={editing}
        channels={channels}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null)
          onChanged()
        }}
      />
    )
  }
  return (
    <Modal title="My categories" onClose={onClose}>
      <div className="form">
        <p className="muted small" style={{ margin: 0 }}>Your own channel groups, shown in the guide's filter. Only you see them.</p>
        <div className="panel list compact">
          {categories.map((c, i) => (
            <div key={c.id} className="list-row">
              <div className="grow">
                <div>{c.name}</div>
                <div className="muted small">{c.channelIds.length} channel{c.channelIds.length === 1 ? '' : 's'}</div>
              </div>
              <button type="button" className="icon-btn" onClick={() => move(i, -1)} disabled={i === 0} aria-label="Move up"><Icon name="arrowUp" size={16} /></button>
              <button type="button" className="icon-btn" onClick={() => move(i, 1)} disabled={i === categories.length - 1} aria-label="Move down"><Icon name="arrowDown" size={16} /></button>
              <button type="button" className="btn btn-glass btn-sm" onClick={() => setEditing(c)}><Icon name="tv" size={16} /> Channels</button>
              <button type="button" className="icon-btn" onClick={() => rename(c)} aria-label="Rename"><Icon name="edit" size={16} /></button>
              <button type="button" className="icon-btn danger" onClick={() => remove(c)} aria-label="Delete category"><Icon name="trash" size={16} /></button>
            </div>
          ))}
          {categories.length === 0 && <div className="list-row muted">No categories yet.</div>}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions">
          <button type="button" className="btn btn-primary" onClick={() => create(false)}><Icon name="plus" size={18} /> New category</button>
          <button type="button" className="btn btn-glass" onClick={() => create(true)} disabled={favorites.length === 0} title={favorites.length ? '' : 'Star some channels first'}>
            <Icon name="star" size={18} /> From my favourites ({favorites.length})
          </button>
        </div>
      </div>
    </Modal>
  )
}

function PickChannels({ category, channels, onClose, onSaved }: { category: ChannelCategory; channels: LiveChannel[]; onClose: () => void; onSaved: () => void }) {
  const [sel, setSel] = useState(() => new Set(category.channelIds))
  const [q, setQ] = useState('')
  const [error, setError] = useState('')
  // Favourites first: they're the usual starting point for a category.
  const sorted = [...channels].sort((a, b) => Number(b.favorite) - Number(a.favorite))
  const shown = sorted.filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()) || String(c.number).startsWith(q) || (c.group ?? '').toLowerCase().includes(q.toLowerCase()))
  const toggle = (id: number) => {
    const n = new Set(sel)
    if (n.has(id)) n.delete(id)
    else n.add(id)
    setSel(n)
  }
  const save = async () => {
    try {
      await api.put(`/api/live/categories/${category.id}`, { name: category.name, channelIds: [...sel] })
      onSaved()
    } catch (e) {
      setError((e as Error).message)
    }
  }
  return (
    <Modal title={`Channels in ${category.name}`} onClose={onClose}>
      <div className="form">
        <div className="actions">
          <input className="grow" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find a channel or provider category" autoFocus />
          <button type="button" className="btn btn-glass btn-sm" onClick={() => setSel(new Set([...sel, ...shown.map((c) => c.id)]))}>Tick all shown</button>
        </div>
        <div className="panel list compact assign-list">
          {shown.map((c) => (
            <label key={c.id} className="list-row">
              <input type="checkbox" checked={sel.has(c.id)} onChange={() => toggle(c.id)} />
              <span className="ch-num">{channelNo(c.number)}</span>
              <span className="grow">{c.name}</span>
              {c.favorite && <Icon name="star" fill size={14} className="fav-mark" />}
              {c.group && <span className="muted small">{c.group}</span>}
            </label>
          ))}
        </div>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <span className="muted small grow">{sel.size} selected</span>
          <button type="button" className="btn btn-glass" onClick={onClose}>Back</button>
          <button type="button" className="btn btn-primary" onClick={save}>Save</button>
        </div>
      </div>
    </Modal>
  )
}

/** "Add to category" for one channel: tick or untick it in each of your categories, or start a new one. */
export function CategoryMenu({ channel, categories, onChanged }: { channel: LiveChannel; categories: ChannelCategory[]; onChanged: () => void }) {
  const pick = async (v: string) => {
    if (v === 'new') {
      const name = prompt(`New category with ${channel.name}`)?.trim()
      if (name) await api.post('/api/live/categories', { name, channelIds: [channel.id] })
    } else if (v) {
      const cat = categories.find((c) => String(c.id) === v)
      if (!cat) return
      const inIt = cat.channelIds.includes(channel.id)
      await api[inIt ? 'del' : 'post'](`/api/live/categories/${cat.id}/channels/${channel.id}`)
    }
    onChanged()
  }
  return (
    <select className="btn btn-glass category-menu" value="" onChange={(e) => pick(e.target.value)} aria-label={`Add ${channel.name} to a category`}>
      <option value="">Add to category…</option>
      {categories.map((c) => (
        <option key={c.id} value={c.id}>
          {c.channelIds.includes(channel.id) ? '✓ ' : ' '}
          {c.name}
        </option>
      ))}
      <option value="new">+ New category…</option>
    </select>
  )
}
