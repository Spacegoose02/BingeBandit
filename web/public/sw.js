// BingeBandit's service worker. It only shows push notifications and opens
// them; it doesn't cache anything, so the app always loads from the server.

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))

self.addEventListener('push', (event) => {
  let msg = {}
  try {
    msg = event.data ? event.data.json() : {}
  } catch {
    msg = { title: event.data ? event.data.text() : '' }
  }
  const title = msg.title || 'BingeBandit'
  const options = {
    body: msg.body || '',
    icon: msg.image || '/brand/favicon-192.png',
    badge: '/brand/favicon-192.png',
    tag: msg.tag || undefined,
    renotify: !!msg.tag,
    data: { link: msg.link || '/' },
  }
  if (msg.kind === 'together') {
    options.requireInteraction = true
    options.actions = [{ action: 'open', title: 'Join' }]
  }
  event.waitUntil(self.registration.showNotification(title, options))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = new URL((event.notification.data && event.notification.data.link) || '/', self.location.origin)
  if (target.origin !== self.location.origin) return
  event.waitUntil(
    (async () => {
      const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      // Reuse an open BingeBandit tab or window when there is one.
      for (const c of open) {
        if (new URL(c.url).origin === self.location.origin && 'focus' in c) {
          await c.focus()
          if ('navigate' in c) {
            try {
              await c.navigate(target.href)
              return
            } catch {
              // Not controlled by this worker yet; ask the page to go there.
            }
          }
          c.postMessage({ type: 'bb:open', link: target.pathname + target.search })
          return
        }
      }
      await self.clients.openWindow(target.href)
    })(),
  )
})

// The browser renewed the subscription on its own: tell the server.
self.addEventListener('pushsubscriptionchange', (event) => {
  const old = event.oldSubscription
  event.waitUntil(
    (async () => {
      const key = old && old.options && old.options.applicationServerKey
      const sub = event.newSubscription || (key ? await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key }) : null)
      if (!sub) return
      const body = sub.toJSON()
      await fetch('/api/push/devices', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'BingeBandit' },
        body: JSON.stringify({ endpoint: body.endpoint, keys: body.keys, device: 'Browser' }),
      })
    })(),
  )
})
