import clsx from 'clsx'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useEffect } from 'react'
import type { AgentStatus } from '../lib/types'

/**
 * The shared controls, styled after Apple's.
 *
 * Three things carry most of that resemblance, and none of them is the colour:
 * a filled accent button against otherwise quiet chrome, hairline separators
 * rather than drawn boxes, and generous soft shadows on anything that floats.
 * The rest is restraint — one radius scale, one type scale, and no borders
 * where a change of tone will do.
 */

export function Button({
  children,
  onClick,
  variant = 'ghost',
  size = 'md',
  disabled,
  title,
  className,
  type = 'button',
}: {
  children: ReactNode
  onClick?: () => void
  variant?: 'primary' | 'ghost' | 'danger' | 'subtle'
  size?: 'sm' | 'md'
  disabled?: boolean
  title?: string
  className?: string
  type?: 'button' | 'submit'
}) {
  return (
    <button
      type={type}
      title={title}
      disabled={disabled}
      onClick={onClick}
      className={clsx(
        // Height is fixed rather than derived from padding and line-height.
        // Letting content decide meant an icon-only button, one with a label,
        // and one carrying a badge all came out different heights, and a row of
        // them looked ragged.
        'inline-flex shrink-0 items-center gap-1.5 rounded-control leading-none font-medium',
        'transition-[background-color,box-shadow,transform] duration-100 active:scale-[0.98]',
        'disabled:pointer-events-none disabled:opacity-40',
        'focus-visible:focus-ring',
        size === 'sm' ? 'h-[22px] px-2 text-[11px]' : 'h-7 px-3 text-xs',

        // The filled accent button is the single strongest Apple cue in the
        // whole interface: one clearly primary action per surface, in system
        // blue, with white text. Everything else stays quiet so it can be loud.
        variant === 'primary' && 'bg-accent text-white shadow-sm hover:brightness-110',

        // The push button: a raised tone with a hairline, not a box. The top
        // highlight is what makes it read as a surface catching light.
        variant === 'ghost' &&
          'border hairline bg-ink-800 text-ink-100 shadow-sm hover:bg-ink-750 ' +
            'inset-shadow-[0_1px_0_rgb(255_255_255/0.06)]',

        variant === 'subtle' && 'bg-transparent text-ink-300 hover:bg-ink-800 hover:text-ink-100',

        variant === 'danger' && 'bg-danger/12 text-danger hover:bg-danger/20',
        className,
      )}
    >
      {children}
    </button>
  )
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <label className="block">
      {/* Apple labels sentence case at normal weight, not shouted small caps:
          the label is scaffolding, and the value is the thing being read. */}
      <span className="mb-1 block text-[11px] font-medium text-ink-400">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-[11px] leading-snug text-ink-500">{hint}</span>}
    </label>
  )
}

const controlBase =
  'w-full rounded-control border hairline bg-ink-850 px-2.5 text-xs text-ink-100 ' +
  'placeholder:text-ink-500 outline-none transition-shadow focus:focus-ring'

/**
 * Single-line fields. The height matches a small Button exactly, because these
 * two almost always sit side by side — a text box with a Send or Create button
 * next to it — and a few pixels of difference there is what reads as sloppy.
 */
export const inputClass = `${controlBase} h-[22px]`

/** Multi-line fields, which size to their rows rather than a fixed height. */
export const textareaClass = `${controlBase} py-1.5 leading-relaxed`

/**
 * Icon-only buttons — the ones in toolbars, panel headers and the right end of
 * a list row.
 *
 * Square, and exactly as tall as a small Button and a field, for the same
 * reason those two agree: they share rows. Left to size themselves, each came
 * out as tall as its own glyph plus whatever padding that surface happened to
 * use, so the same button was 19px in the tree, 20px in the file list and 21px
 * in the terminal toolbar. Colour stays with the caller — the same box is quiet
 * on a toolbar and destructive at the end of a row.
 */
export const iconButtonClass =
  'inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-control ' +
  'transition-colors duration-100 focus-visible:focus-ring ' +
  'disabled:pointer-events-none disabled:opacity-40'

/**
 * The iOS switch, for a setting that takes effect the moment it is flipped.
 *
 * A switch and a checkbox are not interchangeable: a switch says "this is on
 * now", a checkbox says "this will be true when you submit". Everywhere it is
 * used here, there is nothing to submit.
 */
export function Switch({
  checked,
  onChange,
  disabled,
  title,
  label,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  disabled?: boolean
  title?: string
  label?: string
}) {
  return (
    <label className={clsx('inline-flex items-center gap-2', disabled && 'opacity-40')}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        title={title}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={clsx(
          'relative h-[22px] w-[38px] shrink-0 rounded-capsule transition-colors duration-200',
          'focus-visible:focus-ring',
          checked ? 'bg-accent' : 'bg-ink-700',
        )}
      >
        <span
          className={clsx(
            'absolute top-[2px] left-[2px] h-[18px] w-[18px] rounded-capsule bg-white',
            'shadow-knob transition-transform duration-200 ease-out',
            checked && 'translate-x-4',
          )}
        />
      </button>
      {label && <span className="text-xs text-ink-200">{label}</span>}
    </label>
  )
}

