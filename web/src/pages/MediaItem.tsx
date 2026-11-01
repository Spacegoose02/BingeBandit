import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, img } from '../api'
import { useAudio } from '../components/AudioPlayer'
import { PosterCard } from '../components/Cards'
import { Icon } from '../components/Icon'
import { formatTime } from '../libraryKinds'
import type { ItemDetail, Track } from '../types'

/**
 * Pages for music, audiobooks and photos: an artist with their albums, an
 * album or audiobook with its tracks or chapters, and a photo album with a
 * full-screen viewer. A track, chapter or photo opens its album with that
 * one picked out.
 */
export function MediaItemView({ detail, reload }: { detail: ItemDetail; reload: () => void }) {
  const { item } = detail
  if (item.kind === 'artist') return <ArtistView detail={detail} />
  if (item.kind === 'photoalbum' || item.kind === 'photo') return <PhotoAlbumView detail={detail} />
  return <AlbumView detail={detail} reload={reload} />
}

function Cover({ src, label }: { src?: string; label: string }) {
  const [failed, setFailed] = useState(false)
  return (
    <div className="media-cover">
      {src && !failed ? <img src={img(src, 'w500')} alt="" onError={() => setFailed(true)} /> : <div className="poster-fallback">{label}</div>}
    </div>
  )
}

function ArtistView({ detail }: { detail: ItemDetail }) {
  const { item, children = [] } = detail
  const { play } = useAudio()
  const start = async (shuffle: boolean) => play(await api.get<Track[]>(`/api/items/${item.id}/tracks`), 0, { shuffle })
  return (
    <div className="page">
      <header className="media-head">
        <Cover src={item.poster} label={item.title} />
        <div className="media-info">
          <div className="muted small upper">Artist</div>
          <h1>{item.title}</h1>
          <div className="muted">{children.length} {children.length === 1 ? 'album' : 'albums'}</div>
          <div className="actions">
            <button className="btn btn-primary" onClick={() => start(false)}>
              <Icon name="play" fill size={18} /> Play
            </button>
            <button className="btn btn-glass" onClick={() => start(true)}>
              <Icon name="shuffle" size={18} /> Shuffle
            </button>
          </div>
        </div>
      </header>
      <div className="poster-grid square-grid">
        {children.map((a) => (
          <PosterCard key={a.id} item={a} subtitle={a.year ? String(a.year) : ''} />
        ))}
      </div>
    </div>
  )
}

function AlbumView({ detail, reload }: { detail: ItemDetail; reload: () => void }) {
  const picked = detail.item
  // A track or chapter shows its album or book.
  const item = detail.parent ?? picked
  const tracks = detail.tracks ?? []
  const { play, current, playing, toggle } = useAudio()
  const book = item.kind === 'audiobook'
  const total = tracks.reduce((s, t) => s + t.file.duration, 0)
  const discs = new Set(tracks.map((t) => t.item.seasonNumber ?? 1)).size
  const variousArtists = tracks.some((t) => t.item.studio && t.item.studio !== item.studio)
  // An audiobook resumes at the chapter in progress, else the first not finished.
  const resumeIdx = book
    ? Math.max(0, tracks.findIndex((t) => t.item.watch.position > 15 && !t.item.watch.watched)) ||
      Math.max(0, tracks.findIndex((t) => !t.item.watch.watched))
    : 0
  const started = book && tracks.some((t) => t.item.watch.watched || t.item.watch.position > 15)
  const finished = book && tracks.length > 0 && tracks.every((t) => t.item.watch.watched)

  const playAt = (i: number) => {
    if (current && current.item.id === tracks[i].item.id) toggle()
    else play(tracks, i, { resume: book })
  }
  const markBook = async (watched: boolean) => {
    await api.post(`/api/items/${item.id}/${watched ? 'watched' : 'unwatched'}`)
    reload()
  }
  // Reload when listening pauses or a chapter ends, so the ticks and Resume
  // stay right.
  const wasPlaying = useRef(playing)
  useEffect(() => {
    if (book && wasPlaying.current && !playing) window.setTimeout(reload, 500)
    wasPlaying.current = playing
  }, [book, playing, current?.item.id]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="page">
      <header className="media-head">
        <Cover src={item.poster} label={item.title} />
        <div className="media-info">
          <div className="muted small upper">{book ? 'Audiobook' : 'Album'}</div>
          <h1>{item.title}</h1>
          <div className="media-by">
            {detail.artist ? <Link to={`/item/${detail.artist.id}`}>{detail.artist.title}</Link> : item.studio}
          </div>
          <div className="muted small">
            {[item.year, item.genres?.slice(0, 3).join(', '), `${tracks.length} ${book ? (tracks.length === 1 ? 'part' : 'parts') : tracks.length === 1 ? 'song' : 'songs'}`, formatTime(total)]
              .filter(Boolean)
              .join(' · ')}
          </div>
          <div className="actions">
            <button className="btn btn-primary" disabled={!tracks.length} onClick={() => play(tracks, resumeIdx, { resume: book })}>
              <Icon name="play" fill size={18} /> {book && started && !finished ? 'Resume' : 'Play'}
            </button>
            {book ? (
              started && (
                <button className="btn btn-glass" onClick={() => play(tracks, 0)}>
                  From the start
                </button>
              )
            ) : (
              <button className="btn btn-glass" disabled={!tracks.length} onClick={() => play(tracks, 0, { shuffle: true })}>
                <Icon name="shuffle" size={18} /> Shuffle
              </button>
            )}
            {book && (
              <button className="btn btn-glass" onClick={() => markBook(!finished)}>
                <Icon name="check" size={18} /> {finished ? 'Mark unfinished' : 'Mark finished'}
              </button>
            )}
          </div>
        </div>
      </header>
      <ol className="track-list">
        {tracks.map((t, i) => {
          const isCurrent = current?.item.id === t.item.id
          const newDisc = discs > 1 && (i === 0 || (tracks[i - 1].item.seasonNumber ?? 1) !== (t.item.seasonNumber ?? 1))
          const w = t.item.watch
          return (
            <li key={t.item.id} className={`${isCurrent ? 'current' : ''}${t.item.id === picked.id && picked.id !== item.id ? ' picked' : ''}`}>
              {newDisc && <div className="track-disc muted small">Disc {t.item.seasonNumber ?? 1}</div>}
              <button className="track-row" onClick={() => playAt(i)}>
                <span className="track-num">
                  {isCurrent && playing ? <Icon name="pause" fill size={14} /> : isCurrent ? <Icon name="play" fill size={14} /> : t.item.episodeNumber || i + 1}
                </span>
                <span className="track-title grow">
                  {t.item.title}
                  {variousArtists && t.item.studio && <span className="muted small"> · {t.item.studio}</span>}
                </span>
                {book && w.watched && <Icon name="check" size={16} className="track-done" />}
                {book && !w.watched && w.position > 15 && <span className="muted small">{formatTime(t.file.duration - w.position)} left</span>}
                <span className="muted small track-time">{formatTime(t.file.duration)}</span>
              </button>
            </li>
          )
        })}
      </ol>
      {tracks.length === 0 && <div className="empty">No files found for this {book ? 'book' : 'album'}.</div>}
    </div>
  )
}

