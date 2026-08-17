import clsx from 'clsx'
import { AlertTriangle, Info, ShieldAlert } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useConfirm, type ConfirmTone } from '../store/useConfirm'
import { Button, inputClass } from './ui'

const toneStyle: Record<
  ConfirmTone,
  { icon: typeof Info; ring: string; text: string; bg: string }
> = {
  danger: {
    icon: ShieldAlert,
    ring: 'border-danger/40',
    text: 'text-danger',
    bg: 'bg-danger/10',
  },
  warning: {
    icon: AlertTriangle,
    ring: 'border-warn/40',
    text: 'text-warn',
    bg: 'bg-warn/10',
  },
  info: {
    icon: Info,
    ring: 'border-accent-dim',
    text: 'text-accent',
    bg: 'bg-accent/10',
  },
}

/**
 * The application's confirmation prompt.
 *
 * It replaces window.confirm, which renders as an unstyled OS box with an "OK"
 * button that says nothing about what is being agreed to. Here the button names
 * the action, the consequences are listed, and the few operations that destroy
 * remote state can require the name to be typed.
 */
export function ConfirmDialog() {
  const request = useConfirm((s) => s.request)
  const settle = useConfirm((s) => s.settle)

  const [typed, setTyped] = useState('')
  const inputRef = useRef<HTMLInputElement | null>(null)

  const tone = request?.tone ?? 'danger'
  const style = toneStyle[tone]
  const Icon = style.icon
  const needsText = !!request?.requireText
  const ready = !needsText || typed.trim() === request?.requireText

  // Reset and focus whenever a new question is asked.
  useEffect(() => {
    if (!request) return
    setTyped('')
    const t = window.setTimeout(() => inputRef.current?.focus(), 30)
    return () => window.clearTimeout(t)
  }, [request])

  useEffect(() => {
    if (!request) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        settle(false)
      }
      if (e.key === 'Enter' && ready && !request.requireText) {
        e.preventDefault()
        settle(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [request, ready, settle])

  if (!request) return null

  return (
    <div
      className="fixed inset-0 z-80 flex items-center justify-center bg-black/60 p-10 backdrop-blur-sm"
      onClick={() => settle(false)}
      role="presentation"
    >
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        onClick={(e) => e.stopPropagation()}
        className="material max-h-[85vh] w-full max-w-md overflow-y-auto rounded-sheet shadow-sheet"
      >
        <div className="flex gap-3 px-4 pt-4">
          <span
            className={clsx(
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-card border',
              style.ring,
              style.bg,
              style.text,
            )}
          >
            <Icon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <h2 id="confirm-title" className="text-sm font-semibold text-ink-100">
              {request.title}
            </h2>
            <p className="mt-1 text-xs leading-relaxed text-ink-300">{request.message}</p>

            {request.points && request.points.length > 0 && (
              <ul className="mt-2.5 space-y-1">
                {request.points.map((p) => (
                  <li key={p} className="flex gap-2 text-[11px] leading-relaxed text-ink-400">
                    <span className={clsx('mt-[6px] h-1 w-1 shrink-0 rounded-capsule bg-current', style.text)} />
                    <span className="min-w-0">{p}</span>
                  </li>
                ))}
              </ul>
            )}

            {request.reassurance && (
              <p className="mt-2.5 rounded-control border hairline bg-ink-800 px-2.5 py-1.5 text-[11px] leading-relaxed text-ink-400">
                {request.reassurance}
              </p>
            )}

            {needsText && (
              <label className="mt-3 block">
                <span className="mb-1 block text-[11px] text-ink-400">
                  Type <span className="font-mono text-ink-200">{request.requireText}</span> to confirm
                </span>
                <input
                  ref={inputRef}
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && ready) {
                      e.preventDefault()
                      settle(true)
                    }
                  }}
                  className={clsx(inputClass, 'font-mono')}
                  placeholder={request.requireText}
                  autoComplete="off"
                  spellCheck={false}
                />
              </label>
            )}
          </div>
        </div>

        <div className="mt-4 flex items-center justify-end gap-2 border-t hairline bg-ink-900/60 px-4 py-3">
          <Button onClick={() => settle(false)}>{request.cancelLabel ?? 'Cancel'}</Button>
          <Button
            variant={tone === 'info' ? 'primary' : 'danger'}
            disabled={!ready}
            onClick={() => settle(true)}
          >
            {request.confirmLabel ?? 'Confirm'}
          </Button>
        </div>
      </div>
    </div>
  )
}
