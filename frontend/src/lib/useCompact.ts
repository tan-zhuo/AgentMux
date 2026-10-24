import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

/** What the window and the pointer are, as React state. */
function mediaStore(query: string) {
  const mq = typeof window !== 'undefined' ? window.matchMedia(query) : null
  return {
    subscribe(cb: () => void) {
      mq?.addEventListener('change', cb)
      return () => mq?.removeEventListener('change', cb)
    },
    get: () => mq?.matches ?? false,
  }
}

// Compact means a phone, or a tablet in a narrow split view: too little width
// for three columns, so the side panels become overlay drawers instead. 768px
// is the conventional line between the two worlds.
const compact = mediaStore('(max-width: 767px)')

// Medium is a tablet held upright: room for two columns but not three. The
// tree keeps its dock and the right panel floats over the terminal instead,
// the way both do on a phone. From 1024px up everything docks.
const medium = mediaStore('(min-width: 768px) and (max-width: 1023px)')

// A finger and no hover: a phone or a tablet, at any width. Width alone would
// miss an iPad in landscape, which is missing the same keys as a phone.
const touch = mediaStore('(hover: none) and (pointer: coarse)')

// The width itself, for layout that has to fit numbers into it rather than
// ask which side of a line it is on.
const viewport = {
  subscribe(cb: () => void) {
    window.addEventListener('resize', cb)
    return () => window.removeEventListener('resize', cb)
  },
  get: () => window.innerWidth,
}

/** True when the viewport is too narrow for docked side panels. */
export function useCompact(): boolean {
  return useSyncExternalStore(compact.subscribe, compact.get)
}

/** True when the viewport has room to dock one side panel but not both. */
export function useMedium(): boolean {
  return useSyncExternalStore(medium.subscribe, medium.get)
}

/** True on a device driven by touch, where the keyboard is software. */
export function useTouchDevice(): boolean {
  return useSyncExternalStore(touch.subscribe, touch.get)
}

/**
 * `derive(viewport width)`, kept current across resizes.
 *
 * React compares the derived value, not the width, so the caller re-renders
 * when its answer changes and not on every pixel of a window drag — at the
 * root, that would reconcile the whole app sixty times a second. An object
 * answer needs `same` to say when two are alike, since a fresh object never
 * equals the last one.
 */
export function useViewport<T>(
  derive: (width: number) => T,
  same: (a: T, b: T) => boolean = Object.is,
): T {
  // The last answer handed out, so an unchanged one is handed out again by
  // reference — the pattern zustand's useShallow uses, for the same reason.
  const last = useRef<{ value: T } | null>(null)
  return useSyncExternalStore(viewport.subscribe, () => {
    const next = derive(viewport.get())
    if (last.current && same(last.current.value, next)) return last.current.value
    last.current = { value: next }
    return next
  })
}

/**
 * True while the window is being resized, and for a beat afterwards.
 *
 * A width transition is for a panel opening or closing; under a window drag
 * the same transition makes a panel re-fitted to the window trail its edge,
 * so a layout that follows the viewport switches it off for the duration.
 */
export function useViewportResizing(quietMs = 200): boolean {
  const [resizing, setResizing] = useState(false)

  useEffect(() => {
    let timer = 0
    const onResize = () => {
      setResizing(true)
      window.clearTimeout(timer)
      timer = window.setTimeout(() => setResizing(false), quietMs)
    }
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      window.clearTimeout(timer)
    }
  }, [quietMs])

  return resizing
}

/**
 * How much of the page the on-screen keyboard is covering, in CSS pixels.
 *
 * Zero wherever the platform already shrank the page for it — an Android
 * WebView in adjustResize, or a browser honouring `interactive-widget`. iOS
 * shrinks only the visual viewport and leaves the layout alone, which is how a
 * bottom bar ends up underneath the keyboard; there, this is the height to give
 * back. The floor keeps a retracting URL bar from reading as a keyboard.
 */
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(0)

  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const read = () => {
      const covered = window.innerHeight - vv.height - vv.offsetTop
      setInset(covered > 120 ? Math.round(covered) : 0)
    }
    read()
    vv.addEventListener('resize', read)
    vv.addEventListener('scroll', read)
    return () => {
      vv.removeEventListener('resize', read)
      vv.removeEventListener('scroll', read)
    }
  }, [])

  return inset
}
