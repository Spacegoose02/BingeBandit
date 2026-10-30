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
