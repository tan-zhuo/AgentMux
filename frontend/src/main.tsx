import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { DetachedApp } from './DetachedApp.tsx'
import { ErrorBoundary } from './components/ErrorBoundary.tsx'
import { TokenGate } from './components/TokenGate.tsx'
import { getShellOrigin, isDesktop } from './lib/webTransport.ts'
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

// The service worker exists to make the page installable, which the Android
// shell already is — and inside its WebView a service worker is a hazard: its
// network requests bypass the shell's certificate-trust hook, so a pinned
// self-signed serve would break the moment the worker takes over fetches.
if (!isDesktop && !getShellOrigin() && 'serviceWorker' in navigator && window.isSecureContext) {
  // Registration can be refused (a self-signed origin the user clicked
  // through, a browser policy); the app is whole without it.
  navigator.serviceWorker.register('/sw.js').catch(() => {})
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>{root}</ErrorBoundary>
  </StrictMode>,
)
