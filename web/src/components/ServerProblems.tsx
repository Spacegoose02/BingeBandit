import { Link } from 'react-router-dom'
import { useApi } from '../hooks'
import { Icon } from './Icon'

export interface ServerProblem {
  kind: string
  title: string
  body: string
  link: string
}

/**
 * For admins, on every page: something on the server needs fixing, like a
 * damaged database or failing backups. Checked every five minutes; it goes
 * away by itself once the problem is fixed.
 */
export function ServerProblemsBanner({ admin }: { admin: boolean }) {
  const { data } = useApi<{ problems: ServerProblem[] }>(admin ? '/api/admin/status' : null, 5 * 60 * 1000)
  const problems = data?.problems ?? []
  if (problems.length === 0) return null
  return (
    <>
      {problems.map((p) => (
        <div key={p.kind} className="notice danger server-problem" role="alert">
          <Icon name="info" size={18} />
          <div>
            <strong>{p.title}.</strong> {p.body} <Link to={p.link}>Open Tasks</Link>
          </div>
        </div>
      ))}
    </>
  )
}
