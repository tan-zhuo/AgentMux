import clsx from 'clsx'
import { X } from 'lucide-react'
import type { ComponentProps, ReactNode } from 'react'
import { useContext, useEffect, useRef } from 'react'
import { ExitingContext, useSlidingPill } from '../lib/motion'
import type { AgentStatus } from '../lib/types'
import { Tooltip } from './Tooltip'

/**
 * The shared controls, styled after Apple's.
 *
 * Three things carry most of that resemblance, and none of them is the colour:
 * a filled accent button against otherwise quiet chrome, hairline separators
 * rather than drawn boxes, and generous soft shadows on anything that floats.
 * The rest is restraint — one radius scale, one type scale, and no borders
 * where a change of tone will do.
 *
 * Since macOS 26 there is a fourth: everything responds. A button shrinks
 * under the pointer and springs back, a switch's knob stretches while it is
 * held, a selected segment slides to where it was clicked rather than
 * appearing there. None of it takes longer than a glance, and all of it is
 * what makes a control feel like a thing rather than a drawing of one.
 */

/**
 * What makes a small control a finger-sized target without making it bigger.
 *
 * The controls here are 22px because that is the right size under a mouse, and
 * redrawing every toolbar and row at phone size would undo that for everyone.
 * So on a touch screen each one grows a halo instead: an invisible
 * pseudo-element reaching 7px past every edge, so the finger gets 36px while
 * the eye still gets 22 and nothing in the layout moves. The halo hangs off
 * the button's own box, which is why `relative` rides along — a control that
 * is already `absolute` wants the two `before:` classes alone.
 *
 * A halo reaches into the neighbours. In a row of these the one later in the
 * document is hit first, so with a 2px gap the right edge of each button
 * belongs to the next one; a row of haloed buttons therefore widens its gap
 * to 14px on touch (`touch:gap-3.5`), which is where the halos meet.
 */
export const touchHalo = 'touch:relative touch:before:absolute touch:before:-inset-[7px]'

export function Button({
  children,
  onClick,
  variant = 'ghost',
  size = 'md',
  disabled,
  title,
  tip,
  className,
  type = 'button',
}: {
  children: ReactNode
  onClick?: () => void
  variant?: 'primary' | 'ghost' | 'danger' | 'subtle'
  size?: 'sm' | 'md'
  disabled?: boolean
  /** The native tooltip. It is on its way out; new code should say `tip`. */
  title?: string
  /** The label shown while the pointer rests on the button — see Tooltip. */
  tip?: string
  className?: string
  type?: 'button' | 'submit'
}) {
  const button = (
    <button
      type={type}
      title={tip ? undefined : title}
      disabled={disabled}
      onClick={onClick}
      className={clsx(
        // Height is fixed rather than derived from padding and line-height.
        // Letting content decide meant an icon-only button, one with a label,
        // and one carrying a badge all came out different heights, and a row of
        // them looked ragged.
        'pressable inline-flex shrink-0 items-center gap-1.5 rounded-control leading-none font-medium',
        'disabled:pointer-events-none disabled:opacity-40',
        'focus-visible:focus-ring',
        // Under a finger both sizes reach 36px through their halo: 7px around
        // the small one, 4px around the medium.
        size === 'sm'
          ? `h-[22px] px-2 text-[11px] ${touchHalo}`
          : 'h-7 px-3 text-xs touch:relative touch:before:absolute touch:before:-inset-1',

        // Held down, every variant takes its fill one step past hover. That
        // is how macOS gives a small control its press — on a 22px button the
        // shrink alone is a single pixel and reads as nothing. The filled
        // button darkens instead, there being no deeper blue to step to that
        // is still the accent. Disabled buttons take no pointer events, so no
        // press reaches them.

        // The filled accent button is the single strongest Apple cue in the
        // whole interface: one clearly primary action per surface, in system
        // blue, with white text. Everything else stays quiet so it can be loud.
        // The top highlight and the tinted shadow are what make it a glass
        // bead rather than a flat fill.
        variant === 'primary' &&
          'bg-accent text-white hover:brightness-110 active:brightness-95 ' +
            'shadow-[inset_0_1px_0_rgb(255_255_255/0.22),0_1px_2px_rgb(0_0_0/0.2),0_4px_12px_color-mix(in_srgb,var(--color-accent)_35%,transparent)]',

        // The push button: a raised tone with a hairline, not a box. The top
        // highlight is what makes it read as a surface catching light.
        variant === 'ghost' &&
          'border hairline bg-ink-800 text-ink-100 shadow-sm hover:bg-ink-750 active:bg-ink-700 ' +
            'inset-shadow-[0_1px_0_rgb(255_255_255/0.06)]',

        variant === 'subtle' &&
          'bg-transparent text-ink-300 hover:bg-ink-800 hover:text-ink-100 active:bg-ink-750',

        variant === 'danger' && 'bg-danger/12 text-danger hover:bg-danger/20 active:bg-danger/28',
        className,
      )}
    >
      {children}
    </button>
  )
  return tip ? <Tooltip label={tip}>{button}</Tooltip> : button
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
  'placeholder:text-ink-500 outline-none focus:focus-ring ' +
  'transition-[box-shadow,border-color,background-color] duration-[var(--duration-quick)] ease-[var(--ease-out)]'

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
 *
 * The press is firmer than a Button's, 0.92 against 0.96, because at this size
 * the standard shrink is a single pixel and reads as nothing.
 *
 * On a touch screen the square keeps its 22px and gains the halo described
 * above, so a toolbar of these needs `touch:gap-3.5` between them.
 */
export const iconButtonClass =
  'pressable [--press-scale:0.92] inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-control ' +
  `focus-visible:focus-ring disabled:pointer-events-none disabled:opacity-40 ${touchHalo}`

/**
 * An icon-only button with its name in a tooltip: `iconButtonClass`, the
 * label, and nothing else to remember. The label also becomes the button's
 * accessible name, which a glyph on its own does not have. Colour still
 * belongs to the caller, for the reason given above.
 */
export function IconButton({
  tip,
  className,
  children,
  ...rest
}: Omit<ComponentProps<'button'>, 'title'> & { tip: string }) {
  return (
    <Tooltip label={tip}>
      <button type="button" {...rest} className={clsx(iconButtonClass, className)}>
        {children}
      </button>
    </Tooltip>
  )
}

/**
 * A key cap, for the shortcut hints a palette or a menu shows beside a
 * command. Drawn as a small raised tile the way macOS draws them in menus
 * since 26, rather than as bare glyphs in grey.
 */
export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={clsx(
        'inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[5px] px-1',
        'border border-ink-700/60 bg-ink-800 font-sans text-[10.5px] font-medium text-ink-300',
        'shadow-[inset_0_1px_0_rgb(255_255_255/0.05),0_1px_0_rgb(0_0_0/0.25)]',
        className,
      )}
    >
      {children}
    </kbd>
  )
}

