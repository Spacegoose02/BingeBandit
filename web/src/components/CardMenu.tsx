import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth'
import { episodeLabel } from '../format'
import type { Collection, Item, ItemDetail, Playlist, Visibility } from '../types'
import { CollectionEditor } from './CollectionEditor'
import { EditItem } from './EditItem'
import { Icon } from './Icon'
import { KidsDialog, type KidState } from './KidsApprove'
import { MatchDialog } from './MatchDialog'
import { RecommendDialog, useRecommendationsOn } from './Recommend'

/**
 * The ⋮ button in a card's corner and the menu it opens. The menu is drawn
 * on the page body, so rows that scroll sideways don't clip it, and closes on
 * a click outside, Escape or scrolling the page.
 */
function MenuButton({ label, onEdit, children }: { label: string; onEdit?: () => void; children: (close: () => void) => ReactNode }) {
  const [open, setOpen] = useState(false)
  const btn = useRef<HTMLButtonElement>(null)
  return (
    <span className="card-menu-anchor">
      {onEdit && (
        <button
          className="card-menu-btn card-edit-btn"
          aria-label={`Edit ${label.replace(/^More for /, '')}`}
          title="Edit details and poster"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onEdit()
          }}
        >
          <Icon name="edit" size={16} />
        </button>
      )}
      <button
        ref={btn}
        className="card-menu-btn"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        title="More"
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          setOpen((o) => !o)
        }}
      >
        <Icon name="dots" size={18} />
      </button>
      {open && btn.current && (
        <Popover anchor={btn.current} onClose={() => setOpen(false)}>
          {children(() => setOpen(false))}
        </Popover>
      )}
    </span>
  )
}

function Popover({ anchor, onClose, children }: { anchor: HTMLElement; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number; maxHeight: number } | null>(null)
  const close = useRef(onClose)
  close.current = onClose

  // Opens below the button, lined up with its right edge, or above it when
  // there is more room there; it stays inside the window either way.
  useLayoutEffect(() => {
    const place = () => {
      const r = anchor.getBoundingClientRect()
      const el = ref.current
      if (!el) return
      const w = el.offsetWidth
      const h = el.scrollHeight
      const below = window.innerHeight - r.bottom - 12
      const above = r.top - 12
      const down = h <= below || below >= above
      const maxHeight = Math.max(160, down ? below : above)
      const top = down ? r.bottom + 6 : Math.max(8, r.top - 6 - Math.min(h, maxHeight))
      const left = Math.min(Math.max(8, r.right - w), window.innerWidth - w - 8)
      setPos({ top, left, maxHeight })
    }
    place()
    const obs = new ResizeObserver(place)
    if (ref.current) obs.observe(ref.current)
    return () => obs.disconnect()
  }, [anchor])

  useEffect(() => {
    const outside = (e: Event) => {
      const t = e.target as Node
      if (ref.current?.contains(t) || anchor.contains(t)) return
      // Dialogs opened from the menu sit outside it.
      if ((t as Element).closest?.('.modal-backdrop')) return
      close.current()
    }
    const key = (e: KeyboardEvent) => e.key === 'Escape' && close.current()
    document.addEventListener('mousedown', outside)
    document.addEventListener('scroll', outside, true)
    window.addEventListener('resize', close.current)
    window.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', outside)
      document.removeEventListener('scroll', outside, true)
      window.removeEventListener('resize', close.current)
      window.removeEventListener('keydown', key)
    }
  }, [anchor])

  return createPortal(
    <div
      ref={ref}
      className="menu card-menu"
      role="menu"
      style={pos ? { top: pos.top, left: pos.left, maxHeight: pos.maxHeight } : { top: -9999, left: -9999 }}
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>,
    document.body,
  )
}

type Page = 'main' | 'playlists' | 'collections'
type Dialog = null | 'recommend' | 'kids' | 'edit' | 'match'

/**
 * The poster menu, like the TV app's long-press one: play, watch together,
 * mark watched, watchlist, playlists and recommend for everyone; approving
 * for kids for parents; collections, editing (details, poster, artwork),
 * fixing the match and refreshing for admins.
 */
