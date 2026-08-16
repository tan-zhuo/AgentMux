import { useEffect } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { ConfirmDialog } from './components/ConfirmDialog'
import { Dialogs } from './components/Dialogs'
import { RightPanel } from './components/RightPanel'
import { Sidebar } from './components/Sidebar'
import { Splitter } from './components/Splitter'
import { StatusBar } from './components/StatusBar'
import { TerminalArea } from './components/TerminalArea'
import { TitleBar } from './components/TitleBar'
import { Toasts } from './components/Toasts'
import { on } from './lib/api'
import type { Agent, ConnState } from './lib/types'
import {
  RIGHT_DEFAULT,
  RIGHT_MAX,
  RIGHT_MIN,
  SIDEBAR_DEFAULT,
  SIDEBAR_MAX,
  SIDEBAR_MIN,
  useAppStore,
} from './store/useAppStore'
import { useTheme } from './store/useTheme'

export default function App() {
  const loadAll = useAppStore((s) => s.loadAll)
  const initTheme = useTheme((s) => s.init)
  const applyAgents = useAppStore((s) => s.applyAgents)
  const applyConnState = useAppStore((s) => s.applyConnState)
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const rightOpen = useAppStore((s) => s.rightOpen)
  const sidebarWidth = useAppStore((s) => s.sidebarWidth)
  const rightWidth = useAppStore((s) => s.rightWidth)
  const setSidebarWidth = useAppStore((s) => s.setSidebarWidth)
  const setRightWidth = useAppStore((s) => s.setRightWidth)
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
          <>
            <div style={{ width: sidebarWidth }} className="shrink-0">
              <Sidebar />
            </div>
            <Splitter
              label="Sidebar width"
              value={sidebarWidth}
              min={SIDEBAR_MIN}
              max={SIDEBAR_MAX}
              resetTo={SIDEBAR_DEFAULT}
              onChange={(w) => setSidebarWidth(w)}
              onCommit={(w) => setSidebarWidth(w, true)}
            />
          </>
        )}
        <TerminalArea />
        {rightOpen && (
          <>
            <Splitter
              label="Panel width"
              value={rightWidth}
              min={RIGHT_MIN}
              max={RIGHT_MAX}
              resetTo={RIGHT_DEFAULT}
              invert
              onChange={(w) => setRightWidth(w)}
              onCommit={(w) => setRightWidth(w, true)}
            />
            <div style={{ width: rightWidth }} className="shrink-0">
              <RightPanel />
            </div>
          </>
        )}
      </div>

      <StatusBar />
      <Dialogs />
      <CommandPalette />
      <ConfirmDialog />
      <Toasts />
    </div>
  )
}
