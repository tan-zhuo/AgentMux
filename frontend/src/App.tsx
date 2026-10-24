import clsx from 'clsx'
import { useEffect, useRef, useState } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { ConfirmDialog } from './components/ConfirmDialog'
import { ContextMenu } from './components/ContextMenu'
import { Dialogs } from './components/Dialogs'
import { showEditMenu } from './lib/editMenu'
import { MobileNav } from './components/MobileNav'
import { RightPanel } from './components/RightPanel'
import { Sidebar } from './components/Sidebar'
import { Splitter } from './components/Splitter'
import { StatusBar } from './components/StatusBar'
import { TerminalArea } from './components/TerminalArea'
import { TerminalKeyBar } from './components/TerminalKeyBar'
import { TitleBar } from './components/TitleBar'
import { Toasts } from './components/Toasts'
import { UpdateBanner } from './components/UpdateBanner'
import { on } from './lib/api'
import { STREAM_RESUMED } from './lib/webTransport'
import { useCompact, useKeyboardInset, useMedium, useTouchDevice, useViewportResizing } from './lib/useCompact'
import { usePresence } from './lib/motion'
import { useFittedPanes } from './lib/panes'
import type { Agent, ConnState } from './lib/types'
import { RIGHT_DEFAULT, RIGHT_MIN, SIDEBAR_DEFAULT, SIDEBAR_MIN, useAppStore } from './store/useAppStore'
import { useI18n } from './store/useI18n'
import { useTermKeys } from './store/useTermKeys'
import { useTheme } from './store/useTheme'

