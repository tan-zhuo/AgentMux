import { ClipboardPaste, Copy, Eraser, Search, TextSelect } from 'lucide-react'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Terminal } from '@xterm/xterm'
import { useEffect, useRef } from 'react'
import { errText, on, terminal as termApi } from '../lib/api'
import { STREAM_RESUMED } from '../lib/webTransport'
import { copyText, readText } from '../lib/clipboard'
import { applyMods } from '../lib/termKeys'
import type { ShellInfo } from '../lib/types'
import { useAppStore, type Tab } from '../store/useAppStore'
import { openContextMenu, separator } from '../store/useContextMenu'
import { useT } from '../store/useI18n'
import {
  anyModArmed,
  clearKeySink,
  currentMods,
  setKeySink,
  useTermKeys,
  type KeySink,
} from '../store/useTermKeys'
import { useTheme } from '../store/useTheme'
import { Button } from './ui'

const encoder = new TextEncoder()

// ⌘C is what a Mac keyboard reaches for; everywhere else the terminal
// convention is Ctrl+Shift+C, because Ctrl+C is spoken for — it is the
// interrupt, and a terminal that copied instead would be a broken terminal.
const isMac =
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
const copyHint = isMac ? '⌘C' : 'Ctrl+Shift+C'
const pasteHint = isMac ? '⌘V' : 'Ctrl+Shift+V'

function toBase64(text: string): string {
  const bytes = encoder.encode(text)
  let binary = ''
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i])
  return btoa(binary)
}

function fromBase64(b64: string): Uint8Array {
  const binary = atob(b64)
  const out = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
  return out
}

/**
 * xterm ships no touch handling at all — its gesture module is never wired
 * up — so on a phone the terminal would not scroll.
 *
 * Where a drag goes depends on what the terminal is showing. Scrollback lives
 * in xterm's own virtual viewport, which nothing in the page can scroll from
 * the outside: it moves for a real wheel and ignores a synthesised one, so
 * those drags are handed to `scrollLines` instead, in whole rows with the
 * remainder carried to the next frame. A full-screen program — vim, less, an
 * agent's UI — has no scrollback to move, and there a scroll means arrow keys
 * or a mouse report, which xterm's wheel handling already produces from an
 * event the page makes itself. A little inertia after the finger lifts,
 * because that is what a finger expects.
 *
 * `blocked` is select mode: there, the same drag is drawing a selection, and a
 * pane that scrolled at the same time would never let a finger reach the end
 * of what it was selecting.
 */
function enableTouchScroll(
  host: HTMLElement,
  term: () => Terminal | null,
  blocked: () => boolean,
): () => void {
  let lastX = 0
  let lastY = 0
  let lastT = 0
  let velocity = 0
  let target: Element = host
  let raf = 0
  // One row in CSS pixels, measured when the finger lands, and the fraction of
  // a row left over from the last frame.
  let rowPx = 16
  let carry = 0

  const scrollBy = (dy: number) => {
    const t = term()
    if (!t) return
    if (t.buffer.active.type === 'alternate') {
      target.dispatchEvent(
        new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: dy, deltaMode: 0 }),
      )
      return
    }
    carry += dy / rowPx
    const lines = Math.trunc(carry)
    if (!lines) return
    carry -= lines
    t.scrollLines(lines)
  }
  const glide = () => {
    velocity *= 0.94
    if (Math.abs(velocity) < 0.7) return
    scrollBy(velocity)
    raf = requestAnimationFrame(glide)
  }
  const onStart = (e: TouchEvent) => {
    cancelAnimationFrame(raf)
    velocity = 0
    carry = 0
    if (blocked() || e.touches.length !== 1) return
    lastX = e.touches[0].clientX
    lastY = e.touches[0].clientY
    lastT = e.timeStamp
    if (e.target instanceof Element) target = e.target
    const row = host.querySelector('.xterm-rows > div')
    const h = row?.getBoundingClientRect().height ?? 0
    if (h > 0) rowPx = h
  }
  const onMove = (e: TouchEvent) => {
    if (blocked() || e.touches.length !== 1) return
    const dx = lastX - e.touches[0].clientX
    const dy = lastY - e.touches[0].clientY
    lastX = e.touches[0].clientX
    lastY = e.touches[0].clientY
    // A mostly-horizontal drag is not a scroll; leave it to whoever wants it.
    if (dy === 0 || Math.abs(dy) < Math.abs(dx)) return
    e.preventDefault()
    const dt = Math.max(1, e.timeStamp - lastT)
    lastT = e.timeStamp
    velocity = (dy / dt) * 16 // px per 60fps frame, for the glide
    scrollBy(dy)
  }
  const onEnd = () => {
    if (Math.abs(velocity) > 2) raf = requestAnimationFrame(glide)
  }

  host.addEventListener('touchstart', onStart, { passive: true })
  host.addEventListener('touchmove', onMove, { passive: false })
  host.addEventListener('touchend', onEnd)
  return () => {
    cancelAnimationFrame(raf)
    host.removeEventListener('touchstart', onStart)
    host.removeEventListener('touchmove', onMove)
    host.removeEventListener('touchend', onEnd)
  }
}

