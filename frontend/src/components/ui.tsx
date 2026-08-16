import clsx from 'clsx'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useEffect } from 'react'
import type { AgentStatus } from '../lib/types'

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
        'inline-flex items-center gap-1.5 rounded-md border font-medium transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-40',
        size === 'sm' ? 'px-2 py-1 text-[11px]' : 'px-2.5 py-1.5 text-xs',
        variant === 'primary' &&
          'border-accent-dim bg-accent/15 text-accent hover:bg-accent/25 hover:border-accent',
        variant === 'ghost' &&
          'border-ink-700 bg-ink-800 text-ink-200 hover:bg-ink-750 hover:text-ink-100',
        variant === 'subtle' &&
          'border-transparent bg-transparent text-ink-300 hover:bg-ink-800 hover:text-ink-100',
        variant === 'danger' &&
          'border-danger/40 bg-danger/10 text-danger hover:bg-danger/20 hover:border-danger/70',
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
      <span className="mb-1 block text-[11px] font-medium tracking-wide text-ink-300 uppercase">
        {label}
      </span>
      {children}
      {hint && <span className="mt-1 block text-[11px] text-ink-400">{hint}</span>}
    </label>
  )
}

export const inputClass =
  'w-full rounded-md border border-ink-700 bg-ink-850 px-2.5 py-1.5 text-xs text-ink-100 ' +
  'placeholder:text-ink-500 outline-none focus:border-accent focus:ring-1 focus:ring-accent/40'

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
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-10 backdrop-blur-sm">
      <div
        className={clsx(
          'flex max-h-full w-full flex-col overflow-hidden rounded-xl border border-ink-700 bg-ink-850 shadow-2xl',
          wide ? 'max-w-3xl' : 'max-w-lg',
        )}
      >
        <header className="flex items-center justify-between border-b border-ink-750 px-4 py-3">
          <h2 className="text-sm font-semibold text-ink-100">{title}</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
          >
            <X size={15} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto px-4 py-4">{children}</div>
        {footer && (
          <footer className="flex items-center justify-end gap-2 border-t border-ink-750 px-4 py-3">
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
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-ok opacity-60" />
      )}
      <span className={clsx('relative inline-flex h-2 w-2 rounded-full', statusColor[status])} />
    </span>
  )
}

export function ConnDot({ connected }: { connected: boolean }) {
  return (
    <span
      className={clsx('inline-block h-2 w-2 shrink-0 rounded-full', connected ? 'bg-ok' : 'bg-ink-600')}
    />
  )
}

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
        'rounded px-1.5 py-0.5 text-[10px] font-medium tracking-wide',
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
    <div className="flex h-full flex-col items-center justify-center gap-1 px-6 text-center">
      <p className="text-xs font-medium text-ink-300">{title}</p>
      {hint && <p className="max-w-xs text-[11px] leading-relaxed text-ink-500">{hint}</p>}
    </div>
  )
}
