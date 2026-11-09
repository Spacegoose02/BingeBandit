// @ts-check
const { contextBridge, ipcRenderer } = require('electron')

// The bridge between the web app and the desktop app. Only the configured
// server's pages get the player (main.js checks the sender again); the
// local setup page gets the server finder.

// In-app updates, for both: state() and onChange(cb) say whether a new
// version is downloading or ready, install() restarts into it.
const updates = {
  version: ipcRenderer.sendSync('app:version'),
  state: () => ipcRenderer.invoke('update:state'),
  check: () => ipcRenderer.invoke('update:check'),
  install: () => ipcRenderer.invoke('update:install'),
  onChange: (cb) => {
    const listener = (_e, s) => cb(s)
    ipcRenderer.on('update:changed', listener)
    return () => ipcRenderer.removeListener('update:changed', listener)
  },
}

// Starting with Windows, for both: get() and set(on) answer true or false,
// or null when this copy can't (not installed, or not on Windows).
const startup = {
  get: () => ipcRenderer.invoke('startup:get'),
  set: (on) => ipcRenderer.invoke('startup:set', !!on),
}

if (location.protocol === 'file:') {
  contextBridge.exposeInMainWorld('bbSetup', {
    probe: (input) => ipcRenderer.invoke('setup:probe', input),
    scan: () => ipcRenderer.invoke('setup:scan'),
    use: (url) => ipcRenderer.invoke('setup:use', url),
    updates,
    startup,
  })
} else if (location.origin === ipcRenderer.sendSync('bb:origin')) {
  contextBridge.exposeInMainWorld('bbDesktop', {
    player: 'mpv',
    features: ['live', 'together', 'ui'],
    play: (req) => ipcRenderer.invoke('player:play', req),
    command: (name, arg) => ipcRenderer.invoke('player:command', name, arg),
    release: () => ipcRenderer.invoke('player:release'),
    onEvent: (cb) => {
      const listener = (_e, ev) => cb(ev)
      ipcRenderer.on('player:event', listener)
      return () => ipcRenderer.removeListener('player:event', listener)
    },
    updates,
    startup,
  })
}
