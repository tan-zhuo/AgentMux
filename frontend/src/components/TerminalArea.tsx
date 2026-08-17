import clsx from 'clsx'
import {
  Bot,
  ChevronsRight,
  Columns2,
  Expand,
  Minimize2,
  Rows2,
  Shrink,
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

/** The narrowest and shortest a pane is worth being, in CSS pixels: roughly
 *  forty columns and a dozen rows at the terminal's own type size. Below that a
 *  pane stops being a terminal and becomes a sliver, so the grid wraps instead
 *  of dividing further. */
const CELL_MIN_W = 330
const CELL_MIN_H = 176

/** No pane may be dragged below this share of the axis it sits on. */
const MIN_SHARE = 0.1

type Grid = { cols: number; rows: number }

function equalShares(n: number): number[] {
  return Array.from({ length: n }, () => 1 / n)
}

function sum(ns: number[]): number {
  return ns.reduce((a, b) => a + b, 0)
}

/**
 * How many columns and rows a number of panes is arranged in, given the space.
 *
 * Two and three divide along the axis alone, because a terminal would rather be
 * full height and narrow than half of both. From four up the arrangement is the
 * squarest one that fits, with the axis choosing which way a non-square grid
 * leans: six panes are 3×2 side by side and 2×3 stacked.
 *
 * Then the measured area gets a veto. A column too narrow to read is dropped and
 * its panes wrap onto another row, so narrowing the window — or opening both side
 * panels — turns a 3×3 wall into a taller, narrower grid rather than nine
 * unreadable slivers. Width is settled last because a short pane still shows its
 * last lines, while a narrow one wraps every one of them.
 */
function paneGrid(count: number, axis: SplitAxis, w: number, h: number): Grid {
  let cols: number
  let rows: number
  if (count <= 3) {
    cols = axis === 'cols' ? count : 1
    rows = axis === 'cols' ? 1 : count
  } else {
    const major = Math.ceil(Math.sqrt(count))
    const minor = Math.ceil(count / major)
    cols = axis === 'cols' ? major : minor
    rows = axis === 'cols' ? minor : major
  }
  // Zero means not measured yet — the first frame keeps the shape the count
  // asked for rather than collapsing to one column.
  if (h > 0) {
    while (rows > 1 && h / rows < CELL_MIN_H) {
      rows -= 1
      cols = Math.ceil(count / rows)
    }
  }
  if (w > 0) {
    while (cols > 1 && w / cols < CELL_MIN_W) {
      cols -= 1
      rows = Math.ceil(count / cols)
    }
  }
  return { cols, rows: Math.max(rows, Math.ceil(count / cols)) }
}

/** Where a pane sits, as percentages of the terminal area.
 *
 *  Computed rather than measured: a CSS grid would need its cells measured
 *  before the absolutely-positioned tabs could be placed into them, and a frame
 *  of wrong geometry is a frame of wrongly-sized terminal. */
function paneRect(
  index: number,
  count: number,
  grid: Grid,
  colShares: number[],
  rowShares: number[],
): React.CSSProperties {
  if (count <= 1) return { inset: 0 }
  const row = Math.floor(index / grid.cols)
  const col = index % grid.cols
  // A last row with fewer panes than the others divides itself between them in
  // the same proportions, so eight panes are three, three and two rather than a
  // hole where the ninth would have been.
  const inRow = Math.min(grid.cols, count - row * grid.cols)
  const span = colShares.slice(0, inRow)
  const total = sum(span)
  return {
    left: `${(sum(span.slice(0, col)) / total) * 100}%`,
    top: `${sum(rowShares.slice(0, row)) * 100}%`,
    width: `${(span[col] / total) * 100}%`,
    height: `${rowShares[row] * 100}%`,
  }
}

/** Tabs that are open but not on screen stay mounted, laid out, and invisible.
 *
 *  Hiding them with `display: none` would collapse the terminal to zero columns
 *  and make xterm reflow every line the moment it came back. They are laid out
 *  at the size of the first pane rather than of the whole area, so a tab swapped
 *  into the split needs no resize — or at most the one a short last row implies
 *  — instead of being reflowed to a width nobody is looking at. */
function hiddenRect(
  count: number,
  grid: Grid,
  colShares: number[],
  rowShares: number[],
): React.CSSProperties {
  return { ...paneRect(0, count, grid, colShares, rowShares), visibility: 'hidden' }
}

/**
 * The draggable seams between panes.
 *
 * One set of column shares is used by every row and one set of row shares by
 * every column, so the grid stays a grid: dragging a seam moves it in all the
 * rows at once rather than knocking the panes out of alignment. Only full rows
 * carry vertical seams — a short last row divides itself proportionally, and a
 * handle there would sit somewhere no boundary exists.
 *
 * The shares are not persisted. A grid that changes shape starts even again,
 * which is the only honest thing to restore when the cells themselves are
 * different cells.
 */
function PaneSeams({
  count,
  grid,
  colShares,
  rowShares,
  onCols,
  onRows,
}: {
  count: number
  grid: Grid
  colShares: number[]
  rowShares: number[]
  onCols: (shares: number[]) => void
  onRows: (shares: number[]) => void
}) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const [dragging, setDragging] = useState<string | null>(null)

  function startDrag(e: React.PointerEvent, axis: 'x' | 'y', boundary: number, key: string) {
    const rect = rootRef.current?.getBoundingClientRect()
    if (!rect) return
    e.preventDefault()
    const shares = axis === 'x' ? colShares : rowShares
    const from = [...shares]
    const span = axis === 'x' ? rect.width : rect.height
    const origin = axis === 'x' ? e.clientX : e.clientY
    const before = from[boundary - 1]
    const after = from[boundary]
    setDragging(key)

    const move = (ev: PointerEvent) => {
      const moved = ((axis === 'x' ? ev.clientX : ev.clientY) - origin) / span
      // Neither neighbour may be squeezed past the floor, so a seam stops rather
      // than collapsing the pane on the other side of it.
      const delta = Math.max(MIN_SHARE - before, Math.min(moved, after - MIN_SHARE))
      const next = [...from]
      next[boundary - 1] = before + delta
      next[boundary] = after - delta
      if (axis === 'x') onCols(next)
      else onRows(next)
    }
    const done = () => {
      setDragging(null)
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', done)
      window.removeEventListener('pointercancel', done)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', done)
    window.addEventListener('pointercancel', done)
  }

  const seams: React.ReactNode[] = []

  for (let row = 0; row < grid.rows; row++) {
    const inRow = Math.min(grid.cols, count - row * grid.cols)
    if (inRow < grid.cols) continue
    for (let b = 1; b < grid.cols; b++) {
      const key = `x${b}`
      seams.push(
        <div
          key={`${key}-${row}`}
          onPointerDown={(e) => startDrag(e, 'x', b, key)}
          onDoubleClick={() => onCols(equalShares(grid.cols))}
          title="Drag to resize · double-click to even them out"
          style={{
            left: `${sum(colShares.slice(0, b)) * 100}%`,
            top: `${sum(rowShares.slice(0, row)) * 100}%`,
            height: `${rowShares[row] * 100}%`,
          }}
          className={clsx(
            'pointer-events-auto absolute -ml-[3px] w-[7px] cursor-col-resize touch-none',
            dragging === key ? 'bg-accent/50' : 'hover:bg-accent/30',
          )}
        />,
      )
    }
  }

  for (let b = 1; b < grid.rows; b++) {
    const key = `y${b}`
    seams.push(
      <div
        key={key}
        onPointerDown={(e) => startDrag(e, 'y', b, key)}
        onDoubleClick={() => onRows(equalShares(grid.rows))}
        title="Drag to resize · double-click to even them out"
        style={{ top: `${sum(rowShares.slice(0, b)) * 100}%` }}
        className={clsx(
          'pointer-events-auto absolute inset-x-0 -mt-[3px] h-[7px] cursor-row-resize touch-none',
          dragging === key ? 'bg-accent/50' : 'hover:bg-accent/30',
        )}
      />,
    )
  }

  return (
    // Transparent to the mouse except on the handles themselves, so a click in a
    // pane still reaches the terminal.
    <div ref={rootRef} className="pointer-events-none absolute inset-0 z-10">
      {seams}
    </div>
  )
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
  const toggleZoom = useAppStore((s) => s.toggleZoom)
  // A zoom only exists inside a split; one pane already fills the area.
  const zoomed = useAppStore((s) => s.paneZoom) && panes.length > 1

  // The grid shape depends on the space available, so the space is measured.
  const areaRef = useRef<HTMLDivElement | null>(null)
  const [area, setArea] = useState({ w: 0, h: 0 })
  useEffect(() => {
    const el = areaRef.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth
      const h = el.clientHeight
      setArea((prev) => (prev.w === w && prev.h === h ? prev : { w, h }))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // Dragged seam positions. Held as shares of the axis and only for the shape
  // they were dragged in: a grid of a different size falls back to even, which
  // is what the mismatched length below means.
  const [draggedCols, setDraggedCols] = useState<number[]>([])
  const [draggedRows, setDraggedRows] = useState<number[]>([])
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

  const grid = paneGrid(panes.length, splitAxis, area.w, area.h)
  const colShares = draggedCols.length === grid.cols ? draggedCols : equalShares(grid.cols)
  const rowShares = draggedRows.length === grid.rows ? draggedRows : equalShares(grid.rows)
  // Flipping is only worth offering when it would land on a different shape: at
  // 2×2, or once a narrow window has forced a single column, both axes agree.
  const flipped = paneGrid(panes.length, splitAxis === 'cols' ? 'rows' : 'cols', area.w, area.h)
  const canFlip = grid.cols !== flipped.cols || grid.rows !== flipped.rows

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
            // Zoomed, the strip says what it always says: what you can see.
            const inPane = panes.includes(tab.id)
            const onScreen = inPane && (!zoomed || active)
            const isDragging = dragId === tab.id
            return (
              <div
                key={tab.id}
                data-tab-index={index}
                // Double-clicking the tab zooms its pane. The pane itself cannot
                // take the gesture: inside a terminal a double-click selects a
                // word, and inside the file browser it opens what was hit.
                onDoubleClick={() => {
                  // It means "show me this one big" — and on the pane that is
                  // already filling the area, the opposite.
                  if (active && zoomed) {
                    toggleZoom()
                    return
                  }
                  if (!active) setActiveTab(tab.id)
                  if (!zoomed) toggleZoom()
                }}
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
                      hint: inPane
                        ? 'already in the split'
                        : panes.length >= MAX_PANES
                          ? `${MAX_PANES} panes is the limit`
                          : undefined,
                      disabled: inPane || panes.length >= MAX_PANES,
                      onSelect: () => splitWith(tab.id),
                    },
                    {
                      label: zoomed && active ? 'Back to the split' : 'Fill the area with this pane',
                      icon: zoomed && active ? Shrink : Expand,
                      hint: '⇧⌘↵ · double-click the tab',
                      disabled: panes.length < 2 || !inPane,
                      onSelect: () => {
                        if (active && zoomed) {
                          toggleZoom()
                          return
                        }
                        if (!active) setActiveTab(tab.id)
                        if (!zoomed) toggleZoom()
                      },
                    },
                    {
                      label: 'Close this pane',
                      icon: Minimize2,
                      hint: 'the tab stays open',
                      disabled: !inPane || panes.length < 2,
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
            {panes.length > 1 && canFlip && (
              <button
                onClick={() => setSplitAxis(splitAxis === 'cols' ? 'rows' : 'cols')}
                title={
                  splitAxis === 'cols'
                    ? `Stack the panes instead — ${flipped.cols}×${flipped.rows}`
                    : `Put the panes side by side — ${flipped.cols}×${flipped.rows}`
                }
                className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
              >
                {splitAxis === 'cols' ? <Rows2 size={13} /> : <Columns2 size={13} />}
              </button>
            )}
            {panes.length > 1 && (
              <button
                onClick={toggleZoom}
                title={
                  zoomed
                    ? 'Back to the split — ⇧⌘↵'
                    : 'Fill the area with the focused pane — ⇧⌘↵'
                }
                className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
              >
                {zoomed ? <Shrink size={13} /> : <Expand size={13} />}
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
                  ? `${MAX_PANES} panes is the limit — close one first`
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

      <div ref={areaRef} className="relative min-h-0 flex-1 bg-ink-800">
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
            // Zoomed, the focused pane takes the area and its neighbours are
            // parked at pane size rather than hidden at full size, so putting
            // the split back costs no reflow.
            const filling = zoomed && focused
            const framed = shown && !zoomed && panes.length > 1
            return (
              <div
                key={tab.id}
                onPointerDownCapture={() => shown && !focused && setActiveTab(tab.id)}
                className={clsx(
                  'group/pane absolute overflow-hidden',
                  framed && 'outline outline-1 -outline-offset-1',
                  framed && (focused ? 'outline-accent/60' : 'outline-ink-800'),
                )}
                style={
                  filling
                    ? { inset: 0 }
                    : shown && !zoomed
                      ? paneRect(index, panes.length, grid, colShares, rowShares)
                      : hiddenRect(panes.length, grid, colShares, rowShares)
                }
              >
                {tab.kind === 'files' ? (
                  <FileBrowser tab={tab} />
                ) : tab.kind === 'editor' ? (
                  <EditorPane tab={tab} active={focused} />
                ) : (
                  <TerminalPane tab={tab} active={focused} />
                )}

                {/* Reading one pane of a nine-way grid needs it bigger, and a
                    terminal cannot spare a double-click — inside one that
                    selects a word. So the zoom is a control of its own, on the
                    pane it acts on, appearing on hover. */}
                {panes.length > 1 && (shown || filling) && (
                  <button
                    onClick={toggleZoom}
                    title={
                      zoomed
                        ? 'Back to the split — ⇧⌘↵, or double-click the tab'
                        : 'Fill the area with this pane — ⇧⌘↵, or double-click the tab'
                    }
                    className="absolute top-1 right-1 rounded-control bg-ink-900/80 p-1 text-ink-400 opacity-0 backdrop-blur-sm transition-opacity group-hover/pane:opacity-100 hover:text-ink-100 focus-visible:opacity-100"
                  >
                    {zoomed ? <Shrink size={12} /> : <Expand size={12} />}
                  </button>
                )}
              </div>
            )
          })
        )}

        {!zoomed && panes.length > 1 && (
          <PaneSeams
            count={panes.length}
            grid={grid}
            colShares={colShares}
            rowShares={rowShares}
            onCols={setDraggedCols}
            onRows={setDraggedRows}
          />
        )}
      </div>
    </div>
  )
}
