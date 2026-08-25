import { create } from 'zustand'
import { tree } from '../lib/api'
import { NO_MODS, type Mods } from '../lib/termKeys'

const HIDDEN_KEY = 'keyBarHidden'
const EXPANDED_KEY = 'keyBarExpanded'

/**
 * A modifier is off, armed for exactly one key, or locked until tapped again.
 *
 * One key is what a shell almost always wants — ⌃C, ⌃D, ⌃R — and having the
 * modifier fall off by itself means never wondering why the next letter went
 * missing. The lock is there for the times it is not: walking back through a
 * line with ⌃B, or an editor that wants a run of them.
 */
export type ModState = 'off' | 'once' | 'lock'

/**
 * Where the focused terminal takes its keys from.
 *
 * Deliberately not React state: the bar writes straight to the backend PTY
 * rather than through xterm, so this is a channel, not something to render.
 * Whichever pane has focus owns it; there is only ever one.
 */
export interface KeySink {
  send: (data: string) => void
  focus: () => void
  blur: () => void
  /** Select mode's two actions, which belong to the pane's xterm instance. */
  selectAll: () => void
  copySelection: () => void
}

let sink: KeySink | null = null

export function setKeySink(next: KeySink | null): void {
  sink = next
}

/** Release the channel, but only if it is still this terminal's. Panes hand over
 *  by registering before the outgoing one has cleaned up, and the last word
 *  should belong to whoever has focus now. */
export function clearKeySink(own: KeySink): void {
  if (sink !== own) return
  sink = null
  useTermKeys.getState().clearMods()
  useTermKeys.getState().setSelecting(false)
}

/** True while a live terminal is listening — the bar has nothing to do otherwise. */
export function hasKeySink(): boolean {
  return sink !== null
}

/** Send bytes to the focused terminal, spending any one-shot modifiers. */
export function sendKeys(data: string): void {
  if (!data) return
  sink?.send(data)
  useTermKeys.getState().spendOnce()
}

export function focusTerminal(): void {
  sink?.focus()
}

export function blurTerminal(): void {
  sink?.blur()
}

export function selectAllInTerminal(): void {
  sink?.selectAll()
}

export function copyTerminalSelection(): void {
  sink?.copySelection()
}

/** What is armed right now, for the bar and for characters typed on the keyboard. */
export function currentMods(): Mods {
  const s = useTermKeys.getState()
  return { ctrl: s.ctrl !== 'off', alt: s.alt !== 'off' }
}

export function anyModArmed(): boolean {
  const m = currentMods()
  return m.ctrl || m.alt
}

interface TermKeysState {
  ctrl: ModState
  alt: ModState
  /** The bar is collapsed to its handle. Persisted — it is a standing choice. */
  hidden: boolean
  /** The second tier of keys is showing. Also persisted. */
  expanded: boolean
  /**
   * The focused terminal is in select mode: a finger drags out a selection
   * instead of scrolling. Never persisted — it is a moment, not a preference,
   * and it ends with the copy it was opened for.
   */
  selecting: boolean

  init: () => Promise<void>
  /** Tap: off → armed for one key → off. */
  tap: (mod: keyof Mods) => void
  /** Press and hold: lock on, or unlock. */
  lock: (mod: keyof Mods) => void
  /** Drop the modifiers that were armed for a single key. */
  spendOnce: () => void
  clearMods: () => void
  setHidden: (hidden: boolean) => void
  toggleExpanded: () => void
  setSelecting: (selecting: boolean) => void
}

export const useTermKeys = create<TermKeysState>((set, get) => ({
  ctrl: 'off',
  alt: 'off',
  hidden: false,
  expanded: false,
  selecting: false,

  async init() {
    try {
      const [hidden, expanded] = await Promise.all([
        tree.getSetting(HIDDEN_KEY, '0'),
        tree.getSetting(EXPANDED_KEY, '0'),
      ])
      set({ hidden: hidden === '1', expanded: expanded === '1' })
    } catch {
      /* the bar's default state is a fine answer */
    }
  },

  tap(mod) {
    set({ [mod]: get()[mod] === 'off' ? 'once' : 'off' } as Pick<TermKeysState, 'ctrl' | 'alt'>)
  },

  lock(mod) {
    set({ [mod]: get()[mod] === 'lock' ? 'off' : 'lock' } as Pick<TermKeysState, 'ctrl' | 'alt'>)
  },

  spendOnce() {
    const s = get()
    const next: Partial<TermKeysState> = {}
    if (s.ctrl === 'once') next.ctrl = 'off'
    if (s.alt === 'once') next.alt = 'off'
    if (Object.keys(next).length) set(next)
  },

  clearMods() {
    set({ ctrl: 'off', alt: 'off' })
  },

  setHidden(hidden) {
    set({ hidden })
    void tree.setSetting(HIDDEN_KEY, hidden ? '1' : '0').catch(() => {})
  },

  toggleExpanded() {
    const expanded = !get().expanded
    set({ expanded })
    void tree.setSetting(EXPANDED_KEY, expanded ? '1' : '0').catch(() => {})
  },

  setSelecting(selecting) {
    if (get().selecting === selecting) return
    set({ selecting })
    // The keyboard has nothing to type into a selection, and it covers half of
    // what the user is trying to select.
    if (selecting) blurTerminal()
  },
}))

export { NO_MODS }
