import clsx from 'clsx'
import {
  ChevronDown,
  ChevronRight,
  Pencil,
  Play,
  RefreshCw,
  RotateCw,
  Send,
  Skull,
  Square,
  Terminal,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { agents as agentApi, errText } from '../../lib/api'
import type { Agent, Workspace } from '../../lib/types'
import { agentStatusLabel } from '../../lib/agentStatus'
import { acknowledgeAgent, refreshServerAgents, useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { useDialogs } from '../../store/useDialogs'
import { useFmt, useT } from '../../store/useI18n'
import { Badge, Button, StatusDot, inputClass } from '../ui'

interface ChatEntry {
  id: string
  who: 'you' | 'system'
  text: string
  ok?: boolean
}

export function AgentDetail({ agent, workspace }: { agent: Agent; workspace: Workspace }) {
  const t = useT()
  const fmt = useFmt()
  const openTab = useAppStore((s) => s.openTab)
  const toast = useAppStore((s) => s.toast)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const openDialog = useDialogs((s) => s.open)

  const [logs, setLogs] = useState('')
  const [loadingLogs, setLoadingLogs] = useState(false)
  const [message, setMessage] = useState('')
  const [execute, setExecute] = useState(true)
  const [chat, setChat] = useState<ChatEntry[]>([])
  const [showLogs, setShowLogs] = useState(false)
  const logRef = useRef<HTMLPreElement | null>(null)

  const loadLogs = useCallback(async () => {
    setLoadingLogs(true)
    try {
      setLogs(await agentApi.logs(agent.id, 200))
    } catch (e) {
      setLogs(errText(e))
    } finally {
      setLoadingLogs(false)
    }
  }, [agent.id])

  useEffect(() => {
    setChat([])
    setLogs('')
  }, [agent.id])

  useEffect(() => {
    if (showLogs) void loadLogs()
  }, [showLogs, loadLogs])

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [logs, showLogs])

  async function act(fn: () => Promise<unknown>, okText: string) {
    try {
      await fn()
      toast('ok', okText)
      await refreshServerAgents(workspace.serverId)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function send() {
    const text = message.trim()
    if (!text) return
    setMessage('')
    const r = await agentApi.send(agent.id, text, execute)
    setChat((c) => [
      ...c,
      { id: `${Date.now()}-u`, who: 'you', text },
      {
        id: `${Date.now()}-s`,
        who: 'system',
        ok: r.ok,
        text: r.ok
          ? execute
            ? t('agentDetail.delivered', { target: r.target })
            : t('agentDetail.deliveredNotSubmitted', { target: r.target })
          : t('agentDetail.sendFailed', { error: r.error }),
      },
    ])
    if (!r.ok) toast('error', r.error)
    if (showLogs) setTimeout(() => void loadLogs(), 700)
  }

  return (
    <div className="flex h-full flex-col">
      <div className="border-b hairline px-3 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="flex items-center gap-1.5 truncate text-sm font-semibold text-ink-100">
              <StatusDot status={agent.status} pulse />
              {agent.name}
            </h3>
            <p className="truncate font-mono text-[11px] text-ink-400">{agent.tmuxSession}</p>
          </div>
          <div className="flex gap-1">
            <Button size="sm" onClick={() => openDialog({ kind: 'agent', agent })} title={t('tree.editAgent')}>
              <Pencil size={11} />
            </Button>
            <Button
              size="sm"
              variant="danger"
              title={t('agentDetail.deleteTitle')}
              onClick={async () => {
                const ok = await confirmAction({
                  title: t('confirm.deleteAgent.title', { name: agent.name }),
                  message: t('confirm.deleteAgent.message'),
                  reassurance: t('confirm.deleteAgent.reassuranceAttach', {
                    session: agent.tmuxSession,
                  }),
                  confirmLabel: t('tree.deleteAgent'),
                })
                if (!ok) return
                await agentApi.remove(agent.id)
                await refreshSnapshot()
              }}
            >
              <Trash2 size={11} />
            </Button>
          </div>
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Badge tone={agent.status === 'running' ? 'ok' : 'neutral'}>
            {agentStatusLabel(t, agent.status)}
          </Badge>
          {agent.status === 'running' && agent.activity === 'input' && (
            <Badge tone="warn">{t('agent.activity.input')}</Badge>
          )}
          {agent.status === 'running' && agent.activity === 'quiet' && (
            <Badge>{t('agent.activity.quiet')}</Badge>
          )}
          {agent.pid ? <Badge>{t('agentDetail.pid', { pid: agent.pid })}</Badge> : null}
          {agent.lastSeen ? (
            <Badge>{t('agentDetail.seen', { time: fmt.time(agent.lastSeen) })}</Badge>
          ) : null}
        </div>

        {agent.attention && (
          <div
            className={clsx(
              'mt-2 rounded-control border px-2.5 py-2',
              agent.attention === 'input'
                ? 'border-warn/40 bg-warn/10'
                : 'border-accent/40 bg-accent/10',
            )}
          >
            <p
              className={clsx(
                'text-[11px] font-semibold',
                agent.attention === 'input' ? 'text-warn' : 'text-accent',
              )}
            >
              {t(agent.attention === 'input' ? 'agent.attention.input' : 'agent.attention.done')}
            </p>
            <p className="mt-0.5 text-[11px] leading-relaxed text-ink-300">
              {t(
                agent.attention === 'input'
                  ? 'agent.attention.inputHint'
                  : 'agent.attention.doneHint',
              )}
            </p>
            <div className="mt-1.5 flex gap-1.5">
              <Button
                size="sm"
                variant="primary"
                onClick={() =>
                  // Opening the terminal acknowledges the mark by itself.
                  openTab({
                    title: agent.name,
                    kind: 'agent',
                    serverId: workspace.serverId,
                    workspaceId: workspace.id,
                    agentId: agent.id,
                    tmuxSession: agent.tmuxSession,
                  })
                }
              >
                <Terminal size={11} /> {t('agent.attention.open')}
              </Button>
              <Button size="sm" onClick={() => acknowledgeAgent(agent.id)}>
                {t('agent.attention.dismiss')}
              </Button>
            </div>
          </div>
        )}

        <p className="mt-2 truncate font-mono text-[11px] text-ink-500" title={agent.command}>
          $ {agent.command}
        </p>
        {agent.progressText && (
          <p className="mt-1 truncate rounded-control bg-ink-850 px-2 py-1 font-mono text-[11px] text-ink-300">
            {agent.progressText}
          </p>
        )}

        <div className="mt-2.5 flex flex-wrap gap-1.5">
          <Button
            size="sm"
            variant="primary"
            onClick={() =>
              void act(
                () => agentApi.start(agent.id),
                t('toast.agentStartedShort', { name: agent.name }),
              )
            }
          >
            <Play size={11} /> {t('agent.start')}
          </Button>
          <Button
            size="sm"
            onClick={() =>
              void act(
                () => agentApi.stop(agent.id),
                t('agentDetail.ctrlCSent', { name: agent.name }),
              )
            }
          >
            <Square size={11} /> {t('agent.stop')}
          </Button>
          <Button
            size="sm"
            onClick={() =>
              void act(
                () => agentApi.restart(agent.id),
                t('toast.agentRestarted', { name: agent.name }),
              )
            }
          >
            <RotateCw size={11} /> {t('agent.restart')}
          </Button>
          <Button
            size="sm"
            onClick={() =>
              openTab({
                title: agent.name,
                kind: 'agent',
                serverId: workspace.serverId,
                workspaceId: workspace.id,
                agentId: agent.id,
                tmuxSession: agent.tmuxSession,
              })
            }
          >
            <Terminal size={11} /> {t('agent.attach')}
          </Button>
          <Button
            size="sm"
            variant="danger"
            title={t('agentDetail.killTitle')}
            onClick={async () => {
              const ok = await confirmAction({
                title: t('confirm.killSession.title', { session: agent.tmuxSession }),
                message: t('confirm.killSession.message'),
                points: [
                  t('confirm.killSession.processTerminated'),
                  t('confirm.killSession.scrollback'),
                  t('confirm.killSession.unsaved'),
                ],
                confirmLabel: t('agent.killSession'),
                requireText: agent.name,
              })
              if (!ok) return
              await act(() => agentApi.kill(agent.id), t('toast.sessionKilled'))
            }}
          >
            <Skull size={11} /> {t('agent.kill')}
          </Button>
        </div>
      </div>

      <div className={clsx('flex flex-col', showLogs ? 'min-h-0 flex-1' : 'shrink-0')}>
        {/* The row is a fixed height so the disclosure and the refresh button
            sit on one line rather than one setting the height and the other
            floating inside it. */}
        <div className="flex h-7 shrink-0 items-center justify-between border-b hairline pr-3">
          <button
            type="button"
            onClick={() => setShowLogs((v) => !v)}
            className="flex h-full min-w-0 flex-1 items-center gap-1 px-3 text-left text-[11px] font-semibold text-ink-300 hover:text-ink-100"
            title={showLogs ? t('agentDetail.hideOutput') : t('agentDetail.showOutput')}
          >
            {showLogs ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            {t('agentDetail.paneOutput')}
          </button>
          {showLogs && (
            <Button size="sm" variant="subtle" onClick={loadLogs} disabled={loadingLogs}>
              <RefreshCw size={11} className={loadingLogs ? 'animate-spin' : undefined} />
            </Button>
          )}
        </div>
        {showLogs && (
          <pre
            ref={logRef}
            className="min-h-0 flex-1 overflow-auto bg-ink-950 px-3 py-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-ink-300"
          >
            {logs || (loadingLogs ? t('common.loading') : t('agentDetail.noOutput'))}
          </pre>
        )}
      </div>
      {!showLogs && <div className="min-h-0 flex-1" />}

      {chat.length > 0 && (
        <div className="max-h-40 shrink-0 overflow-y-auto border-t hairline px-3 py-2">
          {chat.map((c) => (
            <p
              key={c.id}
              className={clsx(
                'mb-1 text-[11px] leading-relaxed',
                c.who === 'you' ? 'text-ink-200' : c.ok ? 'text-ok' : 'text-danger',
              )}
            >
              <span className="mr-1.5 text-ink-600">{c.who === 'you' ? '›' : '·'}</span>
              {c.text}
            </p>
          ))}
        </div>
      )}

      <div className="shrink-0 border-t hairline px-3 py-2">
        <div className="flex gap-1.5">
          <input
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
            placeholder={t('agentDetail.message', { name: agent.name })}
            className={inputClass}
          />
          <Button variant="primary" size="sm" onClick={() => void send()}>
            <Send size={11} />
          </Button>
        </div>
        <label className="mt-1.5 flex items-center gap-1.5 text-[11px] text-ink-400">
          <input
            type="checkbox"
            checked={execute}
            onChange={(e) => setExecute(e.target.checked)}
            className="h-3 w-3 accent-[#4c8dff]"
          />
          {t('agentDetail.pressEnter')}
        </label>
      </div>
    </div>
  )
}
