import { Clipboard } from '@wailsio/runtime'
import clsx from 'clsx'
import {
  Activity,
  Bot,
  ChevronDown,
  ChevronRight,
  ClipboardCopy,
  FolderTree,
  Layers,
  Link2,
  Link2Off,
  Play,
  Plus,
  Radio,
  RotateCw,
  Search,
  Server as ServerIcon,
  Skull,
  Sparkles,
  Square,
  TerminalSquare,
  Trash2,
  Pencil,
  Zap,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { agents as agentApi, errText, servers as serverApi, tree as treeApi } from '../lib/api'
import type { Agent, Project, Server, Workspace } from '../lib/types'
import { refreshServerAgents, useAppStore } from '../store/useAppStore'
import { confirmAction } from '../store/useConfirm'
import { openContextMenu, separator } from '../store/useContextMenu'
import { useDialogs } from '../store/useDialogs'
import { Button, ConnDot, StatusDot } from './ui'

const ROW_H = 26
const OVERSCAN = 8

type Row =
  | { key: string; kind: 'section'; label: string; depth: 0; onAdd?: () => void }
  | { key: string; kind: 'project'; depth: number; project: Project; childCount: number }
  | { key: string; kind: 'workspace'; depth: number; workspace: Workspace; server?: Server }
  | { key: string; kind: 'agent'; depth: number; agent: Agent; workspace: Workspace }
  | { key: string; kind: 'server'; depth: number; server: Server }
  | { key: string; kind: 'hint'; depth: number; text: string }

export function Sidebar() {
  const snapshot = useAppStore((s) => s.snapshot)
  const connections = useAppStore((s) => s.connections)
  const search = useAppStore((s) => s.search)
  const setSearch = useAppStore((s) => s.setSearch)
  const expanded = useAppStore((s) => s.expanded)
  const toggleExpanded = useAppStore((s) => s.toggleExpanded)
  const selection = useAppStore((s) => s.selection)
  const select = useAppStore((s) => s.select)
  const openTab = useAppStore((s) => s.openTab)
  const setRightPanel = useAppStore((s) => s.setRightPanel)
  const toast = useAppStore((s) => s.toast)
  const broadcastTargets = useAppStore((s) => s.broadcastTargets)
  const toggleBroadcastTarget = useAppStore((s) => s.toggleBroadcastTarget)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const openDialog = useDialogs((s) => s.open)

  const scrollRef = useRef<HTMLDivElement | null>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [viewportH, setViewportH] = useState(600)

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setViewportH(el.clientHeight))
    ro.observe(el)
    setViewportH(el.clientHeight)
    return () => ro.disconnect()
  }, [])

  const q = search.trim().toLowerCase()

  const rows = useMemo<Row[]>(() => {
    const { projects, workspaces, servers, agents } = snapshot
    const serverById = new Map(servers.map((s) => [s.id, s]))
    const wsByProject = new Map<string, Workspace[]>()
    for (const w of workspaces) {
      const list = wsByProject.get(w.projectId) ?? []
      list.push(w)
      wsByProject.set(w.projectId, list)
    }
    const agentsByWs = new Map<string, Agent[]>()
    for (const a of agents) {
      const list = agentsByWs.get(a.workspaceId) ?? []
      list.push(a)
      agentsByWs.set(a.workspaceId, list)
    }

    const matches = (...vals: string[]) => !q || vals.some((v) => v.toLowerCase().includes(q))

    const out: Row[] = []
    out.push({
      key: 'sec-projects',
      kind: 'section',
      label: 'Projects',
      depth: 0,
      onAdd: () => openDialog({ kind: 'project' }),
    })

    for (const p of projects) {
      const wss = wsByProject.get(p.id) ?? []
      // A project stays visible when it matches, or when any descendant does.
      const visibleWss = wss.filter((w) => {
        const srv = serverById.get(w.serverId)
        const wsAgents = agentsByWs.get(w.id) ?? []
        return (
          matches(p.name) ||
          matches(w.name, w.remotePath, srv?.name ?? '') ||
          wsAgents.some((a) => matches(a.name, a.command, a.tmuxSession))
        )
      })
      if (q && !matches(p.name) && visibleWss.length === 0) continue

      const agentCount = wss.reduce((n, w) => n + (agentsByWs.get(w.id)?.length ?? 0), 0)
      out.push({ key: `p:${p.id}`, kind: 'project', depth: 0, project: p, childCount: agentCount })
      const projectOpen = q ? true : (expanded[`p:${p.id}`] ?? true)
      if (!projectOpen) continue

      const shown = q ? visibleWss : wss
      if (!shown.length) {
        out.push({ key: `p:${p.id}:empty`, kind: 'hint', depth: 1, text: 'No workspaces yet' })
      }
      for (const w of shown) {
        out.push({
          key: `w:${w.id}`,
          kind: 'workspace',
          depth: 1,
          workspace: w,
          server: serverById.get(w.serverId),
        })
        const wsOpen = q ? true : (expanded[`w:${w.id}`] ?? true)
        if (!wsOpen) continue
        for (const a of agentsByWs.get(w.id) ?? []) {
          if (q && !matches(a.name, a.command, a.tmuxSession) && !matches(w.name) && !matches(p.name))
            continue
          out.push({ key: `a:${a.id}`, kind: 'agent', depth: 2, agent: a, workspace: w })
        }
      }
    }

    out.push({
      key: 'sec-servers',
      kind: 'section',
      label: 'Servers',
      depth: 0,
      onAdd: () => openDialog({ kind: 'server' }),
    })
    for (const s of servers) {
      if (q && !matches(s.name, s.host, s.username, ...s.tags)) continue
      out.push({ key: `s:${s.id}`, kind: 'server', depth: 0, server: s })
    }
    if (!servers.length) {
      out.push({
        key: 'servers-empty',
        kind: 'hint',
        depth: 0,
        text: 'Add a server to get started',
      })
    }

    return out
  }, [snapshot, q, expanded, openDialog])

  const total = rows.length * ROW_H
  const first = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN)
  const last = Math.min(rows.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN)
  const visible = rows.slice(first, last)

  async function startAgent(a: Agent, ws: Workspace) {
    try {
      const res = await agentApi.start(a.id)
      if (res.alreadyRunning) toast('info', `${a.name} is already running`)
      else toast('ok', `${a.name} started in ${res.session}`)
      await refreshServerAgents(ws.serverId)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function stopAgent(a: Agent, ws: Workspace) {
    try {
      await agentApi.stop(a.id)
      toast('ok', `Sent Ctrl-C to ${a.name}`)
      await refreshServerAgents(ws.serverId)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function deleteServer(s: Server) {
    const ok = await confirmAction({
      title: `Remove ${s.name}`,
      message: 'This removes the server from AgentMux, along with the workspaces and agent definitions that point at it.',
      points: [
        'Stored credentials and the pinned host key are deleted',
        'Any open terminals for this server will disconnect',
      ],
      reassurance: 'Nothing on the server itself is touched. tmux sessions and the agents inside them keep running.',
      confirmLabel: 'Remove server',
    })
    if (!ok) return
    try {
      await serverApi.remove(s.id)
      await refreshSnapshot()
      toast('ok', `Removed ${s.name}`)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function deleteProject(p: Project) {
    const ok = await confirmAction({
      title: `Delete ${p.name}`,
      message: 'The project and its workspaces and agent definitions are removed from AgentMux.',
      reassurance: 'Remote tmux sessions keep running, and no files on any server are deleted.',
      confirmLabel: 'Delete project',
    })
    if (!ok) return
    try {
      await treeApi.deleteProject(p.id)
      await refreshSnapshot()
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <div className="flex h-full w-full flex-col border-r border-ink-800 bg-ink-900">
      <div className="flex items-center gap-2 border-b border-ink-800 px-2.5 py-2">
        <Search size={13} className="shrink-0 text-ink-500" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter projects, agents, servers…"
          className="w-full bg-transparent text-xs text-ink-100 outline-none placeholder:text-ink-500"
        />
        {search && (
          <button
            onClick={() => setSearch('')}
            className="text-[10px] text-ink-500 hover:text-ink-200"
          >
            clear
          </button>
        )}
      </div>

      <div
        ref={scrollRef}
        onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div style={{ height: total, position: 'relative' }}>
          {visible.map((row, i) => {
            const top = (first + i) * ROW_H
            const style = { position: 'absolute' as const, top, left: 0, right: 0, height: ROW_H }

            if (row.kind === 'section') {
              return (
                <div
                  key={row.key}
                  style={style}
                  className="flex items-center justify-between px-2.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase"
                >
                  <span className="flex items-center gap-1.5">
                    {row.label === 'Projects' ? <FolderTree size={11} /> : <ServerIcon size={11} />}
                    {row.label}
                  </span>
                  {row.onAdd && (
                    <button
                      onClick={row.onAdd}
                      title={`Add ${row.label.slice(0, -1).toLowerCase()}`}
                      className="rounded p-0.5 text-ink-500 hover:bg-ink-800 hover:text-ink-100"
                    >
                      <Plus size={12} />
                    </button>
                  )}
                </div>
              )
            }

            if (row.kind === 'hint') {
              return (
                <div
                  key={row.key}
                  style={style}
                  className="flex items-center px-2.5 text-[11px] text-ink-600"
                >
                  <span style={{ paddingLeft: row.depth * 14 + 16 }}>{row.text}</span>
                </div>
              )
            }

            if (row.kind === 'project') {
              const open = q ? true : (expanded[`p:${row.project.id}`] ?? true)
              const selected = selection.kind === 'project' && selection.id === row.project.id
              return (
                <TreeRow
                  key={row.key}
                  style={style}
                  depth={row.depth}
                  selected={selected}
                  onClick={() => select({ kind: 'project', id: row.project.id })}
                  onContextMenu={(e) => {
                    select({ kind: 'project', id: row.project.id })
                    openContextMenu(e, [
                      {
                        label: 'Add workspace',
                        icon: Plus,
                        onSelect: () => openDialog({ kind: 'workspace', projectId: row.project.id }),
                      },
                      separator,
                      {
                        label: 'Edit project',
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'project', project: row.project }),
                      },
                      {
                        label: 'Delete project',
                        icon: Trash2,
                        danger: true,
                        onSelect: () => void deleteProject(row.project),
                      },
                    ])
                  }}
                  chevron={open ? 'down' : 'right'}
                  onChevron={() => toggleExpanded(`p:${row.project.id}`)}
                  label={row.project.name}
                  meta={row.childCount ? `${row.childCount}` : undefined}
                  actions={
                    <>
                      <RowBtn
                        icon={<Plus size={11} />}
                        title="Add workspace"
                        onClick={() => openDialog({ kind: 'workspace', projectId: row.project.id })}
                      />
                      <RowBtn
                        icon={<Pencil size={11} />}
                        title="Edit project"
                        onClick={() => openDialog({ kind: 'project', project: row.project })}
                      />
                      <RowBtn
                        icon={<Trash2 size={11} />}
                        title="Delete project"
                        danger
                        onClick={() => void deleteProject(row.project)}
                      />
                    </>
                  }
                />
              )
            }

            if (row.kind === 'workspace') {
              const open = q ? true : (expanded[`w:${row.workspace.id}`] ?? true)
              const selected = selection.kind === 'workspace' && selection.id === row.workspace.id
              return (
                <TreeRow
                  key={row.key}
                  style={style}
                  depth={row.depth}
                  selected={selected}
                  onClick={() => select({ kind: 'workspace', id: row.workspace.id })}
                  onContextMenu={(e) => {
                    select({ kind: 'workspace', id: row.workspace.id })
                    openContextMenu(e, [
                      {
                        label: 'Open shell here',
                        icon: TerminalSquare,
                        onSelect: () => {
                          openTab({
                            title: row.workspace.name,
                            kind: 'shell',
                            serverId: row.workspace.serverId,
                            workspaceId: row.workspace.id,
                            agentId: '',
                            tmuxSession: '',
                          })
                        },
                      },
                      {
                        label: 'Browse files',
                        icon: FolderTree,
                        onSelect: () => {
                          openTab({
                            title: row.workspace.name,
                            kind: 'files',
                            serverId: row.workspace.serverId,
                            workspaceId: row.workspace.id,
                            agentId: '',
                            tmuxSession: '',
                            command: row.workspace.remotePath,
                          })
                        },
                      },
                      separator,
                      {
                        label: 'Add agent',
                        icon: Bot,
                        onSelect: () => openDialog({ kind: 'agent', workspaceId: row.workspace.id }),
                      },
                      {
                        label: 'Copy remote path',
                        icon: ClipboardCopy,
                        onSelect: () => void Clipboard.SetText(row.workspace.remotePath),
                      },
                      separator,
                      {
                        label: 'Edit workspace',
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'workspace', workspace: row.workspace }),
                      },
                      {
                        label: 'Delete workspace',
                        icon: Trash2,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: `Delete ${row.workspace.name}`,
                            message:
                              'The workspace and its agent definitions are removed from AgentMux.',
                            reassurance:
                              'No files are deleted and remote tmux sessions keep running.',
                            confirmLabel: 'Delete workspace',
                          })
                          if (!ok) return
                          await treeApi.deleteWorkspace(row.workspace.id)
                          await refreshSnapshot()
                        },
                      },
                    ])
                  }}
                  chevron={open ? 'down' : 'right'}
                  onChevron={() => toggleExpanded(`w:${row.workspace.id}`)}
                  icon={<ConnDot connected={!!connections[row.workspace.serverId]?.connected} />}
                  label={row.workspace.name}
                  meta={row.server?.name ?? 'missing server'}
                  actions={
                    <>
                      <RowBtn
                        icon={<TerminalSquare size={11} />}
                        title="Open shell in workspace"
                        onClick={() =>
                          openTab({
                            title: row.workspace.name,
                            kind: 'shell',
                            serverId: row.workspace.serverId,
                            workspaceId: row.workspace.id,
                            agentId: '',
                            tmuxSession: '',
                          })
                        }
                      />
                      <RowBtn
                        icon={<FolderTree size={11} />}
                        title="Browse files in this workspace"
                        onClick={() =>
                          openTab({
                            title: row.workspace.name,
                            kind: 'files',
                            serverId: row.workspace.serverId,
                            workspaceId: row.workspace.id,
                            agentId: '',
                            tmuxSession: '',
                            command: row.workspace.remotePath,
                          })
                        }
                      />
                      <RowBtn
                        icon={<Bot size={11} />}
                        title="Add agent"
                        onClick={() => openDialog({ kind: 'agent', workspaceId: row.workspace.id })}
                      />
                      <RowBtn
                        icon={<Pencil size={11} />}
                        title="Edit workspace"
                        onClick={() => openDialog({ kind: 'workspace', workspace: row.workspace })}
                      />
                    </>
                  }
                />
              )
            }

            if (row.kind === 'agent') {
              const a = row.agent
              const selected = selection.kind === 'agent' && selection.id === a.id
              const checked = broadcastTargets.includes(a.id)
              return (
                <TreeRow
                  key={row.key}
                  style={style}
                  depth={row.depth}
                  selected={selected}
                  onClick={() => select({ kind: 'agent', id: a.id })}
                  onContextMenu={(e) => {
                    select({ kind: 'agent', id: a.id })
                    const attach = () => {
                      openTab({
                        title: a.name,
                        kind: 'agent',
                        serverId: row.workspace.serverId,
                        workspaceId: row.workspace.id,
                        agentId: a.id,
                        tmuxSession: a.tmuxSession,
                      })
                    }
                    openContextMenu(e, [
                      { label: 'Attach terminal', icon: TerminalSquare, onSelect: attach },
                      separator,
                      {
                        label: 'Start',
                        icon: Play,
                        disabled: a.status === 'running',
                        onSelect: () => void startAgent(a, row.workspace),
                      },
                      {
                        label: 'Stop (Ctrl-C)',
                        icon: Square,
                        disabled: a.status !== 'running',
                        onSelect: () => void stopAgent(a, row.workspace),
                      },
                      {
                        label: 'Restart',
                        icon: RotateCw,
                        onSelect: async () => {
                          try {
                            await agentApi.restart(a.id)
                            toast('ok', `${a.name} restarted`)
                            await refreshServerAgents(row.workspace.serverId)
                          } catch (err) {
                            toast('error', errText(err))
                          }
                        },
                      },
                      separator,
                      {
                        label: checked ? 'Remove from broadcast' : 'Add to broadcast',
                        icon: Radio,
                        onSelect: () => toggleBroadcastTarget(a.id),
                      },
                      {
                        label: 'Copy session name',
                        icon: ClipboardCopy,
                        onSelect: () => void Clipboard.SetText(a.tmuxSession),
                      },
                      separator,
                      {
                        label: 'Edit agent',
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'agent', agent: a }),
                      },
                      {
                        label: 'Kill session',
                        icon: Skull,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: `Kill ${a.tmuxSession}`,
                            message:
                              'The tmux session is destroyed along with everything running inside it.',
                            points: [
                              'The agent is terminated, not asked to stop',
                              'The pane and all of its scrollback are lost',
                            ],
                            confirmLabel: 'Kill session',
                            requireText: a.name,
                          })
                          if (!ok) return
                          try {
                            await agentApi.kill(a.id)
                            await refreshServerAgents(row.workspace.serverId)
                          } catch (err) {
                            toast('error', errText(err))
                          }
                        },
                      },
                      {
                        label: 'Delete agent',
                        icon: Trash2,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: `Delete ${a.name}`,
                            message: 'The agent definition is removed from AgentMux.',
                            reassurance: `Its tmux session ${a.tmuxSession} keeps running on the server.`,
                            confirmLabel: 'Delete agent',
                          })
                          if (!ok) return
                          await agentApi.remove(a.id)
                          await refreshSnapshot()
                        },
                      },
                    ])
                  }}
                  onDoubleClick={() =>
                    openTab({
                      title: a.name,
                      kind: 'agent',
                      serverId: row.workspace.serverId,
                      workspaceId: row.workspace.id,
                      agentId: a.id,
                      tmuxSession: a.tmuxSession,
                    })
                  }
                  icon={<StatusDot status={a.status} pulse />}
                  label={a.name}
                  meta={a.status === 'running' ? a.progressText || 'running' : a.status}
                  metaDim
                  leading={
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleBroadcastTarget(a.id)}
                      onClick={(e) => e.stopPropagation()}
                      title="Include in broadcast"
                      className="h-3 w-3 shrink-0 accent-[#4c8dff]"
                    />
                  }
                  actions={
                    <>
                      {a.status === 'running' ? (
                        <RowBtn
                          icon={<Square size={11} />}
                          title="Stop agent (Ctrl-C)"
                          onClick={() => void stopAgent(a, row.workspace)}
                        />
                      ) : (
                        <RowBtn
                          icon={<Play size={11} />}
                          title="Start agent"
                          onClick={() => void startAgent(a, row.workspace)}
                        />
                      )}
                      <RowBtn
                        icon={<TerminalSquare size={11} />}
                        title="Attach terminal"
                        onClick={() =>
                          openTab({
                            title: a.name,
                            kind: 'agent',
                            serverId: row.workspace.serverId,
                            workspaceId: row.workspace.id,
                            agentId: a.id,
                            tmuxSession: a.tmuxSession,
                          })
                        }
                      />
                      <RowBtn
                        icon={<Pencil size={11} />}
                        title="Edit agent"
                        onClick={() => openDialog({ kind: 'agent', agent: a })}
                      />
                    </>
                  }
                />
              )
            }

            const s = row.server
            const conn = connections[s.id]
            const selected = selection.kind === 'server' && selection.id === s.id
            return (
              <TreeRow
                key={row.key}
                style={style}
                depth={row.depth}
                selected={selected}
                onClick={() => select({ kind: 'server', id: s.id })}
                onContextMenu={(e) => {
                  select({ kind: 'server', id: s.id })
                  openContextMenu(e, [
                    {
                      label: 'Open shell',
                      icon: TerminalSquare,
                      onSelect: () => {
                        openTab({
                          title: s.name,
                          kind: 'shell',
                          serverId: s.id,
                          workspaceId: '',
                          agentId: '',
                          tmuxSession: '',
                        })
                      },
                    },
                    {
                      label: 'Browse files',
                      icon: FolderTree,
                      onSelect: () => {
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
                    },
                    separator,
                    { label: 'Metrics', icon: Activity, onSelect: () => setRightPanel('metrics') },
                    { label: 'tmux sessions', icon: Layers, onSelect: () => setRightPanel('tmux') },
                    {
                      label: 'Install agents',
                      icon: Sparkles,
                      onSelect: () => setRightPanel('toolkit'),
                    },
                    separator,
                    conn?.connected
                      ? {
                          label: 'Disconnect',
                          icon: Link2Off,
                          onSelect: async () => {
                            await serverApi.disconnect(s.id)
                            await useAppStore.getState().refreshConnections()
                          },
                        }
                      : {
                          label: 'Connect',
                          icon: Link2,
                          onSelect: async () => {
                            try {
                              await serverApi.connect(s.id)
                              toast('ok', `Connected to ${s.name}`)
                            } catch (err) {
                              toast('error', errText(err))
                            }
                            await useAppStore.getState().refreshConnections()
                          },
                        },
                    {
                      label: 'Test connection',
                      icon: Zap,
                      onSelect: async () => {
                        const p = await serverApi.test(s.id)
                        if (p.ok) toast('ok', `${s.name}: ${p.latencyMs} ms · ${p.os || 'unknown'}`)
                        else toast('error', `${s.name}: ${p.error}`)
                        await useAppStore.getState().refreshConnections()
                      },
                    },
                    separator,
                    {
                      label: 'Edit server',
                      icon: Pencil,
                      onSelect: () => openDialog({ kind: 'server', server: s }),
                    },
                    {
                      label: 'Remove server',
                      icon: Trash2,
                      danger: true,
                      onSelect: () => void deleteServer(s),
                    },
                  ])
                }}
                onDoubleClick={() =>
                  openTab({
                    title: s.name,
                    kind: 'shell',
                    serverId: s.id,
                    workspaceId: '',
                    agentId: '',
                    tmuxSession: '',
                  })
                }
                icon={<ConnDot connected={!!conn?.connected} />}
                label={s.name}
                meta={`${s.username}@${s.host}`}
                metaDim
                actions={
                  <>
                    <RowBtn
                      icon={<TerminalSquare size={11} />}
                      title="Open shell"
                      onClick={() =>
                        openTab({
                          title: s.name,
                          kind: 'shell',
                          serverId: s.id,
                          workspaceId: '',
                          agentId: '',
                          tmuxSession: '',
                        })
                      }
                    />
                    <RowBtn
                      icon={<FolderTree size={11} />}
                      title="Browse files"
                      onClick={() =>
                        openTab({
                          title: s.name,
                          kind: 'files',
                          serverId: s.id,
                          workspaceId: '',
                          agentId: '',
                          tmuxSession: '',
                          command: '',
                        })
                      }
                    />
                    <RowBtn
                      icon={<Zap size={11} />}
                      title="Test connection"
                      onClick={async () => {
                        const p = await serverApi.test(s.id)
                        if (p.ok)
                          toast(
                            'ok',
                            `${s.name}: ${p.latencyMs} ms · ${p.os || 'unknown OS'} · ${
                              p.hasTmux ? p.tmuxVersion : 'no tmux'
                            }`,
                          )
                        else toast('error', `${s.name}: ${p.error}`)
                        await useAppStore.getState().refreshConnections()
                      }}
                    />
                    <RowBtn
                      icon={<Pencil size={11} />}
                      title="Edit server"
                      onClick={() => openDialog({ kind: 'server', server: s })}
                    />
                    <RowBtn
                      icon={<Trash2 size={11} />}
                      title="Delete server"
                      danger
                      onClick={() => void deleteServer(s)}
                    />
                  </>
                }
              />
            )
          })}
        </div>
      </div>

      <div className="flex shrink-0 gap-1.5 border-t border-ink-800 px-2.5 py-2">
        <Button size="sm" onClick={() => openDialog({ kind: 'server' })} className="flex-1">
          <Plus size={11} /> Server
        </Button>
        <Button size="sm" onClick={() => openDialog({ kind: 'project' })} className="flex-1">
          <Plus size={11} /> Project
        </Button>
      </div>
    </div>
  )
}

