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
          title: `install ${started.toolName}`,
          kind: 'tmux',
          serverId,
          workspaceId: '',
          agentId: '',
          tmuxSession: started.session,
        })
        toast(
          'ok',
          started.needsRoot
            ? `Installing ${started.toolName} — attach the tab to enter your sudo password`
            : `Installing ${started.toolName} in tmux session ${started.session}`,
        )
      } else {
        // tmux is the thing being installed, so there is no session to use yet.
        openTab({
          title: `install ${started.toolName}`,
          kind: 'command',
          serverId,
          workspaceId: '',
          agentId: '',
          tmuxSession: '',
          command: started.command,
        })
        toast('warn', `${started.toolName} is installing in a plain shell — keep this tab open`)
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
      if (p.installed) toast('ok', `${status.tool.name} ${p.version || ''} at ${p.path}`.trim())
      else toast('warn', `${status.tool.name} is still not on PATH — try a fresh login shell`)
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
        title: 'custom install',
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
      <div className="flex items-center justify-between border-b border-ink-800 px-3 py-2">
        <span className="flex items-center gap-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
          <Sparkles size={11} /> Agent toolkit
        </span>
        <div className="flex gap-1">
          <Button size="sm" variant="subtle" onClick={() => setCustomOpen((v) => !v)} title="Custom script">
            <Terminal size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={load} disabled={loading} title="Re-detect">
            <RefreshCw size={11} className={loading ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {report?.os && (
        <p className="border-b border-ink-850 px-3 py-1.5 font-mono text-[10.5px] text-ink-500">
          {report.os} · {report.shell}
        </p>
      )}

      {tmuxMissing && (
        <p className="flex items-start gap-1.5 border-b border-ink-800 bg-warn/8 px-3 py-2 text-[11px] leading-relaxed text-warn">
          <ShieldAlert size={12} className="mt-0.5 shrink-0" />
          tmux is missing. AgentMux keeps every agent alive inside tmux, so install it first —
          without it nothing survives a dropped connection.
        </p>
      )}

      {customOpen && (
        <div className="border-b border-ink-800 px-3 py-2">
          <textarea
            rows={3}
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            placeholder="Any install command — runs in the tmux install session"
            className={clsx(textareaClass, 'resize-none font-mono')}
          />
          <div className="mt-1.5 flex justify-end">
            <Button size="sm" variant="primary" disabled={!custom.trim()} onClick={() => void runCustom()}>
              Run
            </Button>
          </div>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {!report && !loading && <Empty title="Not detected yet" hint="Connect to the server first." />}
        {report && (
          <>
            <Section title="Agents" />
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
              />
            ))}
            <Section title="Runtimes" />
            {report.runtimes.map((s) => (
              <ToolRow
                key={s.tool.id}
                status={s}
                busy={busyTool === s.tool.id}
                onInstall={install}
                onVerify={verify}
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
    <p className="sticky top-0 z-10 bg-ink-900 px-3 py-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
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
}: {
  status: ToolStatus
  busy: boolean
  onInstall: (s: ToolStatus, m: InstallMethod) => void | Promise<void>
  onVerify: (s: ToolStatus) => void | Promise<void>
  onUseAsAgent?: () => void
}) {
  const [showMethods, setShowMethods] = useState(false)
  const methods = status.available ?? []
  const primary = methods[0]

  return (
    <div className="border-b border-ink-850 px-3 py-2">
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
            <Download size={11} /> {busy ? 'Starting…' : `Install · ${primary.label}`}
            {primary.needsRoot && <span className="text-warn">sudo</span>}
          </Button>
        )}
        {methods.length > 1 && (
          <Button size="sm" variant="subtle" onClick={() => setShowMethods((v) => !v)}>
            <ChevronDown size={11} className={showMethods ? 'rotate-180' : undefined} />
            {methods.length - 1} more
          </Button>
        )}
        {status.installed && (
          <>
            <Button size="sm" variant="subtle" onClick={() => void onVerify(status)}>
              Re-check
            </Button>
            {onUseAsAgent && (
              <Button size="sm" onClick={onUseAsAgent}>
                Use as agent
              </Button>
            )}
          </>
        )}
        {!status.installed && !primary && (
          <Button size="sm" variant="subtle" onClick={() => void onVerify(status)}>
            Re-check
          </Button>
        )}
        {status.tool.docs && (
          <a
            href={status.tool.docs}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-[11px] text-ink-500 hover:text-accent"
          >
            <ExternalLink size={10} /> docs
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
