// @ts-check
const { spawn } = require('node:child_process')
const { EventEmitter } = require('node:events')
const fs = require('node:fs')
const net = require('node:net')
const os = require('node:os')
const path = require('node:path')

/**
 * @typedef {object} PlayRequest
 * @property {string} url full address of the file
 * @property {string} title shown in the window's title bar
 * @property {number} start seconds into the file
 * @property {number | 'auto'} audio mpv audio track id (1-based among the file's audio tracks)
 * @property {number | 'no'} sub mpv subtitle track id, or 'no'; ignored when subUrl is set
 * @property {{ url: string, title: string, lang: string } | undefined} subUrl a sidecar subtitle file to load and show
 */

let pipes = 0

/** A fresh IPC address for one mpv process: a named pipe on Windows, a socket elsewhere. */
function ipcPath() {
  const name = `bingebandit-mpv-${process.pid}-${++pipes}`
  return process.platform === 'win32' ? `\\\\.\\pipe\\${name}` : path.join(os.tmpdir(), `${name}.sock`)
}

/**
 * The command line for a player started on `req`. Options that change per
 * file (start, tracks) are set again before each later file, see `load`.
 * @param {PlayRequest} req
 * @param {{ ipc: string, headers: string[], configDir: string, fullscreen: boolean }} o
 */
function mpvArgs(req, o) {
  const args = [
    `--config-dir=${o.configDir}`,
    `--input-ipc-server=${o.ipc}`,
    '--force-window=immediate',
    // Stay open after a file ends, so the next episode plays in the same window.
    '--idle=yes',
    '--keep-open=no',
    '--no-terminal',
    '--user-agent=BingeBandit-Desktop',
    `--title=${req.title}`,
    `--start=${Math.max(0, Math.floor(req.start))}`,
    `--aid=${req.audio}`,
    `--sid=${req.subUrl ? 'no' : req.sub}`,
  ]
  if (o.headers.length) args.push(`--http-header-fields=${o.headers.join(',')}`)
  if (o.fullscreen) args.push('--fullscreen')
  args.push('--', req.url)
  return args
}

/** Where mpv is: a bundled copy first, then one the person set, then PATH. */
function findMpv(resourcesPath, configured) {
  const exe = process.platform === 'win32' ? 'mpv.exe' : 'mpv'
  const candidates = [
    configured,
    resourcesPath && path.join(resourcesPath, 'mpv', exe),
    path.join(__dirname, '..', 'mpv', exe), // `npm run fetch-mpv` while developing
  ]
  for (const c of candidates) if (c && fs.existsSync(c)) return c
  return exe
}

/**
 * One running mpv. Emits 'event' with plain objects for the page:
 *   { type: 'time', position, duration, paused }  about once a second while playing, and on pause/seek
 *   { type: 'end' }                                 the file played to its end (mpv stays open, idle)
 *   { type: 'key', name }                           a BingeBandit key from input.conf (bb-enter, bb-next)
 *   { type: 'closed', position }                    mpv quit (Back, closing the window, or stop())
 *   { type: 'error', message }
 */
class Mpv extends EventEmitter {
  /**
   * @param {string} binary
   * @param {PlayRequest} req
   * @param {{ headers: string[], configDir: string, fullscreen: boolean }} o
   */
  constructor(binary, req, o) {
    super()
    this.ipc = ipcPath()
    this.position = req.start
    this.duration = 0
    this.paused = false
    this.closed = false
    // Times aren't sent until the file has started at its resume point, so a
    // 0 from before the first seek never reaches the server.
    this.ready = false
    this.lastSent = 0
    this.nextId = 1
    /** @type {Map<number, (r: any) => void>} */
    this.waiting = new Map()
    this.pendingSub = req.subUrl
    /** @type {net.Socket | null} */
    this.sock = null
    this.proc = spawn(binary, mpvArgs(req, { ...o, ipc: this.ipc }), { stdio: 'ignore', windowsHide: false })
    this.proc.on('error', (err) => {
      this.emit('event', { type: 'error', message: `Couldn't start mpv (${binary}): ${err.message}` })
      this.finish()
    })
    this.proc.on('exit', () => this.finish())
    this.connect(0)
  }

