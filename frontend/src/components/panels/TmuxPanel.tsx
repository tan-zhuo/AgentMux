import { Plus, RefreshCw, Terminal, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { errText, tmux as tmuxApi } from '../../lib/api'
import type { TmuxServerView } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { Badge, Button, Empty, inputClass } from '../ui'

/** Lists the tmux sessions living on one server, with attach / kill controls.
 *  This is the view that makes "my agent is still running over there" concrete. */
export function TmuxPanel({ serverId }: { serverId: string }) {
  const openTab = useAppStore((s) => s.openTab)
  const toast = useAppStore((s) => s.toast)
  const toggleTarget = useAppStore((s) => s.toggleBroadcastTarget)
  const isTarget = useAppStore((s) => s.isBroadcastTarget)
  // Subscribed so the checkboxes re-render when the selection changes.
  useAppStore((s) => s.broadcastTargets)
  const [view, setView] = useState<TmuxServerView | null>(null)
  const [loading, setLoading] = useState(false)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setView(await tmuxApi.view(serverId))
    } catch (e) {
      toast('error', errText(e))
      setView(null)
    } finally {
      setLoading(false)
    }
  }, [serverId, toast])

  useEffect(() => {
    void load()
  }, [load])

  const sessions = view?.sessions ?? []
  const panes = view?.panes ?? []

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b hairline px-3 py-2">
        <span className="text-[11px] font-semibold text-ink-300">
          tmux sessions {sessions.length > 0 && `(${sessions.length})`}
        </span>
        <div className="flex gap-1">
          <Button size="sm" variant="subtle" onClick={() => setCreating((v) => !v)} title="New session">
            <Plus size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={load} disabled={loading} title="Refresh">
            <RefreshCw size={11} className={loading ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {creating && (
        <div className="flex gap-1.5 border-b hairline px-3 py-2">
          <input
            autoFocus
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="session name (no ':' or '.')"
            className={inputClass}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void create()
              if (e.key === 'Escape') setCreating(false)
            }}
          />
          <Button size="sm" variant="primary" onClick={() => void create()}>
            Create
          </Button>
        </div>
      )}

      {view?.error && (
        <p className="border-b hairline px-3 py-2 text-[11px] text-danger">{view.error}</p>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {sessions.length === 0 && !loading && !view?.error && (
          <Empty
            title="No tmux sessions"
            hint="Start an agent, or create a session here. Anything inside tmux survives losing this connection."
          />
        )}
        {sessions.map((s) => {
          const sessionPanes = panes.filter((p) => p.sessionName === s.name)
          return (
            <div key={s.name} className="border-b hairline px-3 py-2">
              <div className="flex items-center gap-1.5">
                {/* Sessions are broadcast targets in their own right. Most work
                    starts as a session with no agent record behind it. */}
                <input
                  type="checkbox"
                  checked={isTarget({ agentId: '', serverId, session: s.name })}
                  onChange={() => toggleTarget({ agentId: '', serverId, session: s.name })}
                  title="Include in broadcast"
                  className="h-3 w-3 shrink-0 accent-[#4c8dff]"
                />
                <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-100">
                  {s.name}
                </span>
                {s.attached && <Badge tone="ok">attached</Badge>}
                <Badge>{s.windows}w</Badge>
                <button
                  title="Attach in a new tab"
                  onClick={() =>
                    openTab({
                      title: s.name.split('/').pop() || s.name,
                      kind: 'tmux',
                      serverId,
                      workspaceId: '',
                      agentId: '',
                      tmuxSession: s.name,
                    })
                  }
                  className="rounded-control p-1 text-ink-400 hover:bg-ink-750 hover:text-ink-100"
                >
                  <Terminal size={12} />
                </button>
                <button
                  title="Kill session — this stops everything inside it"
                  onClick={async () => {
                    const ok = await confirmAction({
                      title: `Kill ${s.name}`,
                      message: 'The tmux session is destroyed along with every window and pane in it.',
                      points: [
                        `${s.windows} window${s.windows === 1 ? '' : 's'} and their scrollback are lost`,
                        'Processes inside are terminated, not asked to stop',
                      ],
                      confirmLabel: 'Kill session',
                    })
                    if (!ok) return
                    try {
                      await tmuxApi.killSession(serverId, s.name)
                      await load()
                    } catch (e) {
                      toast('error', errText(e))
                    }
                  }}
                  className="rounded-control p-1 text-ink-400 hover:bg-ink-750 hover:text-danger"
                >
                  <Trash2 size={12} />
                </button>
              </div>
              {sessionPanes.length > 0 && (
                <ul className="mt-1 space-y-0.5 pl-1">
                  {sessionPanes.map((p) => (
                    <li
                      key={p.paneId}
                      className="flex items-center gap-2 font-mono text-[10.5px] text-ink-500"
                    >
                      <span className="text-ink-600">
                        {p.windowIndex}.{p.paneIndex}
                      </span>
                      <span className={p.active ? 'text-ink-300' : ''}>{p.command}</span>
                      <span className="min-w-0 flex-1 truncate">{p.path}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )

  async function create() {
    const name = newName.trim()
    if (!name) return
    try {
      await tmuxApi.createSession(serverId, name, '')
      setNewName('')
      setCreating(false)
      await load()
    } catch (e) {
      toast('error', errText(e))
    }
  }
}
