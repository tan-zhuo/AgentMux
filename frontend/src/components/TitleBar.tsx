import { Window } from '@wailsio/runtime'
import clsx from 'clsx'
import { PanelLeft, PanelRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useAppStore } from '../store/useAppStore'

type Light = 'close' | 'minimise' | 'zoom'

const lightColor: Record<Light, { fill: string; ring: string; glyph: string }> = {
  close: { fill: '#ff5f57', ring: '#e0443e', glyph: '#4d0000' },
  minimise: { fill: '#febc2e', ring: '#dea123', glyph: '#985712' },
  zoom: { fill: '#28c840', ring: '#1aab29', glyph: '#0a5c14' },
}

/**
 * macOS-style traffic lights for a frameless window.
 *
 * The glyphs only appear while the pointer is over the cluster, matching macOS
 * where the buttons read as plain dots until you go for them, and the whole
 * cluster desaturates when the window loses focus.
 */
function TrafficLight({
  kind,
  label,
  focused,
  hovered,
  maximised,
  onClick,
}: {
  kind: Light
  label: string
  focused: boolean
  hovered: boolean
  maximised: boolean
  onClick: () => void
}) {
  const c = lightColor[kind]
  const inactive = !focused

  return (
    // Deliberately no title attribute: macOS shows no tooltip here, and the
    // native one pops a white box over the title bar.
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className="no-drag-region relative block h-3 w-3 rounded-full transition-colors duration-150"
      style={{
        // The lit colours are macOS's and stay fixed across themes — that is
        // what makes them recognisable. Only the unfocused grey follows the theme.
        backgroundColor: inactive ? 'var(--color-ink-600)' : c.fill,
        boxShadow: inactive ? 'none' : `inset 0 0 0 0.5px ${c.ring}`,
      }}
    >
      <svg
        viewBox="0 0 12 12"
        className={clsx(
          'absolute inset-0 h-3 w-3 transition-opacity duration-100',
          hovered && !inactive ? 'opacity-100' : 'opacity-0',
        )}
        aria-hidden
      >
        {kind === 'close' && (
          <path
            d="M4 4 L8 8 M8 4 L4 8"
            stroke={c.glyph}
            strokeWidth={1.4}
            strokeLinecap="round"
            fill="none"
          />
        )}
        {kind === 'minimise' && (
          <path
            d="M3.3 6 H8.7"
            stroke={c.glyph}
            strokeWidth={1.4}
            strokeLinecap="round"
            fill="none"
          />
        )}
        {kind === 'zoom' &&
          (maximised ? (
            // Restore: triangles folded back toward the centre.
            <g fill={c.glyph}>
              <path d="M3.2 8.8 H6.4 L3.2 5.6 Z" />
              <path d="M8.8 3.2 H5.6 L8.8 6.4 Z" />
            </g>
          ) : (
            // Zoom: triangles pushing out to opposite corners. They must not
            // meet in the middle or the pair reads as one filled blob.
            <g fill={c.glyph}>
              <path d="M3.1 3.1 H6.3 L3.1 6.3 Z" />
              <path d="M8.9 8.9 H5.7 L8.9 5.7 Z" />
            </g>
          ))}
      </svg>
    </button>
  )
}

/**
 * The window's own title bar. The window is frameless so that these controls
 * replace the platform ones; everything outside the buttons drags the window.
 */
export function TitleBar() {
  const tabs = useAppStore((s) => s.tabs)
  const activeTabId = useAppStore((s) => s.activeTabId)
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const rightOpen = useAppStore((s) => s.rightOpen)
  const toggleSidebar = useAppStore((s) => s.toggleSidebar)
  const toggleRight = useAppStore((s) => s.toggleRight)
  const loading = useAppStore((s) => s.loading)

  const [focused, setFocused] = useState(true)
  const [hovered, setHovered] = useState(false)
  const [maximised, setMaximised] = useState(false)

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

  const syncMaximised = () => {
    void Window.IsMaximised()
      .then(setMaximised)
      .catch(() => {})
  }

  useEffect(() => {
    syncMaximised()
    window.addEventListener('resize', syncMaximised)
    return () => window.removeEventListener('resize', syncMaximised)
  }, [])

  const active = tabs.find((t) => t.id === activeTabId)

  return (
    <header className="drag-region relative flex h-[38px] shrink-0 items-center gap-3 border-b hairline bg-ink-850 pr-2 pl-3 select-none">
      <div
        className="no-drag-region flex items-center gap-2"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <TrafficLight
          kind="close"
          label="Close window"
          focused={focused}
          hovered={hovered}
          maximised={maximised}
          onClick={() => void Window.Close()}
        />
        <TrafficLight
          kind="minimise"
          label="Minimise window"
          focused={focused}
          hovered={hovered}
          maximised={maximised}
          onClick={() => void Window.Minimise()}
        />
        <TrafficLight
          kind="zoom"
          label={maximised ? 'Restore window' : 'Zoom window'}
          focused={focused}
          hovered={hovered}
          maximised={maximised}
          onClick={() => {
            void Window.ToggleMaximise().then(syncMaximised)
          }}
        />
      </div>

      <span
        className={clsx(
          'ml-2 text-xs font-semibold tracking-tight transition-colors',
          focused ? 'text-ink-100' : 'text-ink-500',
        )}
      >
        AgentMux
      </span>

      {/* Centred document title, the way a native window titles itself. */}
      <span
        className={clsx(
          'pointer-events-none absolute inset-x-0 mx-auto max-w-[46%] truncate text-center text-[11px] transition-colors',
          focused ? 'text-ink-400' : 'text-ink-600',
        )}
      >
        {loading ? 'loading' : (active?.title ?? 'multi-server agent control plane')}
      </span>

      <div className="ml-auto flex items-center gap-0.5">
        <TitleBarButton active={sidebarOpen} title="Toggle sidebar (Ctrl+B)" onClick={toggleSidebar}>
          <PanelLeft size={14} />
        </TitleBarButton>
        <TitleBarButton active={rightOpen} title="Toggle panel" onClick={toggleRight}>
          <PanelRight size={14} />
        </TitleBarButton>
      </div>
    </header>
  )
}

function TitleBarButton({
  children,
  title,
  active,
  onClick,
}: {
  children: React.ReactNode
  title: string
  active?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className={clsx(
        'no-drag-region rounded-md p-1.5 transition-colors',
        active
          ? 'text-ink-200 hover:bg-ink-800'
          : 'text-ink-600 hover:bg-ink-800 hover:text-ink-300',
      )}
    >
      {children}
    </button>
  )
}
