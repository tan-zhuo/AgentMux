import clsx from 'clsx'
import type { FocusEvent, PointerEvent as ReactPointerEvent, ReactElement, Ref } from 'react'
import { cloneElement, useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { usePresence } from '../lib/motion'
import { useTouchDevice } from '../lib/useCompact'

/**
 * The label that appears when the pointer rests on a control.
 *
 * Nearly every icon button here used to lean on the `title` attribute, which
 * hands the job to the platform. Under WebView2 that is a white system box
 * with a hard edge, in the system font, a second late — a visitor from another
 * operating system sitting on top of the glass. This draws the same words as a
 * small floating slab of material, in the app's type and on the app's clock,
 * and takes the text over as the control's accessible name, so a screen reader
 * loses nothing by the swap.
 *
 * It wraps exactly one element and attaches to it rather than around it. A
 * wrapper would be one more box in every toolbar's flex row, with ideas of its
 * own about height and alignment. The child keeps its handlers and its ref;
 * ours run after them.
 *
 * The timing is that of a macOS help tag. A label waits half a second, so a
 * pointer crossing a toolbar does not light up every button it passes — but
 * once one label has shown, the next appears at once, because by then the
 * person is plainly reading labels, and making them wait again for each is
 * what makes tooltips feel slow. That hand-off is one shared timestamp: the
 * moment any tooltip last hid.
 *
 * Nothing shows on a touch device. There is no pointer to rest, and a label
 * on a long press would fight the context menu for the same gesture.
 */

/** How long the pointer rests on a control before its label appears. */
const SHOW_MS = 500
/** Within this long of one label hiding, the next one skips the wait. */
const HANDOFF_MS = 300
/** How long the exit is given to play: the length of `animate-pop-out`. */
const EXIT_MS = 120
/** The space between a control and its label. */
const GAP = 6
/** The nearest a label comes to the edge of the window. */
const EDGE = 8

/**
 * When any tooltip last hid. Shared by every instance on purpose: it is what
 * lets the label of one button hand off to the next without a pause.
 */
let lastHiddenAt = 0

/** The props this reads from, and adds to, the element it wraps. */
type AnchorProps = {
  ref?: Ref<HTMLElement>
  children?: unknown
  'aria-label'?: string
  'aria-labelledby'?: string
  'aria-describedby'?: string
  onPointerEnter?: (e: ReactPointerEvent<HTMLElement>) => void
  onPointerLeave?: (e: ReactPointerEvent<HTMLElement>) => void
  onPointerDown?: (e: ReactPointerEvent<HTMLElement>) => void
  onFocus?: (e: FocusEvent<HTMLElement>) => void
  onBlur?: (e: FocusEvent<HTMLElement>) => void
}

/**
 * Whether an element's children include words of their own. A button with a
 * word on it already has a name, and the label describes it; an icon-only
 * button has no name until the label gives it one. This looks at the JSX, not
 * the DOM, so it is settled before the first render — and text buried inside
 * a nested element is missed, which is the trade accepted for that.
 */
function hasText(children: unknown): boolean {
  if (typeof children === 'string') return children.trim() !== ''
  if (typeof children === 'number') return true
  if (Array.isArray(children)) return children.some(hasText)
  return false
}

/** Runs the element's own handler first, then ours. */
function chain<E>(theirs: ((e: E) => void) | undefined, ours: (e: E) => void): (e: E) => void {
  return (e) => {
    theirs?.(e)
    ours(e)
  }
}

/**
 * Whether focus arrived in a way the browser would draw a ring for: the
 * keyboard, in practice. A click focuses a button too, and showing its label
 * on every click would be a nag. A webview too old for the selector shows the
 * label on any focus, which is the harmless side to fail on.
 */
function focusVisible(el: Element): boolean {
  try {
    return el.matches(':focus-visible')
  } catch {
    return true
  }
}

type Placement = { left: number; top: number; origin: string }

export function Tooltip({
  label,
  side = 'top',
  children,
}: {
  /** What the label says. With nothing to say, the child is returned as it was. */
  label?: string
  /** The side of the control the label prefers. It flips when there is no room. */
  side?: 'top' | 'bottom'
  children: ReactElement
}) {
  const touch = useTouchDevice()
  const id = useId()
  const anchorRef = useRef<HTMLElement | null>(null)
  const tipRef = useRef<HTMLDivElement | null>(null)
  const timer = useRef(0)
  const [open, setOpen] = useState(false)
  // Held for the exit, so the label can shrink away rather than vanish.
  const { value: held, exiting } = usePresence(open ? true : null, EXIT_MS)
  const [place, setPlace] = useState<Placement | null>(null)

  const props = children.props as AnchorProps
  const childRef = props.ref
  const setAnchor = useCallback(
    (el: HTMLElement | null) => {
      anchorRef.current = el
      if (typeof childRef === 'function') childRef(el)
      else if (childRef) childRef.current = el
    },
    [childRef],
  )

  const cancel = () => window.clearTimeout(timer.current)
  useEffect(() => cancel, [])

  // A touch pointer is ignored even on a machine that has a mouse as well: a
  // finger that lands on a control is not resting on it.
  const onPointerEnter = (e: ReactPointerEvent<HTMLElement>) => {
    if (e.pointerType === 'touch') return
    cancel()
    if (Date.now() - lastHiddenAt < HANDOFF_MS) setOpen(true)
    else timer.current = window.setTimeout(() => setOpen(true), SHOW_MS)
  }
  const onPointerLeave = () => {
    cancel()
    if (open) lastHiddenAt = Date.now()
    setOpen(false)
  }
  // A press hides the label without starting a hand-off: someone who is
  // clicking has stopped reading.
  const onPointerDown = () => {
    cancel()
    setOpen(false)
  }
  // Keyboard focus shows the label at once; there is no pointer to rest.
  const onFocus = (e: FocusEvent<HTMLElement>) => {
    if (!focusVisible(e.currentTarget)) return
    cancel()
    setOpen(true)
  }
  const onBlur = () => {
    cancel()
    setOpen(false)
  }

  useEffect(() => {
    if (!open) return
    const close = () => setOpen(false)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    // A scroll that carries the control away takes its label along. Scrolls
    // elsewhere are none of its business — a terminal streaming output scrolls
    // its own viewport many times a second — so this checks that the control
    // sits inside whatever scrolled. Scroll events do not bubble, hence the
    // capturing listener.
    const onScroll = (e: Event) => {
      const anchor = anchorRef.current
      if (anchor && e.target instanceof Node && e.target.contains(anchor)) close()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', close)
    window.addEventListener('blur', close)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', close)
      window.removeEventListener('blur', close)
    }
  }, [open])

  // Where the label goes depends on how big it turned out, so it is measured
  // once it is in the page. A layout effect runs before anything is painted,
  // so it is never seen in the wrong place. The position is kept through the
  // exit, so the label shrinks away from where it was rather than jumping.
  useLayoutEffect(() => {
    if (!held) {
      setPlace(null)
      return
    }
    if (!open) return
    const anchor = anchorRef.current
    const tip = tipRef.current
    if (!anchor || !tip) return
    const a = anchor.getBoundingClientRect()
    const w = tip.offsetWidth
    const h = tip.offsetHeight
    const fitsAbove = a.top - GAP - h >= EDGE
    const fitsBelow = a.bottom + GAP + h <= window.innerHeight - EDGE
    const above = side === 'top' ? fitsAbove || !fitsBelow : !fitsBelow && fitsAbove
    const centre = a.left + a.width / 2
    const left = Math.max(EDGE, Math.min(centre - w / 2, window.innerWidth - w - EDGE))
    setPlace({
      left,
      top: above ? a.top - GAP - h : a.bottom + GAP,
      // It grows out of the control rather than out of its own centre, which
      // is what keeps it tied to the thing it names once it has been pushed
      // sideways to stay on screen.
      origin: `${centre - left}px ${above ? '100%' : '0%'}`,
    })
  }, [held, open, label, side])

  if (!label) return children

  // The label becomes the control's name where it had none, and its
  // description where it had one — a button that says "Save" should not be
  // renamed by a hint about what saving does.
  const named =
    props['aria-label'] != null || props['aria-labelledby'] != null || hasText(props.children)
  const anchor = cloneElement(children, {
    ref: setAnchor,
    'aria-label': named ? props['aria-label'] : label,
    'aria-describedby':
      named && held && props['aria-describedby'] == null ? id : props['aria-describedby'],
    ...(touch
      ? {}
      : {
          onPointerEnter: chain(props.onPointerEnter, onPointerEnter),
          onPointerLeave: chain(props.onPointerLeave, onPointerLeave),
          onPointerDown: chain(props.onPointerDown, onPointerDown),
          onFocus: chain(props.onFocus, onFocus),
          onBlur: chain(props.onBlur, onBlur),
        }),
  } as AnchorProps)

  return (
    <>
      {anchor}
      {held &&
        !touch &&
        createPortal(
          <div
            ref={tipRef}
            id={id}
            role="tooltip"
            style={
              place
                ? { left: place.left, top: place.top, transformOrigin: place.origin }
                : { visibility: 'hidden' }
            }
            className={clsx(
              'material pointer-events-none fixed z-100 max-w-[280px] rounded-card px-2 py-1',
              'text-[11px] leading-snug break-words text-ink-100 shadow-pop',
              exiting ? 'animate-pop-out' : 'animate-pop-in',
            )}
          >
            {label}
          </div>,
          document.body,
        )}
    </>
  )
}
