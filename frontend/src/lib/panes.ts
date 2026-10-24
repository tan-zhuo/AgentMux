import { shallow } from 'zustand/shallow'
import { RIGHT_MAX, RIGHT_MIN, SIDEBAR_MAX, SIDEBAR_MIN } from '../store/useAppStore'
import { useViewport } from './useCompact'

/** The least the terminal is left with beside docked panels. A phone-width
 *  terminal still reads at this; much under it and a prompt wraps every word. */
export const TERMINAL_MIN = 360

/** The Splitter's `w-1`, as a number the arithmetic can use. */
const SPLITTER = 4

export type PaneFit = {
  /** Width each docked panel gets, or null for one with no dock: closed,
   *  floating, or — the right one — squeezed out for want of room. */
  side: number | null
  right: number | null
  /** How wide each may be dragged with the other where it is. */
  sideMax: number
  rightMax: number
}

/**
 * Fits the side panels into a viewport beside a terminal of usable width.
 *
 * The stored widths were chosen on some other window — a monitor, usually —
 * and are honoured where they fit. Where they do not, the right panel yields
 * first, down to its minimum, then the sidebar down to its; and if the two
 * minima and the terminal still do not add up, the right panel gives up its
 * dock altogether and the caller floats it instead. The stored values are
 * left as they are throughout: widen the window and the panels come back.
 *
 * `side` and `right` are what each docked panel asks for, or null for one
 * that is not docked at all.
 */
export function fitPanes(viewport: number, side: number | null, right: number | null): PaneFit {
  // Room beside the terminal for one panel, and for two.
  const alone = viewport - TERMINAL_MIN - SPLITTER
  const shared = alone - SPLITTER

  if (side != null && right != null && shared >= SIDEBAR_MIN + RIGHT_MIN) {
    const s = Math.min(side, shared - RIGHT_MIN)
    const r = Math.min(right, shared - s)
    return {
      side: s,
      right: r,
      sideMax: Math.min(SIDEBAR_MAX, shared - RIGHT_MIN),
      rightMax: Math.min(RIGHT_MAX, shared - s),
    }
  }
  // One panel, or two that cannot both dock: the sidebar has the right of way.
  return {
    side: side == null ? null : Math.min(side, Math.max(SIDEBAR_MIN, alone)),
    right: side == null && right != null ? Math.min(right, Math.max(RIGHT_MIN, alone)) : null,
    sideMax: Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, alone)),
    rightMax: Math.min(RIGHT_MAX, Math.max(RIGHT_MIN, alone)),
  }
}

/** `fitPanes` against the live viewport, re-fitted as the window changes. */
export function useFittedPanes(side: number | null, right: number | null): PaneFit {
  return useViewport((width) => fitPanes(width, side, right), shallow)
}
