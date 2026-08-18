import { Clipboard } from '@wailsio/runtime'
import { ClipboardPaste, Copy, Eraser, Search, TextSelect } from 'lucide-react'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Terminal } from '@xterm/xterm'
import { useEffect, useRef } from 'react'
import { errText, on, terminal as termApi } from '../lib/api'
import type { ShellInfo } from '../lib/types'
import { useAppStore, type Tab } from '../store/useAppStore'
import { openContextMenu, separator } from '../store/useContextMenu'
import { useT } from '../store/useI18n'
import { useTheme } from '../store/useTheme'
import { Button } from './ui'

const encoder = new TextEncoder()

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

  const setTabState = useAppStore((s) => s.setTabState)
  const toast = useAppStore((s) => s.toast)
  const terminalTheme = useTheme((s) => s.theme.terminal)

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
    term.open(hostRef.current)

    termRef.current = term
    fitRef.current = fit
    searchRef.current = search

    // Forward keystrokes. onBinary carries bytes xterm could not represent as
    // UTF-8 text (mouse reports and the like).
    term.onData((data) => {
      const id = shellIdRef.current
      if (id) void termApi.write(id, toBase64(data)).catch(() => {})
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
        disposersRef.current.push(offData, offExit)

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

  // Repaint the terminal when the app theme changes.
  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = terminalTheme
  }, [terminalTheme])

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

  function terminalMenu(e: React.MouseEvent) {
    const term = termRef.current
    const selection = term?.getSelection() ?? ''
    openContextMenu(e, [
      {
        label: t('term.copy'),
        icon: Copy,
        hint: 'Ctrl+Shift+C',
        disabled: !selection,
        onSelect: () => void Clipboard.SetText(selection),
      },
      {
        label: t('term.paste'),
        icon: ClipboardPaste,
        hint: 'Ctrl+Shift+V',
        disabled: !shellIdRef.current,
        onSelect: async () => {
          const text = await Clipboard.Text()
          const id = shellIdRef.current
          if (text && id) await termApi.write(id, toBase64(text))
        },
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
    <div className="relative h-full w-full bg-ink-950" onContextMenu={terminalMenu}>
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
