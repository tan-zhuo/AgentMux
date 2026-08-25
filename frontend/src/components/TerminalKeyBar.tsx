import clsx from 'clsx'
import { ChevronDown, ChevronUp, Keyboard, KeyboardOff, TextSelect } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { EXTRA_KEYS, PRIMARY_KEYS, keyBytes, type KeyCap } from '../lib/termKeys'
import {
  blurTerminal,
  copyTerminalSelection,
  currentMods,
  focusTerminal,
  selectAllInTerminal,
  sendKeys,
  useTermKeys,
  type ModState,
} from '../store/useTermKeys'
import { useT } from '../store/useI18n'

/** Held down, a key waits this long before repeating, then repeats this often. */
const REPEAT_DELAY = 400
const REPEAT_EVERY = 55
/** Past this, a press is a hold: the modifier locks instead of arming once. */
const HOLD = 450

/**
 * The row of keys a terminal needs and a software keyboard does not have.
 *
 * It sits directly above the keyboard, which is the only place it can be: these
 * keys are used between letters, so any journey to reach them is a journey made
 * on every command. Presses go straight to the PTY rather than through xterm,
 * and every one of them cancels its own default so focus never leaves the
 * terminal — a bar that closed the keyboard each time it was used would be
 * worse than no bar.
 */
export function TerminalKeyBar() {
  const t = useT()
  const hidden = useTermKeys((s) => s.hidden)
  const expanded = useTermKeys((s) => s.expanded)
  const selecting = useTermKeys((s) => s.selecting)
  const setHidden = useTermKeys((s) => s.setHidden)
  const toggleExpanded = useTermKeys((s) => s.toggleExpanded)
  const setSelecting = useTermKeys((s) => s.setSelecting)

  if (hidden) {
    return (
      <button
        type="button"
        aria-label={t('keys.show')}
        onPointerDown={(e) => {
          e.preventDefault()
          setHidden(false)
        }}
        className="flex h-4 shrink-0 items-center justify-center border-t hairline bg-ink-850 text-ink-500"
      >
        <ChevronUp size={12} />
      </button>
    )
  }

  return (
    <div className="shrink-0 border-t hairline bg-ink-850">
      {expanded && (
        <div className="max-h-[32vh] overflow-y-auto border-b hairline px-1.5 py-1.5">
          <div className="flex flex-wrap gap-1">
            {EXTRA_KEYS.map((cap) => (
              <Cap key={cap.id} cap={cap} />
            ))}
          </div>
          <button
            type="button"
            onPointerDown={(e) => {
              e.preventDefault()
              setHidden(true)
            }}
            className="mt-1.5 h-7 w-full rounded-control text-[11px] text-ink-400 active:bg-ink-800"
          >
            {t('keys.hide')}
          </button>
        </div>
      )}

      <div
        className="flex items-stretch gap-[2px] px-1.5 py-1.5"
        style={{ paddingBottom: 'max(0.375rem, env(safe-area-inset-bottom))' }}
      >
        {/* The keys themselves scroll; the two controls stay put, because a
            control that scrolls out of reach is one that is not there.

            In select mode the keys give their place to what select mode is
            for. A strip over the terminal would have been simpler and would
            have covered the top line of the very output being copied. */}
        {selecting ? (
          <div className="flex min-w-0 flex-1 items-center gap-1.5 pl-1">
            <span className="min-w-0 flex-1 truncate text-[11px] text-ink-400">
              {t('term.select.hint')}
            </span>
            <TextButton label={t('term.selectAll')} onPress={selectAllInTerminal} />
            <TextButton label={t('term.copy')} onPress={copyTerminalSelection} primary />
          </div>
        ) : (
          <div className="no-scrollbar flex flex-1 gap-[2px] overflow-x-auto">
            {PRIMARY_KEYS.map((cap) => (
              <Cap key={cap.id} cap={cap} />
            ))}
          </div>
        )}
        {/* Copying out of a terminal is a two-handed, mouse-shaped act
            everywhere else; on a phone it needs a switch of its own. */}
        <Control
          label={<TextSelect size={16} />}
          title={selecting ? t('term.select.done') : t('keys.select')}
          active={selecting}
          onPress={() => setSelecting(!selecting)}
        />
        <SoftKeyboardToggle />
        <Control
          label={expanded ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
          title={expanded ? t('keys.less') : t('keys.more')}
          active={expanded}
          onPress={toggleExpanded}
        />
      </div>
    </div>
  )
}

/**
 * Show or hide the software keyboard.
 *
 * With it down the terminal is twice as tall and this bar still drives it —
 * which is most of watching an agent work, as opposed to typing at it.
 */
function SoftKeyboardToggle() {
  const t = useT()
  const [up, setUp] = useState(true)

  // The keyboard can also go away on its own — the back gesture, a tap
  // elsewhere. Trust the viewport over our own last instruction.
  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const read = () => setUp(window.innerHeight - vv.height - vv.offsetTop > 120)
    vv.addEventListener('resize', read)
    return () => vv.removeEventListener('resize', read)
  }, [])

  return (
    <Control
      label={up ? <KeyboardOff size={16} /> : <Keyboard size={16} />}
      title={up ? t('keys.keyboardHide') : t('keys.keyboardShow')}
      onPress={() => {
        if (up) blurTerminal()
        else focusTerminal()
        setUp(!up)
      }}
      // Focusing the textarea has to happen inside the gesture for a phone to
      // raise its keyboard, so this one acts on pointerdown like the keys do.
      immediate
    />
  )
}

