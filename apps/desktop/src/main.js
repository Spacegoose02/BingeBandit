// @ts-check
const { app, BrowserWindow, Menu, clipboard, ipcMain, screen, session, shell } = require('electron')
const crypto = require('node:crypto')
const fs = require('node:fs')
const path = require('node:path')
const { Mpv, findMpv, findUi } = require('./mpv')
const updates = require('./updates')
const discovery = require('./discovery')

// BingeBandit for Windows: the server's own web app in a full-screen window,
// with videos handed to mpv, which plays every format without converting.

/** @typedef {{ server: string, fullscreen: boolean, mpvPath: string, zoom: number }} Settings */

const settingsFile = () => path.join(app.getPath('userData'), 'settings.json')

/** @returns {Settings} */
function loadSettings() {
  try {
    return { server: '', fullscreen: true, mpvPath: '', zoom: 0, ...JSON.parse(fs.readFileSync(settingsFile(), 'utf8')) }
  } catch {
    return { server: '', fullscreen: true, mpvPath: '', zoom: 0 }
  }
}

/** @param {Partial<Settings>} patch */
function saveSettings(patch) {
  settings = { ...settings, ...patch }
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true })
  fs.writeFileSync(settingsFile(), JSON.stringify(settings, null, 2))
}

let settings = /** @type {Settings} */ ({ server: '', fullscreen: true, mpvPath: '', zoom: 0 })
/** @type {BrowserWindow | null} */
let win = null
/** @type {Mpv | null} */
let player = null
let releaseTimer = /** @type {NodeJS.Timeout | undefined} */ (undefined)

const serverOrigin = () => {
  try {
    return settings.server ? new URL(settings.server).origin : ''
  } catch {
    return ''
  }
}

/** Only the configured server's pages may use the player bridge. */
function fromServer(e) {
  const origin = serverOrigin()
  try {
    return !!origin && new URL(e.senderFrame?.url ?? '').origin === origin
  } catch {
    return false
  }
}

const fromSetup = (e) => (e.senderFrame?.url ?? '').startsWith('file:')

/**
 * Earlier versions' default settings files (sha256). A file still exactly as
 * an earlier version wrote it is replaced with this version's default; one
 * the person changed is left alone.
 */
const OLD_DEFAULTS = {
  'input.conf': ['f644e1eef30f9593906d0fd11e237eb5017c8c2625bd2ba525074c0e706ffebd'],
  'mpv.conf': [],
}

/**
 * mpv's settings folder in the app's data, filled with BingeBandit's defaults
 * the first time. The files are the person's after that.
 */
function mpvConfigDir() {
  const dir = path.join(app.getPath('userData'), 'mpv')
  const defaults = app.isPackaged ? path.join(process.resourcesPath, 'mpv-config') : path.join(__dirname, '..', 'mpv-config')
  fs.mkdirSync(dir, { recursive: true })
  for (const f of /** @type {const} */ (['mpv.conf', 'input.conf'])) {
    const to = path.join(dir, f)
    let replace = !fs.existsSync(to)
    if (!replace) {
      const hash = crypto.createHash('sha256').update(fs.readFileSync(to)).digest('hex')
      replace = OLD_DEFAULTS[f].includes(hash)
    }
    if (replace) fs.copyFileSync(path.join(defaults, f), to)
  }
  return dir
}

function showSetup(error = '') {
  win?.loadFile(path.join(__dirname, '..', 'setup', 'index.html'), { query: { error, server: settings.server } })
}

function showServer() {
  if (!settings.server) return showSetup()
  win?.loadURL(settings.server)
}

/**
 * How big the page is drawn. Automatic (zoom 0) makes the page about 1536
 * pixels wide, so it reads from the sofa on a TV of any resolution; Ctrl+=
 * and Ctrl+- change it and Ctrl+0 goes back to automatic.
 */
function zoomFactor() {
  if (settings.zoom) return settings.zoom
  const width = win ? screen.getDisplayMatching(win.getBounds()).size.width : 1536
  return Math.min(2, Math.max(1, width / 1536))
}

function applyZoom() {
  win?.webContents.setZoomFactor(zoomFactor())
}

function createWindow() {
  win = new BrowserWindow({
    width: 1600,
    height: 900,
    fullscreen: settings.fullscreen,
    backgroundColor: '#0b0d10',
    title: 'BingeBandit',
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      sandbox: true,
    },
  })
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:/i.test(url)) shell.openExternal(url)
    return { action: 'deny' }
  })
  win.webContents.on('did-finish-load', applyZoom)
  win.webContents.on('did-fail-load', (_e, code, desc, url, isMainFrame) => {
    // -3 is a navigation the page cancelled itself.
    if (isMainFrame && code !== -3 && !url.startsWith('file:')) showSetup(`Couldn't reach ${settings.server} (${desc}).`)
  })
  win.webContents.on('before-input-event', (e, input) => {
    if (input.type !== 'keyDown') return
    if (input.key === 'F11') {
      e.preventDefault()
      const full = !win?.isFullScreen()
      win?.setFullScreen(full)
      saveSettings({ fullscreen: full })
    } else if (input.key === 'S' && input.control && input.shift) {
      e.preventDefault()
      showSetup()
    } else if (input.control && (input.key === '=' || input.key === '+' || input.key === '-')) {
      e.preventDefault()
      const step = input.key === '-' ? -0.1 : 0.1
      saveSettings({ zoom: Math.round(Math.min(3, Math.max(0.5, zoomFactor() + step)) * 10) / 10 })
      applyZoom()
    } else if (input.control && input.key === '0') {
      e.preventDefault()
      saveSettings({ zoom: 0 })
      applyZoom()
    } else if (input.key === 'F5') {
      e.preventDefault()
      win?.webContents.reload()
    }
  })
  win.on('closed', () => {
    win = null
    player?.quit()
  })
  showServer()
}

