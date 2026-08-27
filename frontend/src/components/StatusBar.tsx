import { Bot, RefreshCw, Server, Settings, ShieldAlert } from 'lucide-react'
import { useEffect, useState } from 'react'
import { agents as agentApi, errText } from '../lib/api'
import { isDesktopKind } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'

export function StatusBar() {
  const t = useT()
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
  // Desktop hosts are not counted: nothing is held open to one between
  // sessions, so counting them would mean the tally never reads full.
  const reachable = snapshot.servers.filter((s) => !isDesktopKind(s.kind)).length
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
    <footer className="flex h-7 shrink-0 items-center gap-3 border-t hairline bg-ink-900 px-2.5 text-[11px] text-ink-400">
      <span className="flex items-center gap-1.5">
        <Server size={12} className="opacity-60" />
        {t('status.connected', { n: connected, total: reachable })}
      </span>
      <span className="flex items-center gap-1.5">
        <Bot size={12} className="opacity-60" />
        {t('status.running', { n: running, total: snapshot.agents.length })}
      </span>

      <span className="flex-1" />

      {diagnostics && !diagnostics.keyLocationOk && (
        <button
          onClick={() => openDialog({ kind: 'settings' })}
          className="flex items-center gap-1.5 text-warn hover:underline"
          title={t('status.keyInFile.title')}
        >
          <ShieldAlert size={12} /> {t('status.keyInFile')}
        </button>
      )}

      <span className="hidden text-ink-600 sm:inline">{t('status.refreshedAgo', { n: ago })}</span>
      <button
        onClick={refresh}
        disabled={busy}
        title={t('status.refreshNow')}
        className="hover:text-ink-100"
      >
        <RefreshCw size={12} className={busy ? 'animate-spin' : undefined} />
      </button>
      <button
        onClick={() => openDialog({ kind: 'settings' })}
        title={t('settings.title')}
        className="hover:text-ink-100"
      >
        <Settings size={12} />
      </button>
      {/* A keyboard hint means nothing where there is no keyboard. */}
      <span className="hidden text-ink-600 sm:inline">⌘K</span>
    </footer>
  )
}