/**
 * Select text with a finger.
 *
 * xterm draws its own selection from mouse events, and its rows are marked
 * `user-select: none`, so a touch screen has nothing to drag with: no mouse,
 * and the browser's own selection turned off. Touches become mouse events on
 * the element xterm listens to.
 *
 * Shift is held for exactly one case. When a full-screen program has asked for
 * the mouse — vim, tmux, an agent's UI, which is where copying is hardest —
 * Shift is xterm's "select anyway" and the only way to get a selection at all.
 * Everywhere else a shift-drag means "extend the selection I already have",
 * and with nothing selected yet it does nothing whatsoever. Which mode the
 * terminal is in is on the element: xterm marks it `enable-mouse-events`.
 */
function enableTouchSelect(host: HTMLElement): () => void {
  const screen: Element = host.querySelector('.xterm-screen') ?? host
  const xterm = host.querySelector('.xterm')
  // Decided once per drag, at the press: a program that turns mouse reporting
  // on mid-drag must not change what this gesture means half way through.
  let force = false

  const send = (type: 'mousedown' | 'mousemove' | 'mouseup', t: Touch, buttons: number) => {
    screen.dispatchEvent(
      new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        view: window,
        detail: 1,
        button: 0,
        buttons,
        clientX: t.clientX,
        clientY: t.clientY,
        shiftKey: force,
      }),
    )
  }

  // Every one of these cancels its default: an uncancelled touch becomes the
  // WebView's own tap — scrolling, a text magnifier, and the keyboard coming
  // back up over the very text being selected.
  const onStart = (e: TouchEvent) => {
    if (e.touches.length !== 1) return
    e.preventDefault()
    force = xterm?.classList.contains('enable-mouse-events') ?? false
    send('mousedown', e.touches[0], 1)
  }
  const onMove = (e: TouchEvent) => {
    if (e.touches.length !== 1) return
    e.preventDefault()
    send('mousemove', e.touches[0], 1)
  }
  const onEnd = (e: TouchEvent) => {
    const t = e.changedTouches[0]
    if (!t) return
    send('mouseup', t, 0)
  }

  host.addEventListener('touchstart', onStart, { passive: false })
  host.addEventListener('touchmove', onMove, { passive: false })
  host.addEventListener('touchend', onEnd)
  host.addEventListener('touchcancel', onEnd)
  return () => {
    host.removeEventListener('touchstart', onStart)
    host.removeEventListener('touchmove', onMove)
    host.removeEventListener('touchend', onEnd)
    host.removeEventListener('touchcancel', onEnd)
  }
}

/**
 * One xterm instance bound to one backend PTY. Every open tab keeps its
 * instance mounted (hidden when inactive) so switching tabs never loses
 * scrollback or forces a reattach.
 */
