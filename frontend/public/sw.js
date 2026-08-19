// Minimal service worker: its presence is what makes the served app
// installable to a home screen. Everything is fetched from the network —
// AgentMux is a live control plane, and showing stale state would be worse
// than showing a connection error.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()))
self.addEventListener('fetch', (e) => {
  e.respondWith(fetch(e.request))
})
