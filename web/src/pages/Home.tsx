import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { PosterCard, WideCard } from '../components/Cards'
import { Hero } from '../components/Hero'
import { Icon } from '../components/Icon'
import { Row } from '../components/Row'
import { useApi, useDocumentTitle } from '../hooks'
import type { HomeData } from '../types'

export default function Home() {
  useDocumentTitle('Home')
  const { user } = useAuth()
  const { data, loading, error } = useApi<HomeData>('/api/home')

  if (loading) return <div className="page-loading" />
  if (error) return <div className="page"><div className="empty">Could not load home: {error}</div></div>
  if (!data) return null

  const empty = data.rows.length === 0 && data.continueWatching.length === 0
  if (empty) {
    return (
      <div className="page">
        <div className="empty-hero">
          <img src="/brand/mascot.png" alt="" />
          <h1>Nothing to binge yet</h1>
          {user?.isAdmin ? (
            <>
              <p>Add a library folder and BingeBandit will scan it and pull artwork, ratings and cast from TMDB.</p>
              <Link to="/admin/libraries" className="btn btn-primary">
                <Icon name="plus" size={18} /> Add a library
              </Link>
            </>
          ) : (
            <p>Your server admin has not added any libraries yet.</p>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="home">
      <Hero items={data.hero} />
      <div className="rows">
        {data.continueWatching.length > 0 && (
          <Row title="Continue Watching">
            {data.continueWatching.map((it) => (
              <WideCard key={it.id} item={it} />
            ))}
          </Row>
        )}
        {data.rows.map((row) => (
          <Row key={row.id} title={row.title}>
            {row.items.map((it) => (
              <PosterCard key={it.id} item={it} />
            ))}
          </Row>
        ))}
      </div>
    </div>
  )
}
