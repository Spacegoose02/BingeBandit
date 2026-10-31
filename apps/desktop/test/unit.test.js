const test = require('node:test')
const assert = require('node:assert/strict')
const { mpvArgs } = require('../src/mpv')
const { addressCandidates } = require('../src/discovery')

test('mpv args carry the start, tracks, sign-in and file', () => {
  const args = mpvArgs(
    { url: 'http://s:8484/api/files/1/direct', title: 'Show S01E02', start: 61.7, audio: 2, sub: 'no', subUrl: undefined },
    { ipc: '\\\\.\\pipe\\x', headers: ['X-Requested-With: BingeBandit', 'Authorization: Bearer abc'], configDir: 'C:\\cfg', fullscreen: true },
  )
  assert.ok(args.includes('--start=61'))
  assert.ok(args.includes('--aid=2'))
  assert.ok(args.includes('--sid=no'))
  assert.ok(args.includes('--fullscreen'))
  assert.ok(args.includes('--http-header-fields=X-Requested-With: BingeBandit,Authorization: Bearer abc'))
  assert.deepEqual(args.slice(-2), ['--', 'http://s:8484/api/files/1/direct'])
})

test('a sidecar subtitle starts with mpv subtitles off', () => {
  const args = mpvArgs(
    { url: 'u', title: 't', start: 0, audio: 'auto', sub: 3, subUrl: { url: 's', title: '', lang: 'en' } },
    { ipc: 'p', headers: [], configDir: 'c', fullscreen: false },
  )
  assert.ok(args.includes('--sid=no'))
  assert.ok(!args.includes('--fullscreen'))
  assert.ok(!args.some((a) => a.startsWith('--http-header-fields')))
})

test('typed addresses', () => {
  assert.deepEqual(addressCandidates('192.168.1.10'), ['http://192.168.1.10:8484', 'http://192.168.1.10'])
  assert.deepEqual(addressCandidates('tv.example.com/link'), ['https://tv.example.com', 'http://tv.example.com:8484', 'http://tv.example.com'])
  assert.deepEqual(addressCandidates('http://nas:8484/'), ['http://nas:8484'])
  assert.deepEqual(addressCandidates('nas:9000'), ['http://nas:9000', 'https://nas:9000'])
  assert.deepEqual(addressCandidates('  '), [])
})
