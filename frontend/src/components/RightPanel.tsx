import clsx from 'clsx'
import {
  Activity,
  Bot,
  Brain,
  PanelRightClose,
  Plus,
  Radio,
  Sparkles,
  TerminalSquare,
  Wand2,
  Workflow,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { on, orch as orchApi, skills as skillApi } from '../lib/api'
import { agentActivityLabel } from '../lib/agentStatus'
import type { MsgKey } from '../lib/i18n'
import { useAppStore, type RightPanel as PanelKind } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'
import { AgentDetail } from './panels/AgentDetail'
import { BroadcastPanel } from './panels/BroadcastPanel'
import { MemoryPanel } from './panels/MemoryPanel'
import { MetricsPanel } from './panels/MetricsPanel'
import { OrchestratorPanel } from './panels/OrchestratorPanel'
import { ServerDetail } from './panels/ServerDetail'
import { SkillPanel } from './panels/SkillPanel'
import { TmuxPanel } from './panels/TmuxPanel'
import { ToolkitPanel } from './panels/ToolkitPanel'
import { AttentionDot, Badge, Button, Empty } from './ui'

/**
 * Resolves the server behind the current selection — directly, or via the
 * selected workspace or agent.
 *
 * It returns a plain string on purpose: the store compares selector results with
 * Object.is, so a component reading this only re-renders when the server
 * actually changes, not on every agent status poll.
 */
function useSelectedServerId(): string {
  return useAppStore((s) => {
    const sel = s.selection
    if (sel.kind === 'server') return sel.id
    if (sel.kind === 'workspace') {
      return s.snapshot.workspaces.find((w) => w.id === sel.id)?.serverId ?? ''
    }
    if (sel.kind === 'agent') {
      const agent = s.snapshot.agents.find((a) => a.id === sel.id)
      return s.snapshot.workspaces.find((w) => w.id === agent?.workspaceId)?.serverId ?? ''
    }
    return ''
  })
}

/**
 * Renders a server-scoped panel for the current selection.
 *
 * This must stay at module scope. Declared inside RightPanel it would be a new
 * function identity on every render, and React compares element types by
 * reference — so the whole subtree would unmount and remount, re-running the
 * child's data fetch and flashing its loading state several times a minute.
 */
function ForServer({ render }: { render: (serverId: string) => React.ReactNode }) {
  const t = useT()
  const serverId = useSelectedServerId()
  if (!serverId) {
    return <Empty title={t('panel.selectServer')} hint={t('panel.selectServer.hint')} />
  }
  return <>{render(serverId)}</>
}

export function RightPanel() {
  const t = useT()
  const panel = useAppStore((s) => s.rightPanel)
  const setPanel = useAppStore((s) => s.setRightPanel)
  const toggleRight = useAppStore((s) => s.toggleRight)
  const targets = useAppStore((s) => s.broadcastTargets)
  const width = useAppStore((s) => s.rightWidth)
  // Agents waiting on a human. A number, so the store only re-renders this
  // component when the count itself moves, not on every status poll.
  const attention = useAppStore((s) =>
    s.snapshot.agents.reduce((n, a) => n + (a.attention ? 1 : 0), 0),
  )

  const stripRef = useRef<HTMLDivElement | null>(null)
  const activeRef = useRef<HTMLButtonElement | null>(null)
  const [overflow, setOverflow] = useState({ left: false, right: false })
  const [drafts, setDrafts] = useState(0)
  const [pending, setPending] = useState(0)

  // A review queue nobody can see is a review queue nobody works through, so
  // the count rides on the tab. Re-read on every panel change rather than on a
  // timer: it is one local query, and the moment that matters is coming back
  // from having approved something.
  useEffect(() => {
    let alive = true
    void skillApi
      .stats()
      .then((s) => alive && setDrafts(s?.draft ?? 0))
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [panel])

  // An approval nobody notices is a run that stalls for half an hour and then
  // lapses, so the count is on the tab and it is pushed, not polled: the
  // request happens while you are looking at something else.
  useEffect(() => {
    let alive = true
    const load = () =>
      void orchApi
        .pending()
        .then((list) => alive && setPending(list?.length ?? 0))
        .catch(() => {})
    load()
    const offApproval = on('orch:approval', load)
    const offRun = on('orch:run', load)
    return () => {
      alive = false
      offApproval()
      offRun()
    }
  }, [])

  const syncOverflow = useCallback(() => {
    const el = stripRef.current
    if (!el) return
    setOverflow({
      left: el.scrollLeft > 2,
      right: el.scrollLeft + el.clientWidth < el.scrollWidth - 2,
    })
  }, [])

  useEffect(() => {
    const el = stripRef.current
    if (!el) return
    syncOverflow()

    // A vertical wheel does not move a strip that only scrolls horizontally,
    // so the gesture people actually make over a row of tabs is translated
    // here. Registered non-passive because it has to preventDefault, otherwise
    // the wheel keeps bubbling and scrolls something else.
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) return
      const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
      if (delta === 0) return
      e.preventDefault()
      el.scrollLeft += delta
    }

    el.addEventListener('scroll', syncOverflow, { passive: true })
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      el.removeEventListener('scroll', syncOverflow)
      el.removeEventListener('wheel', onWheel)
    }
  }, [syncOverflow])

  // Re-check when the panel is resized or the badge changes the strip's width.
  useEffect(() => {
    syncOverflow()
  }, [width, targets.length, syncOverflow])

  // Selecting a tab that is scrolled out of view should bring it into view.
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    syncOverflow()
  }, [panel, syncOverflow])

  const tabs: Array<{ id: PanelKind; label: MsgKey; icon: typeof Bot; badge?: number }> = [
    { id: 'detail', label: 'panel.detail', icon: Bot, badge: attention },
    { id: 'broadcast', label: 'panel.broadcast', icon: Radio, badge: targets.length },
    { id: 'metrics', label: 'panel.metrics', icon: Activity },
    { id: 'tmux', label: 'panel.tmux', icon: TerminalSquare },
    { id: 'toolkit', label: 'panel.install', icon: Sparkles },
    { id: 'memory', label: 'panel.memory', icon: Brain },
    { id: 'skills', label: 'panel.skills', icon: Wand2, badge: drafts },
    { id: 'orchestrator', label: 'panel.run', icon: Workflow, badge: pending },
  ]

  return (
    <div className="flex h-full w-full flex-col border-l hairline bg-ink-900">
      <div className="flex h-9 shrink-0 items-stretch border-b hairline px-1">
        {/* The strip scrolls rather than squeezing: five tabs do not fit a
            narrow panel, and shrinking them to fit makes every label unreadable
            instead of just the ones off-screen. */}
        <div className="relative min-w-0 flex-1">
          <div ref={stripRef} className="no-scrollbar flex h-full items-stretch gap-0.5 overflow-x-auto">
            {tabs.map((tab) => {
              const Icon = tab.icon
              const active = panel === tab.id
              return (
                <button
                  key={tab.id}
                  ref={active ? activeRef : undefined}
                  onClick={() => setPanel(tab.id)}
                  className={clsx(
                    // A raised pill rather than an underline. Underlined tabs
                    // are a web convention; on Apple's platforms selection in a
                    // strip like this is shown by lifting the chosen one.
                    'my-[5px] flex shrink-0 items-center gap-1.5 rounded-control px-2.5',
                    'text-[11px] font-medium whitespace-nowrap transition-colors duration-100',
                    active
                      ? 'bg-raised text-ink-100 shadow-sm'
                      : 'text-ink-400 hover:bg-ink-800 hover:text-ink-200',
                  )}
                >
                  <Icon size={12} />
                  {t(tab.label)}
                  {!!tab.badge && <Badge tone="accent">{tab.badge}</Badge>}
                </button>
              )
            })}
          </div>
          {/* Fades hint that the strip continues. They are opaque for the first
              half so a clipped tab dissolves instead of looking sliced off. */}
          {overflow.left && (
            <span className="pointer-events-none absolute inset-y-0 left-0 w-9 bg-gradient-to-r from-ink-900 via-ink-900/80 to-transparent" />
          )}
          {overflow.right && (
            <span className="pointer-events-none absolute inset-y-0 right-0 w-9 bg-gradient-to-l from-ink-900 via-ink-900/80 to-transparent" />
          )}
        </div>
        <button
          onClick={toggleRight}
          title={t('panel.collapse')}
          className="shrink-0 border-l hairline px-2 text-ink-500 hover:text-ink-100"
        >
          <PanelRightClose size={14} />
        </button>
      </div>

      <div className="min-h-0 flex-1">
        {panel === 'broadcast' && <BroadcastPanel />}
        {panel === 'metrics' && <ForServer render={(id) => <MetricsPanel serverId={id} />} />}
        {panel === 'tmux' && <ForServer render={(id) => <TmuxPanel serverId={id} />} />}
        {panel === 'toolkit' && <ForServer render={(id) => <ToolkitPanel serverId={id} />} />}
        {/* Memory is not server-scoped: what is remembered spans the fleet. */}
        {panel === 'memory' && <MemoryPanel />}
        {panel === 'skills' && <SkillPanel />}
        {panel === 'orchestrator' && <OrchestratorPanel />}
        {panel === 'detail' && <DetailRouter />}
      </div>
    </div>
  )
}

