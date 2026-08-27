import clsx from 'clsx'
import {
  Activity,
  Bot,
  ChevronDown,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  ClipboardCopy,
  FolderTree,
  Laptop,
  Layers,
  Link2,
  Link2Off,
  Monitor,
  Pencil,
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
  Unplug,
  X,
  Zap,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { agents as agentApi, errText, servers as serverApi, tree as treeApi } from '../lib/api'
import { agentActivityLabel } from '../lib/agentStatus'
import { copyText } from '../lib/clipboard'
import type { MsgKey } from '../lib/i18n'
import { isDesktopKind, isLocalKind } from '../lib/types'
import type { Agent, Project, Server, Workspace } from '../lib/types'
import { refreshServerAgents, useAppStore } from '../store/useAppStore'
import { confirmAction } from '../store/useConfirm'
import { openContextMenu, separator } from '../store/useContextMenu'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'
import { AttentionDot, Badge, Button, ConnDot, StatusDot, iconButtonClass } from './ui'

const ROW_H = 26
const OVERSCAN = 8

type Row =
  | { key: string; kind: 'project'; depth: number; project: Project; childCount: number; alerts: number }
  | { key: string; kind: 'workspace'; depth: number; workspace: Workspace; server?: Server; alerts: number }
  | { key: string; kind: 'agent'; depth: number; agent: Agent; workspace: Workspace }
  | { key: string; kind: 'server'; depth: number; server: Server }
  | { key: string; kind: 'hint'; depth: number; text: MsgKey }

/** One half of the tree, as a tab: an icon, a name and how much is in it. */
function TabBtn({
  active,
  icon,
  label,
  count,
  onClick,
}: {
  active: boolean
  icon: React.ReactNode
  label: string
  count: number
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={clsx(
        'flex items-center gap-1.5 rounded-control px-2 py-1 text-[11px] font-medium',
        active ? 'bg-ink-750 text-ink-100' : 'text-ink-400 hover:bg-ink-800 hover:text-ink-200',
      )}
    >
      {icon}
      {label}
      <span className={clsx('tabular-nums', active ? 'text-ink-400' : 'text-ink-600')}>{count}</span>
    </button>
  )
}

export function Sidebar() {
  const t = useT()
  const snapshot = useAppStore((s) => s.snapshot)
  const connections = useAppStore((s) => s.connections)
  const desktopSupport = useAppStore((s) => s.desktopSupport)
  const search = useAppStore((s) => s.search)
  const setSearch = useAppStore((s) => s.setSearch)
  const expanded = useAppStore((s) => s.expanded)
  const sidebarTab = useAppStore((s) => s.sidebarTab)
  const setSidebarTab = useAppStore((s) => s.setSidebarTab)
  const setAllExpanded = useAppStore((s) => s.setAllExpanded)
  const selectedServers = useAppStore((s) => s.selectedServers)
  const toggleServerSelected = useAppStore((s) => s.toggleServerSelected)
  const clearServerSelection = useAppStore((s) => s.clearServerSelection)
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

  // Every project and workspace, so "open everything" means everything rather
  // than everything currently on screen.
  const foldKeys = useMemo(
    () => [
      ...snapshot.projects.map((p) => `p:${p.id}`),
      ...snapshot.workspaces.map((w) => `w:${w.id}`),
    ],
    [snapshot.projects, snapshot.workspaces],
  )

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
    // The two halves are pages now rather than sections, so only the one on
    // screen is built. The tab strip carries the heading and the add button
    // that each section header used to.
    const showProjects = sidebarTab === 'projects'

    for (const p of showProjects ? projects : []) {
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
      // Attention bubbles up: a collapsed project or workspace still shows how
      // many of its agents are waiting on a human, otherwise folding the tree
      // is how a flag gets missed for an afternoon.
      const alertsIn = (w: Workspace) =>
        (agentsByWs.get(w.id) ?? []).filter((a) => a.attention).length
      const projectAlerts = wss.reduce((n, w) => n + alertsIn(w), 0)
      out.push({
        key: `p:${p.id}`,
        kind: 'project',
        depth: 0,
        project: p,
        childCount: agentCount,
        alerts: projectAlerts,
      })
      const projectOpen = q ? true : (expanded[`p:${p.id}`] ?? true)
      if (!projectOpen) continue

      const shown = q ? visibleWss : wss
      if (!shown.length) {
        out.push({ key: `p:${p.id}:empty`, kind: 'hint', depth: 1, text: 'tree.noWorkspaces' })
      }
      for (const w of shown) {
        out.push({
          key: `w:${w.id}`,
          kind: 'workspace',
          depth: 1,
          workspace: w,
          server: serverById.get(w.serverId),
          alerts: alertsIn(w),
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

    if (showProjects && !projects.length) {
      out.push({ key: 'projects-empty', kind: 'hint', depth: 0, text: 'tree.noProjects' })
    }
    for (const s of showProjects ? [] : servers) {
      if (q && !matches(s.name, s.host, s.username, ...s.tags)) continue
      out.push({ key: `s:${s.id}`, kind: 'server', depth: 0, server: s })
    }
    if (!showProjects && !servers.length) {
      out.push({
        key: 'servers-empty',
        kind: 'hint',
        depth: 0,
        text: 'tree.getStarted',
      })
    }

    return out
  }, [snapshot, q, expanded, sidebarTab])

  const total = rows.length * ROW_H
  const first = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN)
  const last = Math.min(rows.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN)
  const visible = rows.slice(first, last)

  async function startAgent(a: Agent, ws: Workspace) {
    try {
      const res = await agentApi.start(a.id)
      if (res.alreadyRunning) toast('info', t('toast.agentAlreadyRunning', { name: a.name }))
      else toast('ok', t('toast.agentStarted', { name: a.name, session: res.session }))
      await refreshServerAgents(ws.serverId)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function stopAgent(a: Agent, ws: Workspace) {
    try {
      await agentApi.stop(a.id)
      toast('ok', t('toast.sentCtrlC', { name: a.name }))
      await refreshServerAgents(ws.serverId)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  /** The ticked servers, in tree order, skipping any that have gone away. */
  function selectedList(): Server[] {
    return snapshot.servers.filter((s) => selectedServers.includes(s.id))
  }

  async function testSelected() {
    const list = selectedList()
    const results = await Promise.all(
      list.map(async (s) => ({ s, p: await serverApi.test(s.id).catch(() => null) })),
    )
    const ok = results.filter((r) => r.p?.ok).length
    const failed = results.filter((r) => !r.p?.ok)
    toast(
      failed.length ? 'warn' : 'ok',
      t('toast.testedMany', {
        ok,
        total: list.length,
        failed: failed.map((r) => r.s.name).join(', ') || '—',
      }),
    )
    await useAppStore.getState().refreshConnections()
  }

  async function disconnectSelected() {
    const list = selectedList()
    await Promise.all(list.map((s) => serverApi.disconnect(s.id).catch(() => {})))
    toast('ok', t('toast.disconnectedMany', { n: list.length }))
    await useAppStore.getState().refreshConnections()
  }

  async function removeSelected() {
    const list = selectedList()
    if (!list.length) return
    const ok = await confirmAction({
      title: t('confirm.removeServers.title', { n: list.length }),
      message: list.map((s) => s.name).join(', '),
      points: [t('confirm.removeServer.credentials'), t('confirm.removeServer.terminals')],
      reassurance: t('confirm.removeServer.reassurance'),
      confirmLabel: t('tree.removeServer'),
    })
    if (!ok) return
    for (const s of list) {
      await serverApi.remove(s.id).catch((e) => toast('error', errText(e)))
    }
    clearServerSelection()
    await refreshSnapshot()
    toast('ok', t('toast.removedMany', { n: list.length }))
  }

  async function deleteServer(s: Server) {
    const ok = await confirmAction({
      title: t('confirm.removeServer.title', { name: s.name }),
      message: t('confirm.removeServer.message'),
      points: [t('confirm.removeServer.credentials'), t('confirm.removeServer.terminals')],
      reassurance: t('confirm.removeServer.reassurance'),
      confirmLabel: t('tree.removeServer'),
    })
    if (!ok) return
    try {
      await serverApi.remove(s.id)
      await refreshSnapshot()
      toast('ok', t('toast.removed', { name: s.name }))
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function deleteProject(p: Project) {
    const ok = await confirmAction({
      title: t('confirm.deleteProject.title', { name: p.name }),
      message: t('confirm.deleteProject.message'),
      reassurance: t('confirm.deleteProject.reassurance'),
      confirmLabel: t('tree.deleteProject'),
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
    <div className="flex h-full w-full flex-col border-r hairline bg-ink-900">
      <div className="flex items-center gap-2 border-b hairline px-2.5 py-2">
        <Search size={13} className="shrink-0 text-ink-500" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('tree.filter')}
          className="w-full bg-transparent text-xs text-ink-100 outline-none placeholder:text-ink-500"
        />
        {search && (
          <button
            onClick={() => setSearch('')}
            className="text-[10px] text-ink-500 hover:text-ink-200"
          >
            {t('tree.clear')}
          </button>
        )}
      </div>

      <div className="flex items-center gap-0.5 border-b hairline px-1.5 py-1">
        <TabBtn
          active={sidebarTab === 'projects'}
          icon={<FolderTree size={11} />}
          label={t('tree.projects')}
          count={snapshot.projects.length}
          onClick={() => setSidebarTab('projects')}
        />
        <TabBtn
          active={sidebarTab === 'servers'}
          icon={<ServerIcon size={11} />}
          label={t('tree.servers')}
          count={snapshot.servers.length}
          onClick={() => setSidebarTab('servers')}
        />
        <span className="flex-1" />
        {sidebarTab === 'projects' && (
          <>
            <RowBtn
              icon={<ChevronsDownUp size={11} />}
              title={t('tree.collapseAll')}
              onClick={() => setAllExpanded(foldKeys, false)}
            />
            <RowBtn
              icon={<ChevronsUpDown size={11} />}
              title={t('tree.expandAll')}
              onClick={() => setAllExpanded(foldKeys, true)}
            />
          </>
        )}
        <RowBtn
          icon={<Plus size={12} />}
          title={sidebarTab === 'projects' ? t('tree.addProject') : t('tree.addServer')}
          onClick={() =>
            openDialog(sidebarTab === 'projects' ? { kind: 'project' } : { kind: 'server' })
          }
        />
      </div>

      {sidebarTab === 'servers' && selectedServers.length > 0 && (
        // Only while a selection stands, and only on the tab it belongs to: a
        // bar that is always there is a bar nobody reads. Icons rather than
        // words, because the sidebar is narrow by design and four labelled
        // buttons wrap into a paragraph in it.
        <div className="flex items-center gap-1 border-b hairline bg-ink-850 px-2 py-1">
          <span className="text-[11px] text-ink-300">
            {t('tree.selected', { n: selectedServers.length })}
          </span>
          <span className="flex-1" />
          <RowBtn
            icon={<Zap size={11} />}
            title={t('tree.testConnection')}
            onClick={() => void testSelected()}
          />
          <RowBtn
            icon={<Unplug size={11} />}
            title={t('tree.disconnect')}
            onClick={() => void disconnectSelected()}
          />
          <RowBtn
            icon={<Trash2 size={11} />}
            title={t('tree.removeServer')}
            danger
            onClick={() => void removeSelected()}
          />
          <RowBtn
            icon={<X size={11} />}
            title={t('tree.clearSelection')}
            onClick={clearServerSelection}
          />
        </div>
      )}

      <div
        ref={scrollRef}
        onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div style={{ height: total, position: 'relative' }}>
          {visible.map((row, i) => {
            const top = (first + i) * ROW_H
            const style = { position: 'absolute' as const, top, left: 0, right: 0, height: ROW_H }

            if (row.kind === 'hint') {
              return (
                <div
                  key={row.key}
                  style={style}
                  className="flex items-center px-2.5 text-[11px] text-ink-600"
                >
                  <span style={{ paddingLeft: row.depth * 14 + 16 }}>{t(row.text)}</span>
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
                        label: t('tree.addWorkspace'),
                        icon: Plus,
                        onSelect: () => openDialog({ kind: 'workspace', projectId: row.project.id }),
                      },
                      separator,
                      {
                        label: t('tree.editProject'),
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'project', project: row.project }),
                      },
                      {
                        label: t('tree.deleteProject'),
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
                  badge={
                    row.alerts > 0 ? (
                      <span title={t('tree.needsAttention', { n: row.alerts })}>
                        <Badge tone="warn">{row.alerts}</Badge>
                      </span>
                    ) : undefined
                  }
                  actions={
                    <>
                      <RowBtn
                        icon={<Plus size={11} />}
                        title={t('tree.addWorkspace')}
                        onClick={() => openDialog({ kind: 'workspace', projectId: row.project.id })}
                      />
                      <RowBtn
                        icon={<Pencil size={11} />}
                        title={t('tree.editProject')}
                        onClick={() => openDialog({ kind: 'project', project: row.project })}
                      />
                      <RowBtn
                        icon={<Trash2 size={11} />}
                        title={t('tree.deleteProject')}
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
                        label: t('tree.openShellHere'),
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
                        label: t('tree.browseFiles'),
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
                        label: t('tree.addAgent'),
                        icon: Bot,
                        onSelect: () => openDialog({ kind: 'agent', workspaceId: row.workspace.id }),
                      },
                      {
                        label: t('tree.copyRemotePath'),
                        icon: ClipboardCopy,
                        onSelect: () => void copyText(row.workspace.remotePath),
                      },
                      separator,
                      {
                        label: t('tree.editWorkspace'),
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'workspace', workspace: row.workspace }),
                      },
                      {
                        label: t('tree.deleteWorkspace'),
                        icon: Trash2,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: t('confirm.deleteWorkspace.title', { name: row.workspace.name }),
                            message: t('confirm.deleteWorkspace.message'),
                            reassurance: t('confirm.deleteWorkspace.reassurance'),
                            confirmLabel: t('tree.deleteWorkspace'),
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
                  meta={row.server?.name ?? t('tree.missingServer')}
                  badge={
                    row.alerts > 0 ? (
                      <span title={t('tree.needsAttention', { n: row.alerts })}>
                        <Badge tone="warn">{row.alerts}</Badge>
                      </span>
                    ) : undefined
                  }
                  actions={
                    <>
                      <RowBtn
                        icon={<TerminalSquare size={11} />}
                        title={t('tree.openShellInWorkspace')}
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
                        title={t('tree.browseFilesInWorkspace')}
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
                        title={t('tree.addAgent')}
                        onClick={() => openDialog({ kind: 'agent', workspaceId: row.workspace.id })}
                      />
                      <RowBtn
                        icon={<Pencil size={11} />}
                        title={t('tree.editWorkspace')}
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
              const checked = broadcastTargets.some((t) => t.agentId === a.id)
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
                      { label: t('tree.attachTerminal'), icon: TerminalSquare, onSelect: attach },
                      separator,
                      {
                        label: t('agent.start'),
                        icon: Play,
                        disabled: a.status === 'running',
                        onSelect: () => void startAgent(a, row.workspace),
                      },
                      {
                        label: t('agent.stopCtrlC'),
                        icon: Square,
                        disabled: a.status !== 'running',
                        onSelect: () => void stopAgent(a, row.workspace),
                      },
                      {
                        label: t('agent.restart'),
                        icon: RotateCw,
                        onSelect: async () => {
                          try {
                            await agentApi.restart(a.id)
                            toast('ok', t('toast.agentRestarted', { name: a.name }))
                            await refreshServerAgents(row.workspace.serverId)
                          } catch (err) {
                            toast('error', errText(err))
                          }
                        },
                      },
                      separator,
                      {
                        label: checked ? t('tree.removeFromBroadcast') : t('tree.addToBroadcast'),
                        icon: Radio,
                        onSelect: () => toggleBroadcastTarget({ agentId: a.id, serverId: '', session: '' }),
                      },
                      {
                        label: t('tree.copySessionName'),
                        icon: ClipboardCopy,
                        onSelect: () => void copyText(a.tmuxSession),
                      },
                      separator,
                      {
                        label: t('tree.editAgent'),
                        icon: Pencil,
                        onSelect: () => openDialog({ kind: 'agent', agent: a }),
                      },
                      {
                        label: t('agent.killSession'),
                        icon: Skull,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: t('confirm.killSession.title', { session: a.tmuxSession }),
                            message: t('confirm.killSession.message'),
                            points: [
                              t('confirm.killSession.terminated'),
                              t('confirm.killSession.scrollback'),
                            ],
                            confirmLabel: t('agent.killSession'),
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
                        label: t('tree.deleteAgent'),
                        icon: Trash2,
                        danger: true,
                        onSelect: async () => {
                          const ok = await confirmAction({
                            title: t('confirm.deleteAgent.title', { name: a.name }),
                            message: t('confirm.deleteAgent.message'),
                            reassurance: t('confirm.deleteAgent.reassurance', {
                              session: a.tmuxSession,
                            }),
                            confirmLabel: t('tree.deleteAgent'),
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
                  icon={<StatusDot status={a.status} pulse={a.activity === 'working'} />}
                  label={a.name}
                  meta={agentActivityLabel(t, a)}
                  metaDim
                  metaWarn={a.status === 'running' && a.activity === 'input'}
                  badge={
                    a.attention ? (
                      <AttentionDot
                        kind={a.attention}
                        title={t(
                          a.attention === 'input' ? 'agent.attention.input' : 'agent.attention.done',
                        )}
                      />
                    ) : undefined
                  }
                  leading={
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleBroadcastTarget({ agentId: a.id, serverId: '', session: '' })}
                      onClick={(e) => e.stopPropagation()}
                      title={t('tree.includeInBroadcast')}
                      className="h-3 w-3 shrink-0 accent-[#4c8dff]"
                    />
                  }
                  actions={
                    <>
                      {a.status === 'running' ? (
                        <RowBtn
                          icon={<Square size={11} />}
                          title={t('tree.stopAgent')}
                          onClick={() => void stopAgent(a, row.workspace)}
                        />
                      ) : (
                        <RowBtn
                          icon={<Play size={11} />}
                          title={t('tree.startAgent')}
                          onClick={() => void startAgent(a, row.workspace)}
                        />
                      )}
                      <RowBtn
                        icon={<TerminalSquare size={11} />}
                        title={t('tree.attachTerminal')}
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
                        title={t('tree.editAgent')}
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
            // A desktop host has one thing to offer, and none of the rest of
            // this row applies to it: there is no shell to open, no files to
            // browse and no agents to run.
            const deskOnly = isDesktopKind(s.kind)
            const openDesktopTab = () =>
              openTab({
                title: s.name,
                kind: 'desktop',
                serverId: s.id,
                workspaceId: '',
                agentId: '',
                tmuxSession: '',
                command: `${s.desktopOs === 'windows' ? 'rdp' : 'vnc'}:${s.port || (s.desktopOs === 'windows' ? 3389 : 5900)}`,
              })
            return (
              <TreeRow
                key={row.key}
                style={style}
                depth={row.depth}
                selected={selected}
                onClick={() => select({ kind: 'server', id: s.id })}
                onContextMenu={(e) => {
                  select({ kind: 'server', id: s.id })
                  if (deskOnly) {
                    openContextMenu(e, [
                      { label: t('tree.openDesktop'), icon: Monitor, onSelect: openDesktopTab },
                      {
                        label: t('tree.testConnection'),
                        icon: Zap,
                        onSelect: async () => {
                          const p = await serverApi.test(s.id)
                          toast(p.ok ? 'ok' : 'error', p.ok ? p.os : p.error)
                        },
                      },
                      separator,
                      { label: t('tree.editServer'), icon: Pencil, onSelect: () => openDialog({ kind: 'server', server: s }) },
                      {
                        label: t('tree.removeServer'),
                        icon: Trash2,
                        danger: true,
                        onSelect: () => void deleteServer(s),
                      },
                    ])
                    return
                  }
                  openContextMenu(e, [
                    {
                      label: t('tree.openShell'),
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
                      label: t('tree.browseFiles'),
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
                    // Opening a desktop on the computer you are sitting at is
                    // not a thing anybody wants, so it is offered only for the
                    // machines that are somewhere else. It is offered on every
                    // platform now: the viewer can run in a pane here, which a
                    // phone has as much as a desktop does.
                    isLocalKind(s.kind)
                      ? {}
                      : {
                          label: t('tree.openDesktop'),
                          icon: Monitor,
                          // Greyed once a probe has come back with nothing, so
                          // the door is not offered onto a machine that has no
                          // desktop behind it. Until then it is offered: asking
                          // costs three dials and nobody has asked yet.
                          disabled: desktopSupport[s.id] === false,
                          hint:
                            desktopSupport[s.id] === false
                              ? t('tree.openDesktop.none')
                              : undefined,
                          onSelect: () => openDialog({ kind: 'desktop', server: s }),
                        },
                    separator,
                    {
                      label: t('tree.metrics'),
                      icon: Activity,
                      onSelect: () => setRightPanel('metrics'),
                    },
                    {
                      label: t('tree.tmuxSessions'),
                      icon: Layers,
                      onSelect: () => setRightPanel('tmux'),
                    },
                    {
                      label: t('tree.installAgents'),
                      icon: Sparkles,
                      onSelect: () => setRightPanel('toolkit'),
                    },
                    separator,
                    // There is no connection to this computer to open or close.
                    isLocalKind(s.kind)
                      ? {}
                      : conn?.connected
                      ? {
                          label: t('tree.disconnect'),
                          icon: Link2Off,
                          onSelect: async () => {
                            await serverApi.disconnect(s.id)
                            await useAppStore.getState().refreshConnections()
                          },
                        }
                      : {
                          label: t('tree.connect'),
                          icon: Link2,
                          onSelect: async () => {
                            try {
                              await serverApi.connect(s.id)
                              toast('ok', t('toast.connectedTo', { name: s.name }))
                            } catch (err) {
                              toast('error', errText(err))
                            }
                            await useAppStore.getState().refreshConnections()
                          },
                        },
                    {
                      label:
                        isLocalKind(s.kind) ? t('tree.checkThisComputer') : t('tree.testConnection'),
                      icon: Zap,
                      onSelect: async () => {
                        const p = await serverApi.test(s.id)
                        if (p.ok)
                          toast(
                            'ok',
                            t('toast.testOk', {
                              name: s.name,
                              ms: p.latencyMs,
                              os: p.os || t('common.unknown'),
                            }),
                          )
                        else toast('error', t('toast.testFailed', { name: s.name, error: p.error }))
                        await useAppStore.getState().refreshConnections()
                      },
                    },
                    separator,
                    {
                      label: isLocalKind(s.kind) ? t('tree.editThisHost') : t('tree.editServer'),
                      icon: Pencil,
                      onSelect: () => openDialog({ kind: 'server', server: s }),
                    },
                    {
                      label: isLocalKind(s.kind) ? t('tree.removeThisHost') : t('tree.removeServer'),
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
                leading={
                  <input
                    type="checkbox"
                    checked={selectedServers.includes(s.id)}
                    onChange={() => toggleServerSelected(s.id)}
                    onClick={(e) => e.stopPropagation()}
                    title={t('tree.selectServer')}
                    className="h-3 w-3 shrink-0 accent-[#4c8dff]"
                  />
                }
                icon={
                  isLocalKind(s.kind) ? (
                    <Laptop size={11} className={conn?.connected ? 'text-accent' : 'text-ink-500'} />
                  ) : deskOnly ? (
                    // No connection dot: there is no connection held open to a
                    // desktop host between sessions, so a light would only ever
                    // say "off" and mean nothing.
                    <Monitor size={11} className="text-ink-500" />
                  ) : (
                    <ConnDot connected={!!conn?.connected} />
                  )
                }
                label={s.name}
                meta={
                  s.kind === 'local'
                    ? t('tree.thisComputer')
                    : s.kind === 'localwin'
                    ? t('tree.thisComputerWin')
                    : deskOnly
                      ? `${t('tree.desktopHost')} · ${s.host}`
                      : `${s.username}@${s.host}`
                }
                metaDim
                actions={
                  deskOnly ? (
                    <RowBtn
                      icon={<Monitor size={11} />}
                      title={t('tree.openDesktop')}
                      onClick={openDesktopTab}
                    />
                  ) : (
                  <>
                    <RowBtn
                      icon={<TerminalSquare size={11} />}
                      title={t('tree.openShell')}
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
                      title={t('tree.browseFiles')}
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
                      title={t('tree.testConnection')}
                      onClick={async () => {
                        const p = await serverApi.test(s.id)
                        if (p.ok)
                          toast(
                            'ok',
                            t('toast.testOkFull', {
                              name: s.name,
                              ms: p.latencyMs,
                              os: p.os || t('common.unknownOS'),
                              tmux: p.hasTmux ? p.tmuxVersion : t('common.noTmux'),
                            }),
                          )
                        else toast('error', t('toast.testFailed', { name: s.name, error: p.error }))
                        await useAppStore.getState().refreshConnections()
                      }}
                    />
                    <RowBtn
                      icon={<Pencil size={11} />}
                      title={t('tree.editServer')}
                      onClick={() => openDialog({ kind: 'server', server: s })}
                    />
                    <RowBtn
                      icon={<Trash2 size={11} />}
                      title={t('tree.deleteServer')}
                      danger
                      onClick={() => void deleteServer(s)}
                    />
                  </>
                  )
                }
              />
            )
          })}
        </div>
      </div>

      <div className="flex shrink-0 gap-1.5 border-t hairline px-2.5 py-2">
        <Button size="sm" onClick={() => openDialog({ kind: 'server' })} className="flex-1">
          <Plus size={11} /> {t('tree.newServer')}
        </Button>
        <Button size="sm" onClick={() => openDialog({ kind: 'project' })} className="flex-1">
          <Plus size={11} /> {t('tree.newProject')}
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
  metaWarn,
  badge,
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
  /** Meta in the warning colour — for "waiting for your input", which must not
   *  read like ordinary dimmed status text. */
  metaWarn?: boolean
  /** Always-visible marker on the row's right edge; unlike actions it does not
   *  wait for a hover, because it exists for the eye that is only scanning. */
  badge?: React.ReactNode
  actions?: React.ReactNode
}) {
  return (
    <div
      style={style}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onContextMenu={onContextMenu}
      className={clsx(
        // An inset rounded pill, the way a macOS sidebar shows selection —
        // not a full-bleed band, which reads as a highlighted table row.
        'group mx-1.5 flex cursor-default items-center gap-1.5 rounded-control pr-1.5 text-xs',
        selected
          ? // Inside the pill everything adopts the selection foreground.
            // Children carry their own greys, and grey on system blue is
            // unreadable — the icons and row actions disappeared entirely
            // until this was here.
            'bg-accent text-white [&_button]:text-white/80 [&_button:hover]:bg-white/20 ' +
            '[&_button:hover]:text-white [&_svg]:text-current'
          : 'text-ink-200 hover:bg-ink-800',
      )}
    >
      <div style={{ width: depth * 14 + 6 }} className="shrink-0" />
      {chevron ? (
        <button
          onClick={(e) => {
            e.stopPropagation()
            onChevron?.()
          }}
          className={clsx(
            'shrink-0 rounded-control p-0.5',
            selected ? 'text-white/80 hover:text-white' : 'text-ink-500 hover:text-ink-100',
          )}
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
            selected ? 'text-white/70' : metaWarn ? 'text-warn' : metaDim ? 'text-ink-500' : 'text-ink-400',
          )}
        >
          {meta}
        </span>
      )}
      {!meta && <span className="flex-1" />}
      {badge && <span className="flex shrink-0 items-center">{badge}</span>}
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
        iconButtonClass,
        'text-ink-400 hover:bg-ink-750',
        danger ? 'hover:text-danger' : 'hover:text-ink-100',
      )}
    >
      {icon}
    </button>
  )
}
