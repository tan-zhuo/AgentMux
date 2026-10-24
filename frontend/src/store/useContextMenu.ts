import type { ComponentType } from 'react'
import { create } from 'zustand'

export interface MenuItem {
  /** Omit the label to render a separator. */
  label?: string
  icon?: ComponentType<{ size?: number | string; className?: string }>
  hint?: string
  danger?: boolean
  disabled?: boolean
  onSelect?: () => void | Promise<void>
}

interface ContextMenuState {
  open: boolean
  x: number
  y: number
  items: MenuItem[]
  show: (x: number, y: number, items: MenuItem[]) => void
  hide: () => void
}

export const useContextMenu = create<ContextMenuState>((set) => ({
  open: false,
  x: 0,
  y: 0,
  items: [],
  show: (x, y, items) => set({ open: true, x, y, items }),
  hide: () => set({ open: false, items: [] }),
}))

/**
 * What a menu needs from the event that opens it: a point, and the chance to
 * swallow the event. A right-click hands over its own event; a button that
 * opens the same menu by a tap hands over a stand-in (see `anchorBelow`).
 */
export type MenuAnchor = Pick<
  React.MouseEvent,
  'clientX' | 'clientY' | 'preventDefault' | 'stopPropagation'
>

/**
 * Opens a context menu for a right-click, replacing the webview's own.
 *
 * The default menu in a webview offers Reload and Back, which are meaningless
 * in an application window and actively harmful: a stray Reload throws away
 * every attached terminal view in one click.
 */
export function openContextMenu(e: MenuAnchor, items: MenuItem[]) {
  e.preventDefault()
  e.stopPropagation()
  const usable = items.filter(Boolean)
  if (usable.length === 0) return
  useContextMenu.getState().show(e.clientX, e.clientY, usable)
}

/**
 * A stand-in for the pointer event, hanging the menu from an element's lower
 * left corner the way a pull-down menu hangs from its button.
 *
 * A touch screen has no right-click, and the long press that stands in for
 * one is not offered everywhere, so rows carry a "more" button that opens the
 * same menu. It is anchored to the button rather than to wherever the finger
 * landed on it, so the menu opens in the same place every time.
 */
export function anchorBelow(el: Element): MenuAnchor {
  const r = el.getBoundingClientRect()
  return {
    clientX: r.left,
    clientY: r.bottom + 2,
    preventDefault() {},
    stopPropagation() {},
  }
}

/** A divider between groups of items. */
export const separator: MenuItem = {}
