import clsx from 'clsx'
import {
  Activity,
  Bot,
  FolderTree,
  Minimize2,
  Palette,
  Play,
  Plus,
  Radio,
  Rows2,
  Server,
  Sparkles,
  SplitSquareHorizontal,
  TerminalSquare,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { agents as agentApi, errText } from '../lib/api'
import { themes } from '../lib/themes'
import { MAX_PANES, refreshServerAgents, useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useTheme } from '../store/useTheme'

interface Command {
  id: string
  label: string
  hint?: string
  icon: typeof Bot
  run: () => void | Promise<void>
}

/** Cmd/Ctrl+K palette. With hundreds of projects, typing a name beats hunting
 *  through the tree. */
export function CommandPalette() {
  const open = useAppStore((s) => s.paletteOpen)
  const setOpen = useAppStore((s) => s.setPaletteOpen)
  const snapshot = useAppStore((s) => s.snapshot)
  const openTab = useAppStore((s) => s.openTab)
  const select = useAppStore((s) => s.select)
  const setRightPanel = useAppStore((s) => s.setRightPanel)
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)
  const setTheme = useTheme((s) => s.setTheme)

  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const listRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (open) {
      setQuery('')
      setCursor(0)
    }
  }, [open])

  const commands = useMemo<Command[]>(() => {
    const wsById = new Map(snapshot.workspaces.map((w) => [w.id, w]))
    const out: Command[] = [
      {
        id: 'new-server',
        label: 'Add server',
        icon: Plus,
        run: () => openDialog({ kind: 'server' }),
      },
      {
        id: 'new-project',
        label: 'New project',
        icon: Plus,
        run: () => openDialog({ kind: 'project' }),
      },
      {
        id: 'new-workspace',
        label: 'New workspace',
        icon: Plus,
        run: () => openDialog({ kind: 'workspace' }),
      },
      { id: 'new-agent', label: 'New agent', icon: Plus, run: () => openDialog({ kind: 'agent' }) },
      {
        id: 'split-pane',
        label: 'Add a pane',
        hint: '⌘\\ — a host, a workspace, an agent or an open tab',
        icon: SplitSquareHorizontal,
        run: () => {
          const s = useAppStore.getState()
          if (s.paneIds.length >= MAX_PANES) {
            s.toast('info', 'Four panes is the limit — close one before adding another')
          } else {
            openDialog({ kind: 'split' })
          }
        },
      },
      {
        id: 'close-pane',
        label: 'Close this pane',
        hint: '⇧⌘\\',
        icon: Minimize2,
        run: () => {
          const s = useAppStore.getState()
          if (s.activeTabId) s.closePane(s.activeTabId)
        },
      },
      {
        id: 'flip-split',
        label: 'Flip the split between rows and columns',
        icon: Rows2,
        run: () => {
          const s = useAppStore.getState()
          s.setSplitAxis(s.splitAxis === 'cols' ? 'rows' : 'cols')
        },
      },
      {
        id: 'broadcast',
        label: 'Open broadcast panel',
        icon: Radio,
        run: () => setRightPanel('broadcast'),
      },
    ]

    for (const t of themes) {
      out.push({
        id: `theme:${t.id}`,
        label: `Theme: ${t.name}`,
        hint: t.blurb,
        icon: Palette,
        run: () => setTheme(t.id),
      })
    }

    for (const s of snapshot.servers) {
      out.push({
        id: `toolkit:${s.id}`,
        label: `Install agent CLIs on ${s.name}`,
        hint: 'detect and install claude, codex, opencode',
        icon: Sparkles,
        run: () => {
          select({ kind: 'server', id: s.id })
          setRightPanel('toolkit')
        },
      })
      out.push({
        id: `files:${s.id}`,
        label: `Browse files on ${s.name}`,
        hint: 'SFTP: upload, download, rename, delete',
        icon: FolderTree,
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
        label: `Metrics for ${s.name}`,
        hint: 'CPU, memory, disks, network, GPU',
        icon: Activity,
        run: () => {
          select({ kind: 'server', id: s.id })
          setRightPanel('metrics')
        },
      })
      out.push({
        id: `shell:${s.id}`,
        label: `Shell on ${s.name}`,
        hint: `${s.username}@${s.host}`,
        icon: Server,
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
        label: `Attach ${a.name}`,
        hint: a.tmuxSession,
        icon: TerminalSquare,
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
        label: `Start ${a.name}`,
        hint: a.command,
        icon: Play,
        run: async () => {
          try {
            await agentApi.start(a.id)
            await refreshServerAgents(ws.serverId)
            toast('ok', `${a.name} started`)
          } catch (e) {
            toast('error', errText(e))
          }
        },
      })
      out.push({
        id: `select:${a.id}`,
        label: `Go to ${a.name}`,
        hint: ws.name,
        icon: Bot,
        run: () => select({ kind: 'agent', id: a.id }),
      })
    }

    return out
  }, [snapshot, openDialog, openTab, select, setRightPanel, setTheme, toast])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return commands.slice(0, 40)
    return commands
      .filter((c) => `${c.label} ${c.hint ?? ''}`.toLowerCase().includes(q))
      .slice(0, 40)
  }, [commands, query])

  useEffect(() => {
    const el = listRef.current?.children[cursor] as HTMLElement | undefined
    el?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-60 flex items-start justify-center bg-black/50 p-24 backdrop-blur-sm"
      onClick={() => setOpen(false)}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="material flex max-h-[60vh] w-full max-w-xl flex-col overflow-hidden rounded-sheet shadow-sheet"
      >
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
          placeholder="Run a command, attach an agent, open a shell"
          className="border-b hairline bg-transparent px-4 py-3 text-sm text-ink-100 outline-none placeholder:text-ink-500"
        />
        <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto py-1">
          {filtered.map((c, i) => {
            const Icon = c.icon
            return (
              <button
                key={c.id}
                onMouseEnter={() => setCursor(i)}
                onClick={() => {
                  setOpen(false)
                  void c.run()
                }}
                className={clsx(
                  // Spotlight fills the row under the cursor rather than
                  // tinting it, which is what makes the selection legible at a
                  // glance while the list is scrolling under the keyboard.
                  'mx-2 flex items-center gap-2.5 rounded-control px-2.5 py-1.5 text-left text-xs',
                  i === cursor ? 'bg-accent text-white' : 'text-ink-300',
                )}
              >
                <Icon size={13} className="shrink-0 opacity-60" />
                <span className="shrink-0">{c.label}</span>
                {c.hint && (
                  <span
                    className={clsx(
                      'min-w-0 flex-1 truncate font-mono text-[11px]',
                      i === cursor ? 'text-white/70' : 'text-ink-600',
                    )}
                  >
                    {c.hint}
                  </span>
                )}
              </button>
            )
          })}
          {!filtered.length && <p className="px-4 py-3 text-xs text-ink-500">No matches.</p>}
        </div>
      </div>
    </div>
  )
}
