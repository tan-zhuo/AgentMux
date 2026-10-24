import type { Terminal } from '@xterm/xterm'

/**
 * How much of a terminal the text view shows: the rows on screen right now,
 * or everything there is to be had.
 */
export type TextScope = 'screen' | 'history'

/**
 * How far back to ask a multiplexer for. A couple of thousand lines is more
 * than anyone scrolls through on a phone, and tmux hands it over in a single
 * round trip; the whole scrollback could be a hundred times that.
 */
export const HISTORY_LINES = 2000

/**
 * Captured terminal text, made fit to read as text.
 *
 * What comes off a pane is laid out for a screen, not for a page: every row
 * is padded out to the terminal's width — tmux's -J keeps those trailing
 * spaces on purpose, so that the wrapped rows it joins come out whole — and a
 * full-screen program leaves runs of blank rows wherever it drew nothing. The
 * padding goes, a run of blank lines becomes one, and the empty rows above
 * the first line of output and below the last go too. Nothing inside a line
 * is touched: a box-drawing frame stays a frame.
 */
export function tidyTerminalText(raw: string): string {
  const out: string[] = []
  let blank = false
  for (const line of raw.replace(/\r\n?/g, '\n').split('\n')) {
    const text = line.replace(/\s+$/, '')
    if (!text) {
      if (out.length && !blank) out.push('')
      blank = true
      continue
    }
    out.push(text)
    blank = false
  }
  while (out.length && !out[out.length - 1]) out.pop()
  return out.join('\n')
}

/**
 * The text of xterm's own buffer: the rows on screen, or the whole of what it
 * holds.
 *
 * A line longer than the terminal is wide is stored as several rows, each
 * after the first flagged as wrapped; those are stitched back into the line
 * they were, so a path or a command copied out of a phone-width terminal is
 * not broken wherever the screen happened to end. A row that continues on
 * the next keeps its trailing spaces — a space at the fold is part of the
 * text — and only the row that ends a line is trimmed.
 *
 * The screen is the viewport, not the live screen: someone who scrolled up to
 * look at something and then asked for the text meant that something.
 */
export function bufferText(term: Terminal, scope: TextScope): string {
  const buf = term.buffer.active
  const start = scope === 'screen' ? buf.viewportY : 0
  const end = scope === 'screen' ? Math.min(buf.length, buf.viewportY + term.rows) : buf.length
  const lines: string[] = []
  for (let y = start; y < end; y++) {
    const row = buf.getLine(y)
    if (!row) continue
    const continues = y + 1 < end && (buf.getLine(y + 1)?.isWrapped ?? false)
    const text = row.translateToString(!continues)
    if (row.isWrapped && lines.length) lines[lines.length - 1] += text
    else lines.push(text)
  }
  return tidyTerminalText(lines.join('\n'))
}
