import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { DetachedApp } from './DetachedApp.tsx'
import { ErrorBoundary } from './components/ErrorBoundary.tsx'
import { TokenGate } from './components/TokenGate.tsx'
import { isDesktop } from './lib/webTransport.ts'
import './index.css'

// A window opened by tearing a tab out carries its handover token in the hash.
// Everything else is the main window.
const detachToken = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('d')

// In a browser — the serve mode a tablet connects to — the app sits behind the
// access token, and the service worker makes it installable to the home screen.
const root = detachToken ? (
  <DetachedApp token={detachToken} />
) : isDesktop ? (
  <App />
) : (
  <TokenGate>
    <App />
  </TokenGate>
)

if (!isDesktop && 'serviceWorker' in navigator && window.isSecureContext) {
  void navigator.serviceWorker.register('/sw.js')
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>{root}</ErrorBoundary>
  </StrictMode>,
)