// Setup page

ipcMain.handle('setup:probe', (e, input) => (fromSetup(e) ? discovery.probeTyped(String(input)) : null))
ipcMain.handle('setup:scan', (e) => (fromSetup(e) ? discovery.scan() : []))
ipcMain.handle('setup:use', (e, url) => {
  if (!fromSetup(e)) return
  saveSettings({ server: String(url) })
  showServer()
})

// Player bridge (preload.js exposes it to the server's pages as window.bbDesktop)

ipcMain.on('bb:origin', (e) => {
  e.returnValue = serverOrigin()
})

ipcMain.handle('player:play', async (e, req) => {
  if (!fromServer(e)) throw new Error('Not allowed')
  clearTimeout(releaseTimer)
  const origin = serverOrigin()
  const abs = (p) => {
    const u = new URL(String(p), origin)
    if (u.origin !== origin || !u.pathname.startsWith('/api/')) throw new Error('Not a BingeBandit file')
    return u.href
  }
  const live = !!req.live
  /** @type {import('./mpv').PlayRequest} */
  const play = {
    url: abs(req.url),
    title: String(req.title || 'BingeBandit'),
    // A live channel starts at its live edge.
    start: live ? null : Number(req.start) || 0,
    audio: typeof req.audio === 'number' && req.audio > 0 ? req.audio : 'auto',
    sub: typeof req.sub === 'number' && req.sub > 0 ? req.sub : live ? 'auto' : 'no',
    subUrl: req.subUrl ? { url: abs(req.subUrl.url), title: String(req.subUrl.title || ''), lang: String(req.subUrl.lang || '') } : undefined,
  }
  if (player && !player.closed) return player.load(play)

  // mpv signs in with this window's session, as the web app does.
  const cookies = await session.defaultSession.cookies.get({ url: origin, name: 'bb_session' })
  const headers = ['X-Requested-With: BingeBandit']
  if (cookies[0]) headers.push(`Authorization: Bearer ${cookies[0].value}`)
  const p = new Mpv(findMpv(app.isPackaged ? process.resourcesPath : '', settings.mpvPath), play, {
    headers,
    configDir: mpvConfigDir(),
    fullscreen: win?.isFullScreen() ?? true,
    uiDir: findUi(app.isPackaged ? process.resourcesPath : ''),
  })
  player = p
  const sender = e.sender
  p.on('event', (ev) => {
    if (!sender.isDestroyed()) sender.send('player:event', ev)
    if (ev.type === 'closed') {
      if (player === p) player = null
      // Back to the app, in front.
      win?.show()
      win?.focus()
    }
  })
})

ipcMain.handle('player:command', (e, name, arg) => {
  if (!fromServer(e)) return
  if (name === 'copy') return clipboard.writeText(String(arg))
  if (!player) return
  switch (name) {
    case 'seek':
      return player.send(['seek', Number(arg) || 0, 'absolute'])
    case 'pause':
      return player.send(['set_property', 'pause', !!arg])
    case 'osd':
      // Without BingeBandit's controls (a copy without mpv-scripts), mpv's own text.
      return player.hasUi ? player.ui('bb-toast', String(arg)) : player.send(['show-text', String(arg), 4000])
    case 'state':
      // What BingeBandit's controls show: title, prompts, the party badge.
      return player.ui('bb-state', arg && typeof arg === 'object' ? arg : {})
    case 'menu':
      // The page's own menu entries (channels, Watch Together).
      return player.ui('bb-menu', Array.isArray(arg) ? arg : [])
    case 'stop':
      return player.quit()
  }
})

// The player page is leaving. Quit mpv unless another file starts right
// away (the next episode), which then plays in the same window.
ipcMain.handle('player:release', (e) => {
  if (!fromServer(e) || !player) return
  clearTimeout(releaseTimer)
  releaseTimer = setTimeout(() => player?.quit(), 2000)
})

// In-app updates (updates.js). The server's pages show the notice; the
// setup page shows the version.

ipcMain.on('app:version', (e) => {
  e.returnValue = app.getVersion()
})
ipcMain.handle('update:state', (e) => (fromServer(e) || fromSetup(e) ? updates.state() : null))
ipcMain.handle('update:check', (e) => (fromServer(e) || fromSetup(e) ? updates.check() : null))
ipcMain.handle('update:install', (e) => {
  if (!fromServer(e) && !fromSetup(e)) return
  player?.quit()
  updates.install()
})

if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on('second-instance', () => {
    if (win?.isMinimized()) win.restore()
    win?.focus()
  })
  app.whenReady().then(() => {
    Menu.setApplicationMenu(null)
    settings = loadSettings()
    createWindow()
    updates.start((st) => {
      if (win && !win.webContents.isDestroyed()) win.webContents.send('update:changed', st)
    })
  })
  app.on('window-all-closed', () => app.quit())
}
