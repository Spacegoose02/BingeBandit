// Plays a short generated file through a real mpv, signed in like the app
// signs in. Skipped where mpv or ffmpeg isn't installed.
const test = require('node:test')
const assert = require('node:assert/strict')
const { execFileSync } = require('node:child_process')
const fs = require('node:fs')
const http = require('node:http')
const os = require('node:os')
const path = require('node:path')
const { Mpv } = require('../src/mpv')

const has = (bin, flag) => {
  try {
    execFileSync(bin, [flag], { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

test('plays, reports time, ends and quits', { skip: !(has('mpv', '--version') && has('ffmpeg', '-version')) && 'needs mpv and ffmpeg', timeout: 30000 }, async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bb-mpv-'))
  const file = path.join(dir, 'clip.mkv')
  execFileSync('ffmpeg', ['-loglevel', 'error', '-f', 'lavfi', '-i', 'testsrc=duration=3:size=160x120:rate=25', '-f', 'lavfi', '-i', 'sine=duration=3', '-shortest', file])
  // No window or sound while testing.
  fs.writeFileSync(path.join(dir, 'mpv.conf'), 'vo=null\nao=null\n')
  const auths = []
  const paths = []
  const server = http.createServer((req, res) => {
    auths.push(req.headers.authorization)
    paths.push(req.url)
    if (req.headers.authorization !== 'Bearer t0k') return res.writeHead(401).end()
    if (req.url.endsWith('.vtt')) return res.writeHead(200, { 'Content-Type': 'text/vtt' }).end('WEBVTT\n\n00:00.000 --> 00:02.000\nHello\n')
    // Byte ranges, like the server's direct play, so mpv can seek.
    const data = fs.readFileSync(file)
    const m = /bytes=(\d+)-(\d*)/.exec(req.headers.range ?? '')
    if (!m) return res.writeHead(200, { 'Accept-Ranges': 'bytes', 'Content-Length': data.length }).end(data)
    const from = Number(m[1])
    const to = m[2] ? Number(m[2]) : data.length - 1
    res.writeHead(206, { 'Accept-Ranges': 'bytes', 'Content-Range': `bytes ${from}-${to}/${data.length}`, 'Content-Length': to - from + 1 })
    res.end(data.subarray(from, to + 1))
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const url = `http://127.0.0.1:${server.address().port}/api/files/1/direct`

  let p
  t.after(() => {
    if (p && !p.closed) p.proc.kill()
    server.closeAllConnections()
    server.close()
    fs.rmSync(dir, { recursive: true, force: true })
  })
  const events = []
  // Waits for one event; mpv quitting or failing first fails the test at once.
  const next = (type) =>
    new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`no ${type} in 10 s: ${JSON.stringify(events)}`)), 10000)
      p.on('event', (e) => {
        if (e.type === type) {
          clearTimeout(timer)
          resolve(e)
        } else if (e.type === 'error' || e.type === 'closed') {
          clearTimeout(timer)
          reject(new Error(`mpv ${e.type} before ${type}: ${JSON.stringify(e)}`))
        }
      })
    })
  p = new Mpv('mpv', { url, title: 'Test', start: 1, audio: 'auto', sub: 'no', subUrl: undefined }, {
    headers: ['Authorization: Bearer t0k'],
    configDir: dir,
    fullscreen: false,
  })
  p.on('event', (e) => events.push(e))
  await next('end')
  const times = events.filter((e) => e.type === 'time')
  assert.ok(times.length > 0, 'time events')
  assert.ok(times[0].position >= 0.9, `started at the resume point (${times[0].position})`)
  assert.ok(times.at(-1).duration > 2.5, 'knows the length')
  assert.ok(auths.every((a) => a === 'Bearer t0k'), 'signed in')

  // The next episode plays in the same mpv, with a sidecar subtitle file.
  events.length = 0
  await p.load({ url, title: 'Next', start: 0, audio: 'auto', sub: 'no', subUrl: { url: url.replace('direct', 'subtitles/2.vtt'), title: 'English', lang: 'en' } })
  await next('end')
  assert.ok(events.some((e) => e.type === 'time' && e.position < 1), 'next file from the start')
  assert.ok(paths.some((u) => u.endsWith('/subtitles/2.vtt')), 'subtitle file loaded')

  // Idle after the end; quitting reports closed.
  p.quit()
  await next('closed')
})