  /** mpv makes its pipe a moment after starting, so keep trying for a few seconds. */
  connect(attempt) {
    if (this.closed) return
    const sock = net.connect(this.ipc)
    sock.once('connect', () => {
      this.sock = sock
      let buf = ''
      sock.setEncoding('utf8')
      sock.on('data', (d) => {
        buf += d
        let nl
        while ((nl = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, nl)
          buf = buf.slice(nl + 1)
          if (line.trim()) this.onMessage(line)
        }
      })
      sock.on('error', () => undefined)
      this.send(['observe_property', 1, 'time-pos'])
      this.send(['observe_property', 2, 'pause'])
      this.send(['observe_property', 3, 'duration'])
      // The file may already be playing (its first playback-restart came
      // before this connection): then it's past the resume seek.
      this.send(['get_property', 'seeking']).then((r) => {
        if (r?.error === 'success' && r.data === false && !this.ready) {
          this.ready = true
          this.sendTime()
        }
      })
    })
    sock.once('error', () => {
      sock.destroy()
      if (attempt < 50) setTimeout(() => this.connect(attempt + 1), 100)
      else this.emit('event', { type: 'error', message: "Couldn't talk to mpv." })
    })
  }

  onMessage(line) {
    let m
    try {
      m = JSON.parse(line)
    } catch {
      return
    }
    if (m.request_id && this.waiting.has(m.request_id)) {
      this.waiting.get(m.request_id)?.(m)
      this.waiting.delete(m.request_id)
      return
    }
    switch (m.event) {
      case 'property-change':
        if (m.name === 'time-pos' && typeof m.data === 'number') {
          this.position = m.data
          if (Date.now() - this.lastSent >= 1000) this.sendTime()
        } else if (m.name === 'pause') {
          this.paused = !!m.data
          this.sendTime()
        } else if (m.name === 'duration' && typeof m.data === 'number') {
          this.duration = m.data
        }
        break
      case 'file-loaded':
        if (this.pendingSub) {
          const s = this.pendingSub
          this.pendingSub = undefined
          this.send(['sub-add', s.url, 'select', s.title, s.lang])
        }
        break
      case 'playback-restart': // started, or done seeking
        this.ready = true
        this.sendTime()
        break
      case 'end-file':
        if (m.reason === 'eof') this.emit('event', { type: 'end' })
        else if (m.reason === 'error') this.emit('event', { type: 'error', message: `mpv couldn't play this file (${m.file_error || 'error'}).` })
        break
      case 'client-message':
        if (Array.isArray(m.args) && typeof m.args[0] === 'string' && m.args[0].startsWith('bb-')) {
          this.emit('event', { type: 'key', name: m.args[0] })
        }
        break
    }
  }

  sendTime() {
    if (!this.ready) return
    this.lastSent = Date.now()
    this.emit('event', { type: 'time', position: this.position, duration: this.duration, paused: this.paused })
  }

  /** @param {unknown[]} command */
  send(command) {
    if (!this.sock || this.closed) return Promise.resolve(null)
    const id = this.nextId++
    return new Promise((resolve) => {
      this.waiting.set(id, resolve)
      this.sock?.write(JSON.stringify({ command, request_id: id }) + '\n')
    })
  }

  /**
   * Plays another file in the same window (the next episode).
   * @param {PlayRequest} req
   */
  async load(req) {
    this.ready = false
    this.position = req.start
    this.pendingSub = req.subUrl
    await this.send(['set_property', 'start', String(Math.max(0, Math.floor(req.start)))])
    await this.send(['set_property', 'aid', String(req.audio)])
    await this.send(['set_property', 'sid', req.subUrl ? 'no' : String(req.sub)])
    await this.send(['set_property', 'title', req.title])
    await this.send(['loadfile', req.url, 'replace'])
  }

  quit() {
    if (this.closed) return
    this.send(['quit'])
    // A player that doesn't answer still goes.
    setTimeout(() => !this.closed && this.proc.kill(), 3000)
  }

  finish() {
    if (this.closed) return
    this.closed = true
    this.sock?.destroy()
    if (process.platform !== 'win32') fs.rm(this.ipc, { force: true }, () => undefined)
    this.emit('event', { type: 'closed', position: this.position })
  }
}

module.exports = { Mpv, mpvArgs, findMpv }
