import { Bot, RefreshCw, Server, Settings, ShieldAlert } from 'lucide-react'
import { useEffect, useState } from 'react'
import { agents as agentApi, errText } from '../lib/api'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'

export function StatusBar() {
  const snapshot = useAppStore((s) => s.snapshot)
  const connections = useAppStore((s) => s.connections)
  const diagnostics = useAppStore((s) => s.diagnostics)
  const applyAgents = useAppStore((s) => s.applyAgents)
  const refreshConnections = useAppStore((s) => s.refreshConnections)
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)

  const [lastRefresh, setLastRefresh] = useState(Date.now())
  const [ago, setAgo] = useState(0)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const t = window.setInterval(() => setAgo(Math.floor((Date.now() - lastRefresh) / 1000)), 1000)
    return () => window.clearInterval(t)
  }, [lastRefresh])

  const connected = Object.values(connections).filter((c) => c.connected).length
  const running = snapshot.agents.filter((a) => a.status === 'running').length

  async function refresh() {
    setBusy(true)
    try {
      applyAgents(await agentApi.refreshAll())
      await refreshConnections()
      setLastRefresh(Date.now())
      setAgo(0)
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <footer className="flex h-7 shrink-0 items-center gap-3 border-t hairline bg-ink-850 px-2.5 text-[11px] text-ink-400">
      <span className="flex items-center gap-1.5">
        <Server size={12} className="opacity-60" />
        {connected}/{snapshot.servers.length} connected
      </span>
      <span className="flex items-center gap-1.5">
        <Bot size={12} className="opacity-60" />
        {running}/{snapshot.agents.length} running
      </span>

      <span className="flex-1" />

      {diagnostics && !diagnostics.keyLocationOk && (
        <button
          onClick={() => openDialog({ kind: 'settings' })}
          className="flex items-center gap-1.5 text-warn hover:underline"
          title="The OS keychain was unavailable, so the master key lives in a 0600 file"
        >
          <ShieldAlert size={12} /> key in file
        </button>
      )}

      <span className="text-ink-600">refreshed {ago}s ago</span>
      <button onClick={refresh} disabled={busy} title="Refresh now" className="hover:text-ink-100">
        <RefreshCw size={12} className={busy ? 'animate-spin' : undefined} />
      </button>
      <button
        onClick={() => openDialog({ kind: 'settings' })}
        title="Settings"
        className="hover:text-ink-100"
      >
        <Settings size={12} />
      </button>
      <span className="text-ink-600">⌘K</span>
    </footer>
  )
}
