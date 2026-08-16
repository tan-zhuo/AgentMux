import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { DetachedApp } from './DetachedApp.tsx'
import './index.css'

// A window opened by tearing a tab out carries its handover token in the hash.
// Everything else is the main window.
const detachToken = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('d')

createRoot(document.getElementById('root')!).render(
  <StrictMode>{detachToken ? <DetachedApp token={detachToken} /> : <App />}</StrictMode>,
)