export default function App() {
  const loadAll = useAppStore((s) => s.loadAll)
  const initTheme = useTheme((s) => s.init)
  const initI18n = useI18n((s) => s.init)
  const applyAgents = useAppStore((s) => s.applyAgents)
  const applyConnState = useAppStore((s) => s.applyConnState)
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const rightOpen = useAppStore((s) => s.rightOpen)
  const sidebarWidth = useAppStore((s) => s.sidebarWidth)
  const rightWidth = useAppStore((s) => s.rightWidth)
  const setSidebarWidth = useAppStore((s) => s.setSidebarWidth)
  const setRightWidth = useAppStore((s) => s.setRightWidth)
  const setPaletteOpen = useAppStore((s) => s.setPaletteOpen)
  const toggleSidebar = useAppStore((s) => s.toggleSidebar)
  const toggleRight = useAppStore((s) => s.toggleRight)
  const activeTabId = useAppStore((s) => s.activeTabId)
  const activeKind = useAppStore((s) => s.tabs.find((tb) => tb.id === s.activeTabId)?.kind)
  const initTermKeys = useTermKeys((s) => s.init)
  const compact = useCompact()
  const medium = useMedium()
  const touch = useTouchDevice()
  const keyboardInset = useKeyboardInset()

  // Each side panel is held through its exit so it can slide shut rather than
  // vanish, and the width transition is switched off while its splitter is
  // being dragged — a transition on a value the pointer is driving makes the
  // seam trail the hand. The window edge drives it too, while the window is
  // being resized and the panels re-fitted to it.
  const side = usePresence(sidebarOpen ? true : null, 300)
  const right = usePresence(rightOpen ? true : null, 300)
  const [resizing, setResizing] = useState(false)
  const windowResizing = useViewportResizing()
  // Nor while the persisted widths are still being read: a width that lands
  // from the store should be where the panel starts, not somewhere it slides to.
  const loading = useAppStore((s) => s.loading)
  const animateWidth = !resizing && !windowResizing && !loading

  // Where each panel goes. Below 768px neither can dock, so both ride over the
  // terminal as drawers; up to 1023px — a tablet held upright — the tree docks
  // and the panel floats; from 1024px both dock. A docked panel gets its stored
  // width where the window has room for it beside a usable terminal, and a
  // narrower one where it has not: widths chosen on a monitor would otherwise
  // squeeze a tablet's terminal down to a word a line. A panel fitted to null
  // has no dock at all and floats.
  const fit = useFittedPanes(
    !compact && side.value ? sidebarWidth : null,
    !compact && !medium && right.value ? rightWidth : null,
  )

  // The key bar belongs to a terminal. A file browser or an editor has its own
  // controls, and Esc into either of them means nothing.
  const onTerminal = activeKind !== undefined && activeKind !== 'files' && activeKind !== 'editor'

  // A phone starts with the terminal, not with two open drawers.
  useEffect(() => {
    if (!compact) return
    const s = useAppStore.getState()
    if (s.sidebarOpen) s.toggleSidebar()
    if (s.rightOpen) s.toggleRight()
    // Only on entering compact — the user may reopen them afterwards.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [compact])

  // Likewise a tablet held upright starts with the tree and the terminal, not
  // with the panel it has no room to dock lying over the terminal. It comes
  // back as a drawer when asked for.
  useEffect(() => {
    if (!medium) return
    const s = useAppStore.getState()
    if (s.rightOpen) s.toggleRight()
  }, [medium])

  // Picking a tab from the drawer means "show me that", so the drawer yields.
  useEffect(() => {
    if (!compact || !activeTabId) return
    const s = useAppStore.getState()
    if (s.sidebarOpen) s.toggleSidebar()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTabId])

  // One drawer at a time on a phone: whichever was opened second wins.
  const prevDrawers = useRef({ side: sidebarOpen, right: rightOpen })
  useEffect(() => {
    const prev = prevDrawers.current
    prevDrawers.current = { side: sidebarOpen, right: rightOpen }
    if (!compact || !sidebarOpen || !rightOpen) return
    if (!prev.right) toggleSidebar()
    else if (!prev.side) toggleRight()
  }, [compact, sidebarOpen, rightOpen, toggleSidebar, toggleRight])

  useEffect(() => {
    // Theme first: it only needs one settings read, so the window is painted in
    // the right colours before the (slower) tree snapshot lands.
    void initTheme()
    void initI18n()
    void initTermKeys()
    void loadAll()
  }, [loadAll, initTheme, initI18n, initTermKeys])

  // Backend pushes: agent status polling and connection state changes.
  useEffect(() => {
    const offAgents = on<Agent[]>('agents:updated', (list) => applyAgents(list ?? []))
    const offConn = on<ConnState>('server:state', (s) => s && applyConnState(s))
    // A stream that was away missed whatever changed while it was, and state
    // that only arrives when it changes does not come back on its own: a host
    // that dropped, an agent that finished. Ask once, rather than show
    // yesterday's answer until something else happens to move.
    const refresh = () => void loadAll()
    window.addEventListener(STREAM_RESUMED, refresh)
    return () => {
      offAgents()
      offConn()
      window.removeEventListener(STREAM_RESUMED, refresh)
    }
  }, [applyAgents, applyConnState, loadAll])

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
      // Zoom the focused pane, for reading something a crowded grid made too
      // small. Enter rather than a letter: the terminal owns the letters.
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === 'Enter') {
        e.preventDefault()
        useAppStore.getState().toggleZoom()
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
    <div
      className="flex h-full w-full flex-col overflow-hidden bg-ink-900"
      // On iOS the page keeps its full height behind the keyboard, so the
      // terminal's last lines and the key bar would both sit under it. Giving
      // the covered height back as padding compresses the whole app above the
      // keyboard instead. Elsewhere this is zero and changes nothing.
      style={keyboardInset ? { paddingBottom: keyboardInset } : undefined}
    >
      <TitleBar />
      <UpdateBanner />

      <div className="relative flex min-h-0 flex-1">
        {side.value &&
          (fit.side == null ? (
            // On a phone there is no room to dock three columns, so the tree
            // rides over the terminal as a drawer; picking something closes it.
            <>
              <button
                type="button"
                aria-label="Close sidebar"
                className={clsx(
                  'absolute inset-0 z-30 bg-black/45',
                  // Inert on the way out, scrim and drawer both: the scrim
                  // toggles the drawer, so a tap during its fade would bring
                  // back what the previous tap had just closed.
                  side.exiting ? 'animate-fade-out pointer-events-none' : 'animate-fade-in',
                )}
                onClick={toggleSidebar}
              />
              <div
                className={clsx(
                  'material-bar absolute inset-y-0 left-0 z-40 w-[min(320px,85vw)] border-r hairline shadow-2xl',
                  side.exiting ? 'animate-drawer-out pointer-events-none' : 'animate-drawer-in',
                )}
              >
                <Sidebar />
              </div>
            </>
          ) : (
            <>
              <div
                style={{ width: side.exiting ? 0 : fit.side }}
                className={clsx(
                  'relative shrink-0 overflow-hidden',
                  animateWidth && 'transition-[width] duration-[var(--duration-base)] ease-[var(--ease-out)]',
                  !side.exiting && 'animate-reveal-left',
                )}
              >
                {/* Pinned to its full width inside the clipping wrapper, so the
                    tree slides behind the seam rather than squashing as the
                    panel closes. */}
                <div style={{ width: fit.side }} className="absolute inset-y-0 right-0">
                  <Sidebar />
                </div>
              </div>
              {!side.exiting && (
                <Splitter
                  label="Sidebar width"
                  value={fit.side}
                  min={SIDEBAR_MIN}
                  max={fit.sideMax}
                  resetTo={SIDEBAR_DEFAULT}
                  onChange={(w) => setSidebarWidth(w)}
                  onCommit={(w) => setSidebarWidth(w, true)}
                  onDragging={setResizing}
                />
              )}
            </>
          ))}
        <TerminalArea />
        {right.value &&
          (fit.right == null ? (
            // Without a dock — a phone, a tablet upright, or a window with no
            // room for it beside the tree — the panel floats the same way.
            <>
              <button
                type="button"
                aria-label="Close panel"
                className={clsx(
                  'absolute inset-0 z-30 bg-black/45',
                  right.exiting ? 'animate-fade-out pointer-events-none' : 'animate-fade-in',
                )}
                onClick={toggleRight}
              />
              <div
                className={clsx(
                  'material-bar absolute inset-y-0 right-0 z-40 w-[min(360px,92vw)] border-l hairline shadow-2xl',
                  right.exiting ? 'animate-drawer-out-right pointer-events-none' : 'animate-drawer-in-right',
                )}
              >
                <RightPanel />
              </div>
            </>
          ) : (
            <>
              {!right.exiting && (
                <Splitter
                  label="Panel width"
                  value={fit.right}
                  min={RIGHT_MIN}
                  max={fit.rightMax}
                  resetTo={RIGHT_DEFAULT}
                  invert
                  onChange={(w) => setRightWidth(w)}
                  onCommit={(w) => setRightWidth(w, true)}
                  onDragging={setResizing}
                />
              )}
              <div
                style={{ width: right.exiting ? 0 : fit.right }}
                className={clsx(
                  'relative shrink-0 overflow-hidden',
                  animateWidth && 'transition-[width] duration-[var(--duration-base)] ease-[var(--ease-out)]',
                  !right.exiting && 'animate-reveal-right',
                )}
              >
                <div style={{ width: fit.right }} className="absolute inset-y-0 left-0">
                  <RightPanel />
                </div>
              </div>
            </>
          ))}
      </div>

      {touch && onTerminal && <TerminalKeyBar />}
      {compact ? <MobileNav /> : <StatusBar />}
      <Dialogs />
      <CommandPalette />
      <ConfirmDialog />
      <ContextMenu />
      <Toasts />
    </div>
  )
}
