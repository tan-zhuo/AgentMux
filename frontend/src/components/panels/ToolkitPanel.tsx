import clsx from 'clsx'
import {
  CheckCircle2,
  ChevronDown,
  Circle,
  Download,
  ExternalLink,
  RefreshCw,
  ShieldAlert,
  Sparkles,
  Terminal,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { errText, toolkit } from '../../lib/api'
import type { InstallMethod, ToolReport, ToolStatus } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { useDialogs } from '../../store/useDialogs'
import { useT } from '../../store/useI18n'
import type { TFunc } from '../../lib/i18n'
import { Badge, Button, Empty, textareaClass } from '../ui'

/**
 * One-click install for the common agent CLIs.
 *
 * Installs are never fired into a bare exec channel: they land in a tmux
 * session, so losing the connection cannot abandon a half-written package tree,
 * and prompts — a sudo password, a licence — are answerable by attaching.
 */
export function ToolkitPanel({ serverId }: { serverId: string }) {
  const openTab = useAppStore((s) => s.openTab)
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)
  const t = useT()

  const [report, setReport] = useState<ToolReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [busyTool, setBusyTool] = useState<string | null>(null)
  const [customOpen, setCustomOpen] = useState(false)
  const [custom, setCustom] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await toolkit.detect(serverId)
      setReport(r)
      if (r.error) toast('error', r.error)
    } catch (e) {
      toast('error', errText(e))
      setReport(null)
    } finally {
      setLoading(false)
    }
  }, [serverId, toast])

  useEffect(() => {
    void load()
  }, [load])

  async function install(status: ToolStatus, method: InstallMethod) {
    setBusyTool(status.tool.id)
    try {
      const started = await toolkit.install(serverId, status.tool.id, method.id)
      if (started.usesTmux) {
        openTab({
          title: t('toolkit.installTab', { name: started.toolName }),
          kind: 'tmux',
          serverId,
          workspaceId: '',
          agentId: '',
          tmuxSession: started.session,
        })
        toast(
          'ok',
          started.needsRoot
            ? t('toolkit.installingSudo', { name: started.toolName })
            : t('toolkit.installingTmux', {
                name: started.toolName,
                session: started.session,
              }),
        )
      } else {
        // tmux is the thing being installed, so there is no session to use yet.
        openTab({
          title: t('toolkit.installTab', { name: started.toolName }),
          kind: 'command',
          serverId,
          workspaceId: '',
          agentId: '',
          tmuxSession: '',
          command: started.command,
        })
        toast('warn', t('toolkit.installingShell', { name: started.toolName }))
      }
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusyTool(null)
    }
  }

  async function verify(status: ToolStatus) {
    try {
      const p = await toolkit.verify(serverId, status.tool.id)
      if (p.installed)
        toast(
          'ok',
          p.version
            ? t('toolkit.verified', {
                name: status.tool.name,
                version: p.version,
                path: p.path,
              })
            : t('toolkit.verifiedNoVersion', { name: status.tool.name, path: p.path }),
        )
      else toast('warn', t('toolkit.notOnPath', { name: status.tool.name }))
      await load()
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function runCustom() {
    const script = custom.trim()
    if (!script) return
    try {
      const started = await toolkit.installCustom(serverId, 'custom install', script)
      openTab({
        title: t('toolkit.customInstall'),
        kind: started.usesTmux ? 'tmux' : 'command',
        serverId,
        workspaceId: '',
        agentId: '',
        tmuxSession: started.session,
        command: started.command,
      })
      setCustom('')
      setCustomOpen(false)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  const tmuxMissing = report?.runtimes.find((r) => r.tool.id === 'tmux' && !r.installed)

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b hairline px-3 py-2">
        <span className="flex items-center gap-1.5 text-[11px] font-semibold text-ink-300">
          <Sparkles size={11} /> {t('toolkit.title')}
        </span>
        <div className="flex gap-1">
          <Button size="sm" variant="subtle" onClick={() => setCustomOpen((v) => !v)} title={t('toolkit.customScript')}>
            <Terminal size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={load} disabled={loading} title={t('toolkit.redetect')}>
            <RefreshCw size={11} className={loading ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {report?.os && (
        <p className="border-b hairline px-3 py-1.5 font-mono text-[10.5px] text-ink-500">
          {report.os} · {report.shell}
        </p>
      )}

      {tmuxMissing && (
        <p className="flex items-start gap-1.5 border-b hairline bg-warn/8 px-3 py-2 text-[11px] leading-relaxed text-warn">
          <ShieldAlert size={12} className="mt-0.5 shrink-0" />
          {t('toolkit.tmuxMissing')}
        </p>
      )}

      {customOpen && (
        <div className="border-b hairline px-3 py-2">
          <textarea
            rows={3}
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            placeholder={t('toolkit.customPlaceholder')}
            className={clsx(textareaClass, 'resize-none font-mono')}
          />
          <div className="mt-1.5 flex justify-end">
            <Button size="sm" variant="primary" disabled={!custom.trim()} onClick={() => void runCustom()}>
              {t('toolkit.run')}
            </Button>
          </div>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {!report && !loading && (
          <Empty title={t('toolkit.notDetected')} hint={t('toolkit.notDetected.hint')} />
        )}
        {report && (
          <>
            <Section title={t('toolkit.agents')} />
            {report.agents.map((s) => (
              <ToolRow
                key={s.tool.id}
                status={s}
                busy={busyTool === s.tool.id}
                onInstall={install}
                onVerify={verify}
                onUseAsAgent={() =>
                  openDialog({ kind: 'agent', presetCommand: s.tool.runCommand })
                }
                t={t}
              />
            ))}
            <Section title={t('toolkit.runtimes')} />
            {report.runtimes.map((s) => (
              <ToolRow
                key={s.tool.id}
                status={s}
                busy={busyTool === s.tool.id}
                onInstall={install}
                onVerify={verify}
                t={t}
              />
            ))}
          </>
        )}
      </div>
    </div>
  )
}

function Section({ title }: { title: string }) {
  return (
    <p className="sticky top-0 z-10 bg-ink-900 px-3 py-1.5 text-[11px] font-semibold text-ink-300">
      {title}
    </p>
  )
}

function ToolRow({
  status,
  busy,
  onInstall,
  onVerify,
  onUseAsAgent,
  t,
}: {
  status: ToolStatus
  busy: boolean
  onInstall: (s: ToolStatus, m: InstallMethod) => void | Promise<void>
  onVerify: (s: ToolStatus) => void | Promise<void>
  onUseAsAgent?: () => void
  // Passed down rather than subscribed to again: the rows re-render with the
  // panel anyway, and one subscription per tool would be a hook for nothing.
  t: TFunc
}) {
  const [showMethods, setShowMethods] = useState(false)
  const methods = status.available ?? []
  const primary = methods[0]

  return (
    <div className="border-b hairline px-3 py-2">
      <div className="flex items-start gap-2">
        {status.installed ? (
          <CheckCircle2 size={13} className="mt-0.5 shrink-0 text-ok" />
        ) : (
          <Circle size={13} className="mt-0.5 shrink-0 text-ink-600" />
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-xs font-medium text-ink-100">{status.tool.name}</span>
            {status.tool.vendor && <span className="text-[10px] text-ink-600">{status.tool.vendor}</span>}
            {status.installed && status.version && <Badge tone="ok">{status.version}</Badge>}
          </div>
          <p className="mt-0.5 text-[11px] leading-relaxed text-ink-500">{status.tool.description}</p>
          {status.installed && status.path && (
            <p className="mt-0.5 truncate font-mono text-[10.5px] text-ink-600">{status.path}</p>
          )}
          {!status.installed && status.blocked && (
            <p className="mt-0.5 text-[10.5px] text-warn">{status.blocked}</p>
          )}
        </div>
      </div>

      <div className="mt-1.5 flex flex-wrap items-center gap-1.5 pl-5">
        {!status.installed && primary && (
          <Button size="sm" variant="primary" disabled={busy} onClick={() => void onInstall(status, primary)}>
            <Download size={11} />{' '}
            {busy ? t('toolkit.starting') : t('toolkit.install', { method: primary.label })}
            {primary.needsRoot && <span className="text-warn">sudo</span>}
          </Button>
        )}
        {methods.length > 1 && (
          <Button size="sm" variant="subtle" onClick={() => setShowMethods((v) => !v)}>
            <ChevronDown size={11} className={showMethods ? 'rotate-180' : undefined} />
            {t('toolkit.moreMethods', { n: methods.length - 1 })}
          </Button>
        )}
        {status.installed && (
          <>
            <Button size="sm" variant="subtle" onClick={() => void onVerify(status)}>
              {t('toolkit.recheck')}
            </Button>
            {onUseAsAgent && (
              <Button size="sm" onClick={onUseAsAgent}>
                {t('toolkit.useAsAgent')}
              </Button>
            )}
          </>
        )}
        {!status.installed && !primary && (
          <Button size="sm" variant="subtle" onClick={() => void onVerify(status)}>
            {t('toolkit.recheck')}
          </Button>
        )}
        {status.tool.docs && (
          <a
            href={status.tool.docs}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-[11px] text-ink-500 hover:text-accent"
          >
            <ExternalLink size={10} /> {t('toolkit.docs')}
          </a>
        )}
      </div>

      {showMethods && (
        <div className="mt-1.5 space-y-1 pl-5">
          {methods.slice(1).map((m) => (
            <div key={m.id} className="flex items-center gap-2">
              <Button size="sm" disabled={busy} onClick={() => void onInstall(status, m)}>
                <Download size={11} /> {m.label}
                {m.needsRoot && <span className="text-warn">sudo</span>}
              </Button>
              <code className="min-w-0 flex-1 truncate font-mono text-[10px] text-ink-600">
                {m.script}
              </code>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
