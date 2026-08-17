import { Window } from '@wailsio/runtime'
import clsx from 'clsx'
import { useEffect, useState } from 'react'
import { ContextMenu } from './components/ContextMenu'
import { EditorPane } from './components/EditorPane'
import { FileBrowser } from './components/FileBrowser'
import { TerminalPane } from './components/TerminalPane'
import { Toasts } from './components/Toasts'
import { Empty } from './components/ui'
import { errText, terminal as termApi, windows } from './lib/api'
import type { DetachedTab } from './lib/types'
import { useAppStore, type Tab } from './store/useAppStore'
import { useTheme } from './store/useTheme'

const lightColor = {
  close: { fill: '#ff5f57', ring: '#e0443e' },
  minimise: { fill: '#febc2e', ring: '#dea123' },
  zoom: { fill: '#28c840', ring: '#1aab29' },
}

/**
 * A window holding a single torn-out tab.
 *
 * It talks to the same backend as the main window and adopts the PTY that was
 * already open, so tearing a tab out continues the session rather than starting
 * a second one beside it.
 */
export function DetachedApp({ token }: { token: string }) {
  const initTheme = useTheme((s) => s.init)
  const [tab, setTab] = useState<Tab | null>(null)
  const [error, setError] = useState('')
  const [focused, setFocused] = useState(true)

  useEffect(() => {
    useAppStore.setState({ detached: true })
    void initTheme()
  }, [initTheme])

  // Same reasoning as the main window: the webview's own menu offers Reload,
  // which would throw away this window's adopted session.
  useEffect(() => {
    const onMenu = (e: MouseEvent) => {
      if (!e.defaultPrevented) e.preventDefault()
    }
    document.addEventListener('contextmenu', onMenu)
    return () => document.removeEventListener('contextmenu', onMenu)
  }, [])

  useEffect(() => {
    const on = () => setFocused(true)
    const off = () => setFocused(false)
    window.addEventListener('focus', on)
    window.addEventListener('blur', off)
    return () => {
      window.removeEventListener('focus', on)
      window.removeEventListener('blur', off)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    windows
      .claim(token)
      .then((d: DetachedTab) => {
        if (cancelled) return
        setTab({
          id: `detached-${token}`,
          title: d.title || 'Terminal',
          kind: d.kind,
          serverId: d.serverId,
          workspaceId: d.workspaceId,
          agentId: d.agentId,
          tmuxSession: d.tmuxSession,
          command: d.command,
          adoptShellId: d.shellId || undefined,
          status: 'pending',
        })
      })
      .catch((e) => !cancelled && setError(errText(e)))
    return () => {
      cancelled = true
    }
  }, [token])

  // Closing the window ends its session, the same as closing a tab. For tmux
  // and agent tabs that is a detach, so the remote work carries on.
  useEffect(() => {
    const shutdown = () => {
      const id = useAppStore.getState().tabs.find((t) => t.id === tab?.id)?.shellId
      if (id) void termApi.close(id).catch(() => {})
    }
    window.addEventListener('pagehide', shutdown)
    return () => window.removeEventListener('pagehide', shutdown)
  }, [tab?.id])

  // The pane reports its state through the shared store, so mirror it back.
  const stored = useAppStore((s) => s.tabs.find((t) => t.id === tab?.id))
  useEffect(() => {
    if (!tab) return
    const exists = useAppStore.getState().tabs.some((t) => t.id === tab.id)
    if (!exists) {
      useAppStore.setState((s) => ({ tabs: [...s.tabs, tab], activeTabId: tab.id }))
    }
  }, [tab])

  const live = stored ?? tab

  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-ink-900">
      <header className="drag-region flex h-[38px] shrink-0 items-center gap-3 border-b hairline bg-ink-900 pr-2 pl-3 select-none">
        <div className="no-drag-region flex items-center gap-2">
          <TrafficLight kind="close" focused={focused} onClick={() => void Window.Close()} />
          <TrafficLight kind="minimise" focused={focused} onClick={() => void Window.Minimise()} />
          <TrafficLight kind="zoom" focused={focused} onClick={() => void Window.ToggleMaximise()} />
        </div>
        <span
          className={clsx(
            'ml-2 truncate text-xs font-medium',
            focused ? 'text-ink-100' : 'text-ink-500',
          )}
        >
          {live?.title ?? 'AgentMux'}
        </span>
        <span className="ml-auto text-[10px] text-ink-600">detached</span>
      </header>

      <div className="relative min-h-0 flex-1">
        {error ? (
          <Empty title="Could not open this tab" hint={error} />
        ) : !live ? (
          <Empty title="Opening" hint="Taking over the session from the main window." />
        ) : live.kind === 'files' ? (
          <FileBrowser tab={live} />
        ) : live.kind === 'editor' ? (
          <EditorPane tab={live} active />
        ) : (
          <TerminalPane tab={live} active />
        )}
      </div>

      <ContextMenu />
      <Toasts />
    </div>
  )
}

function TrafficLight({
  kind,
  focused,
  onClick,
}: {
  kind: keyof typeof lightColor
  focused: boolean
  onClick: () => void
}) {
  const c = lightColor[kind]
  return (
    <button
      type="button"
      aria-label={kind}
      onClick={onClick}
      className="no-drag-region block h-3 w-3 rounded-capsule transition-colors"
      style={{
        backgroundColor: focused ? c.fill : 'var(--color-ink-600)',
        boxShadow: focused ? `inset 0 0 0 0.5px ${c.ring}` : 'none',
      }}
    />
  )
}
