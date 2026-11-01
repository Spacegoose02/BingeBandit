import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { AuthProvider } from './auth'
import App from './App'
import { ScrollRestorer } from './backState'
import './styles.css'
import { enableTvMode, tvModeOn } from './tvNav'
import { registerServiceWorker } from './push'
import { listenForInstallPrompt } from './install'

if (tvModeOn()) enableTvMode()
registerServiceWorker()
listenForInstallPrompt()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <ScrollRestorer />
      <AuthProvider>
        <App />
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