function DetailRouter() {
  const t = useT()
  const selection = useAppStore((s) => s.selection)
  const snapshot = useAppStore((s) => s.snapshot)
  const openTab = useAppStore((s) => s.openTab)
  const openDialog = useDialogs((s) => s.open)

  if (selection.kind === 'server') {
    const server = snapshot.servers.find((s) => s.id === selection.id)
    if (!server) return <Empty title={t('panel.serverNotFound')} />
    return <ServerDetail server={server} />
  }

  if (selection.kind === 'agent') {
    const agent = snapshot.agents.find((a) => a.id === selection.id)
    const workspace = snapshot.workspaces.find((w) => w.id === agent?.workspaceId)
    if (!agent || !workspace) return <Empty title={t('panel.agentNotFound')} />
    return <AgentDetail agent={agent} workspace={workspace} />
  }

  if (selection.kind === 'workspace') {
    const ws = snapshot.workspaces.find((w) => w.id === selection.id)
    if (!ws) return <Empty title={t('panel.workspaceNotFound')} />
    const server = snapshot.servers.find((s) => s.id === ws.serverId)
    const wsAgents = snapshot.agents.filter((a) => a.workspaceId === ws.id)
    return (
      <div className="flex h-full flex-col">
        <div className="border-b hairline px-3 py-3">
          <h3 className="truncate text-sm font-semibold text-ink-100">{ws.name}</h3>
          <p className="truncate font-mono text-[11px] text-ink-400">{ws.remotePath}</p>
          <p className="mt-1 text-[11px] text-ink-500">
            {t('panel.onServer', { name: server?.name ?? t('tree.missingServer') })}
          </p>
          <div className="mt-2.5 flex flex-wrap gap-1.5">
            <Button
              size="sm"
              variant="primary"
              onClick={() =>
                openTab({
                  title: ws.name,
                  kind: 'shell',
                  serverId: ws.serverId,
                  workspaceId: ws.id,
                  agentId: '',
                  tmuxSession: '',
                })
              }
            >
              <TerminalSquare size={11} /> {t('tree.openShell')}
            </Button>
            <Button size="sm" onClick={() => openDialog({ kind: 'agent', workspaceId: ws.id })}>
              <Plus size={11} /> {t('tree.addAgent')}
            </Button>
          </div>
          {Object.keys(ws.env).length > 0 && (
            <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 font-mono text-[11px]">
              {Object.entries(ws.env).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-ink-500">{k}</dt>
                  <dd className="truncate text-ink-300">{v}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <p className="px-3 py-1.5 text-[11px] font-semibold text-ink-300">
            {t('panel.agentsCount', { n: wsAgents.length })}
          </p>
          {wsAgents.map((a) => (
            <button
              key={a.id}
              onClick={() => useAppStore.getState().select({ kind: 'agent', id: a.id })}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[11px] hover:bg-ink-850"
            >
              <Bot size={12} className="text-ink-500" />
              <span className="min-w-0 flex-1 truncate text-ink-200">{a.name}</span>
              {a.attention && (
                <AttentionDot
                  kind={a.attention}
                  title={t(
                    a.attention === 'input' ? 'agent.attention.input' : 'agent.attention.done',
                  )}
                />
              )}
              <Badge
                tone={
                  a.status === 'running'
                    ? a.activity === 'input'
                      ? 'warn'
                      : a.activity === 'quiet'
                        ? 'neutral'
                        : 'ok'
                    : 'neutral'
                }
              >
                {agentActivityLabel(t, a)}
              </Badge>
            </button>
          ))}
          {!wsAgents.length && (
            <p className="px-3 py-2 text-[11px] text-ink-600">{t('panel.noAgentsHere')}</p>
          )}
        </div>
      </div>
    )
  }

  if (selection.kind === 'project') {
    const project = snapshot.projects.find((p) => p.id === selection.id)
    if (!project) return <Empty title={t('panel.projectNotFound')} />
    const wss = snapshot.workspaces.filter((w) => w.projectId === project.id)
    return (
      <div className="flex h-full flex-col">
        <div className="border-b hairline px-3 py-3">
          <h3 className="truncate text-sm font-semibold text-ink-100">{project.name}</h3>
          {project.description && (
            <p className="mt-1 text-[11px] leading-relaxed text-ink-400">{project.description}</p>
          )}
          <Button
            size="sm"
            className="mt-2.5"
            onClick={() => openDialog({ kind: 'workspace', projectId: project.id })}
          >
            <Plus size={11} /> {t('tree.addWorkspace')}
          </Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <p className="px-3 py-1.5 text-[11px] font-semibold text-ink-300">
            {t('panel.workspacesCount', { n: wss.length })}
          </p>
          {wss.map((w) => {
            const server = snapshot.servers.find((s) => s.id === w.serverId)
            return (
              <button
                key={w.id}
                onClick={() => useAppStore.getState().select({ kind: 'workspace', id: w.id })}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[11px] hover:bg-ink-850"
              >
                <span className="min-w-0 flex-1 truncate text-ink-200">{w.name}</span>
                <span className="truncate text-ink-600">{server?.name ?? t('common.none')}</span>
              </button>
            )
          })}
        </div>
      </div>
    )
  }

  return (
    <Empty title={t('panel.nothingSelected')} hint={t('panel.nothingSelected.hint')} />
  )
}