export function ItemMenu({ item, onChanged }: { item: Item; onChanged: (it: Item) => void }) {
  const [dialog, setDialog] = useState<Dialog>(null)
  const [detail, setDetail] = useState<ItemDetail | null>(null)
  const [kids, setKids] = useState<KidState[]>([])
  const [toast, setToast] = useState('')
  const { user } = useAuth()

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(''), 2500)
    return () => window.clearTimeout(t)
  }, [toast])

  // Picks up the new state after a change, so the card's badge and poster follow.
  const reload = () =>
    api.get<ItemDetail>(`/api/items/${item.id}`).then(
      (d) => {
        setDetail(d)
        onChanged(d.item)
      },
      () => undefined,
    )

  const it = detail?.item ?? item
  const top: Item | undefined = it.kind === 'movie' || it.kind === 'show' ? it : detail?.show
  const portal = (node: ReactNode) => createPortal(node, document.body)

  return (
    <>
      <MenuButton
        label={`More for ${item.title}`}
        onEdit={
          user?.isAdmin
            ? () =>
                api.get<ItemDetail>(`/api/items/${item.id}`).then(
                  (d) => {
                    setDetail(d)
                    setDialog('edit')
                  },
                  (e) => alert(e.message),
                )
            : undefined
        }
      >
        {(close) => (
          <ItemMenuBody
            item={item}
            detail={detail}
            setDetail={setDetail}
            kids={kids}
            setKids={setKids}
            canApprove={!!user && (user.isAdmin || !user.ownerId)}
            admin={!!user?.isAdmin}
            close={close}
            done={(msg) => {
              close()
              if (msg) setToast(msg)
              reload()
            }}
            open={(d) => {
              close()
              setDialog(d)
            }}
          />
        )}
      </MenuButton>
      {toast && <div className="card-menu-note">{toast}</div>}
      {dialog === 'recommend' && top && portal(<RecommendDialog item={top} onClose={(msg) => (setDialog(null), msg && setToast(msg))} />)}
      {dialog === 'kids' &&
        top &&
        portal(
          <KidsDialog
            path={`/api/items/${top.id}/kids`}
            name={top.title}
            what={`${top.kind === 'show' ? 'every episode of this show' : 'this movie'} from ${top.kind === 'show' ? 'Approved Shows' : 'Approved Movies'}`}
            kids={kids}
            onClose={(saved) => {
              setDialog(null)
              if (saved) setToast('Saved who can watch it.')
            }}
          />,
        )}
      {dialog === 'edit' &&
        detail &&
        portal(
          <EditItem
            item={detail.item}
            collectionNames={detail.collectionNames ?? []}
            markers={detail.markers}
            onClose={() => setDialog(null)}
            onChanged={reload}
            onSaved={() => {
              setDialog(null)
              reload()
            }}
          />,
        )}
      {dialog === 'match' &&
        detail &&
        portal(
          <MatchDialog
            item={detail.item}
            tvdb={detail.tvdbId ? { id: detail.tvdbId, order: detail.tvdbOrder ?? '' } : undefined}
            onClose={() => setDialog(null)}
            onMatched={() => {
              setDialog(null)
              reload()
            }}
          />,
        )}
    </>
  )
}

