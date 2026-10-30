import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { useApi, useDocumentTitle } from '../hooks'

type Device = 'fire' | 'google'

/**
 * /tv-setup — a large-print page to print or send to someone setting up a
 * TV, with this server's address filled in. The download link (/tv) hands
 * out an app that already knows the server, so the TV goes straight to its
 * sign-in code.
 */
export default function TVSetupSheet() {
  useDocumentTitle('TV setup instructions')
  const { user } = useAuth()
  const { data: info } = useApi<{ serverUrl: string }>('/api/link/info')
  const [device, setDevice] = useState<Device>('fire')
  const [helper, setHelper] = useState(user?.profileName || user?.username || '')

  // Downloader assumes http://, so an https address keeps its https://.
  const server = info?.serverUrl ?? ''
  const download = server ? (server.startsWith('https://') ? server : server.replace(/^http:\/\//, '')) + '/tv' : '…'
  const linkPage = server ? server.replace(/^https?:\/\//, '') + '/link' : '…'
  const fire = device === 'fire'

  return (
    <div className="tvsheet-wrap">
      <div className="tvsheet-tools">
        <Link to="/link" className="btn">← Back</Link>
        <div className="segmented">
          <button className={fire ? 'active' : ''} onClick={() => setDevice('fire')}>Fire TV</button>
          <button className={!fire ? 'active' : ''} onClick={() => setDevice('google')}>Google TV / Android TV</button>
        </div>
        <label>
          Who to call for the code{' '}
          <input value={helper} onChange={(e) => setHelper(e.target.value)} placeholder="Your name" />
        </label>
        <button className="btn btn-primary" onClick={() => window.print()}>Print</button>
      </div>

      <article className="tvsheet">
        <h1>Putting BingeBandit on your TV</h1>
        <p className="tvsheet-lead">
          This takes about 10 minutes. You only do it once. Use your {fire ? 'Fire TV' : 'TV'} remote for every step.
        </p>

        <ol>
          <li>
            <h2>Get the Downloader app</h2>
            {fire ? (
              <p>Press the <b>Home</b> button. Go to <b>Find</b> (the magnifying glass), search for <b>Downloader</b>, pick the orange app and choose <b>Get</b>.</p>
            ) : (
              <p>Press the <b>Home</b> button. Open <b>Search</b> or the <b>Play Store</b>, search for <b>Downloader</b>, pick the orange app and choose <b>Install</b>.</p>
            )}
          </li>
          <li>
            <h2>Let Downloader install apps</h2>
            {fire ? (
              <>
                <p>Go to <b>Settings</b> (the gear) → <b>My Fire TV</b> → <b>About</b>. Press the select button on the first line (your TV's name) <b>7 times</b>, until it says you are a developer.</p>
                <p>Press <b>Back</b> once. Open <b>Developer options</b> → <b>Install unknown apps</b> and turn <b>Downloader</b> to <b>ON</b>.</p>
              </>
            ) : (
              <>
                <p>Go to <b>Settings</b> → <b>System</b> → <b>About</b>. Scroll down to <b>Android TV OS build</b> and press the select button on it <b>7 times</b>, until it says you are a developer.</p>
                <p>Go back to <b>Settings</b> → <b>Apps</b> → <b>Security &amp; restrictions</b> → <b>Unknown sources</b> and turn <b>Downloader</b> on.</p>
              </>
            )}
          </li>
          <li>
            <h2>Download BingeBandit</h2>
            <p>Open <b>Downloader</b>. If it asks for permission, choose <b>Allow</b>. Type this into the box at the top and choose <b>Go</b>:</p>
            <p className="tvsheet-address">{download}</p>
            <p>When it finishes, choose <b>Install</b>, then <b>Open</b>. If Downloader offers to delete the file afterwards, choose <b>Delete</b>.</p>
          </li>
          <li>
            <h2>Sign in with the code</h2>
            <p>
              BingeBandit opens and shows a <b>short code</b> of letters and numbers.{' '}
              {helper ? <>Call or text <b>{helper}</b> and read it to them.</> : <>Give it to whoever set up your account.</>}{' '}
              The TV signs in by itself a few seconds later.
            </p>
            <p className="tvsheet-small">For {helper || 'them'}: enter the code at <b>{linkPage}</b>.</p>
          </li>
        </ol>

        <div className="tvsheet-done">
          <h2>That's it</h2>
          <p>From now on, BingeBandit is with your other apps on the Home screen. When a dot shows next to <b>Settings</b> in BingeBandit, a new version is ready: open <b>Settings</b> and choose <b>Update</b>.</p>
        </div>
      </article>
    </div>
  )
}
