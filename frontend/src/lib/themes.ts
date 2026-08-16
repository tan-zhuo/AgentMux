/**
 * Theme definitions.
 *
 * This file is the single source of truth for colour. The UI tokens are written
 * onto the document element as `--color-*` custom properties at runtime, which
 * is exactly what Tailwind's generated utilities (`bg-ink-900`, `text-accent`, …)
 * dereference — so switching a theme re-colours the whole app without a reload
 * and without any component knowing a theme exists.
 *
 * The one thing that cannot follow at runtime is the Windows system border
 * colour, which Wails only applies at window creation. `internal/app/chrome.go`
 * mirrors the `window` block below and reads the persisted theme at startup.
 */

export type ThemeMode = 'dark' | 'light'

/** xterm.js palette. Terminal colours are genuinely theme-specific — Nord and
 *  Solarized are defined by their ANSI mappings — so they are not derived. */
export interface TerminalPalette {
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

/** UI tokens. Keys become `--color-<key>`; the ink ramp runs from the base
 *  surface (950) to the most prominent text (100), which holds in light mode
 *  too — there the surfaces are pale and the text end is near-black. */
export interface ThemeColors {
  'ink-950': string
  'ink-900': string
  'ink-850': string
  'ink-800': string
  'ink-750': string
  'ink-700': string
  'ink-600': string
  'ink-500': string
  'ink-400': string
  'ink-300': string
  'ink-200': string
  'ink-100': string
  accent: string
  'accent-dim': string
  ok: string
  warn: string
  danger: string
  idle: string
}

export interface Theme {
  id: string
  name: string
  blurb: string
  mode: ThemeMode
  colors: ThemeColors
  terminal: TerminalPalette
  /** Native window background, painted before the webview draws. */
  window: string
}

export const themes: Theme[] = [
  {
    id: 'midnight',
    name: 'Midnight',
    blurb: 'Cool blue-grey. The default.',
    mode: 'dark',
    window: '#0b0d12',
    colors: {
      'ink-950': '#080a0f',
      'ink-900': '#0b0d12',
      'ink-850': '#10131a',
      'ink-800': '#151922',
      'ink-750': '#1b202b',
      'ink-700': '#232936',
      'ink-600': '#333b4c',
      'ink-500': '#4a5468',
      'ink-400': '#6b768c',
      'ink-300': '#939eb3',
      'ink-200': '#c2cad8',
      'ink-100': '#e6eaf2',
      accent: '#4c8dff',
      'accent-dim': '#2f5fb3',
      ok: '#3ddc97',
      warn: '#ffc857',
      danger: '#ff6b6b',
      idle: '#8b93a7',
    },
    terminal: {
      background: '#080a0f',
      foreground: '#e6eaf2',
      cursor: '#4c8dff',
      cursorAccent: '#080a0f',
      selectionBackground: '#2f5fb355',
      black: '#151922',
      red: '#ff6b6b',
      green: '#3ddc97',
      yellow: '#ffc857',
      blue: '#4c8dff',
      magenta: '#c77dff',
      cyan: '#4dd0e1',
      white: '#c2cad8',
      brightBlack: '#4a5468',
      brightRed: '#ff8f8f',
      brightGreen: '#68e8b3',
      brightYellow: '#ffd88a',
      brightBlue: '#7fb0ff',
      brightMagenta: '#dba5ff',
      brightCyan: '#7fe3f0',
      brightWhite: '#ffffff',
    },
  },

  {
    id: 'graphite',
    name: 'Graphite',
    blurb: 'Neutral true grey, no colour cast.',
    mode: 'dark',
    window: '#101010',
    colors: {
      'ink-950': '#0a0a0a',
      'ink-900': '#101010',
      'ink-850': '#161616',
      'ink-800': '#1c1c1c',
      'ink-750': '#232323',
      'ink-700': '#2c2c2c',
      'ink-600': '#3d3d3d',
      'ink-500': '#565656',
      'ink-400': '#767676',
      'ink-300': '#9c9c9c',
      'ink-200': '#c8c8c8',
      'ink-100': '#ededed',
      accent: '#a78bfa',
      'accent-dim': '#6d54c4',
      ok: '#6cc164',
      warn: '#e0b341',
      danger: '#e35d5d',
      idle: '#8a8a8a',
    },
    terminal: {
      background: '#0a0a0a',
      foreground: '#ededed',
      cursor: '#a78bfa',
      cursorAccent: '#0a0a0a',
      selectionBackground: '#a78bfa44',
      black: '#1c1c1c',
      red: '#e35d5d',
      green: '#6cc164',
      yellow: '#e0b341',
      blue: '#6a9fe0',
      magenta: '#a78bfa',
      cyan: '#4db6ac',
      white: '#c8c8c8',
      brightBlack: '#565656',
      brightRed: '#ef8a8a',
      brightGreen: '#92d98b',
      brightYellow: '#ecc96f',
      brightBlue: '#93bcec',
      brightMagenta: '#c3aefc',
      brightCyan: '#78cec6',
      brightWhite: '#ffffff',
    },
  },

  {
    id: 'nord',
    name: 'Nord',
    blurb: 'Arctic blue, low contrast.',
    mode: 'dark',
    window: '#2e3440',
    colors: {
      'ink-950': '#242933',
      'ink-900': '#2e3440',
      'ink-850': '#333b49',
      'ink-800': '#3b4252',
      'ink-750': '#434c5e',
      'ink-700': '#4c566a',
      'ink-600': '#5b6678',
      'ink-500': '#6d798d',
      'ink-400': '#8b97a8',
      'ink-300': '#aab4c2',
      'ink-200': '#d8dee9',
      'ink-100': '#eceff4',
      accent: '#88c0d0',
      'accent-dim': '#5e81ac',
      ok: '#a3be8c',
      warn: '#ebcb8b',
      danger: '#bf616a',
      idle: '#7b879b',
    },
    terminal: {
      background: '#2e3440',
      foreground: '#d8dee9',
      cursor: '#88c0d0',
      cursorAccent: '#2e3440',
      selectionBackground: '#4c566a88',
      black: '#3b4252',
      red: '#bf616a',
      green: '#a3be8c',
      yellow: '#ebcb8b',
      blue: '#81a1c1',
      magenta: '#b48ead',
      cyan: '#88c0d0',
      white: '#e5e9f0',
      brightBlack: '#4c566a',
      brightRed: '#d08770',
      brightGreen: '#a3be8c',
      brightYellow: '#ebcb8b',
      brightBlue: '#81a1c1',
      brightMagenta: '#b48ead',
      brightCyan: '#8fbcbb',
      brightWhite: '#eceff4',
    },
  },

  {
    id: 'solarized-dark',
    name: 'Solarized Dark',
    blurb: 'Ethan Schoonover’s classic.',
    mode: 'dark',
    window: '#002b36',
    colors: {
      'ink-950': '#00212b',
      'ink-900': '#002b36',
      'ink-850': '#03303c',
      'ink-800': '#073642',
      'ink-750': '#0d4552',
      'ink-700': '#14505f',
      'ink-600': '#2b6070',
      'ink-500': '#586e75',
      'ink-400': '#657b83',
      'ink-300': '#839496',
      'ink-200': '#93a1a1',
      'ink-100': '#eee8d5',
      accent: '#268bd2',
      'accent-dim': '#1a5f90',
      ok: '#859900',
      warn: '#b58900',
      danger: '#dc322f',
      idle: '#657b83',
    },
    terminal: {
      background: '#002b36',
      foreground: '#839496',
      cursor: '#93a1a1',
      cursorAccent: '#002b36',
      selectionBackground: '#07364299',
      black: '#073642',
      red: '#dc322f',
      green: '#859900',
      yellow: '#b58900',
      blue: '#268bd2',
      magenta: '#d33682',
      cyan: '#2aa198',
      white: '#eee8d5',
      brightBlack: '#002b36',
      brightRed: '#cb4b16',
      brightGreen: '#586e75',
      brightYellow: '#657b83',
      brightBlue: '#839496',
      brightMagenta: '#6c71c4',
      brightCyan: '#93a1a1',
      brightWhite: '#fdf6e3',
    },
  },

  {
    id: 'gruvbox-dark',
    name: 'Gruvbox Dark',
    blurb: 'Warm retro, easy on the eyes.',
    mode: 'dark',
    window: '#282828',
    colors: {
      'ink-950': '#1d2021',
      'ink-900': '#282828',
      'ink-850': '#32302f',
      'ink-800': '#3c3836',
      'ink-750': '#453f3a',
      'ink-700': '#504945',
      'ink-600': '#665c54',
      'ink-500': '#7c6f64',
      'ink-400': '#928374',
      'ink-300': '#a89984',
      'ink-200': '#bdae93',
      'ink-100': '#ebdbb2',
      accent: '#83a598',
      'accent-dim': '#458588',
      ok: '#b8bb26',
      warn: '#fabd2f',
      danger: '#fb4934',
      idle: '#928374',
    },
    terminal: {
      background: '#282828',
      foreground: '#ebdbb2',
      cursor: '#ebdbb2',
      cursorAccent: '#282828',
      selectionBackground: '#504945aa',
      black: '#282828',
      red: '#cc241d',
      green: '#98971a',
      yellow: '#d79921',
      blue: '#458588',
      magenta: '#b16286',
      cyan: '#689d6a',
      white: '#a89984',
      brightBlack: '#928374',
      brightRed: '#fb4934',
      brightGreen: '#b8bb26',
      brightYellow: '#fabd2f',
      brightBlue: '#83a598',
      brightMagenta: '#d3869b',
      brightCyan: '#8ec07c',
      brightWhite: '#ebdbb2',
    },
  },

  {
    id: 'daylight',
    name: 'Daylight',
    blurb: 'Light theme for bright rooms.',
    mode: 'light',
    window: '#f7f8fa',
    colors: {
      'ink-950': '#ffffff',
      'ink-900': '#f7f8fa',
      'ink-850': '#ffffff',
      'ink-800': '#eef0f4',
      'ink-750': '#e4e7ec',
      'ink-700': '#d5dae2',
      'ink-600': '#b9c0cc',
      'ink-500': '#8b94a3',
      'ink-400': '#69717e',
      'ink-300': '#4d5561',
      'ink-200': '#2b323c',
      'ink-100': '#11151b',
      accent: '#2563eb',
      'accent-dim': '#93b4fb',
      ok: '#16a34a',
      warn: '#b45309',
      danger: '#dc2626',
      idle: '#8b94a3',
    },
    terminal: {
      background: '#ffffff',
      foreground: '#24292f',
      cursor: '#2563eb',
      cursorAccent: '#ffffff',
      selectionBackground: '#2563eb26',
      black: '#24292f',
      red: '#cf222e',
      green: '#116329',
      yellow: '#7d4e00',
      blue: '#0969da',
      magenta: '#8250df',
      cyan: '#1b7c83',
      white: '#6e7781',
      brightBlack: '#57606a',
      brightRed: '#a40e26',
      brightGreen: '#1a7f37',
      brightYellow: '#9a6700',
      brightBlue: '#218bff',
      brightMagenta: '#a475f9',
      brightCyan: '#3192aa',
      brightWhite: '#8c959f',
    },
  },
]

export const defaultThemeId = 'midnight'

export function getTheme(id: string): Theme {
  return themes.find((t) => t.id === id) ?? themes[0]
}

/** Splits `#rrggbb` into 8-bit channels for the native window background. */
export function hexToRgb(hex: string): { r: number; g: number; b: number } {
  const v = hex.replace('#', '')
  return {
    r: parseInt(v.slice(0, 2), 16),
    g: parseInt(v.slice(2, 4), 16),
    b: parseInt(v.slice(4, 6), 16),
  }
}