function ItemMenuBody({
  item,
  detail,
  setDetail,
  kids,
  setKids,
  canApprove,
  admin,
  close,
  done,
  open,
}: {
  item: Item
  detail: ItemDetail | null
  setDetail: (d: ItemDetail) => void
  kids: KidState[]
  setKids: (k: KidState[]) => void
  canApprove: boolean
  admin: boolean
  close: () => void
  done: (msg?: string) => void
  open: (d: Dialog) => void
}) {
  const nav = useNavigate()
  const [page, setPage] = useState<Page>('main')
  const [error, setError] = useState('')
  const [playlists, setPlaylists] = useState<Playlist[] | null>(null)
  const [newName, setNewName] = useState('')
  const [cols, setCols] = useState<{ collections: string[]; library: string[] } | null>(null)

  // Everything past the basics (up next, the show, trailers) comes with the details.
  useEffect(() => {
    api.get<ItemDetail>(`/api/items/${item.id}`).then(setDetail, (e) => setError(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id])
  const it = detail?.item ?? item
  const top: Item | undefined = it.kind === 'movie' || it.kind === 'show' ? it : detail?.show
  const recommendOn = useRecommendationsOn()
  useEffect(() => {
    if (canApprove && top) api.get<KidState[]>(`/api/items/${top.id}/kids`).then(setKids, () => setKids([]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canApprove, top?.id])

  const playable = it.kind === 'movie' || it.kind === 'episode'
  const target = playable ? it : detail?.upNext
  const resume = target && target.watch.position > 15 ? Math.floor(target.watch.position) : 0
  const watched = playable ? !!it.watch.watched : !!it.childCount && !it.unwatchedCount
  const partWatched = playable ? it.watch.position > 15 : !!it.childCount && !!it.unwatchedCount && it.unwatchedCount < it.childCount
  const file = detail?.canDownload ? detail.files?.[0] : undefined
  const go = (to: string) => {
    close()
    nav(to)
  }
  const act = (p: Promise<unknown>, msg: string) => p.then(() => done(msg), (e) => setError(e.message))
  const watchTogether = async () => {
    try {
      const r = await api.post<{ id: string }>('/api/together', { itemId: target!.id, position: resume, paused: false })
      go(`/play/${target!.id}?together=${r.id}`)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const openPage = (p: Page) => {
    setError('')
    setPage(p)
    if (p === 'playlists') api.get<Playlist[]>('/api/playlists').then(setPlaylists, (e) => setError(e.message))
    if (p === 'collections' && top) api.get<typeof cols>(`/api/admin/items/${top.id}/collections`).then(setCols, (e) => setError(e.message))
  }
  const back = (
    <button onClick={() => setPage('main')}>
      <Icon name="back" size={16} /> Back
    </button>
  )

  const head = <div className="menu-section card-menu-title">{it.kind === 'episode' ? `${it.showTitle ?? ''} · ${episodeLabel(it)}` : it.title}</div>
  const err = error && <div className="form-error small menu-pad">{error}</div>

  if (page === 'playlists') {
    const what = it.kind === 'show' ? 'all episodes' : it.kind === 'season' ? 'the season' : 'it'
    const add = async (p: Playlist) => {
      try {
        const res = await api.post<{ added: number }>(`/api/playlists/${p.id}/items`, { itemIds: [it.id] })
        done(res.added ? `Added ${what} to ${p.name}.` : `Already in ${p.name}.`)
      } catch (e) {
        setError((e as Error).message)
      }
    }
    return (
      <>
        {back}
        <div className="menu-section">Add to playlist</div>
        {playlists?.map((p) => (
          <button key={p.id} role="menuitem" onClick={() => add(p)}>
            <Icon name="list" size={16} /> {p.name}
            <span className="muted small push">{p.itemCount}</span>
          </button>
        ))}
        {playlists?.length === 0 && <div className="muted small menu-pad">No playlists yet</div>}
        <form
          className="menu-create"
          onSubmit={(e) => {
            e.preventDefault()
            const name = newName.trim()
            if (name) act(api.post('/api/playlists', { name, itemIds: [it.id] }), `Made ${name}.`)
          }}
        >
          <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="New playlist name" autoFocus />
          <button className="btn btn-primary btn-sm" disabled={!newName.trim()}>Create</button>
        </form>
        {err}
      </>
    )
  }

  if (page === 'collections' && top) {
    const toggle = async (name: string) => {
      if (!cols) return
      const next = cols.collections.includes(name) ? cols.collections.filter((n) => n !== name) : [...cols.collections, name]
      try {
        await api.put(`/api/admin/items/${top.id}/collections`, { collections: next })
        setCols({ ...cols, collections: next })
      } catch (e) {
        setError((e as Error).message)
      }
    }
    return (
      <>
        {back}
        <div className="menu-section">Collections</div>
        {cols?.library.map((name) => (
          <button key={name} role="menuitemcheckbox" aria-checked={cols.collections.includes(name)} className={cols.collections.includes(name) ? 'active' : ''} onClick={() => toggle(name)}>
            <Icon name="stack" size={16} /> {name}
          </button>
        ))}
        {cols?.library.length === 0 && (
          <div className="muted small menu-pad">
            This library has no collections to add to yet. Make one on the library's <Link to={`/library/${top.libraryId}?tab=collections`}>Collections</Link> tab.
          </div>
        )}
        {err}
      </>
    )
  }

  return (
    <>
      {head}
      {target && (
        <button role="menuitem" onClick={() => go(`/play/${target.id}${resume ? `?t=${resume}` : ''}`)}>
          <Icon name="play" size={16} /> {resume ? 'Resume' : playable ? 'Play' : `Play ${episodeLabel(target)}`}
        </button>
      )}
      {resume > 0 && (
        <button role="menuitem" onClick={() => go(`/play/${target!.id}`)}>
          <Icon name="replay10" size={16} /> Play from the start
        </button>
      )}
      {target && (
        <button role="menuitem" onClick={watchTogether}>
          <Icon name="users" size={16} /> Watch together
        </button>
      )}
      {it.kind === 'episode' && (it.showId || detail?.show) && (
        <button role="menuitem" onClick={() => go(`/item/${detail?.show?.id ?? it.showId}`)}>
          <Icon name="tv" size={16} /> Go to show
        </button>
      )}
      {/* A show or season that's part watched offers both, like Plex. */}
      {!watched && (
        <button role="menuitem" onClick={() => act(api.post(`/api/items/${it.id}/watched`), 'Marked watched.')}>
          <Icon name="check" size={16} /> Mark watched
        </button>
      )}
      {(watched || partWatched) && (
        <button role="menuitem" onClick={() => act(api.post(`/api/items/${it.id}/unwatched`), 'Marked unwatched.')}>
          <Icon name="x" size={16} /> Mark unwatched
        </button>
      )}
      {top && (
        <button
          role="menuitem"
          onClick={() =>
            act(api[top.onWatchlist ? 'del' : 'post'](`/api/watchlist/${top.id}`), top.onWatchlist ? 'Removed from your watchlist.' : 'Added to your watchlist.')
          }
        >
          <Icon name="bookmark" size={16} fill={top.onWatchlist} /> {top.onWatchlist ? 'Remove from watchlist' : 'Add to watchlist'}
        </button>
      )}
      <button role="menuitem" onClick={() => openPage('playlists')}>
        <Icon name="list" size={16} /> Add to playlist…
      </button>
      {top && recommendOn && (
        <button role="menuitem" onClick={() => open('recommend')}>
          <Icon name="users" size={16} /> Recommend to…
        </button>
      )}
      {file && (
        <a role="menuitem" className="menu-link" href={`/api/files/${file.id}/direct?download=1`} download onClick={close}>
          <Icon name="download" size={16} /> Download
        </a>
      )}
      <button role="menuitem" onClick={() => go(`/item/${it.id}`)}>
        <Icon name="info" size={16} /> Get info
      </button>
      {top && kids.length > 0 && (
        <>
          <div className="menu-section">{admin ? 'Admin' : 'Parent'}</div>
          <button role="menuitem" onClick={() => open('kids')}>
            <Icon name="kids" size={16} className={kids.some((k) => k.approved) ? 'accent' : ''} />
            {kids.some((k) => k.approved) ? `Approved for ${kids.filter((k) => k.approved).map((k) => k.name).join(', ')}` : 'Approve for kids…'}
          </button>
        </>
      )}
      {admin && (
        <>
          {!(top && kids.length > 0) && <div className="menu-section">Admin</div>}
          {top && (
            <button role="menuitem" onClick={() => openPage('collections')}>
              <Icon name="stack" size={16} /> Add to collection…
            </button>
          )}
          <button role="menuitem" disabled={!detail} onClick={() => open('edit')}>
            <Icon name="edit" size={16} /> Edit details and poster…
          </button>
          {(it.kind === 'movie' || it.kind === 'show') && (
            <button role="menuitem" disabled={!detail} onClick={() => open('match')}>
              <Icon name="search" size={16} /> Fix match…
            </button>
          )}
          {it.kind !== 'episode' && (
            <button role="menuitem" onClick={() => act(api.post(`/api/admin/items/${it.id}/detect-markers`), 'Looking for the intro and credits…')}>
              <Icon name="activity" size={16} /> Find intro and credits
            </button>
          )}
          {it.kind !== 'season' && (
            <button role="menuitem" onClick={() => act(api.post(`/api/admin/items/${it.id}/refresh`), 'Refreshing the details…')}>
              <Icon name="refresh" size={16} /> Refresh metadata
            </button>
          )}
        </>
      )}
      {err}
    </>
  )
}

const places: [keyof Visibility, string][] = [
  ['library', 'Library'],
  ['home', 'Home'],
  ['friends', "Friends' Home"],
]

/**
 * The collection card's menu: approve for kids for parents; for admins, edit
 * (name, poster, rules), where it shows as a row, and delete.
 */
export function CollectionMenu({ collection, libraryKind, onChanged }: { collection: Collection; libraryKind?: string; onChanged?: () => void }) {
  const { user } = useAuth()
  const admin = !!user?.isAdmin
  const canApprove = !!user && (admin || !user.ownerId)
  const shows = libraryKind === 'shows'
  const [dialog, setDialog] = useState<null | 'edit' | 'kids'>(null)
  const [kids, setKids] = useState<KidState[] | null>(null)
  const [visible, setVisible] = useState<Visibility | undefined>(collection.visible)
  const kidsPath = `/api/collections/${collection.id}/kids`
  useEffect(() => setVisible(collection.visible), [collection.visible])

  if (!admin && !canApprove) return null
  const toggle = async (place: keyof Visibility) => {
    if (!visible) return
    const next = { ...visible, [place]: !visible[place] }
    setVisible(next)
    try {
      await api.put(`/api/admin/collections/${collection.id}/visibility`, next)
      onChanged?.()
    } catch {
      setVisible(visible)
    }
  }
  const remove = async () => {
    if (!confirm(`Delete the collection "${collection.name}"? The movies and shows stay in your library.`)) return
    try {
      await api.del(`/api/admin/collections/${collection.id}`)
      onChanged?.()
    } catch (e) {
      alert((e as Error).message)
    }
  }

  return (
    <>
      <MenuButton label={`More for ${collection.name}`} onEdit={admin ? () => setDialog('edit') : undefined}>
        {(close) => (
          <CollectionMenuBody
            kidsPath={canApprove ? kidsPath : ''}
            kids={kids}
            setKids={setKids}
            open={(d) => {
              close()
              setDialog(d)
            }}
            name={collection.name}
            admin={admin}
          >
            {admin && (
              <>
                <button role="menuitem" onClick={() => (close(), setDialog('edit'))}>
                  <Icon name="edit" size={16} /> Edit details and poster…
                </button>
                {visible && (
                  <>
                    <div className="menu-section">Visible on</div>
                    {places.map(([p, label]) => (
                      <button key={p} role="menuitemcheckbox" aria-checked={visible[p]} className={visible[p] ? 'active' : ''} onClick={() => toggle(p)}>
                        {label}
                      </button>
                    ))}
                  </>
                )}
                <div className="menu-section" />
                <button
                  role="menuitem"
                  className="danger"
                  onClick={() => {
                    close()
                    remove()
                  }}
                >
                  <Icon name="trash" size={16} /> Delete collection
                </button>
              </>
            )}
          </CollectionMenuBody>
        )}
      </MenuButton>
      {dialog === 'edit' &&
        createPortal(
          <CollectionEditor
            libraryId={collection.libraryId}
            libraryKind={libraryKind}
            collection={collection}
            onClose={() => setDialog(null)}
            onSaved={() => {
              setDialog(null)
              onChanged?.()
            }}
          />,
          document.body,
        )}
      {dialog === 'kids' &&
        kids &&
        createPortal(
          <KidsDialog
            path={kidsPath}
            name={collection.name}
            what={`everything in this collection, and anything added to it later, from ${shows ? 'Approved Shows' : 'Approved Movies'}`}
            kids={kids}
            onClose={(saved) => {
              setDialog(null)
              if (saved) setKids(null)
            }}
          />,
          document.body,
        )}
    </>
  )
}

function CollectionMenuBody({
  kidsPath,
  kids,
  setKids,
  open,
  name,
  admin,
  children,
}: {
  kidsPath: string
  kids: KidState[] | null
  setKids: (k: KidState[]) => void
  open: (d: 'kids') => void
  name: string
  admin: boolean
  children: ReactNode
}) {
  useEffect(() => {
    if (kidsPath) api.get<KidState[]>(kidsPath).then(setKids, () => setKids([]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kidsPath])
  const approved = (kids ?? []).filter((k) => k.approved)
  if (!admin && !kids?.length) return <div className="muted small menu-pad">Nothing to do here: there are no kid profiles yet.</div>
  return (
    <>
      <div className="menu-section card-menu-title">{name}</div>
      {!!kids?.length && (
        <button role="menuitem" onClick={() => open('kids')}>
          <Icon name="kids" size={16} className={approved.length ? 'accent' : ''} />
          {approved.length ? `Approved for ${approved.map((k) => k.name).join(', ')}` : 'Approve collection for kids…'}
        </button>
      )}
      {children}
    </>
  )
}
