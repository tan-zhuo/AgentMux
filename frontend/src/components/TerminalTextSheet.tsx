import clsx from 'clsx'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { isDesktop } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { usePresence } from '../lib/motion'
import type { TextScope } from '../lib/termText'
import { useAppStore } from '../store/useAppStore'
import { useT } from '../store/useI18n'
import { useTermKeys, type TextSource } from '../store/useTermKeys'
import { Segmented } from './ui'

/**
 * The terminal's text, as text.
 *
 * xterm paints its rows on a canvas and marks them `user-select: none`, so on
 * a phone there is none of the apparatus a person reaches for when copying
 * out of anything else: no long press that picks a word, no handles to drag,
 * no magnifier, no system Copy or Share. Select mode gets round that by
 * turning a finger into a mouse, and a mouse is a poor thing to hand a
 * finger — the selection is fixed the moment it lifts, and the next touch
 * starts over. This sheet puts the same rows into a <pre> where all of that
 * works as it does everywhere else, and gets out of the way once something
 * has been copied.
 *
 * Where the text comes from is the pane's business — see `KeySink.textOf`.
 * The sheet knows two scopes: the screen, which is instant, and the history,
 * which for a tmux pane is a round trip to the server.
 */
export function TerminalTextSheet() {
  const t = useT()
  const live = useTermKeys((s) => s.textView)
  const close = useTermKeys((s) => s.closeTextView)
  const toast = useAppStore((s) => s.toast)
  // Held through the exit so the sheet can slide back down the way it came.
  const { value: source, exiting } = usePresence(live)
  const preRef = useRef<HTMLPreElement | null>(null)

  // The scope is remembered against the source it was picked for, so every
  // opening starts on the screen — it is what the person was just looking at,
  // and it costs nothing to show — while a sheet on its way out keeps the
  // scope it had rather than flicking back for its last frames.
  const [picked, setPicked] = useState<{ of: TextSource; scope: TextScope } | null>(null)
  const scope: TextScope = picked && picked.of === source ? picked.scope : 'screen'
  const pickScope = (next: TextScope) => source && setPicked({ of: source, scope: next })

  // What has been read, tagged with where and what for: a stale answer for
  // another scope, or another terminal, shows as loading rather than as the
  // wrong text for a frame.
  const [loaded, setLoaded] = useState<{ of: TextSource; scope: TextScope; text: string } | null>(
    null,
  )
  const ready = loaded && loaded.of === source && loaded.scope === scope ? loaded : null
  const text = ready?.text ?? ''

  useEffect(() => {
    if (!live) return
    let stale = false
    live
      .read(scope)
      .catch(() => '')
      .then((got) => {
        if (!stale) setLoaded({ of: live, scope, text: got })
      })
    return () => {
      stale = true
    }
  }, [live, scope])

  // The newest output is at the end, and it is what was on screen: start there.
  useLayoutEffect(() => {
    const pre = preRef.current
    if (pre) pre.scrollTop = pre.scrollHeight
  }, [ready])

  // Focus moves into the sheet so the keyboard's arrows scroll it, and so the
  // terminal behind it stops being where keystrokes go.
  useEffect(() => {
    if (source && !exiting) preRef.current?.focus({ preventScroll: true })
  }, [source, exiting])

  useEffect(() => {
    if (!source || exiting) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault()
      close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [source, exiting, close])

  // Called straight from the release and nothing later: on a plain-http
  // address the clipboard is execCommand, which only works inside the gesture
  // that asked for it. Closing on success is the same bargain select mode
  // makes — the sheet was opened for this copy, and it has happened.
  const copyAll = () => {
    if (!text) return
    void copyText(text).then((ok) => {
      if (!ok) {
        toast('error', t('term.copyFailed'))
        return
      }
      toast('ok', t('term.copied', { n: text.length }))
      close()
    })
  }

  // The system share sheet, which is where a phone sends text to anything
  // else. The desktop app's webview has no such sheet to show, so it is not
  // offered one.
  const canShare = !isDesktop && typeof navigator.share === 'function'
  const share = () => {
    void navigator.share({ text }).catch(() => {
      /* dismissed, or nothing on this device would take it */
    })
  }

  if (!source) return null

  return (
    <div
      role="presentation"
      className={clsx(
        'fixed inset-0 z-60 flex flex-col items-center justify-end bg-black/35 backdrop-blur-[3px]',
        // Inert while it fades, so the first tap after a dismissal lands on
        // the terminal and not on a scrim that is still leaving.
        exiting ? 'animate-fade-out pointer-events-none' : 'animate-fade-in',
      )}
      // Dismissed by a press that lands on the scrim itself — not by a click,
      // which would also fire when a selection dragged out of the text was
      // let go over the scrim. That is a selection, not a dismissal.
      onPointerDown={(e) => {
        if (e.target === e.currentTarget && !exiting) close()
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('term.textView')}
        className={clsx(
          // Most of the screen, but not all of it: the strip of terminal left
          // showing above is what says this is a sheet over the terminal and
          // not a page that replaced it. On a wide window it floats instead,
          // capped at a readable width and clear of the bottom edge.
          'material flex h-[80dvh] w-full max-w-3xl flex-col overflow-hidden rounded-t-sheet shadow-sheet',
          'sm:mb-6 sm:rounded-sheet',
          exiting ? 'animate-drawer-out-bottom pointer-events-none' : 'animate-drawer-in-bottom',
        )}
      >
        <header className="flex h-12 shrink-0 items-center justify-center border-b hairline px-4">
          <Segmented
            value={scope}
            onChange={pickScope}
            options={[
              { value: 'screen', label: t('term.text.screen') },
              { value: 'history', label: t('term.text.history') },
            ]}
          />
        </header>
        {/* Selectable, and wrapped rather than scrolled sideways: a soft wrap
            is not copied, so the text comes out as it was, and everything in
            a line is reachable without a second axis of scrolling under a
            selection handle. */}
        <pre
          ref={preRef}
          tabIndex={0}
          className={clsx(
            'min-h-0 flex-1 overflow-y-auto px-4 py-3 outline-none select-text',
            'font-mono text-[12.5px] leading-[1.45] whitespace-pre-wrap [overflow-wrap:anywhere] text-ink-200',
          )}
        >
          {text || (
            <span className="text-ink-500">{ready ? t('term.text.empty') : t('common.loading')}</span>
          )}
        </pre>
        <footer
          className="flex shrink-0 items-center justify-end gap-2 border-t hairline px-4 pt-3"
          style={{ paddingBottom: 'max(0.75rem, env(safe-area-inset-bottom))' }}
        >
          <SheetButton label={t('common.close')} onPress={close} />
          {canShare && <SheetButton label={t('term.text.share')} onPress={share} disabled={!text} />}
          <SheetButton label={t('term.text.copyAll')} onPress={copyAll} primary disabled={!text} />
        </footer>
      </div>
    </div>
  )
}

/**
 * A footer button. Like the key bar's, it cancels the press and acts on the
 * release: the copy has to begin inside the gesture, and this is the one
 * handler that is both inside it and after the finger has committed.
 */
function SheetButton({
  label,
  onPress,
  primary,
  disabled,
}: {
  label: string
  onPress: () => void
  primary?: boolean
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      className={clsx(
        'pressable h-9 shrink-0 rounded-control px-3.5 text-[12.5px] leading-none font-medium select-none touch-manipulation',
        'disabled:pointer-events-none disabled:opacity-40',
        primary
          ? 'bg-accent text-white active:brightness-95 ' +
              'shadow-[inset_0_1px_0_rgb(255_255_255/0.22),0_1px_2px_rgb(0_0_0/0.2)]'
          : 'bg-ink-800 text-ink-200 active:bg-ink-700',
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
