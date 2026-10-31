import { useSearchParams } from 'react-router-dom'
import { img } from '../api'
import { PosterCard } from '../components/Cards'
import { Icon } from '../components/Icon'
import { useApi, useDocumentTitle } from '../hooks'
import type { Person } from '../types'

/** A cast or crew member: everything in the libraries you can see that credits them. */
export default function PersonPage() {
  const [params] = useSearchParams()
  const name = params.get('name') ?? ''
  const { data, error } = useApi<Person>(name ? `/api/people?name=${encodeURIComponent(name)}` : null)
  useDocumentTitle(name)
  if (error) return <div className="page"><div className="empty">Nothing in your libraries credits {name}.</div></div>
  if (!data) return <div className="page-loading" />
  const src = img(data.photo, 'w342')
  const acting = data.titles.filter((t) => t.kind === 'cast')
  const crew = data.titles.filter((t) => t.kind === 'crew')
  const section = (title: string, list: Person['titles']) =>
    list.length > 0 && (
      <section>
        <h2 className="section-title">{title}</h2>
        <div className="poster-grid">
          {list.map((t) => (
            <div key={t.item.id} className="person-title">
              <PosterCard item={t.item} />
              {t.role && <div className="card-sub" title={t.role}>{title === 'Acting' ? `as ${t.role}` : t.role}</div>}
            </div>
          ))}
        </div>
      </section>
    )
  return (
    <div className="page">
      <header className="person-head">
        <div className="person-photo">{src ? <img src={src} alt="" /> : <Icon name="user" size={56} />}</div>
        <div>
          <h1>{data.name}</h1>
          <div className="muted">
            {data.titles.length} {data.titles.length === 1 ? 'title' : 'titles'} in your libraries
          </div>
        </div>
      </header>
      {section('Acting', acting)}
      {section(acting.length ? 'Behind the camera' : 'Credits', crew)}
    </div>
  )
}
