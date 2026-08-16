import { useEffect } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { Dialogs } from './components/Dialogs'
import { RightPanel } from './components/RightPanel'
import { Sidebar } from './components/Sidebar'
import { StatusBar } from './components/StatusBar'
import { TerminalArea } from './components/TerminalArea'
import { TitleBar } from './components/TitleBar'
import { Toasts } from './components/Toasts'
import { on } from './lib/api'
import type { Agent, ConnState } from './lib/types'
import { useAppStore } from './store/useAppStore'
import { useTheme } from './store/useTheme'

export default function App() {
  const loadAll = useAppStore((s) => s.loadAll)
  const initTheme = useTheme((s) => s.init)
  const applyAgents = useAppStore((s) => s.applyAgents)
  const applyConnState = useAppStore((s) => s.applyConnState)
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const rightOpen = useAppStore((s) => s.rightOpen)
  const setPaletteOpen = useAppStore((s) => s.setPaletteOpen)

  useEffect(() => {
    // Theme first: it only needs one settings read, so the window is painted in
    // the right colours before the (slower) tree snapshot lands.
    void initTheme()
    void loadAll()
  }, [loadAll, initTheme])

  // Backend pushes: agent status polling and connection state changes.
  useEffect(() => {
    const offAgents = on<Agent[]>('agents:updated', (list) => applyAgents(list ?? []))
    const offConn = on<ConnState>('server:state', (s) => s && applyConnState(s))
    return () => {
      offAgents()
      offConn()
    }
  }, [applyAgents, applyConnState])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen(!useAppStore.getState().paletteOpen)
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') {
        e.preventDefault()
        useAppStore.getState().toggleSidebar()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setPaletteOpen])

  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-ink-900">
      <TitleBar />

      <div className="flex min-h-0 flex-1">
        {sidebarOpen && (
          <div className="w-72 shrink-0">
            <Sidebar />
          </div>
        )}
        <TerminalArea />
        {rightOpen && (
          <div className="w-96 shrink-0">
            <RightPanel />
          </div>
        )}
      </div>

      <StatusBar />
      <Dialogs />
      <CommandPalette />
      <Toasts />
    </div>
  )
}