function TreeRow({
  style,
  depth,
  selected,
  onClick,
  onDoubleClick,
  onContextMenu,
  chevron,
  onChevron,
  icon,
  leading,
  label,
  meta,
  metaDim,
  actions,
}: {
  style: React.CSSProperties
  depth: number
  selected?: boolean
  onClick?: () => void
  onDoubleClick?: () => void
  onContextMenu?: (e: React.MouseEvent) => void
  chevron?: 'right' | 'down'
  onChevron?: () => void
  icon?: React.ReactNode
  leading?: React.ReactNode
  label: string
  meta?: string
  metaDim?: boolean
  actions?: React.ReactNode
}) {
  return (
    <div
      style={style}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onContextMenu={onContextMenu}
      className={clsx(
        'group flex cursor-default items-center gap-1.5 pr-1.5 text-xs',
        selected ? 'bg-accent/12 text-ink-100' : 'text-ink-200 hover:bg-ink-850',
      )}
    >
      <div style={{ width: depth * 14 + 6 }} className="shrink-0" />
      {chevron ? (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onChevron?.()
          }}
          className="shrink-0 rounded p-0.5 text-ink-500 hover:text-ink-100"
        >
          {chevron === 'down' ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </button>
      ) : (
        <div className="w-[18px] shrink-0" />
      )}
      {leading}
      {icon}
      <span className="shrink-0 truncate font-medium">{label}</span>
      {meta && (
        <span
          className={clsx(
            'min-w-0 flex-1 truncate text-[11px]',
            metaDim ? 'text-ink-500' : 'text-ink-400',
          )}
        >
          {meta}
        </span>
      )}
      {!meta && <span className="flex-1" />}
      <span className="hidden shrink-0 items-center gap-0.5 group-hover:flex">{actions}</span>
    </div>
  )
}

function RowBtn({
  icon,
  title,
  onClick,
  danger,
}: {
  icon: React.ReactNode
  title: string
  onClick: () => void
  danger?: boolean
}) {
  return (
    <button
      title={title}
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
      className={clsx(
        'rounded p-1 text-ink-400 hover:bg-ink-750',
        danger ? 'hover:text-danger' : 'hover:text-ink-100',
      )}
    >
      {icon}
    </button>
  )
}
