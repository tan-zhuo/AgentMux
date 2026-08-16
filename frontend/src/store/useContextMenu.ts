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
 * Opens a context menu for a right-click, replacing the webview's own.
 *
 * The default menu in a webview offers Reload and Back, which are meaningless
 * in an application window and actively harmful: a stray Reload throws away
 * every attached terminal view in one click.
 */
export function openContextMenu(e: React.MouseEvent, items: MenuItem[]) {
  e.preventDefault()
  e.stopPropagation()
  const usable = items.filter(Boolean)
  if (usable.length === 0) return
  useContextMenu.getState().show(e.clientX, e.clientY, usable)
}

/** A divider between groups of items. */
export const separator: MenuItem = {}
