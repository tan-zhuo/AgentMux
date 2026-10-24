import clsx from 'clsx'
import { useRef, useState } from 'react'

/**
 * A draggable divider between two panes.
 *
 * The hit area is wider than the visible line, because a 1px target is a target
 * you miss. Pointer capture keeps the drag attached to this element even as the
 * pointer travels over the terminal, which otherwise swallows the events.
 */
export function Splitter({
  value,
  min,
  max,
  invert,
  resetTo,
  label,
  onChange,
  onCommit,
  onDragging,
}: {
  value: number
  min: number
  max: number
  /** True for the right-hand pane, which grows as the pointer moves left. */
  invert?: boolean
  /** Width restored on double-click. */
  resetTo: number
  label: string
  onChange: (v: number) => void
  onCommit: (v: number) => void
  /** Told when a drag starts and ends, so a parent can pause any width
   *  transition it has on the pane for the duration. */
  onDragging?: (dragging: boolean) => void
}) {
  const [dragging, setDragging] = useState(false)
  const start = useRef({ x: 0, value: 0, latest: 0 })

  // `max` comes from the parent already fitted to the window and to whatever
  // else is docked in it, so a pane can never squeeze the terminal out of
  // existence on a small display.
  const clamp = (v: number) => Math.round(Math.max(min, Math.min(max, v)))

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      title={`${label} — drag to resize, double-click to reset`}
      onDoubleClick={() => onCommit(resetTo)}
      onKeyDown={(e) => {
        // Keyboard resizing, because a pointer-only control is unreachable for
        // anyone driving the app from the keyboard.
        const step = e.shiftKey ? 40 : 10
        if (e.key === 'ArrowLeft') {
          e.preventDefault()
          onCommit(clamp(value + (invert ? step : -step)))
        } else if (e.key === 'ArrowRight') {
          e.preventDefault()
          onCommit(clamp(value + (invert ? -step : step)))
        } else if (e.key === 'Home') {
          e.preventDefault()
          onCommit(resetTo)
        }
      }}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        start.current = { x: e.clientX, value, latest: value }
        setDragging(true)
        onDragging?.(true)
      }}
      onPointerMove={(e) => {
        if (!dragging) return
        const delta = e.clientX - start.current.x
        const next = clamp(start.current.value + (invert ? -delta : delta))
        start.current.latest = next
        onChange(next)
      }}
      onPointerUp={(e) => {
        if (!dragging) return
        e.currentTarget.releasePointerCapture(e.pointerId)
        setDragging(false)
        onDragging?.(false)
        onCommit(start.current.latest)
      }}
      className={clsx(
        'group relative z-10 w-1 shrink-0 cursor-col-resize touch-none select-none',
        'focus:outline-none',
      )}
    >
      {/* Widen the grab area without widening the layout. */}
      <span className="absolute inset-y-0 -left-1 -right-1" />
      {/* The seam thickens under the pointer rather than only recolouring,
          which is how the eye learns it can be grabbed before it tries. */}
      <span
        className={clsx(
          'absolute inset-y-0 left-0 w-px transition-[background-color,width,box-shadow] duration-[var(--duration-quick)]',
          dragging
            ? 'w-[2px] bg-accent shadow-[0_0_8px_color-mix(in_srgb,var(--color-accent)_55%,transparent)]'
            : 'bg-ink-800 group-hover:w-[2px] group-hover:bg-accent-dim group-focus:bg-accent',
        )}
      />
    </div>
  )
}
