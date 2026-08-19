import { ArrowUpCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { errText, isDesktop, on, update as updateApi } from '../lib/api'
import type { UpdateInfo, UpdateProgress } from '../lib/types'
import { useT } from '../store/useI18n'
import { Button } from './ui'

function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / Math.pow(1024, i)
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/**
 * The manual check, for the settings dialog. A found update raises the
 * banner (the backend announces every hit), so this only has to say whether
 * anything was found.
 */
export function UpdateCheckButton({ current }: { current: string }) {
  const t = useT()
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')

  const check = async () => {
    setBusy(true)
    setNote('')
    try {
      const info = await updateApi.check()
      if (info.error) setNote(t('update.checkFailed', { error: info.error }))
      else if (info.hasUpdate) setNote(t('update.found', { version: info.latestVersion }))
      else if (current === 'dev') setNote(t('update.devBuild'))
      else setNote(t('update.upToDate'))
    } catch (e) {
      setNote(t('update.checkFailed', { error: errText(e) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <span className="inline-flex items-center gap-2">
      <Button size="sm" variant="subtle" disabled={busy} onClick={() => void check()}>
        {busy ? t('update.checking') : t('update.check')}
      </Button>
      {note && <span className="font-sans text-[11px] text-ink-400">{note}</span>}
    </span>
  )
}

/**
 * A strip under the title bar that appears when a newer release exists.
 *
 * It stays out of the way — one line, dismissable for the session — until the
 * user clicks upgrade, at which point it narrates the download with a real
 * progress bar and ends with the app restarting itself. Errors land in the
 * same strip: the user who clicked is looking exactly here.
 */
// Self-update replaces the desktop binary; in serve mode the server binary is
// updated on the server, so the banner has nothing to offer a browser.
export function UpdateBanner() {
  if (!isDesktop) return null
  return <UpdateBannerInner />
}

function UpdateBannerInner() {
  const t = useT()
  const [info, setInfo] = useState<UpdateInfo | null>(null)
  const [progress, setProgress] = useState<UpdateProgress | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [dismissed, setDismissed] = useState('')

  useEffect(() => {
    const offAvail = on<UpdateInfo>('update:available', (u) => {
      if (u) setInfo(u)
    })
    const offProg = on<UpdateProgress>('update:progress', (p) => {
      if (p) setProgress(p)
    })
    return () => {
      offAvail()
      offProg()
    }
  }, [])

  if (!info || !info.hasUpdate || dismissed === info.latestVersion) return null

  const upgrade = async () => {
    setBusy(true)
    setError('')
    setProgress(null)
    try {
      const res = await updateApi.apply()
      if (!res.ok) {
        setError(res.error || t('update.failed.generic'))
        setBusy(false)
        setProgress(null)
      }
      // On ok the app restarts itself; the "restarting" phase event already
      // holds the strip in its final state until the window goes away.
    } catch (e) {
      setError(errText(e))
      setBusy(false)
      setProgress(null)
    }
  }

  const phase = progress?.phase
  const pct = Math.max(0, Math.min(100, progress?.percent ?? 0))

  return (
    <div
      className="flex items-center gap-3 border-b hairline px-3 py-1.5 text-[11px]"
      style={{ background: 'color-mix(in oklab, var(--color-accent) 12%, transparent)' }}
    >
      <ArrowUpCircle size={13} className="shrink-0 text-accent" />

      {!busy && !error && (
        <>
          <span className="min-w-0 truncate text-ink-200">
            {t('update.available', { latest: info.latestVersion, current: info.currentVersion })}
          </span>
          <span className="flex-1" />
          <Button size="sm" variant="subtle" onClick={() => void updateApi.openReleasePage()}>
            {t('update.notes')}
          </Button>
          <Button size="sm" variant="subtle" onClick={() => setDismissed(info.latestVersion)}>
            {t('update.later')}
          </Button>
          <Button size="sm" variant="primary" onClick={() => void upgrade()}>
            {t('update.upgrade')}
          </Button>
        </>
      )}

      {busy && (
        <>
          <span className="shrink-0 text-ink-200">
            {phase === 'install' && t('update.installing')}
            {phase === 'restart' && t('update.restarting')}
            {(!phase || phase === 'download') &&
              t('update.downloading', {
                done: bytes(progress?.doneBytes ?? 0),
                total: bytes(progress?.totalBytes ?? info.assetSize),
              })}
          </span>
          <div
            className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-capsule"
            style={{ background: 'color-mix(in oklab, var(--color-accent) 18%, transparent)' }}
          >
            <div
              className="h-full rounded-capsule bg-accent transition-[width] duration-300"
              style={{ width: `${phase === 'download' || !phase ? pct : 100}%` }}
            />
          </div>
          <span className="shrink-0 tabular-nums text-ink-300">
            {phase === 'download' || !phase ? `${pct.toFixed(0)}%` : ''}
          </span>
        </>
      )}

      {error && (
        <>
          <span className="min-w-0 flex-1 truncate text-danger" title={error}>
            {t('update.failed', { error })}
          </span>
          <Button size="sm" variant="subtle" onClick={() => void updateApi.openReleasePage()}>
            {t('update.notes')}
          </Button>
          <Button size="sm" variant="primary" onClick={() => void upgrade()}>
            {t('update.retry')}
          </Button>
        </>
      )}
    </div>
  )
}
