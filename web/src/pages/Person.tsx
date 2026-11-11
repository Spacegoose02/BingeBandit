import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { img } from '../api'
import { PosterCard } from '../components/Cards'
import { Icon } from '../components/Icon'
import { RequestModal, RequestStatus } from '../components/RequestKit'
import { useApi, useDocumentTitle } from '../hooks'
import type { Filmography, FilmographyCredit, Person } from '../types'

type Filter = 'all' | 'movie' | 'show' | 'crew' | 'missing'

/**
 * A cast or crew member: what the libraries you can see have of theirs, then
 * everything they were in (from TMDB), with what's here to watch and what
 * isn't to request.
 */
export default function PersonPage() {
  const [params] = useSearchParams()
  const name = params.get('name') ?? ''
  const q = encodeURIComponent(name)
  const lib = useApi<Person>(name ? `/api/people?name=${q}` : null)
  const film = useApi<Filmography>(name ? `/api/people/filmography?name=${q}` : null)
  useDocumentTitle(name)

  const libDone = !!lib.data || !!lib.error
  const filmDone = !!film.data || !!film.error
  if (!libDone || (!lib.data && !filmDone)) return <div className="page-loading" />

  const f = film.data
  const titles = lib.data?.titles ?? []
  const src = img(f?.photo || lib.data?.photo, 'w342')
  const acting = titles.filter((t) => t.kind === 'cast')
  const crew = titles.filter((t) => t.kind === 'crew')
  const section = (title: string, list: Person['titles']) =>
    list.length > 0 && (
      <section>
        <h2 className="section-title">{title}</h2>
        <div className="poster-grid">
          {list.map((t) => (
            <div key={t.item.id} className="person-title">
              <PosterCard item={t.item} />
              {t.role && <div className="card-sub" title={t.role}>{t.kind === 'cast' ? `as ${t.role}` : t.role}</div>}
            </div>
          ))}
        </div>
      </section>
    )
  const facts = [
    f?.knownFor,
    f?.birthday && `Born ${longDate(f.birthday)}${f.birthplace ? ` in ${f.birthplace}` : ''}`,
    f?.deathday && `Died ${longDate(f.deathday)}`,
  ].filter(Boolean)
  return (
    <div className="page">
      <header className="person-head">
        <div className="person-photo">{src ? <img src={src} alt="" /> : <Icon name="user" size={56} />}</div>
        <div className="grow">
          <h1>{f?.name || lib.data?.name || name}</h1>
          {facts.length > 0 && <div className="muted">{facts.join(' · ')}</div>}
          <div className="muted">
            {titles.length} {titles.length === 1 ? 'title' : 'titles'} in your libraries
            {f ? ` · ${f.credits.length} in all` : ''}
          </div>
          {f?.biography && <Bio text={f.biography} />}
        </div>
      </header>
      {titles.length > 0 && (acting.length && crew.length ? (
        <>
          {section('In your libraries', acting)}
          {section('Behind the camera', crew)}
        </>
      ) : (
        section('In your libraries', titles)
      ))}
      {f && f.credits.length > 0 && <FilmographySection f={f} onAsked={film.reload} />}
      {!filmDone && <p className="muted">Looking up the rest of {name}’s work…</p>}
      {!f && titles.length === 0 && <div className="empty">Nothing in your libraries credits {name}.</div>}
    </div>
  )
}

function longDate(d: string) {
  return new Date(d + 'T12:00').toLocaleDateString([], { year: 'numeric', month: 'long', day: 'numeric' })
}