/**
 * The iOS switch, for a setting that takes effect the moment it is flipped.
 *
 * A switch and a checkbox are not interchangeable: a switch says "this is on
 * now", a checkbox says "this will be true when you submit". Everywhere it is
 * used here, there is nothing to submit.
 *
 * The knob stretches while the button is held and snaps back when it lands,
 * which is what the system switch does and is the whole difference between a
 * switch that feels mechanical and one that feels painted.
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
          'group/switch relative h-[22px] w-[38px] shrink-0 rounded-capsule',
          'transition-colors duration-[var(--duration-base)] ease-[var(--ease-out)]',
          'focus-visible:focus-ring',
          // Already positioned, so the halo alone; the knob's stops are in
          // pixels, which is why the track itself does not grow on touch.
          'touch:before:absolute touch:before:-inset-[7px]',
          checked ? 'bg-accent' : 'bg-ink-700',
          // A faint inner edge so the track reads as a groove the knob sits
          // in, rather than a pill the knob sits on.
          'shadow-[inset_0_0_0_0.5px_rgb(0_0_0/0.12)]',
        )}
      >
        <span
          className={clsx(
            'absolute top-[2px] h-[18px] w-[18px] rounded-capsule bg-white shadow-knob',
            'transition-[left,width,transform] duration-[var(--duration-base)] ease-[var(--ease-spring)]',
            'group-active/switch:w-[22px] group-active/switch:duration-[var(--duration-quick)]',
            checked ? 'left-[18px] group-active/switch:left-[14px]' : 'left-[2px]',
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
 *
 * One raised pill slides between the segments rather than each segment
 * painting its own, so a change of choice is seen as a move and not as two
 * things blinking.
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
  const stripRef = useRef<HTMLDivElement | null>(null)
  const pill = useSlidingPill(stripRef, value)

  return (
    <div
      ref={stripRef}
      role="tablist"
      className={clsx(
        'relative inline-flex items-stretch gap-[2px] rounded-control bg-ink-800 p-[2px]',
        'shadow-[inset_0_0_0_0.5px_rgb(0_0_0/0.1)]',
        // The segments are the whole control, so a halo has nothing to hang
        // off that is not another segment; the track grows instead. The pill
        // is measured, so it follows.
        size === 'sm' ? 'h-[22px] touch:h-9' : 'h-7 touch:h-9',
        className,
      )}
    >
      {/* The selected segment is a raised card inside the track, which is how
          Apple shows selection without colour — leaving colour free to mean
          something else. "Raised" is its own token because it is lighter than
          the track in dark mode and white in light, and no step of the ink
          ramp is both. */}
      <span
        aria-hidden
        style={pill.style}
        className={clsx(
          'pointer-events-none absolute top-0 left-0 rounded-[5px] bg-raised shadow-sm',
          'inset-shadow-[0_1px_0_rgb(255_255_255/0.08)]',
          pill.ready && 'transition-[transform,width] duration-[var(--duration-base)] ease-[var(--ease-spring)]',
        )}
      />
      {options.map((o) => {
        const selected = o.value === value
        return (
          <button
            key={o.value}
            role="tab"
            aria-selected={selected}
            title={o.title}
            data-pill-key={o.value}
            onClick={() => onChange(o.value)}
            className={clsx(
              'relative z-10 flex flex-1 items-center justify-center gap-1.5 rounded-[5px] px-2.5 leading-none',
              'font-medium whitespace-nowrap transition-colors duration-[var(--duration-quick)]',
              'active:scale-[0.97]',
              size === 'sm' ? 'text-[11px] touch:text-xs' : 'text-xs touch:text-[13px]',
              selected ? 'text-ink-100' : 'text-ink-400 hover:text-ink-200',
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
  const exiting = useContext(ExitingContext)

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
    <div
      className={clsx(
        'fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-3 backdrop-blur-[3px] sm:p-10',
        // The scrim is held for the fade along with the sheet, and a held
        // scrim still covers the window. Without this the first click after
        // a dismissal lands on it and goes nowhere.
        exiting ? 'animate-fade-out pointer-events-none' : 'animate-fade-in',
      )}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={clsx(
          // A sheet: rounded, floating on a wide soft shadow, with no border.
          // Capped rather than full height so a tall dialog stays centred and
          // scrolls inside itself instead of growing to touch both edges.
          'material flex max-h-[85vh] w-full flex-col overflow-hidden rounded-sheet shadow-sheet',
          wide ? 'max-w-3xl' : 'max-w-lg',
          // Inert on the way out: a Save that could be clicked twice during
          // the exit would be submitted twice.
          exiting ? 'animate-sheet-out pointer-events-none' : 'animate-sheet-in',
        )}
      >
        {/* A centred title with the close button floated over it, which is how
            a macOS sheet is titled. */}
        <header className="relative flex h-11 shrink-0 items-center justify-center border-b hairline px-4">
          <h2 className="text-[13px] font-semibold text-ink-100">{title}</h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className={clsx(
              'pressable absolute right-3 flex h-[22px] w-[22px] items-center justify-center rounded-capsule',
              'bg-ink-800/70 text-ink-400 hover:bg-ink-750 hover:text-ink-100',
              'touch:before:absolute touch:before:-inset-[7px]',
            )}
          >
            <X size={13} />
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
        <span className="absolute inline-flex h-full w-full animate-breathe rounded-capsule bg-ok" />
      )}
      <span
        className={clsx(
          'relative inline-flex h-2 w-2 rounded-capsule transition-colors duration-[var(--duration-base)]',
          statusColor[status],
        )}
      />
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
    <span title={title} className="relative inline-flex h-2 w-2 shrink-0 animate-pop-in">
      {kind === 'input' && (
        <span className="absolute inline-flex h-full w-full animate-breathe rounded-capsule bg-warn" />
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
        'inline-block h-2 w-2 shrink-0 rounded-capsule transition-colors duration-[var(--duration-base)]',
        connected ? 'bg-ok shadow-[0_0_6px_color-mix(in_srgb,var(--color-ok)_60%,transparent)]' : 'bg-ink-600',
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
        'transition-colors duration-[var(--duration-quick)]',
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
    <div className="flex h-full animate-rise-in flex-col items-center justify-center gap-1.5 px-6 text-center">
      <p className="text-[13px] font-medium text-ink-300">{title}</p>
      {hint && <p className="max-w-xs text-[11px] leading-relaxed text-ink-500">{hint}</p>}
    </div>
  )
}
