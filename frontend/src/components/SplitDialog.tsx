import clsx from 'clsx'
import {
  Bot,
  FileCode2,
  FolderTree,
  Layers,
  Server,
  TerminalSquare,
  type LucideIcon,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { MAX_PANES, useAppStore, type Tab } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { ConnDot, Modal, StatusDot, inputClass } from './ui'

interface Choice {
  key: string
  group: string
  label: string
  hint?: string
  icon: LucideIcon
  /** Rendered before the label when the row has live state worth showing. */
  lead?: React.ReactNode
  run: () => void
}

const kindIcon: Record<Tab['kind'], LucideIcon> = {
  shell: TerminalSquare,
  tmux: Layers,
  agent: Bot,
  command: TerminalSquare,
  files: FolderTree,
  editor: FileCode2,
}

/**
 * Asks what goes in a new pane.
 *
 * A split needs a second thing to show, and the useful answer is usually not a
 * tab that happens to be open already: it is another shell on the host being
 * worked on, the workspace directory next to it, or the agent running on the
 * box beside it. So this offers the sessions worth starting as well as the tabs
 * that exist, and everything it opens lands in a pane of its own rather than on
 * top of the pane in front.
 */
export function SplitDialog() {
  const close = useDialogs((s) => s.close)
  const snapshot = useAppStore((s) => s.snapshot)
  const connections = useAppStore((s) => s.connections)
  const tabs = useAppStore((s) => s.tabs)
  const paneIds = useAppStore((s) => s.paneIds)
  const openTab = useAppStore((s) => s.openTab)
  const splitWith = useAppStore((s) => s.splitWith)

  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const listRef = useRef<HTMLDivElement | null>(null)

  const choices = useMemo<Choice[]>(() => {
    const out: Choice[] = []
    const serverById = new Map(snapshot.servers.map((s) => [s.id, s]))
    const wsById = new Map(snapshot.workspaces.map((w) => [w.id, w]))

    // Tabs that are open but not on screen: one click and they are, with the
    // shell they are already attached to.
    for (const t of tabs) {
      if (paneIds.includes(t.id)) continue
      out.push({
        key: `tab:${t.id}`,
        group: 'Already open',
        label: t.title,
        hint: serverById.get(t.serverId)?.name ?? t.kind,
        icon: kindIcon[t.kind],
        run: () => splitWith(t.id),
      })
    }

    for (const a of snapshot.agents) {
      const ws = wsById.get(a.workspaceId)
      if (!ws) continue
      out.push({
        key: `agent:${a.id}`,
        group: 'Agents',
        label: a.name,
        // The host is in the hint so typing a host name finds its agents too.
        hint: [serverById.get(ws.serverId)?.name, a.tmuxSession].filter(Boolean).join(' · '),
        icon: Bot,
        lead: <StatusDot status={a.status} />,
        run: () =>
          openTab(
            {
              title: a.name,
              kind: 'agent',
              serverId: ws.serverId,
              workspaceId: ws.id,
              agentId: a.id,
              tmuxSession: a.tmuxSession,
            },
            { newPane: true },
          ),
      })
    }

    for (const w of snapshot.workspaces) {
      const server = serverById.get(w.serverId)
      out.push({
        key: `ws:${w.id}`,
        group: 'Shell in a workspace',
        label: w.name,
        hint: `${server ? `${server.name}:` : ''}${w.remotePath}`,
        icon: TerminalSquare,
        lead: <ConnDot connected={!!connections[w.serverId]?.connected} />,
        run: () =>
          openTab(
            {
              title: w.name,
              kind: 'shell',
              serverId: w.serverId,
              workspaceId: w.id,
              agentId: '',
              tmuxSession: '',
            },
            { newPane: true },
          ),
      })
    }

    // A second shell on a host that is already connected costs nothing: the
    // terminals are channels on the one SSH connection, so this does not
    // re-authenticate.
    for (const s of snapshot.servers) {
      out.push({
        key: `shell:${s.id}`,
        group: 'Shell on a host',
        label: s.name,
        hint:
          s.kind === 'local'
            ? 'this computer'
            : `${s.username}@${s.host}${s.port === 22 ? '' : `:${s.port}`}`,
        icon: Server,
        lead: <ConnDot connected={!!connections[s.id]?.connected} />,
        run: () =>
          openTab(
            {
              title: s.name,
              kind: 'shell',
              serverId: s.id,
              workspaceId: '',
              agentId: '',
              tmuxSession: '',
            },
            { newPane: true },
          ),
      })
    }

    // A separate pass, because rows are grouped by the order they are pushed in
    // and a file browser next to a terminal is its own kind of pane.
    for (const s of snapshot.servers) {
      out.push({
        key: `files:${s.id}`,
        group: 'Files beside a terminal',
        label: `Browse ${s.name}`,
        hint: 'SFTP',
        icon: FolderTree,
        run: () =>
          openTab(
            {
              title: s.name,
              kind: 'files',
              serverId: s.id,
              workspaceId: '',
              agentId: '',
              tmuxSession: '',
              command: '',
            },
            { newPane: true },
          ),
      })
    }

    return out
  }, [snapshot, connections, tabs, paneIds, openTab, splitWith])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return choices
    return choices.filter((c) => `${c.label} ${c.hint ?? ''} ${c.group}`.toLowerCase().includes(q))
  }, [choices, query])

  useEffect(() => {
    setCursor(0)
  }, [query])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-cursor="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [cursor, filtered])

  function choose(c: Choice | undefined) {
    if (!c) return
    close()
    c.run()
  }

  // The group header is drawn by the first row of each group, so filtering
  // never leaves a heading with nothing under it.
  let lastGroup = ''

  return (
    <Modal
      title="Add a pane"
      onClose={close}
      footer={
        <p className="mr-auto text-[11px] leading-relaxed text-ink-500">
          Pane {Math.min(paneIds.length + 1, MAX_PANES)} of {MAX_PANES}, opening beside the current
          one. <span className="font-mono">⌘\</span> skips this and splits with the next tab;{' '}
          <span className="font-mono">⇧⌘\</span> closes a pane again.
        </p>
      }
    >
      <input
        autoFocus
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setCursor((c) => Math.min(c + 1, filtered.length - 1))
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setCursor((c) => Math.max(c - 1, 0))
          } else if (e.key === 'Enter') {
            e.preventDefault()
            choose(filtered[cursor])
          }
        }}
        placeholder="A host, a workspace, an agent, or a tab already open"
        className={clsx(inputClass, 'mb-1')}
      />

      <div ref={listRef} className="-mx-1">
        {filtered.map((c, i) => {
          const Icon = c.icon
          const header = c.group !== lastGroup ? c.group : null
          lastGroup = c.group
          return (
            <div key={c.key}>
              {header && (
                <p className="px-1 pb-1 pt-2.5 text-[10px] font-semibold uppercase tracking-wide text-ink-500">
                  {header}
                </p>
              )}
              <button
                data-cursor={i === cursor}
                onMouseEnter={() => setCursor(i)}
                onClick={() => choose(c)}
                className={clsx(
                  'flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-xs',
                  i === cursor ? 'bg-accent text-white' : 'text-ink-200 hover:bg-ink-800',
                )}
              >
                {c.lead ?? <Icon size={13} className="shrink-0 opacity-60" />}
                <span className="shrink-0 truncate">{c.label}</span>
                {c.hint && (
                  <span
                    className={clsx(
                      'min-w-0 flex-1 truncate text-right font-mono text-[10.5px]',
                      i === cursor ? 'text-white/70' : 'text-ink-600',
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
          <p className="px-1 py-3 text-xs leading-relaxed text-ink-500">
            {choices.length
              ? 'Nothing matches that.'
              : 'Add a host on the left first — a pane needs something to attach to.'}
          </p>
        )}
      </div>
    </Modal>
  )
}
