/**
 * Browser transport for AgentMux's headless serve mode.
 *
 * The desktop app talks to Go through the Wails webview bridge. In a plain
 * browser — a tablet opening `agentmux --serve` — the same two primitives are
 * carried over HTTP instead: calls as POSTs to /api/call, events as one shared
 * Server-Sent Events stream. api.ts switches between the two; nothing above it
 * knows which transport it is on.
 */

/** True when running inside the Wails webview rather than a plain browser. */
export const isDesktop = typeof window !== 'undefined' && '_wails' in window

const TOKEN_KEY = 'agentmux.token'

// A native shell (the Android app with its embedded core) hands the token
// over in the hash on first navigation: http://127.0.0.1:8642/#token=…
// It is claimed into storage and scrubbed from the address before anything
// else — the other hash params (a detached tab's 'd') stay untouched.
if (!isDesktop && typeof window !== 'undefined') {
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const handed = params.get('token')
  if (handed) {
    localStorage.setItem(TOKEN_KEY, handed)
    params.delete('token')
    const rest = params.toString()
    window.history.replaceState(null, '', window.location.pathname + (rest ? '#' + rest : ''))
  }
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

/** Validates a token against the server before storing it. */
export async function verifyToken(token: string): Promise<boolean> {
  const res = await fetch('/api/auth', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
  return res.ok
}

/** Fired when the server rejects the stored token, so the gate can reappear. */
export const AUTH_EVENT = 'agentmux:auth-required'

function authRequired() {
  window.dispatchEvent(new Event(AUTH_EVENT))
}

/** One RPC over HTTP, mirroring Wails Call.ByName semantics. */
export async function httpCall<T>(name: string, args: unknown[]): Promise<T> {
  const res = await fetch('/api/call', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getToken()}`,
    },
    body: JSON.stringify({ name, args }),
  })
  if (res.status === 401) {
    authRequired()
    throw new Error('not authorized')
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null
    throw new Error(body?.error ?? `${name} failed (${res.status})`)
  }
  return (await res.json()) as T
}

type Listener = (data: unknown) => void

const listeners = new Map<string, Set<Listener>>()
let source: EventSource | null = null

/**
 * Lazily opens the shared event stream. EventSource reconnects on its own,
 * which is exactly what a tablet waking from sleep needs; the only case
 * handled by hand is an auth failure, where reconnecting would loop forever.
 */
function ensureStream() {
  if (source) return
  source = new EventSource(`/api/events?token=${encodeURIComponent(getToken())}`)
  source.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data as string) as { name: string; data: unknown }
      listeners.get(msg.name)?.forEach((cb) => cb(msg.data))
    } catch {
      // A malformed frame is dropped; the next one stands alone.
    }
  }
}

/** Re-opens the stream after the token changes. */
export function reconnectStream() {
  source?.close()
  source = null
  if (listeners.size > 0) ensureStream()
}

/** Subscribe to a backend event over the stream. Returns an unsubscribe. */
export function sseOn<T>(event: string, cb: (data: T) => void): () => void {
  ensureStream()
  let set = listeners.get(event)
  if (!set) {
    set = new Set()
    listeners.set(event, set)
  }
  const listener = cb as Listener
  set.add(listener)
  return () => {
    set.delete(listener)
    if (set.size === 0) listeners.delete(event)
  }
}
