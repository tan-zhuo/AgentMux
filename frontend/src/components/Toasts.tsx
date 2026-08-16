import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { useAppStore, type Toast } from '../store/useAppStore'

const toneStyle: Record<
  Toast['tone'],
  { icon: typeof Info; border: string; accent: string; label: string }
> = {
  ok: { icon: CheckCircle2, border: 'border-ok/35', accent: 'text-ok', label: 'Done' },
  error: { icon: XCircle, border: 'border-danger/40', accent: 'text-danger', label: 'Failed' },
  warn: { icon: AlertTriangle, border: 'border-warn/40', accent: 'text-warn', label: 'Warning' },
  info: { icon: Info, border: 'border-ink-700', accent: 'text-accent', label: 'Note' },
}

/**
 * Notifications sit above the status bar, right-aligned.
 *
 * The body is on its own line rather than inline with the icon so long
 * messages — a remote error, a path — wrap into a readable block instead of a
 * narrow ragged column beside the glyph.
 */
export function Toasts() {
  const toasts = useAppStore((s) => s.toasts)
  const dismiss = useAppStore((s) => s.dismissToast)

  if (!toasts.length) return null
  return (
    <div className="pointer-events-none fixed right-4 bottom-10 z-70 flex w-[22rem] flex-col gap-2">
      {toasts.map((t) => {
        const style = toneStyle[t.tone]
        const Icon = style.icon
        return (
          <div
            key={t.id}
            role="status"
            className={clsx(
              'pointer-events-auto overflow-hidden rounded-lg border bg-ink-850/97 shadow-xl backdrop-blur',
              style.border,
            )}
          >
            <div className="flex items-start gap-2.5 px-3 py-2.5">
              <Icon size={14} className={clsx('mt-px shrink-0', style.accent)} />
              <div className="min-w-0 flex-1">
                <p className={clsx('text-[10px] font-semibold tracking-widest uppercase', style.accent)}>
                  {style.label}
                </p>
                <p className="mt-1 text-xs leading-relaxed break-words text-ink-200">{t.text}</p>
              </div>
              <button
                onClick={() => dismiss(t.id)}
                aria-label="Dismiss"
                className="-mr-1 shrink-0 rounded p-1 text-ink-500 hover:bg-ink-800 hover:text-ink-100"
              >
                <X size={12} />
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}
