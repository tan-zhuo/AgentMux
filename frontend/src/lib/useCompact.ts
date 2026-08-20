import { useEffect, useState, useSyncExternalStore } from 'react'

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

// A finger and no hover: a phone or a tablet, at any width. Width alone would
// miss an iPad in landscape, which is missing the same keys as a phone.
const touch = mediaStore('(hover: none) and (pointer: coarse)')

/** True when the viewport is too narrow for docked side panels. */
export function useCompact(): boolean {
  return useSyncExternalStore(compact.subscribe, compact.get)
}

/** True on a device driven by touch, where the keyboard is software. */
export function useTouchDevice(): boolean {
  return useSyncExternalStore(touch.subscribe, touch.get)
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
