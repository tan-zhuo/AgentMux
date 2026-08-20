/**
 * The keys a phone keyboard does not have.
 *
 * A shell needs Esc, Tab, the arrows and Ctrl-something constantly, and a
 * software keyboard offers none of them. What it does offer is letters — so
 * Ctrl and Alt are sticky here rather than held: arm one, type a letter on the
 * keyboard that already exists, and the letter becomes the control code. That
 * transformation happens on the data xterm emits rather than on key events,
 * because an Android keyboard delivers characters through composition and
 * input events that never look like a clean keydown.
 */

export interface Mods {
  ctrl: boolean
  alt: boolean
}

export const NO_MODS: Mods = { ctrl: false, alt: false }

/** The control byte a character produces under Ctrl, or null if it produces none. */
export function ctrlByte(ch: string): string | null {
  if (ch.length !== 1) return null
  const c = ch.charCodeAt(0)
  if (c >= 0x61 && c <= 0x7a) return String.fromCharCode(c - 0x60) // a–z
  if (c >= 0x41 && c <= 0x5a) return String.fromCharCode(c - 0x40) // A–Z
  switch (ch) {
    case ' ':
    case '@':
      return '\x00'
    case '[':
      return '\x1b'
    case '\\':
      return '\x1c'
    case ']':
      return '\x1d'
    case '^':
      return '\x1e'
    case '_':
    case '-':
      return '\x1f'
    case '?':
      return '\x7f'
    default:
      return null
  }
}

/**
 * What typed text becomes with the bar's modifiers armed.
 *
 * Only a single character can become a control code; anything longer is a paste
 * or a composed sequence and passes through, because turning a pasted line into
 * one control byte would be a silent way to lose it.
 */
export function applyMods(data: string, mods: Mods): string {
  let out = data
  if (mods.ctrl) out = ctrlByte(data) ?? out
  if (mods.alt) out = `\x1b${out}`
  return out
}

/** The modifier parameter shared by every CSI sequence: 1 + 2·alt + 4·ctrl. */
function modParam(mods: Mods): number {
  return 1 + (mods.alt ? 2 : 0) + (mods.ctrl ? 4 : 0)
}

/** A cursor key, in the plain form or the modified one the parameter calls for. */
export function csiFinal(final: string, mods: Mods): string {
  const p = modParam(mods)
  return p === 1 ? `\x1b[${final}` : `\x1b[1;${p}${final}`
}

/** A `~`-terminated key (Delete, Page Up and the rest), likewise. */
export function csiTilde(n: number, mods: Mods): string {
  const p = modParam(mods)
  return p === 1 ? `\x1b[${n}~` : `\x1b[${n};${p}~`
}

export type KeyAction =
  /** Arms or disarms a sticky modifier. */
  | { kind: 'mod'; mod: keyof Mods }
  /** Literal bytes, with the armed modifiers applied on the way out. */
  | { kind: 'raw'; data: string }
  /** A cursor or navigation key that encodes the modifiers into itself. */
  | { kind: 'csi'; final?: string; tilde?: number }
  /** A fixed control byte, whatever is armed — the ^C on the front row. */
  | { kind: 'code'; data: string }

export interface KeyCap {
  id: string
  label: string
  action: KeyAction
  /** Held down, this key repeats — the arrows, and nothing else by default. */
  repeat?: boolean
  /** A wider cap for a wider label. */
  wide?: boolean
  /** A narrower cap for a single glyph, so the front row fits a small phone. */
  narrow?: boolean
}

const raw = (id: string, label: string, data: string): KeyCap => ({
  id,
  label,
  action: { kind: 'raw', data },
})
const code = (id: string, label: string, data: string): KeyCap => ({
  id,
  label,
  action: { kind: 'code', data },
})
// Captioned `^C` rather than `⌃C`: it is what the terminal itself echoes, and
// the Apple glyph is missing from the mono fonts most phones ship with.
const ctrlKey = (ch: string): KeyCap =>
  code(`c-${ch}`, `^${ch.toUpperCase()}`, ctrlByte(ch) ?? '')
