import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, img } from '../api'
import { useAuth } from '../auth'
import { PersonCard, PosterCard } from '../components/Cards'
import { MetaLine } from '../components/Hero'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import { channelLabel, episodeLabel, fileSummary, formatBytes, formatClock, formatRuntime, languageName, progressFraction } from '../format'
import type { Item, ItemDetail, MediaFile } from '../types'

export default function ItemPage() {
  const { id } = useParams()
  const { data, error, reload } = useApi<ItemDetail>(`/api/items/${id}`)
  useDocumentTitle(data?.item.title)
  if (error) return <div className="page"><div className="empty">{error}</div></div>
  if (!data) return <div className="page-loading" />
  return <ItemView key={id} detail={data} reload={reload} />
}

function ItemView({ detail, reload }: { detail: ItemDetail; reload: () => void }) {
  const { item, files = [], children = [], show, season, upNext, collection } = detail
  const { user } = useAuth()
  const nav = useNavigate()
  const [fileIdx, setFileIdx] = useState(0)
  const [trailer, setTrailer] = useState(false)
  const [busy, setBusy] = useState(false)
  const file: MediaFile | undefined = files[fileIdx]

  const backdrop = img(item.backdrop || show?.backdrop || item.showBackdrop, 'original')
  const poster = img(item.kind === 'episode' ? season?.poster || show?.poster : item.poster || show?.poster, 'w500')
  const playable = item.kind === 'movie' || item.kind === 'episode'
  const resumeAt = item.watch.position > 15 ? Math.floor(item.watch.position) : 0
  const fileQuery = file && files.length > 1 ? `file=${file.id}` : ''

  const play = (fromStart = false) => {
    const q = [fileQuery, !fromStart && resumeAt ? `t=${resumeAt}` : ''].filter(Boolean).join('&')
    nav(`/play/${item.id}${q ? `?${q}` : ''}`)
  }

  const toggleWatched = async () => {
    const fullyWatched = playable ? item.watch.watched : item.unwatchedCount === 0
    await api.post(`/api/items/${item.id}/${fullyWatched ? 'unwatched' : 'watched'}`)
    reload()
  }

  const refresh = async () => {
    setBusy(true)
    try {
      await api.post(`/api/admin/items/${item.id}/refresh`)
      reload()
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const watchedNow = playable ? item.watch.watched : !!item.childCount && item.unwatchedCount === 0
  const cast = (item.kind === 'season' || item.kind === 'episode' ? show?.credits : item.credits) ?? []
  const directors = cast.filter((c) => c.kind === 'crew')

  return (
    <div className="detail">
      {backdrop && <div className="detail-backdrop" style={{ backgroundImage: `url(${backdrop})` }} />}
      <div className="detail-shade" />
      <div className="detail-content">
        <div className="detail-main">
          <div className={`detail-poster ${item.kind === 'episode' ? 'is-episode' : ''}`}>
            {item.kind === 'episode' && img(item.backdrop, 'w780') ? (
              <img src={img(item.backdrop, 'w780')} alt="" />
            ) : poster ? (
              <img src={poster} alt="" />
            ) : (
              <div className="poster-fallback">{item.title}</div>
            )}
          </div>
          <div className="detail-info">
            {(item.kind === 'season' || item.kind === 'episode') && show && (
              <div className="crumbs">
                <Link to={`/item/${show.id}`}>{show.title}</Link>
                {item.kind === 'episode' && season && (
                  <>
                    <Icon name="next" size={14} />
                    <Link to={`/item/${season.id}`}>{season.title}</Link>
                  </>
                )}
              </div>
            )}
            {item.logo && item.kind !== 'season' && item.kind !== 'episode' ? (
              <img className="detail-logo" src={img(item.logo, 'w500')} alt={item.title} />
            ) : (
              <h1 className="detail-title">
                {item.kind === 'episode' && <span className="ep-label">{episodeLabel(item)}</span>}
                {item.title}
              </h1>
            )}
            <MetaLine item={item} />
            {item.tagline && <p className="tagline">{item.tagline}</p>}

            <div className="actions">
              {playable && (
                <button className="btn btn-primary" onClick={() => play()} disabled={!file}>
                  <Icon name="play" fill size={18} />
                  {resumeAt ? `Resume from ${formatClock(resumeAt)}` : 'Play'}
                </button>
              )}
              {playable && resumeAt > 0 && (
                <button className="btn btn-glass" onClick={() => play(true)}>
                  <Icon name="replay10" size={18} /> From start
                </button>
              )}
              {item.kind === 'show' && upNext && (
                <Link className="btn btn-primary" to={`/play/${upNext.id}${upNext.watch.position > 15 ? `?t=${Math.floor(upNext.watch.position)}` : ''}`}>
                  <Icon name="play" fill size={18} />
                  {upNext.watch.position > 15 ? 'Resume' : 'Play'} {episodeLabel(upNext)}
                </Link>
              )}
              {item.trailer && (
                <button className="btn btn-glass" onClick={() => setTrailer(true)}>
                  <Icon name="film" size={18} /> Trailer
                </button>
              )}
              <button className="btn btn-glass icon-only" onClick={toggleWatched} title={watchedNow ? 'Mark unwatched' : 'Mark watched'}>
                <Icon name="check" size={18} className={watchedNow ? 'accent' : ''} />
              </button>
              {user?.isAdmin && item.kind !== 'season' && (
                <button className="btn btn-glass icon-only" onClick={refresh} disabled={busy} title="Refresh metadata">
                  <Icon name="refresh" size={18} className={busy ? 'spin' : ''} />
                </button>
              )}
            </div>
            {playable && progressFraction(item) > 0 && (
              <div className="progress inline">
                <div style={{ width: `${progressFraction(item) * 100}%` }} />
              </div>
            )}

            {item.overview && <p className="overview">{item.overview}</p>}

            <dl className="facts">
              {directors.length > 0 && (
                <>
                  <dt>{directors[0].role === 'Creator' ? 'Created by' : 'Directed by'}</dt>
                  <dd>{directors.filter((d) => d.role === directors[0].role).map((d) => d.name).join(', ')}</dd>
                </>
              )}
              {item.studio && (
                <>
                  <dt>{item.kind === 'show' ? 'Network' : 'Studio'}</dt>
                  <dd>{item.studio}</dd>
                </>
              )}
              {item.releaseDate && (
                <>
                  <dt>{item.kind === 'show' ? 'First aired' : 'Released'}</dt>
                  <dd>{new Date(item.releaseDate + 'T00:00:00').toLocaleDateString(undefined, { dateStyle: 'long' })}</dd>
                </>
              )}
            </dl>

            {file && <FileInfo files={files} index={fileIdx} onChange={setFileIdx} admin={!!user?.isAdmin} />}
          </div>
        </div>

        {item.kind === 'show' && children.length > 0 && (
          <Row title="Seasons">
            {children.map((s) => (
              <PosterCard key={s.id} item={s} />
            ))}
          </Row>
        )}

        {item.kind === 'season' && <EpisodeList episodes={children} />}

        {cast.filter((c) => c.kind === 'cast').length > 0 && (
          <Row title="Cast & Crew">
            {cast.map((c, i) => (
              <PersonCard key={`${c.name}-${i}`} name={c.name} role={c.role} photo={c.photo} />
            ))}
          </Row>
        )}

        {collection && collection.length > 1 && (
          <Row title={item.collectionName ?? 'Collection'}>
            {collection.map((m) => (
              <PosterCard key={m.id} item={m} />
            ))}
          </Row>
        )}
      </div>

      {trailer && item.trailer && (
        <Modal title={`${item.title} — Trailer`} onClose={() => setTrailer(false)} wide>
          <div className="video-embed">
            <iframe
              src={`https://www.youtube-nocookie.com/embed/${item.trailer}?autoplay=1&rel=0`}
              title="Trailer"
              allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
              allowFullScreen
            />
          </div>
        </Modal>
      )}
    </div>
  )
}

function FileInfo({ files, index, onChange, admin }: { files: MediaFile[]; index: number; onChange: (i: number) => void; admin: boolean }) {
  const f = files[index]
  const audio = f.streams.filter((s) => s.type === 'audio')
  const subs = f.streams.filter((s) => s.type === 'subtitle')
  return (
    <div className="file-info">
      {files.length > 1 && (
        <label className="select-label">
          Version
          <select value={index} onChange={(e) => onChange(Number(e.target.value))}>
            {files.map((x, i) => (
              <option key={x.id} value={i}>{fileSummary(x)} · {formatBytes(x.size)}</option>
            ))}
          </select>
        </label>
      )}
      <div className="chips">
        <span className="chip strong">{fileSummary(f)}</span>
        {f.bitDepth > 8 && <span className="chip">{f.bitDepth}-bit</span>}
        <span className="chip">{f.container.toUpperCase()}</span>
        <span className="chip">{formatBytes(f.size)}</span>
        {f.duration > 0 && <span className="chip">{formatRuntime(Math.round(f.duration / 60))}</span>}
      </div>
      {audio.length > 0 && (
        <div className="track-line">
          <span className="muted">Audio</span>
          {audio.map((a) => `${languageName(a.language)} (${a.codec.toUpperCase()} ${channelLabel(a.channels ?? 2)})`).join(' · ')}
        </div>
      )}
      {subs.length > 0 && (
        <div className="track-line">
          <span className="muted">Subtitles</span>
          {subs.map((s) => languageName(s.language) + (s.forced ? ' (forced)' : '')).join(' · ')}
        </div>
      )}
      {admin && <div className="track-line path" title={f.path}>{f.path}</div>}
    </div>
  )
}

function EpisodeList({ episodes }: { episodes: Item[] }) {
  return (
    <section className="episodes">
      <h2>{episodes.length} Episodes</h2>
      {episodes.map((ep) => {
        const still = img(ep.backdrop, 'w500')
        const pct = progressFraction(ep)
        return (
          <div key={ep.id} className="episode">
            <Link to={`/play/${ep.id}${ep.watch.position > 15 ? `?t=${Math.floor(ep.watch.position)}` : ''}`} className="episode-still">
              {still ? <img src={still} alt="" loading="lazy" /> : <div className="poster-fallback">{episodeLabel(ep)}</div>}
              {pct > 0 && (
                <div className="progress">
                  <div style={{ width: `${pct * 100}%` }} />
                </div>
              )}
              {ep.watch.watched && <span className="badge-watched"><Icon name="check" size={14} /></span>}
              <div className="poster-hover"><Icon name="play" fill size={30} /></div>
            </Link>
            <div className="episode-body">
              <Link to={`/item/${ep.id}`} className="episode-title">
                <span className="ep-num">{ep.episodeNumber}.</span> {ep.title}
              </Link>
              <div className="muted small">
                {[formatRuntime(ep.runtime), ep.releaseDate && new Date(ep.releaseDate + 'T00:00:00').toLocaleDateString()].filter(Boolean).join(' · ')}
              </div>
              {ep.overview && <p className="episode-overview">{ep.overview}</p>}
            </div>
          </div>
        )
      })}
    </section>
  )
}
