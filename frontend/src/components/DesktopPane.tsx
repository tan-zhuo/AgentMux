import { Monitor, RotateCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { desktop as desktopApi, errText } from '../lib/api'
import { getToken } from '../lib/webTransport'
import type { DesktopInApp, DesktopProtocol } from '../lib/types'
import { useTouchDevice } from '../lib/useCompact'
import { useAppStore, type Tab } from '../store/useAppStore'
import { useT } from '../store/useI18n'
import { Button, inputClass } from './ui'

/** The endpoint a desktop tab carries, stored as "rdp:3389". */
function endpointOf(tab: Tab): { protocol: DesktopProtocol; port: number } | null {
  const [protocol, port] = (tab.command ?? '').split(':')
  if ((protocol !== 'rdp' && protocol !== 'vnc') || !Number(port)) return null
  return { protocol, port: Number(port) }
}

/**
 * Resolves what the backend handed back into a URL a socket can be opened on.
 *
 * The desktop app gets an absolute address, because its page is served from a
 * scheme nothing can be relative to. Everywhere else the answer is relative
 * and this end completes it, which is what makes one reply work for a phone on
 * the LAN, a tablet through a tunnel and a browser on the same machine.
 */
function socketURL(session: DesktopInApp): string {
  if (session.url.startsWith('ws://') || session.url.startsWith('wss://')) return session.url
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const token = encodeURIComponent(getToken())
  return `${scheme}://${window.location.host}${session.url}${token ? `&token=${token}` : ''}`
}

/**
 * A host's screen, in a pane of this app.
 *
 * The viewers are real protocol clients running in the page — noVNC for RFB,
 * IronRDP's WebAssembly build for RDP — and what this component owns is
 * everything around them: the ticket, the socket, the credentials RDP cannot
 * start without, and saying plainly what happened when a session ends.
 *
 * Both viewers are loaded on demand. The RDP one is a WebAssembly module of
 * several megabytes, and a person who never opens a desktop should never pay
 * for it.
 */
export function DesktopPane({ tab }: { tab: Tab }) {
  const t = useT()
  const touch = useTouchDevice()
  const toast = useAppStore((s) => s.toast)
  const setTabState = useAppStore((s) => s.setTabState)
  const hostRef = useRef<HTMLDivElement | null>(null)
  const teardownRef = useRef<(() => void) | null>(null)

  const endpoint = endpointOf(tab)
  const needsCredentials = endpoint?.protocol === 'rdp'

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [domain, setDomain] = useState('')
  // RDP cannot open a session without an account to open it as, so it waits;
  // VNC is asked for a password only if the server wants one.
  const [connecting, setConnecting] = useState(!needsCredentials)
  const [error, setError] = useState('')
  const [live, setLive] = useState(false)

  // Tear the session down when the tab goes away. A viewer left running would
  // hold the host's SSH connection open for a window nobody can see.
  useEffect(() => {
    return () => {
      teardownRef.current?.()
      teardownRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!connecting || !endpoint || !hostRef.current) return
    let cancelled = false
    const host = hostRef.current

    async function start() {
      // Whatever was here goes first. Reconnecting on top of a live session
      // would leave the old viewer holding the host's SSH connection with
      // nothing on screen to close it with.
      teardownRef.current?.()
      teardownRef.current = null
      setError('')
      setTabState(tab.id, { status: 'opening', error: undefined })
      try {
        const session = await desktopApi.inApp(tab.serverId, endpoint!)
        if (cancelled) return
        const url = socketURL(session)
        const stop =
          session.protocol === 'vnc'
            ? await startVNC(host, url, (e) => onEnded(e))
            : await startRDP(host, url, session, { username, password, domain }, (e) => onEnded(e))
        if (cancelled) {
          stop()
          return
        }
        teardownRef.current = stop
        setLive(true)
        setTabState(tab.id, { status: 'open', error: undefined })
      } catch (e) {
        if (cancelled) return
        onEnded(errText(e))
      }
    }

    function onEnded(reason: string) {
      if (cancelled) return
      teardownRef.current = null
      setLive(false)
      setConnecting(false)
      setError(reason)
      setTabState(tab.id, { status: reason ? 'error' : 'closed', error: reason || undefined })
      if (reason) toast('error', reason)
    }

    void start()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connecting, tab.id, tab.serverId])

  if (!endpoint) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-ink-950 p-6 text-center">
        <p className="text-[11px] text-danger">{t('desktop.pane.badEndpoint')}</p>
      </div>
    )
  }

  return (
    <div className="relative flex h-full w-full flex-col bg-black">
      <div ref={hostRef} className="min-h-0 flex-1 overflow-hidden" />

      {!live && (
        <div className="absolute inset-0 flex items-center justify-center overflow-y-auto bg-ink-950/95 p-6">
          <div className="w-full max-w-xs space-y-2.5">
            <p className="flex items-center gap-1.5 text-xs font-semibold text-ink-200">
              <Monitor size={13} /> {t('desktop.pane.title', { ep: `${endpoint.protocol.toUpperCase()}:${endpoint.port}` })}
            </p>

            {error && <p className="text-[11px] leading-relaxed text-danger">{error}</p>}

            {needsCredentials ? (
              <form
                className="space-y-2"
                onSubmit={(e) => {
                  e.preventDefault()
                  setConnecting(true)
                }}
              >
                <input
                  className={inputClass}
                  placeholder={t('desktop.pane.username')}
                  value={username}
                  autoFocus
                  onChange={(e) => setUsername(e.target.value)}
                />
                <input
                  className={inputClass}
                  type="password"
                  placeholder={t('desktop.pane.password')}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <input
                  className={inputClass}
                  placeholder={t('desktop.pane.domain')}
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                />
                <p className="text-[10.5px] leading-relaxed text-ink-500">
                  {t('desktop.pane.credentialsHint')}
                </p>
                {touch && (
                  // Said before connecting rather than discovered afterwards:
                  // the RDP client draws the screen but takes mouse and
                  // keyboard only, so a finger gets taps and little else.
                  <p className="text-[10.5px] leading-relaxed text-warn">
                    {t('desktop.pane.touchLimited')}
                  </p>
                )}
                <Button type="submit" variant="primary" className="w-full" disabled={connecting}>
                  {connecting ? t('desktop.pane.connecting') : t('desktop.pane.connect')}
                </Button>
              </form>
            ) : (
              <div className="space-y-2">
                <p className="text-[11px] text-ink-400">
                  {connecting ? t('desktop.pane.connecting') : t('desktop.pane.ended')}
                </p>
                {!connecting && (
                  <Button variant="primary" className="w-full" onClick={() => setConnecting(true)}>
                    <RotateCw size={11} /> {t('desktop.pane.reconnect')}
                  </Button>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * noVNC, pointed at the bridge.
 *
 * Its own WebSocket, its own canvas, its own touch handling — which is why the
 * phone gets a usable desktop out of this and not just a picture of one.
 */
async function startVNC(
  host: HTMLElement,
  url: string,
  onEnded: (reason: string) => void,
): Promise<() => void> {
  const { default: RFB } = await import('@novnc/novnc')

  // The socket is opened here rather than by noVNC, for one reason: when the
  // bridge refuses a session it says why in the close frame, and a viewer
  // handed a URL keeps that to itself. "Connection refused" is the difference
  // between a wrong port and a broken app.
  const socket = new WebSocket(url)
  let closed = ''
  socket.addEventListener('close', (e) => {
    if (e.reason) closed = e.reason
  })

  const rfb = new RFB(host, socket, {})
  rfb.scaleViewport = true
  rfb.clipViewport = true
  rfb.background = '#000'

  // noVNC rescales on a window resize, and a pane can change size without the
  // window doing anything — a split, a drawer, a rotated phone. Telling it
  // what it already listens for is cheaper than reaching inside it.
  const watchPane = new ResizeObserver(() => window.dispatchEvent(new Event('resize')))
  watchPane.observe(host)

  const disconnected = (e: Event) => {
    const detail = (e as CustomEvent<{ clean: boolean }>).detail
    if (detail?.clean && !closed) {
      onEnded('')
      return
    }
    onEnded(closed || 'the VNC session ended unexpectedly')
  }
  const needsPassword = () => {
    const answer = window.prompt('VNC password')
    if (answer === null) {
      rfb.disconnect()
      return
    }
    rfb.sendCredentials({ password: answer })
  }
  const failed = (e: Event) => {
    const detail = (e as CustomEvent<{ reason: string }>).detail
    onEnded(detail?.reason || 'the VNC server refused the connection')
  }
  rfb.addEventListener('disconnect', disconnected)
  rfb.addEventListener('credentialsrequired', needsPassword)
  rfb.addEventListener('securityfailure', failed)

  return () => {
    watchPane.disconnect()
    rfb.removeEventListener('disconnect', disconnected)
    rfb.removeEventListener('credentialsrequired', needsPassword)
    rfb.removeEventListener('securityfailure', failed)
    try {
      rfb.disconnect()
    } catch {
      /* already gone */
    }
  }
}

/**
 * IronRDP's web component, pointed at the same bridge.
 *
 * The element is a real RDP client compiled to WebAssembly: it does the
 * protocol itself, including the network level authentication that needs the
 * server's certificate — which is exactly why the bridge hands the certificate
 * chain over before stepping out of the way.
 */
async function startRDP(
  host: HTMLElement,
  url: string,
  session: DesktopInApp,
  creds: { username: string; password: string; domain: string },
  onEnded: (reason: string) => void,
): Promise<() => void> {
  const [, rdp] = await Promise.all([
    // Importing the component registers <iron-remote-desktop>; the backend is
    // a WebAssembly module and has to be started before anything it exports
    // can be constructed.
    import('@devolutions/iron-remote-desktop'),
    import('@devolutions/iron-remote-desktop-rdp'),
  ])
  await rdp.init('WARN')

  const el = document.createElement('iron-remote-desktop') as HTMLElement & {
    module?: unknown
  }
  el.module = rdp.Backend
  el.setAttribute('scale', 'fit')
  // A custom element is inline until told otherwise, and an inline box ignores
  // a height: without this the component lays itself out inside nothing, and
  // the session paints into a canvas the size of a rounding error.
  el.style.display = 'block'
  el.style.width = '100%'
  el.style.height = '100%'
  host.appendChild(el)

  const ready = new Promise<IronUserInteraction>((resolve) => {
    el.addEventListener(
      'ready',
      (e) => resolve((e as CustomEvent<{ irgUserInteraction: IronUserInteraction }>).detail.irgUserInteraction),
      { once: true },
    )
  })
  const ui = await ready

  // The ticket travels twice: in the URL, which is what the bridge checks, and
  // here, because the client will not build a configuration without a token.
  const ticket = new URL(url, window.location.href).searchParams.get('ticket') ?? ''
  const config = ui
    .configBuilder()
    // The display control channel, which is what lets a session follow the
    // pane's size instead of staying whatever it was when it opened.
    .withExtension(rdp.displayControl(true))
    .withDestination(session.destination)
    .withProxyAddress(url)
    .withAuthToken(ticket)
    .withUsername(creds.username)
    .withPassword(creds.password)
    .withServerDomain(creds.domain)
    .build()

  // connect resolves once the session is up, and run resolves when it ends —
  // so a failure to connect is raised to the caller, which is what puts the
  // reason in front of the person waiting, while the end of a session that did
  // start is reported later through the same callback the VNC side uses.
  const info = await ui.connect(config)
  // The component starts hidden and stays that way until a session is worth
  // showing, which is this moment.
  ui.setVisibility(true)
  ui.setScale(screenScaleFit)

  // Resizing a desktop makes the server redraw all of it, so this waits for
  // the dragging to stop rather than asking on every frame of it.
  let settle = 0
  const watchPane = new ResizeObserver(() => {
    window.clearTimeout(settle)
    settle = window.setTimeout(() => {
      const { clientWidth, clientHeight } = host
      if (clientWidth > 0 && clientHeight > 0) ui.resize(clientWidth, clientHeight)
    }, 400)
  })
  watchPane.observe(host)
  void info
    .run()
    .then(() => onEnded(''))
    .catch((e: unknown) => onEnded(errText(e)))

  return () => {
    window.clearTimeout(settle)
    watchPane.disconnect()
    try {
      ui.shutdown()
    } catch {
      /* already gone */
    }
    el.remove()
  }
}

/** The slice of IronRDP's interaction API this pane uses. */
/** ScreenScale.Fit, from the component's own enum. */
const screenScaleFit = 1

interface IronUserInteraction {
  configBuilder: () => IronConfigBuilder
  connect: (config: unknown) => Promise<{ run: () => Promise<unknown> }>
  setVisibility: (visible: boolean) => void
  setScale: (scale: number) => void
  resize: (width: number, height: number, scale?: number) => void
  shutdown: () => void
}

interface IronConfigBuilder {
  withExtension: (ext: unknown) => IronConfigBuilder
  withDestination: (v: string) => IronConfigBuilder
  withProxyAddress: (v: string) => IronConfigBuilder
  withAuthToken: (v: string) => IronConfigBuilder
  withUsername: (v: string) => IronConfigBuilder
  withPassword: (v: string) => IronConfigBuilder
  withServerDomain: (v: string) => IronConfigBuilder
  build: () => unknown
}
