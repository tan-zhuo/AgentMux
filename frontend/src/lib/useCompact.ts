import { useSyncExternalStore } from 'react'

// Compact means a phone, or a tablet in a narrow split view: too little width
// for three columns, so the side panels become overlay drawers instead. 768px
// is the conventional line between the two worlds.
const query = typeof window !== 'undefined' ? window.matchMedia('(max-width: 767px)') : null

function subscribe(cb: () => void) {
  query?.addEventListener('change', cb)
  return () => query?.removeEventListener('change', cb)
}

/** True when the viewport is too narrow for docked side panels. */
export function useCompact(): boolean {
  return useSyncExternalStore(subscribe, () => query?.matches ?? false)
}