export function TerminalPane({ tab, active }: { tab: Tab; active: boolean }) {
  const t = useT()
  const hostRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const searchRef = useRef<SearchAddon | null>(null)
  const shellIdRef = useRef<string | undefined>(undefined)
  const disposersRef = useRef<Array<() => void>>([])
  // Attaching is async and flips tab.status while it runs, so the effect that
  // starts it must not treat its own re-render as an unmount. These two refs
  // separate "the component went away" from "the status changed".
  const mountedRef = useRef(true)
  const attachingRef = useRef(false)
  // Read inside the touch handlers, which are bound once and outlive any
  // render: a ref is what they can see.
  const selectingRef = useRef(false)
  // The key bar copies through the sink, which is registered once per focus
  // change; this keeps it pointed at the current render's copy.
  const copyRef = useRef<() => Promise<void>>(async () => {})
  const pasteRef = useRef<() => Promise<void>>(async () => {})
  // The selection as it stood the instant a click arrived, captured before
  // anything the engine does with that click can drop it. Some webviews clear
  // xterm's selection on the right-click that opens the menu, which left the
  // menu's Copy pointing at nothing — and on a desktop that menu is the whole
  // of copying.
  const clickedSelectionRef = useRef('')

  const setTabState = useAppStore((s) => s.setTabState)
  const toast = useAppStore((s) => s.toast)
  const terminalTheme = useTheme((s) => s.theme.terminal)
  // Select mode belongs to whichever pane has focus; an inactive pane ignores it.
  const selectMode = useTermKeys((s) => s.selecting) && active

  // Create the xterm instance once per tab.
  useEffect(() => {
    if (!hostRef.current) return
    const term = new Terminal({
      fontFamily:
        "'JetBrains Mono', 'Cascadia Code', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 12.5,
      lineHeight: 1.25,
      cursorBlink: true,
      allowProposedApi: true,
      scrollback: 20000,
      theme: useTheme.getState().theme.terminal,
    })
    const fit = new FitAddon()
    const search = new SearchAddon()
    term.loadAddon(fit)
    term.loadAddon(search)
    term.loadAddon(new WebLinksAddon())
    // The clipboard shortcuts. xterm has no opinion about them — it forwards
    // the keystroke to the shell like any other — so the menu's Ctrl+Shift+C
    // was a promise nothing kept: on a desktop the right-click menu was the
    // only way to copy anything out of a terminal.
    term.attachCustomKeyEventHandler((e) => {
      if (e.type !== 'keydown') return true
      const combo = isMac ? e.metaKey && !e.ctrlKey && !e.altKey : e.ctrlKey && e.shiftKey && !e.altKey
      if (!combo) return true
      const key = e.key.toLowerCase()
      if (key === 'c') {
        // Nothing selected is not this shortcut's business; on a Mac ⌘C then
        // means whatever the platform wants it to mean.
        if (!term.hasSelection()) return true
        e.preventDefault()
        void copyRef.current()
        return false
      }
      if (key === 'v') {
        e.preventDefault()
        void pasteRef.current()
        return false
      }
      return true
    })
    term.open(hostRef.current)
    const offTouch = enableTouchScroll(
      hostRef.current,
      () => termRef.current,
      () => selectingRef.current,
    )

    termRef.current = term
    fitRef.current = fit
    searchRef.current = search

    // Forward keystrokes. onBinary carries bytes xterm could not represent as
    // UTF-8 text (mouse reports and the like).
    term.onData((data) => {
      const id = shellIdRef.current
      if (!id) return
      // A phone has no Ctrl key, so the key bar arms one and the next character
      // typed on the software keyboard becomes the control code. It has to
      // happen here rather than on key events: Android delivers letters through
      // composition, and what arrives as a keydown is often nothing at all.
      let out = data
      if (anyModArmed()) {
        out = applyMods(data, currentMods())
        useTermKeys.getState().spendOnce()
      }
      void termApi.write(id, toBase64(out)).catch(() => {})
    })
    term.onBinary((data) => {
      const id = shellIdRef.current
      if (!id) return
      let binary = ''
      for (let i = 0; i < data.length; i++) binary += data[i]
      void termApi.write(id, btoa(binary)).catch(() => {})
    })

    let tellFar = 0
    const observer = new ResizeObserver(() => {
      // A hidden pane reports zero size; fitting then would collapse the PTY.
      if (!hostRef.current?.clientWidth || !hostRef.current?.clientHeight) return
      try {
        fit.fit()
      } catch {
        /* xterm not ready */
      }
      // Refit locally on every frame, but tell the far end once the size settles:
      // dragging a pane seam or a side panel resizes continuously, and a resize
      // per frame per pane on screen is a burst of round trips for geometry that
      // is about to change again. The last one always lands.
      window.clearTimeout(tellFar)
      tellFar = window.setTimeout(() => {
        const id = shellIdRef.current
        if (id) void termApi.resize(id, term.cols, term.rows).catch(() => {})
      }, 90)
    })
    observer.observe(hostRef.current)

    return () => {
      mountedRef.current = false
      window.clearTimeout(tellFar)
      offTouch()
      observer.disconnect()
      for (const d of disposersRef.current) d()
      disposersRef.current = []
      term.dispose()
      termRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Attach the backend PTY once, then stream into the terminal.
  useEffect(() => {
    if (tab.status !== 'pending' || attachingRef.current) return

    async function attach() {
      const term = termRef.current
      const fit = fitRef.current
      if (!term || !fit) return

      attachingRef.current = true
      // Drop listeners from a previous attach so reattaching does not
      // accumulate subscriptions for dead shell ids.
      for (const d of disposersRef.current) d()
      disposersRef.current = []
      setTabState(tab.id, { status: 'opening', error: undefined })
      try {
        if (hostRef.current?.clientWidth) fit.fit()
      } catch {
        /* not laid out yet */
      }
      const cols = term.cols || 120
      const rows = term.rows || 32

      try {
        let info: ShellInfo
        if (tab.adoptShellId) {
          // The PTY is already open — this pane is taking over a session that
          // was started in another window. Opening a second one would leave the
          // first orphaned and the agent talking to nobody.
          const live = await termApi.list()
          const existing = live.find((s) => s.id === tab.adoptShellId)
          if (!existing) {
            throw new Error('that terminal session has already ended')
          }
          info = existing
          await termApi.resize(info.id, cols, rows).catch(() => {})
        } else {
          switch (tab.kind) {
            case 'agent':
              info = await termApi.attachAgent(tab.agentId, cols, rows)
              break
            case 'tmux':
              info = await termApi.attachTmux(tab.serverId, tab.tmuxSession, cols, rows)
              break
            case 'command':
              info = await termApi.openCommand(tab.serverId, tab.command ?? '', cols, rows)
              break
            default:
              info = tab.workspaceId
                ? await termApi.openWorkspace(tab.workspaceId, cols, rows)
                : await termApi.openShell(tab.serverId, cols, rows)
          }
        }
        if (!mountedRef.current) {
          // The tab closed while the connection was still being made; do not
          // leak the PTY we just opened.
          void termApi.close(info.id).catch(() => {})
          return
        }

        shellIdRef.current = info.id
        setTabState(tab.id, { shellId: info.id, status: 'open', error: undefined })

        const offData = on<{ id: string; b64: string }>(`term:data:${info.id}`, (d) => {
          termRef.current?.write(fromBase64(d.b64))
        })
        const offExit = on<{ id: string; reason: string }>(`term:exit:${info.id}`, (d) => {
          shellIdRef.current = undefined
          setTabState(tab.id, { status: 'closed', shellId: undefined, error: d.reason })
          termRef.current?.write(`\r\n\x1b[38;5;245m── ${d.reason} ──\x1b[0m\r\n`)
        })
        // A dropped connection is put back by the backend, under the same shell
        // id — the terminal, its scrollback and this pane all stay as they are.
        // What the tab shows is the difference between "working" and "on its
        // way back", which is the pulse the opening state already draws. The
        // backend writes the explanation into the terminal itself.
        const offRetry = on<{ id: string; attempt: number; of: number; ok: boolean; detail: string }>(
          `term:reconnect:${info.id}`,
          (d) => {
            setTabState(
              tab.id,
              d.ok
                ? { status: 'open', error: undefined }
                : { status: 'opening', error: d.detail },
            )
          },
        )
        disposersRef.current.push(offData, offExit, offRetry)

        // Replay anything the backend buffered between open and subscribe.
        try {
          const b64 = await termApi.scrollback(info.id)
          if (b64) termRef.current?.write(fromBase64(b64))
        } catch {
          /* nothing buffered */
        }
      } catch (e) {
        if (!mountedRef.current) return
        const msg = errText(e)
        setTabState(tab.id, { status: 'error', error: msg })
        termRef.current?.write(`\r\n\x1b[38;5;203m${msg}\x1b[0m\r\n`)
        toast('error', msg)
      } finally {
        attachingRef.current = false
      }
    }

    void attach()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.status])

  // Select mode: a finger draws a selection instead of scrolling the pane.
  useEffect(() => {
    selectingRef.current = selectMode
    if (!selectMode || !hostRef.current) return
    const off = enableTouchSelect(hostRef.current)
    return () => {
      off()
      selectingRef.current = false
      // Leaving with a highlight still painted reads as a selection that is
      // still live, when the next tap would already have replaced it.
      termRef.current?.clearSelection()
    }
  }, [selectMode])

  // Catch up after the event stream has been away.
  //
  // Output that arrived while it was down was broadcast to nobody and is not
  // kept, so carrying on would leave a hole in the middle of the scrollback
  // with nothing to mark it. The backend holds the last stretch of every
  // shell's output for exactly this: the pane throws away what it has and
  // paints that instead, which is the one version known to be true.
  useEffect(() => {
    const resync = () => {
      const id = shellIdRef.current
      if (!id) return
      void termApi
        .scrollback(id)
        .then((b64) => {
          if (!b64 || !termRef.current) return
          termRef.current.reset()
          termRef.current.write(fromBase64(b64))
        })
        .catch(() => {
          /* the shell is gone; the exit event says so on its own */
        })
    }
    window.addEventListener(STREAM_RESUMED, resync)
    return () => window.removeEventListener(STREAM_RESUMED, resync)
  }, [])

  // Repaint the terminal when the app theme changes.
  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = terminalTheme
  }, [terminalTheme])

  // While this pane has focus, the on-screen key bar types into it.
  useEffect(() => {
    if (!active) return
    const own: KeySink = {
      send: (data) => {
        const id = shellIdRef.current
        if (id) void termApi.write(id, toBase64(data)).catch(() => {})
      },
      focus: () => termRef.current?.focus(),
      blur: () => termRef.current?.blur(),
      selectAll: () => termRef.current?.selectAll(),
      copySelection: () => void copyRef.current(),
    }
    setKeySink(own)
    return () => clearKeySink(own)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active])

  // Re-fit and focus when this pane becomes visible.
  useEffect(() => {
    if (!active) return
    const t = window.setTimeout(() => {
      try {
        fitRef.current?.fit()
      } catch {
        /* ignore */
      }
      const term = termRef.current
      const id = shellIdRef.current
      if (term && id) void termApi.resize(id, term.cols, term.rows).catch(() => {})
      term?.focus()
    }, 30)
    return () => window.clearTimeout(t)
  }, [active])

  const dead = tab.status === 'closed' || tab.status === 'error'

  // One copy path for the menu, the select-mode bar and every platform: the
  // desktop's native clipboard, a browser's, or the old execCommand on a LAN
  // address that is not a secure context.
  async function copySelection(explicit?: string) {
    const text = explicit ?? termRef.current?.getSelection() ?? ''
    if (!text) {
      toast('info', t('term.select.empty'))
      return
    }
    if (await copyText(text)) {
      toast('ok', t('term.copied', { n: text.length }))
      useTermKeys.getState().setSelecting(false)
    } else {
      toast('error', t('term.copyFailed'))
    }
  }

  copyRef.current = copySelection

  async function pasteIntoTerminal() {
    const id = shellIdRef.current
    if (!id) return
    const text = await readText()
    if (!text) {
      // Android has no clipboard-read permission to grant, so this is where a
      // paste ends on a phone — and the keyboard's own paste still works.
      toast('info', t('term.pasteUnavailable'))
      return
    }
    await termApi.write(id, toBase64(text))
  }

  pasteRef.current = pasteIntoTerminal

  function terminalMenu(e: React.MouseEvent) {
    const term = termRef.current
    const selection = term?.getSelection() || clickedSelectionRef.current
    openContextMenu(e, [
      {
        label: t('term.copy'),
        icon: Copy,
        hint: copyHint,
        disabled: !selection,
        // The text, not the live selection: by the time this runs the click
        // that opened the menu may have taken the selection with it.
        onSelect: () => void copySelection(selection),
      },
      {
        label: t('term.paste'),
        icon: ClipboardPaste,
        hint: pasteHint,
        disabled: !shellIdRef.current,
        onSelect: () => void pasteIntoTerminal(),
      },
      {
        label: t('term.selectAll'),
        icon: TextSelect,
        onSelect: () => term?.selectAll(),
      },
      separator,
      {
        label: t('term.clear'),
        icon: Eraser,
        onSelect: () => term?.clear(),
      },
      {
        label: t('term.find'),
        icon: Search,
        onSelect: () => {
          const needle = window.prompt(t('term.find.prompt'))
          if (needle) searchRef.current?.findNext(needle)
        },
      },
    ])
  }

  return (
    <div
      className="relative h-full w-full bg-ink-950"
      // Capture phase: ahead of xterm's own listeners and of whatever the
      // webview does with a right-click.
      // Unconditional, so a click that follows a cleared selection replaces
      // the remembered text rather than leaving a stale one behind.
      onMouseDownCapture={() => {
        clickedSelectionRef.current = termRef.current?.getSelection() ?? ''
      }}
      onContextMenu={terminalMenu}
    >
      <div ref={hostRef} className="h-full w-full px-2 py-1.5" />
      {dead && (
        <div className="absolute inset-x-0 bottom-0 flex items-center justify-between gap-3 border-t hairline bg-ink-850 px-3 py-2">
          <span className="truncate text-[11px] text-ink-300">
            {tab.kind === 'shell'
              ? t('term.sessionEnded')
              : t('term.detached')}
            {tab.error ? ` ${tab.error}` : ''}
          </span>
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              termRef.current?.clear()
              setTabState(tab.id, { status: 'pending', error: undefined })
            }}
          >
            {t('term.reattach')}
          </Button>
        </div>
      )}
    </div>
  )
}
