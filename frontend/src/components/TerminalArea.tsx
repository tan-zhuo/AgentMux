import clsx from 'clsx'
import {
  Bot,
  ChevronsRight,
  Columns2,
  Minimize2,
  Rows2,
  SplitSquareHorizontal,
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
import { MAX_PANES, useAppStore, type SplitAxis, type Tab } from '../store/useAppStore'
import { openContextMenu, separator } from '../store/useContextMenu'
import { useDialogs } from '../store/useDialogs'
import { EditorPane } from './EditorPane'
import { FileBrowser } from './FileBrowser'
import { TerminalPane } from './TerminalPane'
import { Empty } from './ui'

/** Where a pane sits, as percentages of the terminal area.
 *
 *  Computed rather than measured: a CSS grid would need its cells measured
 *  before the absolutely-positioned tabs could be placed into them, and a frame
 *  of wrong geometry is a frame of wrongly-sized terminal. */
function paneRect(index: number, count: number, axis: SplitAxis): React.CSSProperties {
  const pct = (n: number) => `${n}%`
  if (count <= 1) return { inset: 0 }
  if (count === 4) {
    const col = index % 2
    const row = index < 2 ? 0 : 1
    return { left: pct(col * 50), top: pct(row * 50), width: '50%', height: '50%' }
  }
  const share = 100 / count
  return axis === 'cols'
    ? { left: pct(index * share), top: 0, width: pct(share), height: '100%' }
    : { left: 0, top: pct(index * share), width: '100%', height: pct(share) }
}

/** Tabs that are open but not on screen stay mounted, laid out, and invisible.
 *
 *  Hiding them with `display: none` would collapse the terminal to zero columns
 *  and make xterm reflow every line the moment it came back. They are laid out
 *  at the size of a pane rather than of the whole area — every pane is the same
 *  size, so a tab swapped into one needs no resize, and the remote session is
 *  never reflowed to a width nobody is looking at. */
function hiddenRect(count: number, axis: SplitAxis): React.CSSProperties {
  return { ...paneRect(0, count, axis), visibility: 'hidden' }
}

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
  const panes = useAppStore((s) => s.paneIds)
  const splitAxis = useAppStore((s) => s.splitAxis)
  const closePane = useAppStore((s) => s.closePane)
  const splitWith = useAppStore((s) => s.splitWith)
  const setSplitAxis = useAppStore((s) => s.setSplitAxis)
  const openDialog = useDialogs((s) => s.open)

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
    // Positioned: the tear-out hint below the strip is placed against this.
    <div className="relative flex min-w-0 flex-1 flex-col bg-ink-950">
      <div className="flex h-9 shrink-0 items-stretch border-b hairline bg-ink-900">
        <div
          ref={stripRef}
          className="relative flex min-w-0 flex-1 items-stretch gap-px overflow-x-auto"
        >
          {tabs.map((tab, index) => {
            const Icon = kindIcon[tab.kind]
            const active = tab.id === activeTabId
            // In a split, more than one tab is on screen. Both are lifted out of
            // the strip's background; only the focused one is marked, because
            // "which pane am I typing into" is the question a split raises.
            const onScreen = panes.includes(tab.id)
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
                      label: 'Show in its own pane',
                      icon: SplitSquareHorizontal,
                      hint: onScreen
                        ? 'already on screen'
                        : panes.length >= MAX_PANES
                          ? 'four panes is the limit'
                          : undefined,
                      disabled: onScreen || panes.length >= MAX_PANES,
                      onSelect: () => splitWith(tab.id),
                    },
                    {
                      label: 'Close this pane',
                      icon: Minimize2,
                      hint: 'the tab stays open',
                      disabled: !onScreen || panes.length < 2,
                      onSelect: () => closePane(tab.id),
                    },
                    separator,
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
                  onScreen
                    ? active
                      ? 'bg-ink-950 text-ink-100'
                      : 'bg-ink-950 text-ink-300'
                    : 'bg-ink-900 text-ink-400 hover:bg-ink-850 hover:text-ink-200',
                  isDragging && 'opacity-50',
                )}
              >
                {/* Which pane has the keyboard, marked only when that is in
                    question — a single pane always has it. */}
                {active && panes.length > 1 && (
                  <span className="pointer-events-none absolute inset-x-0 top-0 h-0.5 bg-accent" />
                )}
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

        {tabs.length > 0 && (
          <div className="flex shrink-0 items-center gap-0.5 border-l hairline px-1.5">
            {/* Two and three panes divide along an axis; four are a 2×2 grid,
                where flipping would mean nothing, so the control goes away. */}
            {panes.length > 1 && panes.length < MAX_PANES && (
              <button
                onClick={() => setSplitAxis(splitAxis === 'cols' ? 'rows' : 'cols')}
                title={splitAxis === 'cols' ? 'Stack the panes instead' : 'Put the panes side by side'}
                className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
              >
                {splitAxis === 'cols' ? <Rows2 size={13} /> : <Columns2 size={13} />}
              </button>
            )}
            {panes.length > 1 && (
              <button
                onClick={() => activeTabId && closePane(activeTabId)}
                title="Close this pane — the tab and its shell stay open"
                className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
              >
                <Minimize2 size={13} />
              </button>
            )}
            {/* A click is deliberate, so it asks what the pane should show —
                including a shell or an agent that is not open yet. ⌘\ is the
                impatient path and takes the next tab without asking. */}
            <button
              onClick={() => openDialog({ kind: 'split' })}
              disabled={panes.length >= MAX_PANES}
              title={
                panes.length >= MAX_PANES
                  ? 'Four panes is the limit — close one first'
                  : 'Add a pane: a host, a workspace, an agent or an open tab (⌘\\ splits with the next tab)'
              }
              className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100 disabled:pointer-events-none disabled:opacity-30"
            >
              <SplitSquareHorizontal size={13} />
            </button>
          </div>
        )}
      </div>

      {tearing && (
        <div className="pointer-events-none absolute inset-x-0 top-9 z-20 flex justify-center">
          <span className="rounded-b-control border border-t-0 border-accent-dim bg-ink-850 px-3 py-1 text-[11px] text-accent shadow-lg">
            Release to open in a new window
          </span>
        </div>
      )}

      <div className="relative min-h-0 flex-1 bg-ink-800">
        {tabs.length === 0 ? (
          <Empty
            title="Nothing attached yet"
            hint="Pick a server, workspace or agent on the left. Agent and tmux tabs reattach to work that is already running remotely."
          />
        ) : (
          // Every tab stays mounted as a sibling of every other, and a split
          // only changes where each one is positioned. Moving a pane into a
          // grid cell would reparent it, and React reparenting means unmount —
          // which for a terminal means losing the scrollback and the shell.
          tabs.map((tab) => {
            const index = panes.indexOf(tab.id)
            const shown = index !== -1
            const focused = tab.id === activeTabId
            return (
              <div
                key={tab.id}
                onPointerDownCapture={() => shown && !focused && setActiveTab(tab.id)}
                className={clsx(
                  'absolute overflow-hidden',
                  shown && panes.length > 1 && 'outline outline-1 -outline-offset-1',
                  shown && panes.length > 1 && (focused ? 'outline-accent/60' : 'outline-ink-800'),
                )}
                style={
                  shown
                    ? paneRect(index, panes.length, splitAxis)
                    : hiddenRect(panes.length, splitAxis)
                }
              >
                {tab.kind === 'files' ? (
                  <FileBrowser tab={tab} />
                ) : tab.kind === 'editor' ? (
                  <EditorPane tab={tab} active={focused} />
                ) : (
                  <TerminalPane tab={tab} active={focused} />
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
