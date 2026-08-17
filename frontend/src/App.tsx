import { useEffect } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { ConfirmDialog } from './components/ConfirmDialog'
import { ContextMenu } from './components/ContextMenu'
import { Dialogs } from './components/Dialogs'
import { showEditMenu } from './lib/editMenu'
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

  // The webview's own menu offers Reload and Back, which are meaningless here
  // and destructive — a stray Reload drops every attached terminal view. Text
  // fields get an editing menu instead of nothing, and everything else is
  // handled by the component that was clicked.
  useEffect(() => {
    const onMenu = (e: MouseEvent) => {
      const el = e.target as HTMLElement | null
      const editable =
        el &&
        (el.tagName === 'INPUT' ||
          el.tagName === 'TEXTAREA' ||
          el.isContentEditable ||
          el.closest('input, textarea'))
      if (editable) {
        e.preventDefault()
        void showEditMenu(e, el as HTMLElement)
        return
      }
      // Components that want a menu call openContextMenu, which already
      // prevented the default. Anything else simply gets no menu.
      if (!e.defaultPrevented) e.preventDefault()
    }
    document.addEventListener('contextmenu', onMenu)
    return () => document.removeEventListener('contextmenu', onMenu)
  }, [])

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
      // Split and unsplit, on the key every terminal app uses for it. The
      // terminal has keyboard focus almost all the time, so this has to be
      // caught here rather than inside a pane. `code` as well as `key`, because
      // shifted backslash is a different character on most layouts.
      if ((e.metaKey || e.ctrlKey) && (e.key === '\\' || e.key === '|' || e.code === 'Backslash')) {
        e.preventDefault()
        const store = useAppStore.getState()
        if (e.shiftKey) {
          if (store.activeTabId) store.closePane(store.activeTabId)
        } else {
          store.requestSplit()
        }
      }
      // Move between panes without reaching for the mouse, which is the whole
      // point of watching two agents at once.
      if ((e.metaKey || e.ctrlKey) && e.altKey && e.key.startsWith('Arrow')) {
        const forward = e.key === 'ArrowRight' || e.key === 'ArrowDown'
        const back = e.key === 'ArrowLeft' || e.key === 'ArrowUp'
        if (forward || back) {
          e.preventDefault()
          useAppStore.getState().focusPane(forward ? 1 : -1)
        }
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
      <ContextMenu />
      <Toasts />
    </div>
  )
}
