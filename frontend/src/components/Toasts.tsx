import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { useAppStore, type Toast } from '../store/useAppStore'
import { useT } from '../store/useI18n'
import { iconButtonClass } from './ui'
import type { MsgKey } from '../lib/i18n'

const toneStyle: Record<
  Toast['tone'],
  { icon: typeof Info; accent: string; label: MsgKey }
> = {
  ok: { icon: CheckCircle2, accent: 'text-ok', label: 'toast.done' },
  error: { icon: XCircle, accent: 'text-danger', label: 'toast.failed' },
  warn: { icon: AlertTriangle, accent: 'text-warn', label: 'toast.warning' },
  info: { icon: Info, accent: 'text-accent', label: 'toast.note' },
}

/**
 * Notifications sit above the status bar, right-aligned.
 *
 * The body is on its own line rather than inline with the icon so long
 * messages — a remote error, a path — wrap into a readable block instead of a
 * narrow ragged column beside the glyph.
 */
export function Toasts() {
  const t = useT()
  const toasts = useAppStore((s) => s.toasts)
  const dismiss = useAppStore((s) => s.dismissToast)

  if (!toasts.length) return null
  return (
    <div className="pointer-events-none fixed right-4 bottom-10 z-70 flex w-[22rem] flex-col gap-2">
      {toasts.map((toast) => {
        const style = toneStyle[toast.tone]
        const Icon = style.icon
        return (
          <div
            key={toast.id}
            role="status"
            className={clsx(
              'material pointer-events-auto overflow-hidden rounded-card shadow-sheet',
            )}
          >
            <div className="flex items-start gap-2.5 px-3 py-2.5">
              <Icon size={14} className={clsx('mt-px shrink-0', style.accent)} />
              <div className="min-w-0 flex-1">
                <p className={clsx('text-[11px] font-semibold', style.accent)}>
                  {t(style.label)}
                </p>
                <p className="mt-1 text-xs leading-relaxed break-words text-ink-200">{toast.text}</p>
              </div>
              <button
                onClick={() => dismiss(toast.id)}
                aria-label={t('toast.dismiss')}
                className={clsx(iconButtonClass, '-mr-1 text-ink-500 hover:bg-ink-800 hover:text-ink-100')}
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
