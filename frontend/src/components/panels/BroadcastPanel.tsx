import clsx from 'clsx'
import { Bot, CheckCircle2, Layers, Radio, XCircle } from 'lucide-react'
import { useState } from 'react'
import { agents as agentApi, errText } from '../../lib/api'
import type { BroadcastTarget, Receipt } from '../../lib/types'
import { targetKey, useAppStore } from '../../store/useAppStore'
import { Button, Empty, StatusDot, textareaClass } from '../ui'

/** Sends one instruction to many recipients and shows a receipt for each, so a
 *  fan-out is verifiable instead of hopeful. */
export function BroadcastPanel() {
  const snapshot = useAppStore((s) => s.snapshot)
  const targets = useAppStore((s) => s.broadcastTargets)
  const setTargets = useAppStore((s) => s.setBroadcastTargets)
  const toggleTarget = useAppStore((s) => s.toggleBroadcastTarget)
  const toast = useAppStore((s) => s.toast)

  const [message, setMessage] = useState('')
  const [execute, setExecute] = useState(true)
  const [sending, setSending] = useState(false)
  const [receipts, setReceipts] = useState<Receipt[]>([])

  const agentById = new Map(snapshot.agents.map((a) => [a.id, a]))
  const serverById = new Map(snapshot.servers.map((s) => [s.id, s]))
  const okCount = receipts.filter((r) => r.ok).length

  /**
   * How a target reads in the list, whichever kind it is.
   *
   * The server is shown for both kinds, because a broadcast can span machines —
   * each target carries its own server — and without it a list of sessions from
   * two hosts is indistinguishable from a list from one.
   */
  function describe(t: BroadcastTarget) {
    if (t.agentId) {
      const a = agentById.get(t.agentId)
      const ws = a ? snapshot.workspaces.find((w) => w.id === a.workspaceId) : undefined
      return {
        name: a?.name ?? t.agentId,
        server: (ws ? serverById.get(ws.serverId)?.name : '') ?? '',
        session: a?.tmuxSession ?? '',
        status: a?.status,
        isAgent: true,
      }
    }
    return {
      name: t.session,
      server: serverById.get(t.serverId)?.name ?? t.serverId,
      session: t.session,
      status: undefined,
      isAgent: false,
    }
  }

  // How many machines this broadcast would reach, which is the thing worth
  // knowing before sending to a mixed list.
  const serverCount = new Set(
    targets.map((t) => {
      if (!t.agentId) return t.serverId
      const a = agentById.get(t.agentId)
      const ws = a ? snapshot.workspaces.find((w) => w.id === a.workspaceId) : undefined
      return ws?.serverId ?? ''
    }),
  ).size

  async function send() {
    const text = message.trim()
    if (!text || !targets.length) return
    setSending(true)
    setReceipts([])
    try {
      const rs = await agentApi.broadcastTo(targets, text, execute)
      setReceipts(rs)
      const failed = rs.filter((r) => !r.ok).length
      if (failed) toast('warn', `${rs.length - failed}/${rs.length} received the message`)
      else toast('ok', `All ${rs.length} received the message`)
      setMessage('')
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-ink-800 px-3 py-2">
        <span className="flex items-center gap-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
          <Radio size={11} /> Broadcast
        </span>
        {/* The count is a label, not a control. Giving it a pill made it a
            short box sitting in a row of taller buttons, which is what read as
            uneven — so it is plain text and the two real buttons match. */}
        <div className="flex items-center gap-1.5">
          <span
            className={clsx(
              'text-[10.5px] tabular-nums',
              targets.length ? 'text-accent' : 'text-ink-500',
            )}
          >
            {targets.length} selected
          </span>
          <Button
            size="sm"
            disabled={!snapshot.agents.length}
            onClick={() =>
              setTargets(snapshot.agents.map((a) => ({ agentId: a.id, serverId: '', session: '' })))
            }
          >
            All agents
          </Button>
          <Button size="sm" disabled={!targets.length} onClick={() => setTargets([])}>
            None
          </Button>
        </div>
      </div>

      <div className="max-h-44 min-h-0 overflow-y-auto border-b border-ink-800">
        {targets.length === 0 ? (
          <div className="px-3 py-3 text-[11px] leading-relaxed text-ink-500">
            <p>Nothing selected yet. You can pick either:</p>
            <ul className="mt-1.5 space-y-1">
              <li className="flex gap-2">
                <Bot size={12} className="mt-px shrink-0" />
                <span>agents in the tree, using the checkbox on each row</span>
              </li>
              <li className="flex gap-2">
                <Layers size={12} className="mt-px shrink-0" />
                <span>
                  tmux sessions in the tmux panel — use these when you started work from the file
                  browser and never registered an agent
                </span>
              </li>
            </ul>
          </div>
        ) : (
          targets.map((t) => {
            const d = describe(t)
            return (
              <div
                key={targetKey(t)}
                title={`${d.server} · ${d.session}`}
                className="flex items-center gap-2 px-3 py-1 text-[11px]"
              >
                {d.isAgent ? (
                  <StatusDot status={d.status ?? 'unknown'} />
                ) : (
                  <Layers size={11} className="shrink-0 text-ink-500" />
                )}
                <span className="min-w-0 flex-1 truncate text-ink-200">{d.name}</span>
                {d.server && (
                  <span className="max-w-[40%] shrink-0 truncate text-ink-500">{d.server}</span>
                )}
                <button
                  onClick={() => toggleTarget(t)}
                  className="shrink-0 text-ink-600 hover:text-danger"
                  title="Remove from broadcast"
                >
                  <XCircle size={12} />
                </button>
              </div>
            )
          })
        )}
      </div>

      <div className="shrink-0 border-b border-ink-800 px-3 py-2">
        <textarea
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void send()
          }}
          rows={3}
          placeholder="Instruction for everything selected…  (Ctrl+Enter to send)"
          className={clsx(textareaClass, 'resize-none font-mono')}
        />
        <div className="mt-1.5 flex items-center justify-between">
          <label className="flex items-center gap-1.5 text-[11px] text-ink-400">
            <input
              type="checkbox"
              checked={execute}
              onChange={(e) => setExecute(e.target.checked)}
              className="h-3 w-3 accent-[#4c8dff]"
            />
            Press Enter after sending
          </label>
          <Button
            variant="primary"
            size="sm"
            disabled={sending || !targets.length || !message.trim()}
            onClick={() => void send()}
          >
            <Radio size={11} />{' '}
            {sending
              ? 'Sending…'
              : serverCount > 1
                ? `Send to ${targets.length} on ${serverCount} servers`
                : `Send to ${targets.length}`}
          </Button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {receipts.length === 0 ? (
          <Empty title="No receipts yet" hint="Every delivery is confirmed individually." />
        ) : (
          <>
            <p className="px-3 py-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
              Receipts — {okCount}/{receipts.length} delivered
            </p>
            {receipts.map((r, i) => (
              <div key={`${r.agentId}-${r.agentName}-${i}`} className="flex items-start gap-2 px-3 py-1 text-[11px]">
                {r.ok ? (
                  <CheckCircle2 size={12} className="mt-0.5 shrink-0 text-ok" />
                ) : (
                  <XCircle size={12} className="mt-0.5 shrink-0 text-danger" />
                )}
                <span className="shrink-0 text-ink-200">{r.agentName || r.agentId}</span>
                <span
                  className={clsx('min-w-0 flex-1 truncate', r.ok ? 'text-ink-600' : 'text-danger')}
                >
                  {r.ok ? r.target : r.error}
                </span>
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  )
}