// BingeBandit's controls (mpv-scripts/bingebandit.lua) in a real mpv: the
// state and menu reach them, keys come back as BingeBandit's events, and a
// pick in the menu reaches the page.
test('the controls take the page menu and send picks and keys back', { skip: !(has('mpv', '--version') && has('ffmpeg', '-version')) && 'needs mpv and ffmpeg', timeout: 30000 }, async (t) => {
  const { findUi } = require('../src/mpv')
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bb-ui-'))
  const file = path.join(dir, 'clip.mkv')
  execFileSync('ffmpeg', ['-loglevel', 'error', '-f', 'lavfi', '-i', 'testsrc=duration=20:size=160x120:rate=25', '-f', 'lavfi', '-i', 'sine=duration=20', '-shortest', file])
  fs.writeFileSync(path.join(dir, 'mpv.conf'), 'vo=null\nao=null\n')
  fs.copyFileSync(path.join(__dirname, '..', 'mpv-config', 'input.conf'), path.join(dir, 'input.conf'))
  let p
  t.after(() => {
    if (p && !p.closed) p.proc.kill()
    fs.rmSync(dir, { recursive: true, force: true })
  })
  p = new Mpv('mpv', { url: file, title: 'Test', start: 0, audio: 'auto', sub: 'no', subUrl: undefined }, {
    headers: [],
    configDir: dir,
    fullscreen: false,
    uiDir: findUi(''),
  })
  const events = []
  p.on('event', (e) => events.push(e))
  const waitFor = (pred, what) =>
    new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`no ${what} in 10 s: ${JSON.stringify(events.slice(-10))}`)), 10000)
      const check = (e) => {
        if (pred(e)) {
          clearTimeout(timer)
          p.off('event', check)
          resolve(e)
        }
      }
      p.on('event', check)
    })
  // Sent at once, before the pipe or the script is up: they arrive anyway.
  p.ui('bb-state', { mode: 'vod', title: 'Test', prompt: 'Skip intro' })
  p.ui('bb-menu', [{ title: 'Watch together', items: [{ label: 'Alex, Sam', disabled: true }, { id: 'tg:leave', label: 'Leave' }] }])
  await waitFor((e) => e.type === 'time', 'time')
  // A moment for the script to start and hear the state again.
  await new Promise((r) => setTimeout(r, 500))

  // OK goes to the page (it decides: skip, up next or pause).
  p.send(['keypress', 'ENTER'])
  await waitFor((e) => e.type === 'key' && e.name === 'bb-enter', 'bb-enter')
  // The menu starts on the first entry that can be picked; OK picks it.
  p.send(['keypress', 'TAB'])
  await new Promise((r) => setTimeout(r, 200))
  p.send(['keypress', 'ENTER'])
  const pick = await waitFor((e) => e.type === 'key' && e.name === 'bb-pick', 'bb-pick')
  assert.equal(pick.arg, 'tg:leave')
  // In live mode, up and down change channel.
  p.ui('bb-state', { mode: 'live', title: '7 News' })
  await new Promise((r) => setTimeout(r, 200))
  p.send(['keypress', 'DOWN'])
  const ch = await waitFor((e) => e.type === 'key' && e.name === 'bb-channel', 'bb-channel')
  assert.equal(ch.arg, 'down')
  // Back leaves the player.
  p.send(['keypress', 'ESC'])
  await waitFor((e) => e.type === 'closed', 'closed')
})
