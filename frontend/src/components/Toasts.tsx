import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { usePresenceList } from '../lib/motion'
import { useAppStore, type Toast } from '../store/useAppStore'
import { useT } from '../store/useI18n'
import { iconButtonClass } from './ui'
import type { MsgKey } from '../lib/i18n'

const toneStyle: Record<
  Toast['tone'],
  { icon: typeof Info; accent: string; tint: string; label: MsgKey }
> = {
  ok: { icon: CheckCircle2, accent: 'text-ok', tint: 'bg-ok/12', label: 'toast.done' },
  error: { icon: XCircle, accent: 'text-danger', tint: 'bg-danger/12', label: 'toast.failed' },
  warn: { icon: AlertTriangle, accent: 'text-warn', tint: 'bg-warn/12', label: 'toast.warning' },
  info: { icon: Info, accent: 'text-accent', tint: 'bg-accent/12', label: 'toast.note' },
}

/**
 * Notifications sit above the status bar, right-aligned.
 *
 * Each one slides in from the edge the way a macOS notification does and
 * slips back out when it is dismissed, rather than popping in and blinking
 * away. The body is on its own line rather than inline with the icon so long
 * messages — a remote error, a path — wrap into a readable block instead of a
 * narrow ragged column beside the glyph.
 */
export function Toasts() {
  const t = useT()
  const toasts = useAppStore((s) => s.toasts)
  const dismiss = useAppStore((s) => s.dismissToast)
  const shown = usePresenceList(toasts, (toast) => toast.id)

  if (!shown.length) return null
  return (
    <div className="pointer-events-none fixed right-4 bottom-10 z-70 flex w-[22rem] flex-col gap-2">
      {shown.map(({ item: toast, exiting }) => {
        const style = toneStyle[toast.tone]
        const Icon = style.icon
        return (
          <div
            key={toast.id}
            role="status"
            className={clsx(
              'material overflow-hidden rounded-card shadow-sheet',
              exiting ? 'animate-toast-out pointer-events-none' : 'animate-toast-in pointer-events-auto',
            )}
          >
            <div className="flex items-start gap-2.5 px-3 py-2.5">
              {/* The glyph sits in a tinted tile, the way an app icon sits in
                  a notification — it gives the eye a place to land before
                  the text. */}
              <span
                className={clsx(
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-[7px]',
                  style.tint,
                  style.accent,
                )}
              >
                <Icon size={14} />
              </span>
              <div className="min-w-0 flex-1">
                <p className={clsx('text-[11px] font-semibold', style.accent)}>
                  {t(style.label)}
                </p>
                <p className="mt-0.5 text-xs leading-relaxed break-words text-ink-200">{toast.text}</p>
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
