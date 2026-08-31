import { ArrowUpCircle } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { errText, isDesktop, on, tree as treeApi, update as updateApi } from '../lib/api'
import type { UpdateInfo, UpdateProgress } from '../lib/types'
import { useT } from '../store/useI18n'
import { Button, inputClass } from './ui'

function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / Math.pow(1024, i)
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/**
 * The acceleration-mirror setting, for networks where GitHub is unreachable:
 * a proxy prefix that both the version check and the download travel through.
 * Saved on blur; the next check simply uses it.
 */
export function UpdateMirrorField() {
  const t = useT()
  const [value, setValue] = useState('')
  const loaded = useRef(false)

  useEffect(() => {
    treeApi
      .getSetting('update.mirror', '')
      .then((v) => {
        setValue(v)
        loaded.current = true
      })
      .catch(() => {})
  }, [])

  const save = () => {
    // Never write before the stored value has been read, or a quick open and
    // close of the dialog would blank a configured mirror.
    if (loaded.current) void treeApi.setSetting('update.mirror', value.trim()).catch(() => {})
  }

  return (
    <input
      className={inputClass}
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onBlur={save}
      placeholder={t('update.mirror.placeholder')}
      title={t('update.mirror.hint')}
      spellCheck={false}
    />
  )
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
  const [pageUrl, setPageUrl] = useState('')

  const check = async () => {
    setBusy(true)
    setNote('')
    setPageUrl('')
    try {
      const info = await updateApi.check()
      if (info.error) setNote(t('update.checkFailed', { error: info.error }))
      else if (info.hasUpdate) {
        // The desktop announces the banner with its upgrade button; a browser
        // or phone cannot replace its own binary, so the release page link is
        // the actionable answer there.
        setNote(
          isDesktop
            ? t('update.found', { version: info.latestVersion })
            : t('update.foundWeb', { version: info.latestVersion }),
        )
        if (!isDesktop) setPageUrl(info.pageUrl)
      } else if (current === 'dev') setNote(t('update.devBuild'))
      else setNote(t('update.upToDate'))
    } catch (e) {
      setNote(t('update.checkFailed', { error: errText(e) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Button size="sm" variant="subtle" disabled={busy} onClick={() => void check()}>
        {busy ? t('update.checking') : t('update.check')}
      </Button>
      {note && <span className="font-sans text-[11px] text-ink-400">{note}</span>}
      {pageUrl && (
        <a
          href={pageUrl}
          target="_blank"
          rel="noreferrer"
          className="font-sans text-[11px] text-accent hover:underline"
        >
          {t('update.releasePage')}
        </a>
      )}
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
// The desktop replaces its own binary, and so does the headless server build —
// a served browser offers the same banner when the server says it can act on
// it. That capability rides on every UpdateInfo (canApply), so there is no
// separate probe to fail: what stays out is what cannot act — a desktop
// binary run as --serve, and the phone's embedded core (the APK is the
// update) — because their UpdateInfo says so.
export function UpdateBanner() {
  return <UpdateBannerInner />
}

/**
 * After a server-side upgrade the process execs over itself: the page is
 * still fine, but it is running the old bundle against a new server. Wait for
 * the flip and reload. The gap can be shorter than one probe, so "it kept
 * answering" after a few seconds is also taken as the restart having happened.
 */
async function reloadWhenServerReturns() {
  const probe = async () => {
    try {
      const res = await fetch('/manifest.webmanifest', { cache: 'no-store' })
      return res.ok
    } catch {
      return false
    }
  }
  const start = Date.now()
  let sawGap = false
  while (Date.now() - start < 120_000) {
    await new Promise((r) => setTimeout(r, 800))
    if (!(await probe())) {
      sawGap = true
      continue
    }
    if (sawGap || Date.now() - start > 8_000) break
  }
  window.location.reload()
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
    // The desktop hears about updates because its own process announces them.
    // A served page may have connected between the server's checks and missed
    // the announcement, so it asks once on load; the answer is cached
    // server-side, so a reload habit costs nothing.
    if (!isDesktop) {
      updateApi
        .bannerCheck()
        .then((i) => {
          if (i?.hasUpdate) setInfo(i)
        })
        .catch(() => {})
    }
    return () => {
      offAvail()
      offProg()
    }
  }, [])

  // A browser can only offer the upgrade when the other side can perform it.
  if (!info || !info.hasUpdate || dismissed === info.latestVersion) return null
  if (!isDesktop && !info.canApply) return null

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
        return
      }
      // On ok the desktop restarts itself and the window goes away; a server
      // execs over in place, and this page — old bundle, new server — reloads
      // itself once the new build answers.
      if (!isDesktop) void reloadWhenServerReturns()
    } catch (e) {
      setError(errText(e))
      setBusy(false)
      setProgress(null)
    }
  }

  // The desktop's Go opens the release page in the machine's browser; from a
  // served page that machine is the server, so the page opens it itself.
  const openNotes = () => {
    if (isDesktop) void updateApi.openReleasePage()
    else if (info.pageUrl) window.open(info.pageUrl, '_blank', 'noopener')
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
          <Button size="sm" variant="subtle" onClick={openNotes}>
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
          <Button size="sm" variant="subtle" onClick={openNotes}>
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