const fn = (n: number): KeyCap =>
  raw(`f${n}`, `F${n}`, n <= 4 ? `\x1bO${'PQRS'[n - 1]}` : `\x1b[${[15, 17, 18, 19, 20, 21, 23, 24][n - 5]}~`)

/**
 * The front row: what a hand reaches for without thinking. Esc and Tab because
 * every prompt wants them, the two modifiers because they unlock the whole
 * alphabet, the arrows because history and cursor movement are most of shell
 * editing, and ^C because interrupting a runaway command should never be more
 * than one tap away.
 */
export const PRIMARY_KEYS: KeyCap[] = [
  raw('esc', 'Esc', '\x1b'),
  raw('tab', 'Tab', '\t'),
  { id: 'ctrl', label: 'Ctrl', action: { kind: 'mod', mod: 'ctrl' }, wide: true },
  { id: 'alt', label: 'Alt', action: { kind: 'mod', mod: 'alt' }, wide: true },
  { id: 'left', label: '←', action: { kind: 'csi', final: 'D' }, repeat: true, narrow: true },
  { id: 'up', label: '↑', action: { kind: 'csi', final: 'A' }, repeat: true, narrow: true },
  { id: 'down', label: '↓', action: { kind: 'csi', final: 'B' }, repeat: true, narrow: true },
  { id: 'right', label: '→', action: { kind: 'csi', final: 'C' }, repeat: true, narrow: true },
  ctrlKey('c'),
]

/** The second tier, one tap further away: the rest of a terminal's keyboard. */
export const EXTRA_KEYS: KeyCap[] = [
  ctrlKey('d'),
  ctrlKey('z'),
  ctrlKey('r'),
  ctrlKey('l'),
  ctrlKey('a'),
  ctrlKey('e'),
  ctrlKey('k'),
  ctrlKey('u'),
  ctrlKey('w'),
  code('c-bslash', '^\\', '\x1c'),
  { id: 'home', label: 'Home', action: { kind: 'csi', final: 'H' }, wide: true },
  { id: 'end', label: 'End', action: { kind: 'csi', final: 'F' }, wide: true },
  { id: 'pgup', label: 'PgUp', action: { kind: 'csi', tilde: 5 }, wide: true, repeat: true },
  { id: 'pgdn', label: 'PgDn', action: { kind: 'csi', tilde: 6 }, wide: true, repeat: true },
  { id: 'del', label: 'Del', action: { kind: 'csi', tilde: 3 }, repeat: true },
  { id: 'ins', label: 'Ins', action: { kind: 'csi', tilde: 2 } },
  { ...raw('btab', '⇧Tab', '\x1b[Z'), wide: true },
  ...Array.from({ length: 12 }, (_, i) => fn(i + 1)),
  raw('pipe', '|', '|'),
  raw('tilde', '~', '~'),
  raw('backtick', '`', '`'),
  raw('bslash', '\\', '\\'),
  raw('slash', '/', '/'),
  raw('dash', '-', '-'),
  raw('under', '_', '_'),
  raw('star', '*', '*'),
  raw('dollar', '$', '$'),
  raw('caret', '^', '^'),
  raw('amp', '&', '&'),
  raw('lt', '<', '<'),
  raw('gt', '>', '>'),
]

/** The bytes a cap sends, given what is armed. Modifier caps send nothing. */
export function keyBytes(cap: KeyCap, mods: Mods): string {
  switch (cap.action.kind) {
    case 'mod':
      return ''
    case 'code':
      return cap.action.data
    case 'raw':
      return applyMods(cap.action.data, mods)
    case 'csi':
      return cap.action.tilde !== undefined
        ? csiTilde(cap.action.tilde, mods)
        : csiFinal(cap.action.final ?? 'A', mods)
  }
}
