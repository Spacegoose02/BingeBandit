// @ts-check
const { app, BrowserWindow, Menu, ipcMain, session, shell } = require('electron')
const fs = require('node:fs')
const path = require('node:path')
const { Mpv, findMpv } = require('./mpv')
const discovery = require('./discovery')

// BingeBandit for Windows: the server's own web app in a full-screen window,
// with videos handed to mpv, which plays every format without converting.

/** @typedef {{ server: string, fullscreen: boolean, mpvPath: string }} Settings */

const settingsFile = () => path.join(app.getPath('userData'), 'settings.json')

/** @returns {Settings} */
function loadSettings() {
  try {
    return { server: '', fullscreen: true, mpvPath: '', ...JSON.parse(fs.readFileSync(settingsFile(), 'utf8')) }
  } catch {
    return { server: '', fullscreen: true, mpvPath: '' }
  }
}

/** @param {Partial<Settings>} patch */
function saveSettings(patch) {
  settings = { ...settings, ...patch }
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true })
  fs.writeFileSync(settingsFile(), JSON.stringify(settings, null, 2))
}

let settings = /** @type {Settings} */ ({ server: '', fullscreen: true, mpvPath: '' })
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
 * mpv's settings folder in the app's data, filled with BingeBandit's defaults
 * the first time. The files are the person's after that.
 */
function mpvConfigDir() {
  const dir = path.join(app.getPath('userData'), 'mpv')
  const defaults = app.isPackaged ? path.join(process.resourcesPath, 'mpv-config') : path.join(__dirname, '..', 'mpv-config')
  fs.mkdirSync(dir, { recursive: true })
  for (const f of ['mpv.conf', 'input.conf']) {
    const to = path.join(dir, f)
    if (!fs.existsSync(to)) fs.copyFileSync(path.join(defaults, f), to)
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
    } else if (input.key === 'BrowserBack') {
      e.preventDefault()
      win?.webContents.navigationHistory.goBack()
    } else if (input.key === 'F5') {
      e.preventDefault()
      win?.webContents.reload()
    }
  })
  win.on('app-command', (_e, cmd) => {
    if (cmd === 'browser-backward') win?.webContents.navigationHistory.goBack()
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
  const play = {
    url: abs(req.url),
    title: String(req.title || 'BingeBandit'),
    start: Number(req.start) || 0,
    audio: typeof req.audio === 'number' && req.audio > 0 ? req.audio : /** @type {'auto'} */ ('auto'),
    sub: typeof req.sub === 'number' && req.sub > 0 ? req.sub : /** @type {'no'} */ ('no'),
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
  if (!fromServer(e) || !player) return
  switch (name) {
    case 'seek':
      return player.send(['seek', Number(arg) || 0, 'absolute'])
    case 'pause':
      return player.send(['set_property', 'pause', !!arg])
    case 'osd':
      return player.send(['show-text', String(arg), 4000])
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
  })
  app.on('window-all-closed', () => app.quit())
}
