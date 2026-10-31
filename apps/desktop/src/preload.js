// @ts-check
const { contextBridge, ipcRenderer } = require('electron')

// The bridge between the web app and the desktop app. Only the configured
// server's pages get the player (main.js checks the sender again); the
// local setup page gets the server finder.

if (location.protocol === 'file:') {
  contextBridge.exposeInMainWorld('bbSetup', {
    probe: (input) => ipcRenderer.invoke('setup:probe', input),
    scan: () => ipcRenderer.invoke('setup:scan'),
    use: (url) => ipcRenderer.invoke('setup:use', url),
  })
} else if (location.origin === ipcRenderer.sendSync('bb:origin')) {
  contextBridge.exposeInMainWorld('bbDesktop', {
    player: 'mpv',
    play: (req) => ipcRenderer.invoke('player:play', req),
    command: (name, arg) => ipcRenderer.invoke('player:command', name, arg),
    release: () => ipcRenderer.invoke('player:release'),
    onEvent: (cb) => {
      const listener = (_e, ev) => cb(ev)
      ipcRenderer.on('player:event', listener)
      return () => ipcRenderer.removeListener('player:event', listener)
    },
  })
}
