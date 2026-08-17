import clsx from 'clsx'
import { Pencil, Play, RefreshCw, RotateCw, Send, Skull, Square, Terminal, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { agents as agentApi, errText } from '../../lib/api'
import type { Agent, Workspace } from '../../lib/types'
import { refreshServerAgents, useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { useDialogs } from '../../store/useDialogs'
import { Badge, Button, StatusDot, inputClass } from '../ui'

interface ChatEntry {
  id: string
  who: 'you' | 'system'
  text: string
  ok?: boolean
}

export function AgentDetail({ agent, workspace }: { agent: Agent; workspace: Workspace }) {
  const openTab = useAppStore((s) => s.openTab)
  const toast = useAppStore((s) => s.toast)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const openDialog = useDialogs((s) => s.open)

  const [logs, setLogs] = useState('')
  const [loadingLogs, setLoadingLogs] = useState(false)
  const [message, setMessage] = useState('')
  const [execute, setExecute] = useState(true)
  const [chat, setChat] = useState<ChatEntry[]>([])
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
    void loadLogs()
  }, [agent.id, loadLogs])

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [logs])

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
          ? `delivered to ${r.target}${execute ? '' : ' (not submitted)'}`
          : `failed: ${r.error}`,
      },
    ])
    if (!r.ok) toast('error', r.error)
    setTimeout(() => void loadLogs(), 700)
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
            <Button size="sm" onClick={() => openDialog({ kind: 'agent', agent })} title="Edit agent">
              <Pencil size={11} />
            </Button>
            <Button
              size="sm"
              variant="danger"
              title="Delete agent definition (remote session keeps running)"
              onClick={async () => {
                const ok = await confirmAction({
                  title: `Delete ${agent.name}`,
                  message: 'The agent definition is removed from AgentMux.',
                  reassurance: `Its tmux session ${agent.tmuxSession} keeps running on the server. You can still attach to it from the tmux panel.`,
                  confirmLabel: 'Delete agent',
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
          <Badge tone={agent.status === 'running' ? 'ok' : 'neutral'}>{agent.status}</Badge>
          {agent.pid ? <Badge>pid {agent.pid}</Badge> : null}
          {agent.lastSeen ? (
            <Badge>seen {new Date(agent.lastSeen * 1000).toLocaleTimeString()}</Badge>
          ) : null}
        </div>

        <p className="mt-2 truncate font-mono text-[11px] text-ink-500" title={agent.command}>
          $ {agent.command}
        </p>
        {agent.progressText && (
          <p className="mt-1 truncate rounded bg-ink-850 px-2 py-1 font-mono text-[11px] text-ink-300">
            {agent.progressText}
          </p>
        )}

        <div className="mt-2.5 flex flex-wrap gap-1.5">
          <Button
            size="sm"
            variant="primary"
            onClick={() => void act(() => agentApi.start(agent.id), `${agent.name} started`)}
          >
            <Play size={11} /> Start
          </Button>
          <Button
            size="sm"
            onClick={() => void act(() => agentApi.stop(agent.id), `Ctrl-C sent to ${agent.name}`)}
          >
            <Square size={11} /> Stop
          </Button>
          <Button
            size="sm"
            onClick={() => void act(() => agentApi.restart(agent.id), `${agent.name} restarted`)}
          >
            <RotateCw size={11} /> Restart
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
            <Terminal size={11} /> Attach
          </Button>
          <Button
            size="sm"
            variant="danger"
            title="Kill the tmux session — loses remote scrollback"
            onClick={async () => {
              const ok = await confirmAction({
                title: `Kill ${agent.tmuxSession}`,
                message: 'The tmux session is destroyed along with everything running inside it.',
                points: [
                  'The agent process is terminated, not asked to stop',
                  'The pane and all of its scrollback are lost',
                  'Unsaved work inside the session is gone',
                ],
                confirmLabel: 'Kill session',
                requireText: agent.name,
              })
              if (!ok) return
              await act(() => agentApi.kill(agent.id), 'Session killed')
            }}
          >
            <Skull size={11} /> Kill
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex items-center justify-between border-b hairline px-3 py-1.5">
          <span className="text-[11px] font-semibold text-ink-300">
            Pane output
          </span>
          <Button size="sm" variant="subtle" onClick={loadLogs} disabled={loadingLogs}>
            <RefreshCw size={11} className={loadingLogs ? 'animate-spin' : undefined} />
          </Button>
        </div>
        <pre
          ref={logRef}
          className="min-h-0 flex-1 overflow-auto bg-ink-950 px-3 py-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-ink-300"
        >
          {logs || (loadingLogs ? 'loading…' : 'No output captured.')}
        </pre>
      </div>

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
            placeholder={`Message ${agent.name}…`}
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
          Press Enter after sending
        </label>
      </div>
    </div>
  )
}
