/**
 * Browser transport for AgentMux's headless serve mode.
 *
 * The desktop app talks to Go through the Wails webview bridge. In a plain
 * browser — a tablet opening `agentmux --serve` — the same two primitives are
 * carried over HTTP instead: calls as POSTs to /api/call, events as one shared
 * Server-Sent Events stream. api.ts switches between the two; nothing above it
 * knows which transport it is on.
 */

/**
 * True when running inside the Wails webview — regardless of what page it is
 * showing. This is about the native shell, not the transport.
 *
 * Checking `window._wails` is the obvious and wrong way: the Wails JS runtime
 * creates it wherever it is imported, browser included. What only the real
 * webview has is the native message channel the runtime posts to — WebView2's
 * `chrome.webview` on Windows, WebKit's `messageHandlers.external` on macOS
 * and Linux — the same probe the runtime itself uses.
 */
export const isShellDesktop =
  typeof window !== 'undefined' &&
  Boolean(
    (window as { chrome?: { webview?: { postMessage?: unknown } } }).chrome?.webview
      ?.postMessage ??
      (
        window as {
          webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } }
        }
      ).webkit?.messageHandlers?.external?.postMessage,
  )

const TOKEN_KEY = 'agentmux.token'
// Where the Android shell's launch page lives, so Settings can send the user
// back there to pick the other connection mode.
const SHELL_KEY = 'agentmux.shell'
// The desktop app's loopback control URL, handed over when its window is
// pointed at a remote serve. Its presence is also what tells this bundle it
// was served remotely: the Wails message channel still exists in that webview,
// but the Go it reaches is not the server this page came from.
const BACK_KEY = 'agentmux.back'
// The remote serve's real address, for the page whose own origin is not it —
// a pinned self-signed serve is reached through a loopback proxy, and
// window.location would name the proxy.
const RADDR_KEY = 'agentmux.raddr'

// A native shell hands markers over in the hash on first navigation:
// http://127.0.0.1:8642/#token=…&shell=… — they are claimed into storage and
// scrubbed from the address before anything else. The other hash params (a
// detached tab's 'd') stay untouched. The Wails-served page never carries any
// of these, so running the claim unconditionally is safe.
if (typeof window !== 'undefined') {
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  let changed = false
  for (const [param, key] of [
    ['token', TOKEN_KEY],
    ['shell', SHELL_KEY],
    ['back', BACK_KEY],
    ['raddr', RADDR_KEY],
  ] as const) {
    const handed = params.get(param)
    if (handed) {
      localStorage.setItem(key, handed)
      params.delete(param)
      changed = true
    }
  }
  if (changed) {
    const rest = params.toString()
    window.history.replaceState(null, '', window.location.pathname + (rest ? '#' + rest : ''))
  }
}

/** The Android shell's launch-page origin, or '' outside that shell. */
export function getShellOrigin(): string {
  try {
    return localStorage.getItem(SHELL_KEY) ?? ''
  } catch {
    return ''
  }
}

/** The desktop app's loopback control URL, or '' when not served into it. */
export function getBackURL(): string {
  try {
    return localStorage.getItem(BACK_KEY) ?? ''
  } catch {
    return ''
  }
}

/** The remote serve's real address when this page reaches it via a proxy. */
export function getRemoteAddr(): string {
  try {
    return localStorage.getItem(RADDR_KEY) ?? ''
  } catch {
    return ''
  }
}

/**
 * True when the Go behind the Wails bridge is the one that served this page —
 * the desktop app showing its own UI. A desktop window pointed at a remote
 * `agentmux --serve` still has the native channel, but its calls must travel
 * over HTTP to the origin like any browser's; the claimed back-URL marks that
 * case.
 */
export const isDesktop = isShellDesktop && !getBackURL()

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
let attempts = 0
let reopenTimer = 0

/**
 * Fired when the stream comes back after having been lost.
 *
 * Events that happened in the meantime are gone — the server broadcasts to
 * whoever is listening at the time and keeps nothing — so anything showing
 * live state has to ask for it again rather than carry on from a picture that
 * stopped being true.
 */
export const STREAM_RESUMED = 'agentmux:stream-resumed'

/**
 * Lazily opens the shared event stream, and keeps it open.
 *
 * EventSource retries by itself while it believes the connection is merely
 * interrupted, but a connection cut abruptly — a phone that slept, a network
 * that changed underneath it — can put it in CLOSED, which is final. Nothing
 * was watching for that: the stream stayed dead, and terminals went quiet
 * while every other part of the app went on saying the host was connected.
 * That is the worst shape a failure can take, because it does not look like
 * one.
 */
function ensureStream() {
  if (source) return
  // Mark the slot synchronously so concurrent subscribers do not each open a
  // stream while the ticket is in flight.
  source = PENDING
  void openStream(++streamGen)
}

// A sentinel occupying the source slot while the ticket round-trip runs, and
// a generation stamp so an open that was superseded mid-flight (a token
// change, a forced reconnect) parks instead of opening a second stream.
const PENDING = {} as EventSource
let streamGen = 0

/**
 * Opens the stream, authenticated by a single-use ticket rather than the
 * token: EventSource cannot send headers, and the long-lived token does not
 * belong in a URL that proxies log. An older server that does not mint
 * tickets gets the token in the query the way it always did.
 */
async function openStream(gen: number) {
  let auth = `token=${encodeURIComponent(getToken())}`
  try {
    const res = await fetch('/api/stream-ticket', {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}` },
    })
    if (res.status === 401) {
      authRequired()
      // Fall through with the token: it will be refused the same way, and the
      // stream's own retry path handles the wait for a fresh one.
    } else if (res.ok) {
      const body = (await res.json()) as { ticket?: string }
      if (body.ticket) auth = `ticket=${encodeURIComponent(body.ticket)}`
    }
  } catch {
    // Ticket endpoint unreachable — the stream open below will fail and
    // schedule its own retry; nothing extra to do here.
  }
  if (gen !== streamGen || source !== PENDING) return
  const es = new EventSource(`/api/events?${auth}`)
  source = es

  es.onopen = () => {
    if (attempts === 0) return
    attempts = 0
    // Only after a real gap: a first connection has nothing to catch up on.
    window.dispatchEvent(new Event(STREAM_RESUMED))
  }
  es.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data as string) as { name: string; data: unknown }
      listeners.get(msg.name)?.forEach((cb) => cb(msg.data))
    } catch {
      // A malformed frame is dropped; the next one stands alone.
    }
  }
  es.onerror = () => {
    // Still CONNECTING means the browser is retrying on its own terms, which
    // is the case this does not need to improve on.
    if (es.readyState !== EventSource.CLOSED) return
    es.close()
    if (source === es) source = null
    scheduleReopen()
  }
}

/**
 * Opens the stream again, backing off up to fifteen seconds.
 *
 * The backoff matters for the case that cannot be told apart from here: a
 * token the server no longer accepts answers 401, which reaches JavaScript as
 * the same silent error as a dropped cable. Retrying slowly costs a request a
 * quarter of a minute and gets the connection back the moment it can be.
 */
function scheduleReopen() {
  window.clearTimeout(reopenTimer)
  const wait = Math.min(15000, 500 * 2 ** attempts)
  attempts++
  reopenTimer = window.setTimeout(() => {
    if (listeners.size > 0) ensureStream()
  }, wait)
}

/** Re-opens the stream after the token changes. */
export function reconnectStream() {
  window.clearTimeout(reopenTimer)
  attempts = 0
  if (source && source !== PENDING) source.close()
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
