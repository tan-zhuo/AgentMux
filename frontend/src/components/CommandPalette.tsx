import clsx from 'clsx'
import {
  Activity,
  Bot,
  Search,
  Expand,
  FolderTree,
  Laptop,
  Minimize2,
  Monitor,
  Palette,
  Play,
  Plus,
  Radio,
  Rows2,
  Server,
  Sparkles,
  SplitSquareHorizontal,
  TerminalSquare,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { agents as agentApi, errText } from '../lib/api'
import { themes } from '../lib/themes'
import { isDesktopKind, isLocalKind } from '../lib/types'
import { MAX_PANES, refreshServerAgents, useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'
import { useTheme } from '../store/useTheme'
import { usePresence } from '../lib/motion'
import type { MsgKey } from '../lib/i18n'
import { Kbd } from './ui'

type Section = 'actions' | 'themes' | 'hosts' | 'agents'

const sectionLabel: Record<Section, MsgKey> = {
  actions: 'palette.section.actions',
  themes: 'palette.section.themes',
  hosts: 'palette.section.hosts',
  agents: 'palette.section.agents',
}

interface Command {
  id: string
  label: string
  hint?: string
  icon: typeof Bot
  /** Which heading the row sits under. The list is long, and a heading tells
   *  the eye whether it is looking at something to do or somewhere to go. */
  section: Section
  run: () => void | Promise<void>
}

/** Cmd/Ctrl+K palette. With hundreds of projects, typing a name beats hunting
 *  through the tree. */
export function CommandPalette() {
  const live = useAppStore((s) => s.paletteOpen)
  const { value: held, exiting } = usePresence(live ? true : null)
  const open = held === true
  const setOpen = useAppStore((s) => s.setPaletteOpen)
  const snapshot = useAppStore((s) => s.snapshot)
  const openTab = useAppStore((s) => s.openTab)
  const select = useAppStore((s) => s.select)
  const setRightPanel = useAppStore((s) => s.setRightPanel)
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)
  const setTheme = useTheme((s) => s.setTheme)
  const t = useT()

  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const listRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (live) {
      setQuery('')
      setCursor(0)
    }
  }, [live])

  const commands = useMemo<Command[]>(() => {
    const wsById = new Map(snapshot.workspaces.map((w) => [w.id, w]))
    const out: Command[] = [
      {
        id: 'new-server',
        label: t('server.addHost'),
        hint: t('palette.addHost.hint'),
        icon: Plus,
        section: 'actions',
        run: () => openDialog({ kind: 'server' }),
      },
      {
        id: 'new-project',
        label: t('project.new'),
        icon: Plus,
        section: 'actions',
        run: () => openDialog({ kind: 'project' }),
      },
      {
        id: 'new-workspace',
        label: t('workspace.new'),
        icon: Plus,
        section: 'actions',
        run: () => openDialog({ kind: 'workspace' }),
      },
      {
        id: 'new-agent',
        label: t('agent.new'),
        icon: Plus,
        section: 'actions',
        run: () => openDialog({ kind: 'agent' }),
      },
      {
        id: 'add-local-host',
        label: t('palette.addLocalHost'),
        hint: t('palette.addLocalHost.hint'),
        icon: Laptop,
        section: 'actions',
        run: () => openDialog({ kind: 'server' }),
      },
      {
        id: 'split-pane',
        label: t('palette.addPane'),
        hint: t('palette.addPane.hint', { key: '⌘\\' }),
        icon: SplitSquareHorizontal,
        section: 'actions',
        run: () => {
          const s = useAppStore.getState()
          if (s.paneIds.length >= MAX_PANES) {
            s.toast('info', t('toast.paneLimit', { n: MAX_PANES }))
          } else {
            openDialog({ kind: 'split' })
          }
        },
      },
      {
        id: 'close-pane',
        label: t('palette.closePane'),
        hint: '⇧⌘\\',
        icon: Minimize2,
        section: 'actions',
        run: () => {
          const s = useAppStore.getState()
          if (s.activeTabId) s.closePane(s.activeTabId)
        },
      },
      {
        id: 'zoom-pane',
        label: t('palette.zoomPane'),
        hint: t('palette.zoomPane.hint', { key: '⇧⌘↵' }),
        icon: Expand,
        section: 'actions',
        run: () => useAppStore.getState().toggleZoom(),
      },
      {
        id: 'flip-split',
        label: t('palette.flipSplit'),
        icon: Rows2,
        section: 'actions',
        run: () => {
          const s = useAppStore.getState()
          s.setSplitAxis(s.splitAxis === 'cols' ? 'rows' : 'cols')
        },
      },
      {
        id: 'broadcast',
        label: t('palette.broadcast'),
        icon: Radio,
        section: 'actions',
        run: () => setRightPanel('broadcast'),
      },
    ]

    for (const theme of themes) {
      out.push({
        id: `theme:${theme.id}`,
        label: t('palette.theme', { name: theme.name }),
        hint: t(theme.blurb),
        icon: Palette,
        section: 'themes',
        run: () => setTheme(theme.id),
      })
    }

    for (const s of snapshot.servers) {
      if (isDesktopKind(s.kind)) {
        // Everything else here asks the host a question through its shell.
        // This one has none, so it is offered the only thing it can do.
        out.push({
          id: `desktop:${s.id}`,
          label: t('desktop.title', { name: s.name }),
          // Not "through the SSH connection": this one has none.
          hint: t('palette.desktop.hintDirect'),
          icon: Monitor,
          section: 'hosts',
          run: () => {
            openTab({
              title: s.name,
              kind: 'desktop',
              serverId: s.id,
              workspaceId: '',
              agentId: '',
              tmuxSession: '',
              command: `${s.desktopOs === 'windows' ? 'rdp' : 'vnc'}:${s.port || (s.desktopOs === 'windows' ? 3389 : 5900)}`,
            })
          },
        })
        continue
      }
      out.push({
        id: `toolkit:${s.id}`,
        label: t('palette.toolkit', { name: s.name }),
        hint: t('palette.toolkit.hint'),
        icon: Sparkles,
        section: 'hosts',
        run: () => {
          select({ kind: 'server', id: s.id })
          setRightPanel('toolkit')
        },
      })
      out.push({
        id: `files:${s.id}`,
        label: t('palette.files', { name: s.name }),
        hint: t('palette.files.hint'),
        icon: FolderTree,
        section: 'hosts',
        run: () => {
          openTab({
            title: s.name,
            kind: 'files',
            serverId: s.id,
            workspaceId: '',
            agentId: '',
            tmuxSession: '',
            command: '',
          })
        },
      })
      out.push({
        id: `metrics:${s.id}`,
        label: t('palette.metrics', { name: s.name }),
        hint: t('palette.metrics.hint'),
        icon: Activity,
        section: 'hosts',
        run: () => {
          select({ kind: 'server', id: s.id })
          setRightPanel('metrics')
        },
      })
      // Not gated on the desktop app any more: the viewer runs in a pane
      // here, which a browser and a phone have as much as a window does.
      if (!isLocalKind(s.kind)) {
        out.push({
          id: `desktop:${s.id}`,
          label: t('desktop.title', { name: s.name }),
          hint: t('palette.desktop.hint'),
          icon: Monitor,
          section: 'hosts',
          run: () => openDialog({ kind: 'desktop', server: s }),
        })
      }
      out.push({
        id: `shell:${s.id}`,
        label: t('palette.shell', { name: s.name }),
        hint:
          s.kind === 'local'
            ? t('tree.thisComputer')
            : s.kind === 'localwin'
            ? t('tree.thisComputerWin')
            : `${s.username}@${s.host}`,
        icon: Server,
        section: 'hosts',
        run: () => {
          openTab({
            title: s.name,
            kind: 'shell',
            serverId: s.id,
            workspaceId: '',
            agentId: '',
            tmuxSession: '',
          })
        },
      })
    }

    for (const a of snapshot.agents) {
      const ws = wsById.get(a.workspaceId)
      if (!ws) continue
      out.push({
        id: `attach:${a.id}`,
        label: t('palette.attach', { name: a.name }),
        hint: a.tmuxSession,
        icon: TerminalSquare,
        section: 'agents',
        run: () => {
          openTab({
            title: a.name,
            kind: 'agent',
            serverId: ws.serverId,
            workspaceId: ws.id,
            agentId: a.id,
            tmuxSession: a.tmuxSession,
          })
        },
      })
      out.push({
        id: `start:${a.id}`,
        label: t('palette.start', { name: a.name }),
        hint: a.command,
        icon: Play,
        section: 'agents',
        run: async () => {
          try {
            await agentApi.start(a.id)
            await refreshServerAgents(ws.serverId)
            toast('ok', t('toast.agentStartedShort', { name: a.name }))
          } catch (e) {
            toast('error', errText(e))
          }
        },
      })
      out.push({
        id: `select:${a.id}`,
        label: t('palette.goto', { name: a.name }),
        hint: ws.name,
        icon: Bot,
        section: 'agents',
        run: () => select({ kind: 'agent', id: a.id }),
      })
    }

    return out
  }, [snapshot, openDialog, openTab, select, setRightPanel, setTheme, toast, t])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return commands.slice(0, 40)
    return commands
      .filter((c) => `${c.label} ${c.hint ?? ''}`.toLowerCase().includes(q))
      .slice(0, 40)
  }, [commands, query])

  useEffect(() => {
    const el = listRef.current?.querySelectorAll<HTMLElement>('[data-palette-row]')[cursor]
    el?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  if (!open) return null

  return (
    <div
      className={clsx(
        'fixed inset-0 z-60 flex items-start justify-center bg-black/40 p-3 pt-10 backdrop-blur-[3px] sm:p-24',
        // Inert while it fades, like the sheet below: Escape and then a click
        // on the status bar should reach the status bar.
        exiting ? 'animate-fade-out pointer-events-none' : 'animate-fade-in',
      )}
      onClick={() => setOpen(false)}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        className={clsx(
          'material flex max-h-[60vh] w-full max-w-xl flex-col overflow-hidden rounded-sheet shadow-sheet',
          exiting ? 'animate-sheet-out pointer-events-none' : 'animate-sheet-in',
        )}
      >
        {/* Spotlight's field: a glyph on the left, large type, and nothing
            else on the line. The strip itself is the field — no inset box. */}
        <div className="flex items-center gap-3 border-b hairline px-4">
          <Search size={17} className="shrink-0 text-ink-500" strokeWidth={2.2} />
          <input
            autoFocus
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setCursor(0)
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setCursor((c) => Math.min(c + 1, filtered.length - 1))
              } else if (e.key === 'ArrowUp') {
                e.preventDefault()
                setCursor((c) => Math.max(c - 1, 0))
              } else if (e.key === 'Enter') {
                e.preventDefault()
                const cmd = filtered[cursor]
                if (cmd) {
                  setOpen(false)
                  void cmd.run()
                }
              } else if (e.key === 'Escape') {
                setOpen(false)
              }
            }}
            placeholder={t('palette.placeholder')}
            spellCheck={false}
            autoComplete="off"
            className="h-12 min-w-0 flex-1 bg-transparent text-[15px] text-ink-100 outline-none placeholder:text-ink-500"
          />
          {query && (
            <button
              type="button"
              onClick={() => {
                setQuery('')
                setCursor(0)
              }}
              aria-label={t('tree.clear')}
              className="pressable flex h-4 w-4 items-center justify-center rounded-capsule bg-ink-600 text-ink-900 hover:bg-ink-500 touch:relative touch:before:absolute touch:before:-inset-2.5"
            >
              <X size={10} strokeWidth={3} />
            </button>
          )}
        </div>
        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto px-2 py-1.5">
          {filtered.map((c, i) => {
            const Icon = c.icon
            const heading = i === 0 || filtered[i - 1].section !== c.section
            return (
              <div key={c.id} data-palette-row>
                {heading && (
                  <p
                    className={clsx(
                      'px-2.5 pb-1 text-[10.5px] font-semibold tracking-wide text-ink-500 uppercase',
                      i === 0 ? 'pt-1' : 'pt-2.5',
                    )}
                  >
                    {t(sectionLabel[c.section])}
                  </p>
                )}
                <button
                  onMouseEnter={() => setCursor(i)}
                  onClick={() => {
                    setOpen(false)
                    void c.run()
                  }}
                  className={clsx(
                    // Spotlight fills the row under the cursor rather than
                    // tinting it, which is what makes the selection legible at a
                    // glance while the list is scrolling under the keyboard.
                    'flex h-8 w-full items-center gap-2.5 rounded-[8px] px-2 text-left text-xs',
                    // A list row for a finger: 44px, with the type and the
                    // glyph tile a step larger so the row does not look empty.
                    'touch:h-11 touch:rounded-[10px] touch:text-[13px]',
                    'transition-colors duration-[var(--duration-quick)]',
                    i === cursor ? 'bg-accent text-white' : 'text-ink-200',
                  )}
                >
                  <span
                    className={clsx(
                      'flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-[6px] touch:h-7 touch:w-7',
                      i === cursor ? 'bg-white/18 text-white' : 'bg-ink-800 text-ink-400',
                    )}
                  >
                    <Icon size={13} />
                  </span>
                  <span className="shrink-0 font-medium">{c.label}</span>
                  {c.hint && (
                    <span
                      className={clsx(
                        'min-w-0 flex-1 truncate text-[11px] touch:text-xs',
                        i === cursor ? 'text-white/70' : 'text-ink-500',
                      )}
                    >
                      {c.hint}
                    </span>
                  )}
                </button>
              </div>
            )
          })}
          {!filtered.length && (
            <p className="animate-rise-in px-3 py-4 text-center text-xs text-ink-500">
              {t('palette.noMatches')}
            </p>
          )}
        </div>
        {/* The key legend macOS puts at the foot of a palette. Only where
            there is a keyboard: a phone has nothing to press. */}
        <div className="hidden h-8 shrink-0 items-center gap-4 border-t hairline px-4 text-[10.5px] text-ink-500 sm:flex">
          <span className="flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd>
            <span className="ml-0.5">{t('palette.hint.navigate')}</span>
          </span>
          <span className="flex items-center gap-1">
            <Kbd>↵</Kbd>
            <span className="ml-0.5">{t('palette.hint.open')}</span>
          </span>
          <span className="flex items-center gap-1">
            <Kbd>esc</Kbd>
            <span className="ml-0.5">{t('palette.hint.close')}</span>
          </span>
          <span className="flex-1" />
          <span className="tabular-nums">{filtered.length}</span>
        </div>
      </div>
    </div>
  )
}
