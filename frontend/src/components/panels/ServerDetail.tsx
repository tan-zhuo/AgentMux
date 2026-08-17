import { Link2, Link2Off, Pencil, ShieldAlert, Sparkles, Zap } from 'lucide-react'
import { useEffect, useState } from 'react'
import { errText, servers as serverApi } from '../../lib/api'
import type { Probe, Server } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { useDialogs } from '../../store/useDialogs'
import { Badge, Button, Empty } from '../ui'
import { TmuxPanel } from './TmuxPanel'

export function ServerDetail({ server }: { server: Server }) {
  const connections = useAppStore((s) => s.connections)
  const connState = useAppStore((s) => s.connState)
  const refreshConnections = useAppStore((s) => s.refreshConnections)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)

  const [probe, setProbe] = useState<Probe | null>(null)
  const [busy, setBusy] = useState(false)
  // Latches once the server has been reachable. A reconnect briefly reports
  // "connecting", and without this the tmux panel below would unmount and
  // remount on every blip, flashing its loading state.
  const [everReached, setEverReached] = useState(false)

  useEffect(() => {
    setProbe(null)
    setEverReached(false)
  }, [server.id])

  const connected = !!connections[server.id]?.connected
  const state = connState[server.id]

  useEffect(() => {
    if (connected || probe?.ok) setEverReached(true)
  }, [connected, probe?.ok])

  async function test() {
    setBusy(true)
    try {
      const p = await serverApi.test(server.id)
      setProbe(p)
      if (!p.ok) toast('error', p.error)
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
      await refreshConnections()
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="border-b hairline px-3 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold text-ink-100">{server.name}</h3>
            <p className="truncate font-mono text-[11px] text-ink-400">
              {server.username}@{server.host}:{server.port}
            </p>
          </div>
          <Button size="sm" onClick={() => openDialog({ kind: 'server', server })}>
            <Pencil size={11} /> Edit
          </Button>
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Badge tone={connected ? 'ok' : 'neutral'}>
            {connected ? 'connected' : 'disconnected'}
          </Badge>
          <Badge tone="accent">{server.authType}</Badge>
          {server.jumpServerId && <Badge tone="warn">via jump host</Badge>}
          {server.tags.map((t) => (
            <Badge key={t}>{t}</Badge>
          ))}
        </div>

        {state?.state === 'error' && (
          <p className="mt-2 rounded border border-danger/30 bg-danger/10 px-2 py-1.5 text-[11px] leading-relaxed text-danger">
            {state.detail}
          </p>
        )}

        <div className="mt-2.5 flex flex-wrap gap-1.5">
          <Button size="sm" variant="primary" disabled={busy} onClick={test}>
            <Zap size={11} /> Test
          </Button>
          {connected ? (
            <Button
              size="sm"
              onClick={async () => {
                await serverApi.disconnect(server.id)
                await refreshConnections()
              }}
            >
              <Link2Off size={11} /> Disconnect
            </Button>
          ) : (
            <Button
              size="sm"
              onClick={async () => {
                try {
                  await serverApi.connect(server.id)
                  toast('ok', `Connected to ${server.name}`)
                } catch (e) {
                  toast('error', errText(e))
                }
                await refreshConnections()
              }}
            >
              <Link2 size={11} /> Connect
            </Button>
          )}
          <Button
            size="sm"
            title="Detect and install agent CLIs on this server"
            onClick={() => useAppStore.getState().setRightPanel('toolkit')}
          >
            <Sparkles size={11} /> Install agents
          </Button>
          {server.hostKey && (
            <Button
              size="sm"
              variant="danger"
              title="Forget the pinned host key so the next connection trusts a new one"
              onClick={async () => {
                const ok = await confirmAction({
                  title: 'Forget the pinned host key',
                  message:
                    'The next connection will trust whatever key the server offers, and pin that one instead.',
                  points: [
                    'Only do this if you rotated the key yourself',
                    'If you did not, a mismatch may mean someone is intercepting the connection',
                  ],
                  tone: 'warning',
                  confirmLabel: 'Clear pinned key',
                })
                if (!ok) return
                await serverApi.clearHostKey(server.id)
                await refreshSnapshot()
                toast('warn', 'Host key pin cleared')
              }}
            >
              <ShieldAlert size={11} /> Clear pin
            </Button>
          )}
        </div>

        {probe && probe.ok && (
          <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px]">
            <dt className="text-ink-500">Latency</dt>
            <dd className="text-ink-200">{probe.latencyMs} ms</dd>
            <dt className="text-ink-500">OS</dt>
            <dd className="truncate text-ink-200">{probe.os || '—'}</dd>
            <dt className="text-ink-500">Uptime</dt>
            <dd className="truncate text-ink-200">{probe.uptime || '—'}</dd>
            <dt className="text-ink-500">Load</dt>
            <dd className="truncate text-ink-200">{probe.load || '—'}</dd>
            <dt className="text-ink-500">tmux</dt>
            <dd className={probe.hasTmux ? 'text-ok' : 'text-danger'}>
              {probe.hasTmux ? probe.tmuxVersion : 'not installed'}
            </dd>
          </dl>
        )}
      </div>

      <div className="min-h-0 flex-1">
        {connected || probe?.ok || everReached ? (
          <TmuxPanel serverId={server.id} />
        ) : (
          <Empty
            title="Not connected"
            hint="Connect or test the server to list its tmux sessions."
          />
        )}
      </div>
    </div>
  )
}
