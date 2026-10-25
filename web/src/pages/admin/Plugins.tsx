import { useState, type FormEvent } from 'react'
import { api } from '../../api'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { CatalogPlugin, Plugin, PluginCatalog } from '../../types'

const stateLabels: Record<Plugin['state'], [string, string]> = {
  running: ['Running', 'ok'],
  starting: ['Starting', 'warn'],
  stopped: ['Off', ''],
  error: ['Error', 'danger'],
  missing: ['Folder missing', 'danger'],
}

export default function AdminPlugins() {
  useDocumentTitle('Plugins')
  const { data, reload } = useApi<{ plugins: Plugin[]; dirs: string[] }>('/api/admin/plugins', 5000)
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Plugin | null>(null)
  const [busy, setBusy] = useState('')

  const act = async (p: Plugin, fn: () => Promise<unknown>) => {
    setBusy(p.id)
    await fn().catch((e) => alert(e.message))
    setBusy('')
    reload()
  }
  const toggle = (p: Plugin, enabled: boolean) => act(p, () => api.put(`/api/admin/plugins/${p.id}`, { enabled }))
  const restart = (p: Plugin) => act(p, () => api.post(`/api/admin/plugins/${p.id}/restart`))
  const uninstall = (p: Plugin) => {
    if (!confirm(`Uninstall ${p.manifest.name}? Its folder and settings are deleted.`)) return
    act(p, () => api.post(`/api/admin/plugins/${p.id}/uninstall`))
  }
  const remove = (p: Plugin) => {
    if (!confirm(`Remove ${p.manifest.name}? Its settings are deleted too.`)) return
    act(p, () => api.del(`/api/admin/plugins/${p.id}`))
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Plugins</h1>
          <div className="muted">Add-ons that run alongside BingeBandit and talk to it over its plugin API.</div>
        </div>
        <button className="btn btn-glass" onClick={() => setAdding(true)}>
          <Icon name="plus" size={16} /> Add by address
        </button>
      </header>

      <div className="panel list">
        {data?.plugins.length === 0 && (
          <p className="muted" style={{ padding: 12 }}>
            No plugins yet. Put a plugin's folder in one of the plugins folders below, or add one that runs as its own service.
          </p>
        )}
        {data?.plugins.map((p) => {
          const [label, tone] = stateLabels[p.state] ?? [p.state, '']
          const m = p.manifest
          return (
            <div key={p.id} className="task-row">
              <label className="switch" title={p.enabled ? 'On' : 'Off'}>
                <input type="checkbox" checked={p.enabled} disabled={busy === p.id} onChange={(e) => toggle(p, e.target.checked)} aria-label={`${m.name} on`} />
                <span />
              </label>
              <div className="grow">
                <div className="task-name">
                  {m.name}
                  {m.version && <span className="muted small"> {m.version}</span>}
                  <span className={`pill ${tone}`}>{label}</span>
                  <span className={`pill ${p.kind === 'remote' ? 'remote' : ''}`}>{p.kind === 'remote' ? 'Own service' : 'Plugins folder'}</span>
                </div>
                {m.description && <div className="muted small">{m.description}</div>}
                {p.kind === 'remote' && <div className="muted small mono" style={{ wordBreak: 'break-all' }}>{p.url}</div>}
                {p.error && <div className="task-error small">{p.error}</div>}
                {p.state === 'running' && !!m.pages?.length && (
                  <div className="small" style={{ marginTop: 4, display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                    {m.pages.map((pg) => (
                      <a key={pg.path} className="accent" href={`/api/plugins/${p.id}${pg.path.startsWith('/') ? '' : '/'}${pg.path}`} target="_blank" rel="noreferrer">
                        {pg.title} ↗
                      </a>
                    ))}
                  </div>
                )}
              </div>
              {!!m.settings?.length && (
                <button className="btn btn-glass btn-sm" onClick={() => setEditing(p)}>
                  <Icon name="sliders" size={14} /> Settings
                </button>
              )}
              {p.enabled && (
                <button className="btn btn-glass btn-sm" disabled={busy === p.id} onClick={() => restart(p)}>
                  <Icon name="refresh" size={14} /> {p.kind === 'remote' ? 'Check' : 'Restart'}
                </button>
              )}
              {p.installed && (
                <button className="btn btn-glass btn-sm danger" disabled={busy === p.id} onClick={() => uninstall(p)}>
                  <Icon name="trash" size={14} /> Uninstall
                </button>
              )}
              {(p.kind === 'remote' || p.state === 'missing') && (
                <button className="btn btn-glass btn-sm danger" disabled={busy === p.id} onClick={() => remove(p)} aria-label={`Remove ${m.name}`}>
                  <Icon name="trash" size={16} />
                </button>
              )}
            </div>
          )
        })}
      </div>

      <Catalogue onChanged={reload} />

      {data && (
        <p className="muted small" style={{ marginTop: 12 }}>
          Plugins folders: {data.dirs.filter(Boolean).map((d, i) => <span key={d}>{i > 0 && ' · '}<code>{d}</code></span>)}. Each plugin is a
          folder with a <code>plugin.json</code>; new ones show up within a minute and start when you turn them on. Only add
          plugins you trust: a plugin can read and change every user's library data, and its pages open inside BingeBandit.
        </p>
      )}

      {adding && <AddPlugin onClose={() => setAdding(false)} onAdded={() => { setAdding(false); reload() }} />}
      {editing && <PluginSettings plugin={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />}
    </div>
  )
}

function AddPlugin({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [url, setUrl] = useState('')
  const [serverUrl, setServerUrl] = useState(window.location.origin)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/api/admin/plugins', { url, serverUrl })
      onAdded()
    } catch (err) {
      setError((err as Error).message)
    }
    setBusy(false)
  }
  return (
    <Modal title="Add a plugin by address" onClose={onClose}>
      <form className="form" onSubmit={submit}>
        <p className="muted small">For a plugin that runs as its own service, such as its own Docker container. BingeBandit reads its manifest and pairs with it.</p>
        <label>
          Plugin address
          <input className="mono" type="url" value={url} onChange={(e) => setUrl(e.target.value)} required autoFocus placeholder="http://192.168.1.20:8490" />
        </label>
        <label>
          BingeBandit's address, as the plugin reaches it
          <input className="mono" type="url" value={serverUrl} onChange={(e) => setServerUrl(e.target.value)} required />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={busy}>{busy ? 'Pairing…' : 'Add'}</button>
        </div>
      </form>
    </Modal>
  )
}

function PluginSettings({ plugin, onClose, onSaved }: { plugin: Plugin; onClose: () => void; onSaved: () => void }) {
  const fields = plugin.manifest.settings ?? []
  const [values, setValues] = useState<Record<string, string>>(plugin.values)
  const [error, setError] = useState('')
  const set = (k: string, v: string) => setValues({ ...values, [k]: v })
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put(`/api/admin/plugins/${plugin.id}/settings`, values)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={`${plugin.manifest.name} settings`} onClose={onClose}>
      <form className="form" onSubmit={save}>
        {fields.map((f) =>
          f.type === 'bool' ? (
            <label key={f.key} className="check">
              <input type="checkbox" checked={(values[f.key] ?? f.default) === 'true'} onChange={(e) => set(f.key, String(e.target.checked))} />
              {f.label}
              {f.help && <span className="muted small"> {f.help}</span>}
            </label>
          ) : (
            <label key={f.key}>
              {f.label}
              {f.type === 'select' ? (
                <select value={values[f.key] ?? f.default ?? ''} onChange={(e) => set(f.key, e.target.value)}>
                  {f.options?.map((o) => <option key={o} value={o}>{o}</option>)}
                </select>
              ) : (
                <input
                  type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'}
                  value={values[f.key] ?? ''}
                  placeholder={f.type === 'password' && plugin.secretsSet.includes(f.key) ? 'Saved; leave empty to keep' : f.default}
                  autoComplete="off"
                  onChange={(e) => set(f.key, e.target.value)}
                />
              )}
              {f.help && <span className="muted small">{f.help}</span>}
            </label>
          ),
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

function Catalogue({ onChanged }: { onChanged: () => void }) {
  const [refresh, setRefresh] = useState(0)
  const { data, reload } = useApi<PluginCatalog>(`/api/admin/plugins/catalog${refresh ? `?refresh=${refresh}` : ''}`)
  const [busy, setBusy] = useState('')
  const [editing, setEditing] = useState(false)

  const install = async (c: CatalogPlugin) => {
    if (!c.installed && !confirm(`Install ${c.name} ${c.version}? It runs as a program on this server, with access to every user's library data.`)) return
    setBusy(c.id)
    try {
      await api.post('/api/admin/plugins/catalog/install', { catalog: c.catalog, id: c.id })
      await reload()
      onChanged()
    } catch (e) {
      alert((e as Error).message)
    }
    setBusy('')
  }
  const source = (url: string) => {
    if (url === data?.official) return 'BingeBandit'
    try {
      return new URL(url).host
    } catch {
      return url
    }
  }

  return (
    <>
      <div className="section-title-row">
        <h2 className="section-title">Catalogue</h2>
        <div className="actions">
          <button className="btn btn-glass btn-sm" onClick={() => setEditing(true)}>
            <Icon name="list" size={14} /> Catalogues
          </button>
          <button className="btn btn-glass btn-sm" onClick={() => setRefresh(Date.now())} aria-label="Check the catalogues again">
            <Icon name="refresh" size={14} />
          </button>
        </div>
      </div>
      <div className="panel list">
        {!data && <p className="muted" style={{ padding: 12 }}>Loading…</p>}
        {data?.errors.map((e) => (
          <div key={e.url} className={`${e.url === data.official ? 'muted' : 'task-error'} small`} style={{ padding: '8px 12px' }}>
            {source(e.url)}: {e.error}
          </div>
        ))}
        {data && data.plugins.length === 0 && data.errors.length === 0 && <p className="muted" style={{ padding: 12 }}>The catalogues list no plugins.</p>}
        {data?.plugins.map((c) => (
          <div key={c.id} className="task-row">
            <div className="grow">
              <div className="task-name">
                {c.name}
                <span className="muted small"> {c.version}</span>
                {c.installed === 'installed' && <span className="pill ok">Installed{c.current && c.update ? ` ${c.current}` : ''}</span>}
                {c.installed === 'bundled' && <span className="pill">Comes with the server{c.current ? ` (${c.current})` : ''}</span>}
                {data.catalogs.length > 1 && <span className="pill remote">{source(c.catalog)}</span>}
              </div>
              {c.description && <div className="muted small">{c.description}</div>}
              <div className="muted small">
                {c.author && <>By {c.author}</>}
                {c.author && c.homepage && ' · '}
                {c.homepage && (
                  <a className="accent" href={c.homepage} target="_blank" rel="noreferrer">
                    About ↗
                  </a>
                )}
              </div>
            </div>
            {!c.available ? (
              <span className="muted small">Not built for {data.platform}</span>
            ) : !c.installed ? (
              <button className="btn btn-primary btn-sm" disabled={!!busy} onClick={() => install(c)}>
                <Icon name="download" size={14} /> {busy === c.id ? 'Installing…' : 'Install'}
              </button>
            ) : c.update ? (
              <button className="btn btn-glass btn-sm" disabled={!!busy} onClick={() => install(c)}>
                <Icon name="download" size={14} /> {busy === c.id ? 'Updating…' : `Update to ${c.version}`}
              </button>
            ) : null}
          </div>
        ))}
      </div>
      {data && (
        <p className="muted small" style={{ marginTop: 8 }}>
          Installs go into <code>{data.installDir}</code> and start turned off. A newer copy there replaces one that comes with the server.
        </p>
      )}
      {editing && data && (
        <CatalogueList
          official={data.official}
          current={data.catalogs.filter((u) => u !== data.official)}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            setRefresh(Date.now())
          }}
        />
      )}
    </>
  )
}

function CatalogueList({ official, current, onClose, onSaved }: { official: string; current: string[]; onClose: () => void; onSaved: () => void }) {
  const [text, setText] = useState(current.join('\n'))
  const [error, setError] = useState('')
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.put('/api/admin/settings', { plugin_catalogs: text })
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title="Plugin catalogues" onClose={onClose}>
      <form className="form" onSubmit={save}>
        <p className="muted small">
          BingeBandit's own catalogue is always used (<code style={{ wordBreak: 'break-all' }}>{official}</code>). Add the addresses of other catalogues
          here, one per line. Only add catalogues you trust: their plugins run as programs on this server.
        </p>
        <label>
          Other catalogues
          <textarea className="mono" rows={4} value={text} onChange={(e) => setText(e.target.value)} placeholder="https://example.com/plugin-catalog.json" />
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}