function Cap({ cap }: { cap: KeyCap }) {
  const t = useT()
  const state = useTermKeys((s) => (cap.action.kind === 'mod' ? s[cap.action.mod] : 'off'))
  const tap = useTermKeys((s) => s.tap)
  const lock = useTermKeys((s) => s.lock)
  // A ref, not a local: locking re-renders this component between the press
  // and the release, and a fresh closure would have forgotten the hold.
  const press = useRef<{ hold?: number; delay?: number; every?: number; held?: boolean }>({})

  function clearTimers() {
    const p = press.current
    window.clearTimeout(p.hold)
    window.clearTimeout(p.delay)
    window.clearInterval(p.every)
    press.current = {}
  }

  useEffect(() => clearTimers, [])

  if (cap.action.kind === 'mod') {
    const mod = cap.action.mod
    return (
      <KeyButton
        cap={cap}
        state={state}
        title={t('keys.modHint')}
        onPointerDown={(e) => {
          e.preventDefault()
          press.current.held = false
          press.current.hold = window.setTimeout(() => {
            press.current.held = true
            lock(mod)
          }, HOLD)
        }}
        onPointerUp={(e) => {
          e.preventDefault()
          const held = press.current.held
          clearTimers()
          if (!held) tap(mod)
        }}
        onPointerCancel={clearTimers}
      />
    )
  }

  // A key press is resolved once, at the moment of the press, and repeated
  // verbatim: an armed modifier belongs to the whole hold, not only its first
  // stroke, or holding Ctrl-← would walk one word and then a hundred letters.
  const strike = () => {
    const bytes = keyBytes(cap, currentMods())
    sendKeys(bytes)
    if (!cap.repeat) return
    press.current.delay = window.setTimeout(() => {
      press.current.every = window.setInterval(() => sendKeys(bytes), REPEAT_EVERY)
    }, REPEAT_DELAY)
  }

  return (
    <KeyButton
      cap={cap}
      state="off"
      onPointerDown={(e) => {
        e.preventDefault()
        strike()
      }}
      onPointerUp={clearTimers}
      onPointerCancel={clearTimers}
      onPointerLeave={clearTimers}
    />
  )
}

function KeyButton({
  cap,
  state,
  title,
  ...handlers
}: {
  cap: KeyCap
  state: ModState
  title?: string
} & React.ComponentProps<'button'>) {
  return (
    <button
      type="button"
      title={title}
      // Touch scrolling of the row must not be mistaken for a key press, and a
      // key press must not become a text selection or a magnifier.
      className={clsx(
        'h-9 shrink-0 rounded-control font-mono text-[12.5px] leading-none select-none',
        'touch-manipulation transition-[background-color] duration-75',
        cap.wide
          ? 'min-w-[2.625rem] px-1'
          : cap.narrow
            ? 'min-w-[1.75rem] px-0.5'
            : 'min-w-[1.875rem] px-1',
        state === 'off'
          ? 'bg-ink-750 text-ink-200 active:bg-ink-700'
          : 'bg-accent text-white',
        // Locked reads as "still on after this key" — a ring, because a second
        // colour for a second degree of the same thing is one colour too many.
        state === 'lock' && 'ring-2 ring-accent/45 ring-offset-1 ring-offset-ink-850',
      )}
      {...handlers}
    >
      {cap.label}
    </button>
  )
}

/**
 * A labelled button in the bar. Like the keys, it acts on the release and
 * cancels its default, so the terminal keeps focus and the keyboard — if it is
 * up — stays where it is.
 */
function TextButton({
  label,
  onPress,
  primary,
}: {
  label: string
  onPress: () => void
  primary?: boolean
}) {
  return (
    <button
      type="button"
      className={clsx(
        'h-9 shrink-0 rounded-control px-2.5 text-[12px] leading-none select-none touch-manipulation',
        primary ? 'bg-accent text-white' : 'bg-ink-750 text-ink-200 active:bg-ink-700',
      )}
      onPointerDown={(e) => e.preventDefault()}
      onPointerUp={(e) => {
        e.preventDefault()
        onPress()
      }}
    >
      {label}
    </button>
  )
}

function Control({
  label,
  title,
  active,
  onPress,
  immediate,
}: {
  label: React.ReactNode
  title: string
  active?: boolean
  onPress: () => void
  immediate?: boolean
}) {
  const act = (e: React.PointerEvent) => {
    e.preventDefault()
    onPress()
  }
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      className={clsx(
        'flex h-9 w-7 shrink-0 items-center justify-center rounded-control',
        'touch-manipulation select-none',
        active ? 'bg-ink-700 text-ink-100' : 'bg-ink-800 text-ink-400 active:bg-ink-700',
      )}
      onPointerDown={immediate ? act : (e) => e.preventDefault()}
      onPointerUp={immediate ? undefined : act}
    >
      {label}
    </button>
  )
}
