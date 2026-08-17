import clsx from 'clsx'
import {
  Bot,
  ChevronsRight,
  ExternalLink,
  FileCode2,
  FolderTree,
  Layers,
  PackagePlus,
  TerminalSquare,
  X,
  XCircle,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useAppStore, type Tab } from '../store/useAppStore'
import { openContextMenu, separator } from '../store/useContextMenu'
import { EditorPane } from './EditorPane'
import { FileBrowser } from './FileBrowser'
import { TerminalPane } from './TerminalPane'
import { Empty } from './ui'

const kindIcon = {
  shell: TerminalSquare,
  tmux: Layers,
  agent: Bot,
  command: PackagePlus,
  files: FolderTree,
  editor: FileCode2,
}

/** How far the pointer must leave the strip before a drag becomes a tear-off
 *  rather than a reorder. Generous enough that a sloppy horizontal drag does
 *  not accidentally spawn a window. */
const TEAR_OFF_DISTANCE = 90

interface DragState {
  id: string
  /** Index the tab started at, used to detect a no-op drop. */
  fromIndex: number
  pointerId: number
  startX: number
  startY: number
  /** Offset of the grab point inside the tab, so it does not jump on grab. */
  grabOffset: number
  moved: boolean
  tearing: boolean
}

export function TerminalArea() {
  const tabs = useAppStore((s) => s.tabs)
  const activeTabId = useAppStore((s) => s.activeTabId)
  const setActiveTab = useAppStore((s) => s.setActiveTab)
  const closeTab = useAppStore((s) => s.closeTab)
  const moveTab = useAppStore((s) => s.moveTab)
  const detachTab = useAppStore((s) => s.detachTab)

  const stripRef = useRef<HTMLDivElement | null>(null)
  const drag = useRef<DragState | null>(null)

  // The tab strip scrolls sideways once there are more tabs than fit, and a
  // vertical wheel does not move a horizontally-scrolling element on its own.
  useEffect(() => {
    const el = stripRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) return
      const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
      if (delta === 0) return
      e.preventDefault()
      el.scrollLeft += delta
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])
  const [dragId, setDragId] = useState<string | null>(null)
  const [dropIndex, setDropIndex] = useState<number | null>(null)
  const [tearing, setTearing] = useState(false)

  /** Which slot the pointer is currently over, measured from the real tab
   *  geometry rather than assumed widths — tab labels differ in length. */
  function slotAt(clientX: number): number {
    const strip = stripRef.current
    if (!strip) return 0
    const els = Array.from(strip.querySelectorAll<HTMLElement>('[data-tab-index]'))
    for (let i = 0; i < els.length; i++) {
      const r = els[i].getBoundingClientRect()
      if (clientX < r.left + r.width / 2) return i
    }
    return els.length
  }

  function onPointerDown(e: React.PointerEvent<HTMLDivElement>, tab: Tab, index: number) {
    // Left button only; the close button stops propagation itself.
    if (e.button !== 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    drag.current = {
      id: tab.id,
      fromIndex: index,
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      grabOffset: e.clientX - rect.left,
      moved: false,
      tearing: false,
    }
    e.currentTarget.setPointerCapture(e.pointerId)
    setActiveTab(tab.id)
  }

  function onPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    const d = drag.current
    if (!d || e.pointerId !== d.pointerId) return

    const dx = e.clientX - d.startX
    const dy = e.clientY - d.startY
    if (!d.moved && Math.abs(dx) < 4 && Math.abs(dy) < 4) return
    d.moved = true

    const strip = stripRef.current?.getBoundingClientRect()
    const outside =
      !!strip &&
      (e.clientY > strip.bottom + TEAR_OFF_DISTANCE ||
        e.clientY < strip.top - TEAR_OFF_DISTANCE ||
        e.clientX < strip.left - TEAR_OFF_DISTANCE ||
        e.clientX > strip.right + TEAR_OFF_DISTANCE)

    if (outside !== d.tearing) {
      d.tearing = outside
      setTearing(outside)
    }
    setDragId(d.id)
    setDropIndex(outside ? null : slotAt(e.clientX))
  }

  async function onPointerUp(e: React.PointerEvent<HTMLDivElement>) {
    const d = drag.current
    if (!d || e.pointerId !== d.pointerId) return
    drag.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      /* already released */
    }

    const wasTearing = d.tearing
    const slot = dropIndex
    setDragId(null)
    setDropIndex(null)
    setTearing(false)

    if (!d.moved) return

    if (wasTearing) {
      // Open the new window roughly where the tab was dropped, so it lands
      // under the pointer the way a browser's does.
      const w = Math.max(720, Math.round(window.innerWidth * 0.62))
      const h = Math.max(480, Math.round(window.innerHeight * 0.72))
      await detachTab(d.id, Math.round(e.screenX - d.grabOffset), Math.round(e.screenY - 20), w, h)
      return
    }

    if (slot === null) return
    // Removing the tab first shifts every later slot down by one.
    const to = slot > d.fromIndex ? slot - 1 : slot
    moveTab(d.fromIndex, to)
  }

  return (
    <div className="flex min-w-0 flex-1 flex-col bg-ink-950">
      <div
        ref={stripRef}
        className="relative flex h-9 shrink-0 items-stretch gap-px overflow-x-auto border-b hairline bg-ink-900"
      >
        {tabs.map((tab, index) => {
          const Icon = kindIcon[tab.kind]
          const active = tab.id === activeTabId
          const isDragging = dragId === tab.id
          return (
            <div
              key={tab.id}
              data-tab-index={index}
              onPointerDown={(e) => onPointerDown(e, tab, index)}
              onPointerMove={onPointerMove}
              onPointerUp={(e) => void onPointerUp(e)}
              onPointerCancel={() => {
                drag.current = null
                setDragId(null)
                setDropIndex(null)
                setTearing(false)
              }}
              onContextMenu={(e) =>
                openContextMenu(e, [
                  {
                    label: 'Open in new window',
                    icon: ExternalLink,
                    onSelect: () =>
                      void detachTab(
                        tab.id,
                        Math.round(window.screenX + 80),
                        Math.round(window.screenY + 80),
                        Math.max(720, Math.round(window.innerWidth * 0.62)),
                        Math.max(480, Math.round(window.innerHeight * 0.72)),
                      ),
                  },
                  separator,
                  {
                    label: 'Close tab',
                    icon: X,
                    onSelect: () => void closeTab(tab.id),
                  },
                  {
                    label: 'Close other tabs',
                    icon: XCircle,
                    disabled: tabs.length < 2,
                    onSelect: () => {
                      for (const other of tabs.filter((t) => t.id !== tab.id)) {
                        void closeTab(other.id)
                      }
                    },
                  },
                  {
                    label: 'Close tabs to the right',
                    icon: ChevronsRight,
                    disabled: index >= tabs.length - 1,
                    onSelect: () => {
                      for (const other of tabs.slice(index + 1)) void closeTab(other.id)
                    },
                  },
                ])
              }
              className={clsx(
                'group relative flex min-w-0 shrink-0 cursor-default touch-none items-center gap-1.5 border-r hairline px-3 text-xs select-none',
                active
                  ? 'bg-ink-950 text-ink-100'
                  : 'bg-ink-900 text-ink-400 hover:bg-ink-850 hover:text-ink-200',
                isDragging && 'opacity-50',
              )}
            >
              {/* Insertion marker, shown on the edge the tab would land at. */}
              {dropIndex === index && dragId && !tearing && (
                <span className="pointer-events-none absolute inset-y-1 -left-px w-0.5 rounded-control bg-accent" />
              )}
              <Icon size={12} className="shrink-0 opacity-70" />
              <span className="max-w-[180px] truncate">{tab.title}</span>
              {tab.status === 'opening' && (
                <span className="h-1.5 w-1.5 animate-pulse rounded-capsule bg-warn" />
              )}
              {(tab.status === 'closed' || tab.status === 'error') && (
                <span className="h-1.5 w-1.5 rounded-capsule bg-ink-600" />
              )}
              <button
                onPointerDown={(e) => e.stopPropagation()}
                onClick={(e) => {
                  e.stopPropagation()
                  void closeTab(tab.id)
                }}
                className="ml-0.5 rounded-control p-0.5 text-ink-500 opacity-0 group-hover:opacity-100 hover:bg-ink-750 hover:text-ink-100"
                title="Close tab"
              >
                <X size={11} />
              </button>
            </div>
          )
        })}
        {/* Marker for dropping after the last tab. */}
        {dragId && !tearing && dropIndex === tabs.length && (
          <span className="pointer-events-none relative -ml-px w-0.5 self-stretch rounded-control bg-accent" />
        )}
        {!tabs.length && (
          <div className="flex items-center px-3 text-[11px] text-ink-500">No open terminals</div>
        )}
      </div>

      {tearing && (
        <div className="pointer-events-none absolute inset-x-0 top-9 z-20 flex justify-center">
          <span className="rounded-b-control border border-t-0 border-accent-dim bg-ink-850 px-3 py-1 text-[11px] text-accent shadow-lg">
            Release to open in a new window
          </span>
        </div>
      )}

      <div className="relative min-h-0 flex-1">
        {tabs.length === 0 ? (
          <Empty
            title="Nothing attached yet"
            hint="Pick a server, workspace or agent on the left. Agent and tmux tabs reattach to work that is already running remotely."
          />
        ) : (
          tabs.map((tab) => (
            <div
              key={tab.id}
              className="absolute inset-0"
              style={{ visibility: tab.id === activeTabId ? 'visible' : 'hidden' }}
            >
              {tab.kind === 'files' ? (
                <FileBrowser tab={tab} />
              ) : tab.kind === 'editor' ? (
                <EditorPane tab={tab} active={tab.id === activeTabId} />
              ) : (
                <TerminalPane tab={tab} active={tab.id === activeTabId} />
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
