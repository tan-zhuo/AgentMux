import { Window } from '@wailsio/runtime'
import { create } from 'zustand'
import { tree } from '../lib/api'
import { defaultThemeId, getTheme, hexToRgb, type Theme } from '../lib/themes'

const SETTING_KEY = 'theme'

/**
 * Writes a theme onto the document. Tailwind's utilities all dereference these
 * custom properties, so this one function re-colours every component.
 */
export function applyTheme(theme: Theme) {
  const root = document.documentElement
  for (const [token, value] of Object.entries(theme.colors)) {
    root.style.setProperty(`--color-${token}`, value)
  }
  // Drives native scrollbars, form controls and the webview's own default paint.
  root.style.colorScheme = theme.mode
  root.dataset.theme = theme.id

  // Paint the native window frame to match, so resizing does not flash the old
  // colour behind the webview.
  const { r, g, b } = hexToRgb(theme.window)
  void Window.SetBackgroundColour(r, g, b, 255).catch(() => {})
}

interface ThemeState {
  themeId: string
  theme: Theme
  /** Loads the persisted choice and applies it. */
  init: () => Promise<void>
  setTheme: (id: string) => void
}

export const useTheme = create<ThemeState>((set) => ({
  themeId: defaultThemeId,
  theme: getTheme(defaultThemeId),

  async init() {
    let id = defaultThemeId
    try {
      id = await tree.getSetting(SETTING_KEY, defaultThemeId)
    } catch {
      /* first run, or the store is unavailable — the default is fine */
    }
    const theme = getTheme(id)
    applyTheme(theme)
    set({ themeId: theme.id, theme })
  },

  setTheme(id) {
    const theme = getTheme(id)
    applyTheme(theme)
    set({ themeId: theme.id, theme })
    void tree.setSetting(SETTING_KEY, theme.id).catch(() => {})
  },
}))
