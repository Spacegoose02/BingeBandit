import { useEffect, useState } from 'react'
import { canPromptInstall, isInstalled, isIOS, onInstallChange, promptInstall } from '../install'

/** Put BingeBandit on a phone's home screen as an app. Hidden once it's running as one. */
export function InstallApp() {
  const [canPrompt, setCanPrompt] = useState(canPromptInstall)
  const [done, setDone] = useState(false)
  useEffect(() => onInstallChange(() => setCanPrompt(canPromptInstall())), [])

  if (isInstalled()) return null
  const ios = isIOS()
  const android = /Android/.test(navigator.userAgent)

  let body
  if (done) body = <p className="muted small">Installed. Open BingeBandit from your home screen or app list.</p>
  else if (!window.isSecureContext)
    body = (
      <p className="notice">
        Phones only install web apps from a secure (https://) address. Open BingeBandit through its public https address on your phone,
        then come back here.
      </p>
    )
  else if (canPrompt)
    body = (
      <div className="actions">
        <button type="button" className="btn btn-primary" onClick={() => promptInstall().then(setDone)}>
          Install app
        </button>
      </div>
    )
  else if (ios)
    body = (
      <p className="muted small">
        In Safari, tap the Share button, then <strong>Add to Home Screen</strong>. Open BingeBandit from the new icon; you'll sign in once
        there.
      </p>
    )
  else if (android)
    body = (
      <p className="muted small">
        In Chrome, open the ⋮ menu and choose <strong>Install app</strong> (or <strong>Add to Home screen</strong>).
      </p>
    )
  else
    body = (
      <p className="muted small">
        On a phone, open this page and add it to your home screen. In Chrome or Edge on a computer, use the install icon in the address bar.
      </p>
    )

  return (
    <div className="panel form" style={{ marginBottom: 20 }}>
      <h3>Install the app</h3>
      <p className="muted small">
        Add BingeBandit to your phone's home screen so it opens full screen like an app. On an iPhone this is also needed for push
        notifications.
      </p>
      {body}
    </div>
  )
}