/**
 * The segmented control: a small set of mutually exclusive choices, all visible.
 *
 * It replaces the row-of-buttons pattern this app used for the same job. A row
 * of buttons where one happens to be highlighted asks the reader to work out
 * that they are alternatives; a segmented control says so by construction.
 */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  size = 'md',
  className,
}: {
  value: T
  onChange: (next: T) => void
  options: Array<{ value: T; label: ReactNode; title?: string }>
  size?: 'sm' | 'md'
  className?: string
}) {
  return (
    <div
      role="tablist"
      className={clsx(
        'inline-flex items-stretch gap-[2px] rounded-control bg-ink-800 p-[2px]',
        size === 'sm' ? 'h-[22px]' : 'h-7',
        className,
      )}
    >
      {options.map((o) => {
        const selected = o.value === value
        return (
          <button
            key={o.value}
            role="tab"
            aria-selected={selected}
            title={o.title}
            onClick={() => onChange(o.value)}
            className={clsx(
              'flex flex-1 items-center justify-center gap-1.5 rounded-[4px] px-2.5 leading-none',
              'font-medium whitespace-nowrap transition-colors duration-100',
              size === 'sm' ? 'text-[11px]' : 'text-xs',
              // The selected segment is a raised card inside the track, which
              // is how Apple shows selection without colour — leaving colour
              // free to mean something else. "Raised" is its own token because
              // it is lighter than the track in dark mode and white in light,
              // and no step of the ink ramp is both.
              selected ? 'bg-raised text-ink-100 shadow-sm' : 'text-ink-400 hover:text-ink-200',
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

export function Modal({
  title,
  onClose,
  children,
  footer,
  wide,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    // No click-to-dismiss on the backdrop: these hold forms, and losing a
    // half-typed server password to a stray click outside the panel is a worse
    // outcome than having to aim for Cancel. Escape still closes.
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-3 backdrop-blur-[2px] sm:p-10">
      <div
        className={clsx(
          // A sheet: rounded, floating on a wide soft shadow, with no border.
          // Capped rather than full height so a tall dialog stays centred and
          // scrolls inside itself instead of growing to touch both edges.
          'material flex max-h-[85vh] w-full flex-col overflow-hidden rounded-sheet shadow-sheet',
          wide ? 'max-w-3xl' : 'max-w-lg',
        )}
      >
        {/* A centred title with the close button floated over it, which is how
            a macOS sheet is titled. */}
        <header className="relative flex h-11 shrink-0 items-center justify-center border-b hairline px-4">
          <h2 className="text-[13px] font-semibold text-ink-100">{title}</h2>
          <button
            onClick={onClose}
            className="absolute right-3 rounded-capsule p-1 text-ink-400 hover:bg-ink-750 hover:text-ink-100"
          >
            <X size={14} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex items-center justify-end gap-2 border-t hairline px-4 py-3">
            {footer}
          </footer>
        )}
      </div>
    </div>
  )
}

const statusColor: Record<AgentStatus, string> = {
  running: 'bg-ok',
  idle: 'bg-idle',
  error: 'bg-danger',
  detached: 'bg-ink-600',
  unknown: 'bg-ink-600',
}

export function StatusDot({ status, pulse }: { status: AgentStatus; pulse?: boolean }) {
  return (
    <span className="relative inline-flex h-2 w-2 shrink-0">
      {pulse && status === 'running' && (
        <span className="absolute inline-flex h-full w-full animate-ping rounded-capsule bg-ok opacity-60" />
      )}
      <span className={clsx('relative inline-flex h-2 w-2 rounded-capsule', statusColor[status])} />
    </span>
  )
}

/**
 * The sticky "come look" mark next to an agent: amber and pulsing when it is
 * blocked on a human decision, calm accent-blue when it finished and left
 * results to review. The two read differently on purpose — one is urgent,
 * the other can wait until you are ready.
 */
export function AttentionDot({ kind, title }: { kind: 'input' | 'done'; title?: string }) {
  return (
    <span title={title} className="relative inline-flex h-2 w-2 shrink-0">
      {kind === 'input' && (
        <span className="absolute inline-flex h-full w-full animate-ping rounded-capsule bg-warn opacity-70" />
      )}
      <span
        className={clsx(
          'relative inline-flex h-2 w-2 rounded-capsule',
          kind === 'input' ? 'bg-warn' : 'bg-accent',
        )}
      />
    </span>
  )
}

export function ConnDot({ connected }: { connected: boolean }) {
  return (
    <span
      className={clsx(
        'inline-block h-2 w-2 shrink-0 rounded-capsule',
        connected ? 'bg-ok' : 'bg-ink-600',
      )}
    />
  )
}

/** A capsule, the way iOS labels things. */
export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode
  tone?: 'neutral' | 'ok' | 'warn' | 'danger' | 'accent'
}) {
  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-capsule px-[7px] py-[2px] text-[10px] font-medium',
        tone === 'neutral' && 'bg-ink-750 text-ink-300',
        tone === 'ok' && 'bg-ok/15 text-ok',
        tone === 'warn' && 'bg-warn/15 text-warn',
        tone === 'danger' && 'bg-danger/15 text-danger',
        tone === 'accent' && 'bg-accent/15 text-accent',
      )}
    >
      {children}
    </span>
  )
}

export function Empty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-1.5 px-6 text-center">
      <p className="text-[13px] font-medium text-ink-300">{title}</p>
      {hint && <p className="max-w-xs text-[11px] leading-relaxed text-ink-500">{hint}</p>}
    </div>
  )
}