/** A biography, cut to a few lines until asked for the rest. */
function Bio({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  const long = text.length > 360
  return (
    <div className="person-bio">
      <p className={long && !open ? 'clamp-4' : ''}>{text}</p>
      {long && (
        <button className="link-btn small" onClick={() => setOpen((o) => !o)}>
          {open ? 'Less' : 'More'}
        </button>
      )}
    </div>
  )
}

/** Everything the person was in, with filters, newest first. */
function FilmographySection({ f, onAsked }: { f: Filmography; onAsked: () => void }) {
  const [filter, setFilter] = useState<Filter>('all')
  const [asking, setAsking] = useState<FilmographyCredit | null>(null)
  const [asked, setAsked] = useState<Record<string, FilmographyCredit['request']>>({})
  const counts = useMemo(() => {
    const c = { all: f.credits.length, movie: 0, show: 0, crew: 0, missing: 0 }
    for (const t of f.credits) {
      if (t.crew) c.crew++
      else c[t.kind]++
      if (!t.itemId) c.missing++
    }
    return c
  }, [f.credits])
  const shown = f.credits.filter((t) =>
    filter === 'all' ? true : filter === 'crew' ? t.crew : filter === 'missing' ? !t.itemId : !t.crew && t.kind === filter,
  )
  const tabs: [Filter, string][] = [
    ['all', 'All'],
    ['movie', 'Movies'],
    ['show', 'Shows'],
    ['crew', 'Behind the camera'],
    ['missing', 'Not here'],
  ]
  return (
    <section>
      <h2 className="section-title">Filmography</h2>
      <div className="tabs film-tabs">
        {tabs
          .filter(([k]) => k === 'all' || counts[k] > 0)
          .map(([k, label]) => (
            <button key={k} className={filter === k ? 'active' : ''} onClick={() => setFilter(k)}>
              {label} <span className="muted small">{counts[k]}</span>
            </button>
          ))}
      </div>
      <div className="poster-grid">
        {shown.map((t) => {
          const key = `${t.kind}:${t.tmdbId}`
          const credit = key in asked ? { ...t, request: asked[key] } : t
          return <FilmCard key={key} t={credit} canRequest={f.canRequest} onAsk={() => setAsking(credit)} />
        })}
      </div>
      {asking && (
        <RequestModal
          title={asking}
          onClose={() => setAsking(null)}
          onDone={(req) => {
            setAsked((a) => ({ ...a, [`${asking.kind}:${asking.tmdbId}`]: req }))
            setAsking(null)
            onAsked()
          }}
        />
      )}
    </section>
  )
}

/** One title of a filmography: open it when it's here, ask for it when it isn't. */
function FilmCard({ t, canRequest, onAsk }: { t: FilmographyCredit; canRequest: boolean; onAsk: () => void }) {
  const req = t.request
  const today = new Date().toISOString().slice(0, 10)
  const soon = t.releaseDate ? t.releaseDate > today : !t.year
  const role = t.role ? (t.crew ? t.role : `as ${t.role}`) : ''
  const sub = [t.year || (soon ? 'Coming' : ''), t.kind === 'show' ? 'Show' : 'Movie', t.episodes && t.kind === 'show' ? `${t.episodes} ep` : '']
    .filter(Boolean)
    .join(' · ')
  const frame = (
    <div className="poster-frame">
      {t.poster ? <img src={img(t.poster, 'w342')} alt="" loading="lazy" /> : <div className="poster-fallback">{t.title}</div>}
      {t.itemId ? (
        <span className="film-here" title="In your libraries">
          <Icon name="check" size={12} /> Here
        </span>
      ) : (
        soon && t.releaseDate && (
          <span className="request-soon">Coming {new Date(t.releaseDate + 'T12:00').toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })}</span>
        )
      )}
      {t.itemId ? (
        <div className="poster-hover">
          <Icon name="play" fill size={34} />
        </div>
      ) : null}
    </div>
  )
  const ask = canRequest && !t.itemId && !(t.onServer && t.kind === 'movie')
  return (
    <div className={`poster-card request-card film-card${t.itemId ? '' : ' film-missing'}`}>
      {t.itemId ? (
        <Link to={`/item/${t.itemId}`} className="poster-link">{frame}</Link>
      ) : (
        <button className="poster-link plain" onClick={ask ? onAsk : undefined} disabled={!ask} aria-label={ask ? `Request ${t.title}` : t.title}>
          {frame}
        </button>
      )}
      <div className="card-title" title={t.title}>{t.title}</div>
      <div className="card-sub">{sub}</div>
      {role && <div className="card-sub" title={role}>{role}</div>}
      {!t.itemId && (
        <div className="request-action">
          {t.onServer && t.kind === 'movie' ? (
            <span className="pill">On the server</span>
          ) : req && req.status !== 'declined' && (req.mine || req.status === 'available') ? (
            <RequestStatus r={req} compact />
          ) : ask ? (
            <button className="btn btn-primary btn-sm" onClick={onAsk}>
              {req ? <><Icon name="heart" size={14} /> Me too</> : <><Icon name="plus" size={14} /> Request</>}
            </button>
          ) : (
            <span className="muted small">Not here</span>
          )}
        </div>
      )}
    </div>
  )
}
