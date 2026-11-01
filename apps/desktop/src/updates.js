// @ts-check
const { app, shell } = require('electron')

// In-app updates from the project's GitHub releases. The installed app
// downloads a new version in the background and installs it when it's next
// closed, or straight away when the person picks "Restart now". The
// portable .exe can't update itself, so it only offers the download page.
//
// The release build publishes latest.yml next to the installer
// (.github/workflows/release.yml); electron-updater reads it through the
// "publish" settings in package.json.

const RELEASES = 'https://github.com/Spacegoose02/BingeBandit/releases/latest'
const EVERY = 6 * 60 * 60 * 1000

/**
 * @typedef {{
 *   current: string,
 *   status: 'off' | 'idle' | 'checking' | 'downloading' | 'ready' | 'available' | 'error',
 *   version?: string,
 *   percent?: number,
 *   portable: boolean,
 *   message?: string,
 * }} UpdateState
 */

const portable = !!process.env.PORTABLE_EXECUTABLE_DIR
/** @type {UpdateState} */
let current = { current: app.getVersion(), status: 'off', portable }
/** @type {(s: UpdateState) => void} */
let notify = () => undefined
/** @type {import('electron-updater').AppUpdater | null} */
let updater = null

/** @param {Partial<UpdateState>} patch */
function set(patch) {
  current = { ...current, ...patch }
  notify(current)
}

/**
 * Starts checking: now and every few hours. Only in a packaged Windows
 * build (a development copy has nothing to update to).
 * @param {(s: UpdateState) => void} onChange
 */
function start(onChange) {
  notify = onChange
  if (!app.isPackaged || process.platform !== 'win32' || process.env.BB_NO_UPDATES) return
  updater = require('electron-updater').autoUpdater
  updater.autoDownload = !portable
  updater.autoInstallOnAppQuit = true
  updater.on('checking-for-update', () => current.status !== 'ready' && set({ status: 'checking' }))
  updater.on('update-not-available', () => set({ status: 'idle', version: undefined, message: undefined }))
  updater.on('update-available', (info) => set({ status: portable ? 'available' : 'downloading', version: info.version, percent: 0 }))
  updater.on('download-progress', (p) => set({ status: 'downloading', percent: Math.round(p.percent) }))
  updater.on('update-downloaded', (info) => set({ status: 'ready', version: info.version }))
  updater.on('error', (err) => {
    // A failed check is quiet (no network, a release still uploading); it's tried again later.
    if (current.status !== 'ready') set({ status: 'error', message: String(err?.message ?? err).split('\n')[0] })
  })
  set({ status: 'idle' })
  check()
  setInterval(check, EVERY).unref()
}

function check() {
  if (!updater || current.status === 'ready' || current.status === 'downloading') return current
  updater.checkForUpdates().catch(() => undefined)
  return current
}

/** Restarts into the downloaded version, or opens the download page for the portable app. */
function install() {
  if (current.status === 'ready' && updater) {
    // Silent install, then start the new version.
    setImmediate(() => updater?.quitAndInstall(true, true))
  } else if (current.status === 'available') {
    shell.openExternal(RELEASES)
  }
}

module.exports = { start, check, install, state: () => current }
