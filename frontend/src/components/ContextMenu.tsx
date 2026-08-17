import clsx from 'clsx'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useContextMenu, type MenuItem } from '../store/useContextMenu'

/**
 * The application's right-click menu.
 *
 * It is positioned at the pointer and then nudged back inside the window, so a
 * menu opened near the bottom right edge does not open off-screen where its
 * last items are unreachable.
 */
/**
 * Drops separators that would render as stray lines.
 *
 * Call sites build menus with conditional entries, so a skipped item leaves an
 * empty slot behind. Normalising here means they can stay declarative instead
 * of each one hand-assembling its array.
 */
function tidy(items: MenuItem[]): MenuItem[] {
  const out: MenuItem[] = []
  for (const item of items) {
    if (!item) continue
    if (!item.label) {
      if (out.length === 0 || !out[out.length - 1].label) continue
      out.push(item)
      continue
    }
    out.push(item)
  }
  while (out.length && !out[out.length - 1].label) out.pop()
  return out
}

export function ContextMenu() {
  // Selected field by field, and the tidied list memoised on the raw one.
  // Rebuilding the array on every render would make it a new dependency each
  // time, so the positioning effect below would re-run, set state, and render
  // again — an infinite loop that React ends by tearing the tree down, which
  // shows up as a blank window.
  const open = useContextMenu((s) => s.open)
  const x = useContextMenu((s) => s.x)
  const y = useContextMenu((s) => s.y)
  const raw = useContextMenu((s) => s.items)
  const hide = useContextMenu((s) => s.hide)
  const items = useMemo(() => tidy(raw), [raw])

  const ref = useRef<HTMLDivElement | null>(null)
  const [pos, setPos] = useState({ left: x, top: y })
  const [cursor, setCursor] = useState(-1)

  useLayoutEffect(() => {
    if (!open) return
    const el = ref.current
    const w = el?.offsetWidth ?? 200
    const h = el?.offsetHeight ?? 200
    const pad = 8
    const next = {
      left: Math.max(pad, Math.min(x, window.innerWidth - w - pad)),
      top: Math.max(pad, Math.min(y, window.innerHeight - h - pad)),
    }
    // Belt and braces: never set identical coordinates, so even an unexpected
    // extra run cannot start a loop.
    setPos((prev) => (prev.left === next.left && prev.top === next.top ? prev : next))
    setCursor(-1)
  }, [open, x, y, items])

  useEffect(() => {
    if (!open) return
    const close = () => hide()
    const onKey = (e: KeyboardEvent) => {
      const selectable = items
        .map((it, i) => ({ it, i }))
        .filter(({ it }) => it.label && !it.disabled)
      if (e.key === 'Escape') {
        e.preventDefault()
        hide()
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        if (!selectable.length) return
        const at = selectable.findIndex((s) => s.i === cursor)
        const next =
          e.key === 'ArrowDown'
            ? selectable[(at + 1 + selectable.length) % selectable.length]
            : selectable[(at - 1 + selectable.length) % selectable.length]
        setCursor(next.i)
      } else if (e.key === 'Enter' && cursor >= 0) {
        e.preventDefault()
        const item = items[cursor]
        if (item?.onSelect && !item.disabled) {
          hide()
          void item.onSelect()
        }
      }
    }
    // Any click elsewhere, any scroll, or a resize dismisses it — the same as
    // every native menu.
    window.addEventListener('pointerdown', close)
    window.addEventListener('resize', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('pointerdown', close)
      window.removeEventListener('resize', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  }, [open, items, cursor, hide])

  if (!open) return null

  return (
    <div
      ref={ref}
      role="menu"
      onPointerDown={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.preventDefault()}
      style={{ left: pos.left, top: pos.top }}
      className="fixed z-90 min-w-52 overflow-hidden rounded-lg border hairline bg-ink-850 py-1 shadow-2xl"
    >
      {items.map((item, i) =>
        !item.label ? (
          <div key={`sep-${i}`} className="my-1 h-px bg-ink-750" />
        ) : (
          <button
            key={`${item.label}-${i}`}
            role="menuitem"
            disabled={item.disabled}
            onMouseEnter={() => setCursor(i)}
            onClick={() => {
              if (item.disabled) return
              hide()
              void item.onSelect?.()
            }}
            className={clsx(
              'flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-xs',
              item.disabled && 'cursor-not-allowed text-ink-600',
              !item.disabled && cursor === i && (item.danger ? 'bg-danger/15' : 'bg-accent/15'),
              !item.disabled && (item.danger ? 'text-danger' : 'text-ink-200'),
            )}
          >
            {item.icon ? (
              <item.icon size={13} className="shrink-0 opacity-70" />
            ) : (
              <span className="w-[13px] shrink-0" />
            )}
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
            {item.hint && <span className="shrink-0 text-[10.5px] text-ink-600">{item.hint}</span>}
          </button>
        ),
      )}
    </div>
  )
}