function PhotoAlbumView({ detail }: { detail: ItemDetail }) {
  const item = detail.parent ?? detail.item
  const photos = detail.tracks ?? []
  const pickedIdx = photos.findIndex((p) => p.item.id === detail.item.id && detail.item.kind === 'photo')
  const [open, setOpen] = useState<number | null>(pickedIdx >= 0 ? pickedIdx : null)
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <div className="muted small upper">Photo album</div>
          <h1>{item.title}</h1>
          <div className="muted">{photos.length} {photos.length === 1 ? 'photo' : 'photos'}</div>
        </div>
        <div className="actions">
          <button className="btn btn-primary" disabled={!photos.length} onClick={() => setOpen(0)}>
            <Icon name="play" fill size={18} /> Slideshow
          </button>
        </div>
      </header>
      <div className="photo-grid">
        {photos.map((p, i) => (
          <button key={p.item.id} className="photo-thumb" onClick={() => setOpen(i)} title={p.item.title}>
            <img src={`${p.item.poster}?w=small`} alt={p.item.title} loading="lazy" />
          </button>
        ))}
      </div>
      {open !== null && photos[open] && (
        <PhotoViewer photos={photos} index={open} onIndex={setOpen} onClose={() => setOpen(null)} slideshow={pickedIdx < 0} canDownload={!!detail.canDownload} />
      )}
    </div>
  )
}

function PhotoViewer({
  photos,
  index,
  onIndex,
  onClose,
  slideshow: startPlaying,
  canDownload,
}: {
  photos: Track[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
  slideshow: boolean
  canDownload: boolean
}) {
  const [playing, setPlaying] = useState(startPlaying && index === 0)
  const go = useCallback((d: number) => onIndex((index + d + photos.length) % photos.length), [index, photos.length, onIndex])
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight') go(1)
      else if (e.key === 'ArrowLeft') go(-1)
      else if (e.key === 'Escape') onClose()
      else if (e.key === ' ') {
        e.preventDefault()
        setPlaying((p) => !p)
      }
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [go, onClose])
  useEffect(() => {
    if (!playing) return
    const t = window.setTimeout(() => go(1), 5000)
    return () => window.clearTimeout(t)
  }, [playing, index, go])
  const p = photos[index]
  // The next photo loads while this one shows.
  const next = photos[(index + 1) % photos.length]
  return (
    <div className="photo-viewer" role="dialog" aria-label={p.item.title}>
      <img className="photo-full" src={`/api/files/${p.file.fileId}/direct`} alt={p.item.title} />
      <link rel="prefetch" href={`/api/files/${next.file.fileId}/direct`} />
      <button className="photo-nav prev" onClick={() => go(-1)} aria-label="Previous photo"><Icon name="back" size={32} /></button>
      <button className="photo-nav next" onClick={() => go(1)} aria-label="Next photo"><Icon name="next" size={32} /></button>
      <div className="photo-bar">
        <span className="grow">
          {p.item.title} <span className="muted small">· {index + 1} of {photos.length}{p.item.releaseDate ? ` · ${p.item.releaseDate}` : ''}</span>
        </span>
        <button className="btn btn-glass btn-sm" onClick={() => setPlaying((x) => !x)}>
          <Icon name={playing ? 'pause' : 'play'} fill size={14} /> {playing ? 'Pause' : 'Slideshow'}
        </button>
        {canDownload && (
          <a className="btn btn-glass btn-sm" href={`/api/files/${p.file.fileId}/direct?download=1`}>
            <Icon name="download" size={14} />
          </a>
        )}
        <button className="icon-btn" onClick={onClose} aria-label="Close"><Icon name="x" size={22} /></button>
      </div>
    </div>
  )
}
