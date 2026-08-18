import { Dialogs } from '@wailsio/runtime'
import { AlertTriangle, Download, Upload } from 'lucide-react'
import { useState } from 'react'
import { config, errText } from '../lib/api'
import { MIN_PASSPHRASE } from '../lib/transfer'
import type { ConfigImport, ConfigManifest } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useFmt, useI18n, useT } from '../store/useI18n'
import { useTheme } from '../store/useTheme'
import { Button, Field, Modal, inputClass } from './ui'

/**
 * Carrying this installation to another machine.
 *
 * Export and import are two faces of one dialog because they are one subject,
 * and because everything either of them says — what travels, what does not,
 * what a passphrase protects — is the same sentence read from either end.
 */
export function TransferDialog({ mode }: { mode: 'export' | 'import' }) {
  return mode === 'export' ? <ExportPane /> : <ImportPane />
}

function ExportPane() {
  const close = useDialogs((s) => s.close)
  const toast = useAppStore((s) => s.toast)
  const t = useT()

  const [secrets, setSecrets] = useState(true)
  const [library, setLibrary] = useState(true)
  const [pass, setPass] = useState('')
  const [again, setAgain] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<{ manifest: ConfigManifest; path: string } | null>(null)

  const short = pass.length > 0 && pass.length < MIN_PASSPHRASE
  const mismatch = again.length > 0 && pass !== again
  const ready = pass.length >= MIN_PASSPHRASE && pass === again && !busy

  async function run() {
    setBusy(true)
    try {
      // The name is suggested by the backend so the date in it is the machine's
      // own, not one this window worked out from a clock it does not own.
      const suggested = await config.suggestFilename()
      const path = await Dialogs.SaveFile({
        Title: t('transfer.export.title'),
        Filename: suggested,
      })
      if (!path) return
      const manifest = await config.export(path, pass, {
        includeSecrets: secrets,
        includeLibrary: library,
      })
      setDone({ manifest, path })
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={t('transfer.export.title')}
      onClose={close}
      footer={
        done ? (
          <Button variant="primary" onClick={close}>
            {t('common.close')}
          </Button>
        ) : (
          <>
            <Button onClick={close}>{t('common.cancel')}</Button>
            <Button variant="primary" disabled={!ready} onClick={() => void run()}>
              <Download size={11} /> {busy ? t('transfer.exporting') : t('transfer.export.go')}
            </Button>
          </>
        )
      }
    >
      {done ? (
        <div className="space-y-3">
          <Counts manifest={done.manifest} />
          <p className="text-[11px] leading-relaxed break-all text-ink-400">
            {t('transfer.exported', { path: done.path })}
          </p>
          {done.manifest.hasSecrets && (
            <Warning>{t('transfer.keyPathNote')}</Warning>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <p className="text-[11px] leading-relaxed text-ink-400">{t('transfer.export.blurb')}</p>

          <Check
            checked={secrets}
            onChange={setSecrets}
            label={t('transfer.includeSecrets')}
            hint={t('transfer.includeSecrets.hint')}
          />
          <Check
            checked={library}
            onChange={setLibrary}
            label={t('transfer.includeLibrary')}
            hint={t('transfer.includeLibrary.hint')}
          />

          <div className="grid grid-cols-2 gap-3">
            <Field
              label={t('transfer.passphrase')}
              hint={t('transfer.passphrase.hint', { n: MIN_PASSPHRASE })}
            >
              <input
                autoFocus
                type="password"
                className={inputClass}
                value={pass}
                onChange={(e) => setPass(e.target.value)}
              />
            </Field>
            <Field label={t('transfer.passphrase.again')}>
              <input
                type="password"
                className={inputClass}
                value={again}
                onChange={(e) => setAgain(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && ready) void run()
                }}
              />
            </Field>
          </div>
          {/* A passphrase typed once and mistyped locks the file for good, so
              the mismatch is said here rather than after the file is written. */}
          {(short || mismatch) && (
            <p className="text-[11px] text-warn">
              {mismatch
                ? t('transfer.passphrase.mismatch')
                : t('transfer.passphrase.hint', { n: MIN_PASSPHRASE })}
            </p>
          )}
        </div>
      )}
    </Modal>
  )
}

function ImportPane() {
  const close = useDialogs((s) => s.close)
  const toast = useAppStore((s) => s.toast)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const initTheme = useTheme((s) => s.init)
  const initI18n = useI18n((s) => s.init)
  const t = useT()
  const fmt = useFmt()

  const [path, setPath] = useState('')
  const [writtenAt, setWrittenAt] = useState(0)
  const [recognised, setRecognised] = useState(true)
  const [pass, setPass] = useState('')
  const [manifest, setManifest] = useState<ConfigManifest | null>(null)
  const [result, setResult] = useState<ConfigImport | null>(null)
  const [busy, setBusy] = useState(false)

  async function pick() {
    try {
      const picked = await Dialogs.OpenFile({
        Title: t('transfer.import.title'),
        CanChooseFiles: true,
      })
      const chosen = Array.isArray(picked) ? picked[0] : picked
      if (!chosen) return
      setPath(chosen)
      setManifest(null)
      setResult(null)
      // The header is not encrypted, so the wrong file can be named as such
      // before anybody types a passphrase at it.
      const info = await config.peek(chosen)
      setRecognised(info.recognised)
      setWrittenAt(info.exportedAt)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function open() {
    setBusy(true)
    try {
      setManifest(await config.inspect(path, pass))
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function run() {
    setBusy(true)
    try {
      const res = await config.import(path, pass)
      setResult(res)
      await refreshSnapshot()
      // A file may have carried the theme, the language and the zone. Both
      // stores read them back from the settings they were just written to.
      if (res.settings.added > 0) {
        await initTheme()
        await initI18n()
      }
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  const primary = result ? (
    <Button variant="primary" onClick={close}>
      {t('common.close')}
    </Button>
  ) : manifest ? (
    <Button variant="primary" disabled={busy} onClick={() => void run()}>
      <Upload size={11} /> {busy ? t('transfer.importing') : t('transfer.go')}
    </Button>
  ) : (
    <Button variant="primary" disabled={!path || !pass || busy} onClick={() => void open()}>
      {busy ? t('transfer.opening') : t('transfer.open')}
    </Button>
  )

  return (
    <Modal
      title={t('transfer.import.title')}
      onClose={close}
      footer={
        <>
          {!result && <Button onClick={close}>{t('common.cancel')}</Button>}
          {primary}
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-[11px] leading-relaxed text-ink-400">{t('transfer.import.blurb')}</p>

        <div className="flex items-center gap-2">
          <Button size="sm" onClick={() => void pick()}>
            {t('transfer.pickFile')}
          </Button>
          <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] text-ink-500">
            {path || t('transfer.noFile')}
          </span>
        </div>
        {path && recognised && writtenAt > 0 && (
          <p className="text-[10.5px] text-ink-600">
            {t('transfer.fileWritten', { date: fmt.dateTime(writtenAt) })}
          </p>
        )}
        {path && !recognised && <Warning>{t('transfer.unrecognised')}</Warning>}

        {!result && (
          <Field label={t('transfer.passphrase')}>
            <input
              type="password"
              className={inputClass}
              value={pass}
              onChange={(e) => {
                setPass(e.target.value)
                setManifest(null)
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && path && pass && !busy) void (manifest ? run() : open())
              }}
            />
          </Field>
        )}

        {manifest && !result && (
          <div>
            <p className="mb-1.5 text-[11px] font-semibold text-ink-300">{t('transfer.contents')}</p>
            <Counts manifest={manifest} />
          </div>
        )}

        {result && <Landed result={result} />}
      </div>
    </Modal>
  )
}

/** What a file holds, as a row of counts. Zeroes are left out: a line saying
 *  "0 skills" is noise in a list whose job is to show what is there. */
function Counts({ manifest }: { manifest: ConfigManifest }) {
  const t = useT()
  const rows: Array<[string, number]> = [
    [t('transfer.count.hosts'), manifest.hosts],
    [t('transfer.count.folders'), manifest.folders],
    [t('transfer.count.projects'), manifest.projects],
    [t('transfer.count.workspaces'), manifest.workspaces],
    [t('transfer.count.agents'), manifest.agents],
    [t('transfer.count.skills'), manifest.skills],
    [t('transfer.count.settings'), manifest.settings],
  ]
  return (
    <div className="rounded-card border hairline bg-ink-850 px-2.5 py-2">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-[11px]">
        {rows
          .filter(([, n]) => n > 0)
          .map(([label, n]) => (
            <div key={label} className="flex items-baseline justify-between gap-2">
              <dt className="text-ink-500">{label}</dt>
              <dd className="tabular-nums text-ink-200">{n}</dd>
            </div>
          ))}
      </dl>
      <p className="mt-1.5 text-[10.5px] text-ink-600">
        {manifest.hasSecrets ? t('transfer.withSecrets') : t('transfer.withoutSecrets')}
      </p>
    </div>
  )
}

/** What an import actually did, kind by kind. */
function Landed({ result }: { result: ConfigImport }) {
  const t = useT()
  const rows: Array<[string, { added: number; skipped: number }]> = [
    [t('transfer.count.hosts'), result.hosts],
    [t('transfer.count.folders'), result.folders],
    [t('transfer.count.projects'), result.projects],
    [t('transfer.count.workspaces'), result.workspaces],
    [t('transfer.count.agents'), result.agents],
    [t('transfer.count.skills'), result.skills],
    [t('transfer.count.settings'), result.settings],
  ]
  const shown = rows.filter(([, tally]) => tally.added > 0 || tally.skipped > 0)
  const addedAnything = shown.some(([, tally]) => tally.added > 0)

  return (
    <div className="space-y-2">
      <p className="text-[11px] font-semibold text-ink-300">{t('transfer.imported')}</p>
      {addedAnything ? (
        <div className="rounded-card border hairline bg-ink-850 px-2.5 py-2">
          <dl className="space-y-1 text-[11px]">
            {shown.map(([label, tally]) => (
              <div key={label} className="flex items-baseline justify-between gap-2">
                <dt className="text-ink-500">{label}</dt>
                <dd className="tabular-nums text-ink-200">
                  {tally.added > 0 && (
                    <span className="text-ok">{t('transfer.added', { n: tally.added })}</span>
                  )}
                  {tally.added > 0 && tally.skipped > 0 && <span className="text-ink-600"> · </span>}
                  {tally.skipped > 0 && (
                    <span className="text-ink-500">
                      {t('transfer.skipped', { n: tally.skipped })}
                    </span>
                  )}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      ) : (
        <p className="text-[11px] leading-relaxed text-ink-400">{t('transfer.nothingNew')}</p>
      )}
      {result.notes.length > 0 && (
        <div>
          <p className="mb-1 text-[11px] font-semibold text-ink-300">{t('transfer.notes')}</p>
          <ul className="space-y-0.5">
            {result.notes.map((note, i) => (
              <li key={i} className="flex gap-1.5 text-[10.5px] leading-relaxed text-warn">
                <span className="shrink-0">·</span>
                <span className="min-w-0">{note}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function Check({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  hint: string
}) {
  return (
    <label className="flex cursor-default gap-2">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 h-3 w-3 shrink-0 accent-[#4c8dff]"
      />
      <span className="min-w-0">
        <span className="block text-[11px] text-ink-200">{label}</span>
        <span className="block text-[10.5px] leading-relaxed text-ink-500">{hint}</span>
      </span>
    </label>
  )
}

function Warning({ children }: { children: React.ReactNode }) {
  return (
    <p className="flex items-start gap-1.5 text-[10.5px] leading-relaxed text-warn">
      <AlertTriangle size={11} className="mt-0.5 shrink-0" />
      <span>{children}</span>
    </p>
  )
}
