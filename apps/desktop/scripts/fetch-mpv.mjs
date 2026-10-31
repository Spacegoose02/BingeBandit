// Downloads a Windows build of mpv into ./mpv, where the installer picks it
// up. Builds come from shinchiro/mpv-winbuild-cmake (the ones mpv.io links
// to). Needs 7-Zip (`7z`) on PATH, which GitHub's Windows runners have.
//
//   node scripts/fetch-mpv.mjs           newest build
//   MPV_RELEASE=20261012 node ...        a given release tag
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = 'shinchiro/mpv-winbuild-cmake'
const out = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'mpv')
const headers = { Accept: 'application/vnd.github+json', 'User-Agent': 'bingebandit-desktop' }
if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`

const tag = process.env.MPV_RELEASE
const res = await fetch(`https://api.github.com/repos/${repo}/releases/${tag ? `tags/${tag}` : 'latest'}`, { headers })
if (!res.ok) throw new Error(`GitHub said ${res.status} for ${repo}'s releases`)
const release = await res.json()
// mpv-x86_64-<date>-git-<hash>.7z: the plain 64-bit player (not -v3, which
// needs AVX2, and not mpv-dev, which is the library).
const asset = release.assets.find((a) => /^mpv-x86_64-\d{8}-git-[0-9a-f]+\.7z$/.test(a.name))
if (!asset) throw new Error(`No 64-bit mpv in ${repo} ${release.tag_name}: ${release.assets.map((a) => a.name).join(', ')}`)

console.log(`Downloading ${asset.name}`)
const file = path.join(os.tmpdir(), asset.name)
const dl = await fetch(asset.browser_download_url, { headers: { 'User-Agent': headers['User-Agent'] } })
if (!dl.ok) throw new Error(`Download failed: ${dl.status}`)
fs.writeFileSync(file, Buffer.from(await dl.arrayBuffer()))

fs.rmSync(out, { recursive: true, force: true })
fs.mkdirSync(out, { recursive: true })
execFileSync('7z', ['x', '-y', `-o${out}`, file], { stdio: 'inherit' })
fs.rmSync(file, { force: true })
if (!fs.existsSync(path.join(out, 'mpv.exe'))) throw new Error('mpv.exe is not in the download')
fs.writeFileSync(path.join(out, 'VERSION.txt'), `${asset.name}\nhttps://github.com/${repo}/releases/tag/${release.tag_name}\n`)
console.log(`mpv is in ${out}`)
