import clsx from 'clsx'
import { X } from 'lucide-react'
import { useAppStore } from '../store/useAppStore'

export function Toasts() {
  const toasts = useAppStore((s) => s.toasts)
  const dismiss = useAppStore((s) => s.dismissToast)

  if (!toasts.length) return null
  return (
    <div className="pointer-events-none fixed right-4 bottom-10 z-70 flex w-80 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={clsx(
            'pointer-events-auto flex items-start gap-2 rounded-lg border px-3 py-2 text-[11px] leading-relaxed shadow-lg backdrop-blur',
            t.tone === 'error' && 'border-danger/40 bg-danger/12 text-danger',
            t.tone === 'warn' && 'border-warn/40 bg-warn/12 text-warn',
            t.tone === 'ok' && 'border-ok/40 bg-ok/12 text-ok',
            t.tone === 'info' && 'border-ink-700 bg-ink-800/95 text-ink-200',
          )}
        >
          <span className="min-w-0 flex-1 break-words">{t.text}</span>
          <button onClick={() => dismiss(t.id)} className="shrink-0 opacity-60 hover:opacity-100">
            <X size={12} />
          </button>
        </div>
      ))}
    </div>
  )
}
