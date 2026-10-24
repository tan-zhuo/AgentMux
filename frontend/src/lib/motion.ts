import { createContext, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

/**
 * Motion helpers.
 *
 * React unmounts a thing the instant its state says it is gone, which leaves
 * no frame in which to animate it out. These hooks hold on a little longer:
 * long enough to play the exit, and no longer. The durations are the ones in
 * `index.css`; a hook that outlives its animation leaves a dead element in the
 * way of the pointer, and one that is shorter cuts the animation off.
 */

/** How long an exit animation is given before the element is dropped. */
export const EXIT_MS = 200

/** Whether the person has asked the system for less motion. */
export function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
  )
}

/**
 * Keeps a value around for `exitMs` after it goes null.
 *
 * Give it the thing that decides whether a component shows — a dialog
 * descriptor, a request, a boolean turned into `true | null` — and render from
 * what comes back. While `exiting` is true the component is on its way out and
 * should wear its exit animation; once the time is up, `value` becomes null and
 * the component unmounts as it would have anyway.
 */
export function usePresence<T>(
  next: T | null | undefined,
  exitMs: number = EXIT_MS,
): { value: T | null; exiting: boolean } {
  const [held, setHeld] = useState<T | null>(next ?? null)
  const [exiting, setExiting] = useState(false)
  const timer = useRef(0)

  useEffect(() => {
    if (next != null) {
      window.clearTimeout(timer.current)
      setHeld(next)
      setExiting(false)
      return
    }
    // Already gone, or never shown: nothing to play.
    if (held == null) return
    const wait = prefersReducedMotion() ? 0 : exitMs
    setExiting(true)
    timer.current = window.setTimeout(() => {
      setHeld(null)
      setExiting(false)
    }, wait)
    return () => window.clearTimeout(timer.current)
    // `held` is deliberately not a dependency: this runs when the input moves,
    // not when the hold itself changes, or the exit timer would restart.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [next, exitMs])

  return { value: held, exiting }
}

/**
 * The same, for a list: items that leave stay on, flagged, until their exit
 * has played. Items are matched by `key`; departed ones keep their place and
 * new ones are appended, which is the order a stack of notifications wants.
 */
export function usePresenceList<T>(
  items: T[],
  key: (item: T) => string,
  exitMs: number = EXIT_MS,
): Array<{ item: T; exiting: boolean }> {
  const [shown, setShown] = useState<Array<{ item: T; exiting: boolean }>>(() =>
    items.map((item) => ({ item, exiting: false })),
  )
  const shownRef = useRef(shown)
  shownRef.current = shown
  const timers = useRef(new Map<string, number>())
  // The live list on every render: the timeout that removes a departed item
  // has to know whether it came back in the meantime.
  const live = useRef(items)
  live.current = items

  useEffect(() => {
    const liveByKey = new Map(items.map((i) => [key(i), i]))
    const wait = prefersReducedMotion() ? 0 : exitMs

    // An item that came back cancels its own removal.
    for (const [k, id] of timers.current) {
      if (liveByKey.has(k)) {
        window.clearTimeout(id)
        timers.current.delete(k)
      }
    }
    // An item that left starts its exit, once.
    for (const p of shownRef.current) {
      const k = key(p.item)
      if (liveByKey.has(k) || timers.current.has(k)) continue
      timers.current.set(
        k,
        window.setTimeout(() => {
          timers.current.delete(k)
          if (live.current.some((i) => key(i) === k)) return
          setShown((prev) => prev.filter((q) => key(q.item) !== k))
        }, wait),
      )
    }

    setShown((prev) => {
      const out: Array<{ item: T; exiting: boolean }> = []
      const seen = new Set<string>()
      for (const p of prev) {
        const k = key(p.item)
        const fresh = liveByKey.get(k)
        if (fresh !== undefined) {
          out.push({ item: fresh, exiting: false })
          seen.add(k)
        } else {
          out.push(p.exiting ? p : { item: p.item, exiting: true })
        }
      }
      for (const item of items) if (!seen.has(key(item))) out.push({ item, exiting: false })
      return out
    })
    // `key` is a stable selector; listing it would only re-run this on every
    // render of a caller that writes it inline.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, exitMs])

  useEffect(() => {
    const t = timers.current
    return () => {
      for (const id of t.values()) window.clearTimeout(id)
    }
  }, [])

  return shown
}

/**
 * Tells a component inside a held-open subtree that the subtree is leaving.
 *
 * The dialog host decides when a sheet is on its way out, but it is the sheet
 * itself that has to wear the exit animation; this is how the one tells the
 * other without every dialog threading a prop through.
 */
export const ExitingContext = createContext(false)

/**
 * Where the selected item in a row sits, so one pill can slide between the
 * options instead of each option painting its own.
 *
 * The strip holds the ref; the selected element is found by data attribute, so
 * callers do not have to thread a ref through every option. The returned style
 * positions an absolutely placed pill inside the strip, which must be
 * `position: relative`. `ready` is false until the first measurement, so the
 * pill can appear in place rather than sliding in from the corner.
 *
 * A strip whose options come and go, or change order, passes something that
 * changes with them as `layoutKey`: closing the tab before the selected one
 * moves it without resizing anything, and no observer sees a move that
 * resizes nothing.
 */
export function useSlidingPill(
  stripRef: React.RefObject<HTMLElement | null>,
  selectedKey: string,
  layoutKey?: unknown,
): { style: React.CSSProperties; ready: boolean } {
  const [rect, setRect] = useState<{ left: number; top: number; width: number; height: number } | null>(
    null,
  )

  const measure = useCallback(() => {
    const strip = stripRef.current
    if (!strip) return
    const el = strip.querySelector<HTMLElement>(`[data-pill-key="${CSS.escape(selectedKey)}"]`)
    if (!el) {
      setRect(null)
      return
    }
    // offsetLeft/Top are relative to the nearest positioned ancestor, which is
    // the strip — and unlike getBoundingClientRect they are not affected by
    // the strip having been scrolled, which a wide tab strip will have been.
    const next = {
      left: el.offsetLeft,
      top: el.offsetTop,
      width: el.offsetWidth,
      height: el.offsetHeight,
    }
    setRect((prev) =>
      prev &&
      prev.left === next.left &&
      prev.top === next.top &&
      prev.width === next.width &&
      prev.height === next.height
        ? prev
        : next,
    )
  }, [stripRef, selectedKey])

  useLayoutEffect(measure, [measure, layoutKey])

  // Labels change with the language, badges come and go, and the panel is
  // resizable: anything that moves the options moves the pill. The children
  // are collected again with the layout key, so an option added later is
  // watched too.
  useEffect(() => {
    const strip = stripRef.current
    if (!strip) return
    const ro = new ResizeObserver(measure)
    ro.observe(strip)
    for (const child of Array.from(strip.children)) ro.observe(child)
    return () => ro.disconnect()
  }, [stripRef, measure, layoutKey])

  return {
    ready: rect !== null,
    style: rect
      ? {
          transform: `translate(${rect.left}px, ${rect.top}px)`,
          width: rect.width,
          height: rect.height,
        }
      : { opacity: 0 },
  }
}

/**
 * Lets the items of a strip slide into their new places rather than jump.
 *
 * Every render, each `data-flip-key` element inside the strip has its place
 * noted; one that is somewhere else than it was last time is started from
 * where it was and let settle where it now is — the FLIP technique. The slide
 * is a Web Animation rather than an inline transition so it touches neither
 * the element's own transitions nor its style, and one that is interrupted by
 * another move carries on from wherever it had got to, which is what a tab
 * dragged back and forth needs. It shares the pill's curve and duration, so a
 * tab and the pill under it move as one thing.
 *
 * Only the horizontal axis is handled: this is for strips. Under reduced
 * motion the places are still noted and nothing slides.
 */
export function useSlideIntoPlace(stripRef: React.RefObject<HTMLElement | null>): void {
  const lefts = useRef(new Map<string, number>())
  const slides = useRef(new Map<string, Animation>())

  useLayoutEffect(() => {
    const strip = stripRef.current
    if (!strip) return
    const was = lefts.current
    const now = new Map<string, number>()
    const still = prefersReducedMotion()
    for (const el of Array.from(strip.querySelectorAll<HTMLElement>('[data-flip-key]'))) {
      const key = el.dataset.flipKey ?? ''
      const left = el.offsetLeft
      now.set(key, left)
      const before = was.get(key)
      // A render that moved nothing — a pointer event, a status change —
      // leaves a slide already under way to finish, or it would start over
      // from wherever it had got to on every one of them, and crawl.
      if (before === undefined || before === left || still) continue
      // Where it is drawn this instant — mid-slide, that is not where it was
      // laid out — read before the old slide is cancelled.
      const from = before + translateXOf(el) - left
      slides.current.get(key)?.cancel()
      slides.current.delete(key)
      if (Math.abs(from) < 0.5) continue
      const slide = el.animate(
        [{ transform: `translateX(${from}px)` }, { transform: 'translateX(0)' }],
        { duration: motionMs('--duration-base', 260), easing: motionEasing('--ease-spring') },
      )
      slides.current.set(key, slide)
      slide.onfinish = () => {
        if (slides.current.get(key) === slide) slides.current.delete(key)
      }
    }
    // A slide for an item that has gone would otherwise be kept for ever.
    for (const key of Array.from(slides.current.keys())) {
      if (!now.has(key)) slides.current.delete(key)
    }
    lefts.current = now
  })

  useEffect(() => {
    const live = slides.current
    return () => {
      for (const slide of live.values()) slide.cancel()
    }
  }, [])
}

/** The horizontal translation an element is drawn with right now, animations
 *  included — which is what `offsetLeft` leaves out. */
function translateXOf(el: HTMLElement): number {
  const transform = getComputedStyle(el).transform
  if (!transform || transform === 'none') return 0
  return new DOMMatrixReadOnly(transform).m41
}

/* The Web Animations API takes its timing as plain numbers and its curve as
   plain CSS, not as a custom property. These read the tokens from `index.css`
   once, so a slide cannot drift from the transitions beside it. */
const tokens = new Map<string, string>()
function motionToken(name: string): string {
  let value = tokens.get(name)
  if (value === undefined) {
    value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    tokens.set(name, value)
  }
  return value
}
function motionMs(name: string, fallback: number): number {
  // The build minifies `260ms` to `.26s`, so the unit has to be honoured.
  const raw = motionToken(name)
  const n = parseFloat(raw)
  if (!Number.isFinite(n)) return fallback
  return /ms$/i.test(raw) ? n : n * 1000
}
function motionEasing(name: string): string {
  return motionToken(name) || 'ease-out'
}
