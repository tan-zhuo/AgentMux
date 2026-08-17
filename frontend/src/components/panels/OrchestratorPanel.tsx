import clsx from 'clsx'
import {
  AlertTriangle,
  Check,
  ChevronRight,
  CircleSlash,
  Clock,
  Play,
  Power,
  Square,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { errText, on, orch as orchApi } from '../../lib/api'
import type { Approval, OrchStatus, Run, Step } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { Badge, Button, Empty, inputClass, textareaClass } from '../ui'

const runTone: Record<string, 'ok' | 'warn' | 'danger' | 'accent' | 'neutral'> = {
  running: 'accent',
  waiting_approval: 'warn',
  succeeded: 'ok',
  failed: 'danger',
  cancelled: 'neutral',
}

/**
 * The orchestrator: give it a goal, watch what it does, allow or refuse the
 * parts that change something.
 *
 * The panel is built around the idea that a person can always see what is being
 * proposed and stop it. Every step it takes appears here as it happens, and
 * anything that touches a server appears as a card with the reason it gave.
 */
export function OrchestratorPanel() {
  const toast = useAppStore((s) => s.toast)

  const [status, setStatus] = useState<OrchStatus | null>(null)
  const [runs, setRuns] = useState<Run[]>([])
  const [goal, setGoal] = useState('')
  const [current, setCurrent] = useState<Run | null>(null)
  const [steps, setSteps] = useState<Step[]>([])
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [s, r] = await Promise.all([orchApi.status(), orchApi.runs(20)])
      setStatus(s)
      setRuns(r ?? [])
      if (s.running && s.runId) {
        const live = (r ?? []).find((x) => x.id === s.runId) ?? null
        setCurrent(live)
        if (live) setSteps(await orchApi.steps(live.id))
      }
    } catch (e) {
      toast('error', errText(e))
    }
  }, [toast])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // Live updates. The run is happening whether or not this panel is open, so
  // the events carry the state rather than the panel polling for it.
  useEffect(() => {
    const offRun = on<Run>('orch:run', (run) => {
      if (!run) return
      setRuns((prev) => [run, ...prev.filter((r) => r.id !== run.id)])
      setCurrent((prev) => (prev?.id === run.id || !prev ? run : prev))
      if (run.status !== 'running' && run.status !== 'waiting_approval') void refresh()
    })
    const offStep = on<Step>('orch:step', (step) => {
      if (!step) return
      setSteps((prev) => (prev.some((s) => s.id === step.id) ? prev : [...prev, step]))
    })
    const offApproval = on<Approval>('orch:approval', () => void refresh())
    const offDraft = on<unknown>('orch:draft', () =>
      toast('ok', 'A skill was proposed — it is waiting in Skills → Draft'),
    )
    return () => {
      offRun()
      offStep()
      offApproval()
      offDraft()
    }
  }, [refresh, toast])

  async function start() {
    if (!goal.trim()) return
    setBusy(true)
    try {
      const run = await orchApi.start(goal.trim(), '')
      setCurrent(run)
      setSteps([])
      setGoal('')
      await refresh()
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  const pending = status?.pending ?? []

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-ink-800 px-3 py-2">
        <span className="text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
          Orchestrator
        </span>
        <div className="flex items-center gap-1.5">
          {status?.running && (
            <Button size="sm" variant="danger" onClick={() => void orchApi.stop()}>
              <Square size={11} /> Stop
            </Button>
          )}
          <Button
            size="sm"
            variant={status?.enabled ? 'primary' : 'ghost'}
            title={
              status?.enabled
                ? 'On. It acts only when you ask, and only with permission.'
                : 'Off. Nothing runs until you turn this on.'
            }
            onClick={async () => {
              if (!status) return
              try {
                const cfg = await orchApi.saveConfig({
                  enabled: !status.enabled,
                  patrolMinutes: status.patrolMinutes,
                })
                toast('ok', cfg.enabled ? 'Orchestrator on' : 'Orchestrator off')
                await refresh()
              } catch (e) {
                toast('error', errText(e))
              }
            }}
          >
            <Power size={11} /> {status?.enabled ? 'On' : 'Off'}
          </Button>
        </div>
      </div>

      {status && !status.enabled && (
        <div className="border-b border-ink-800 px-3 py-2">
          <p className="text-[11px] leading-relaxed text-ink-400">
            The orchestrator is off. Turned on, it can look at your fleet and — with your
            permission on each step — act on it. It is off by default because a thing that acts on
            its own should not start doing so because it was installed.
          </p>
        </div>
      )}

      {pending.length > 0 && (
        <div className="border-b border-ink-800">
          {pending.map((a) => (
            <ApprovalCard key={a.id} approval={a} onDone={refresh} />
          ))}
        </div>
      )}

      <div className="border-b border-ink-800 px-3 py-2">
        <textarea
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          rows={2}
          disabled={!status?.enabled || status?.running}
          placeholder="What should it do? e.g. check every agent and tell me which are stuck"
          className={textareaClass}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void start()
          }}
        />
        <div className="mt-1.5 flex items-center gap-2">
          <Button
            size="sm"
            variant="primary"
            disabled={busy || !goal.trim() || !status?.enabled || status?.running}
            onClick={() => void start()}
          >
            <Play size={11} /> Run
          </Button>
          <PatrolControl status={status} onSaved={refresh} />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {current && <RunView run={current} steps={steps} />}
        {!current && runs.length === 0 && (
          <Empty
            title="Nothing has run yet"
            hint="Give it a goal above. Every step it takes is written down here, including the ones it was refused."
          />
        )}
        {runs
          .filter((r) => r.id !== current?.id)
          .map((r) => (
            <PastRun key={r.id} run={r} />
          ))}
      </div>
    </div>
  )
}

function PatrolControl({ status, onSaved }: { status: OrchStatus | null; onSaved: () => void }) {
  const toast = useAppStore((s) => s.toast)
  if (!status?.enabled) return null

  return (
    <label className="flex items-center gap-1.5 text-[10.5px] text-ink-500">
      <Clock size={11} />
      patrol every
      <input
        type="number"
        min={0}
        value={status.patrolMinutes}
        onChange={async (e) => {
          try {
            await orchApi.saveConfig({
              enabled: status.enabled,
              patrolMinutes: Number(e.target.value) || 0,
            })
            onSaved()
          } catch (err) {
            toast('error', errText(err))
          }
        }}
        className={`${inputClass} w-14`}
        title="0 turns patrols off. A patrol may only read — it can report a problem, never act on one."
      />
      min
    </label>
  )
}

/** One request for permission, with everything needed to answer it in a glance. */
function ApprovalCard({ approval, onDone }: { approval: Approval; onDone: () => void }) {
  const toast = useAppStore((s) => s.toast)
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')

  async function decide(approved: boolean) {
    setBusy(true)
    try {
      await orchApi.decide(approval.id, approved, note)
      onDone()
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  let args: Record<string, unknown> = {}
  try {
    args = JSON.parse(approval.args || '{}')
  } catch {
    /* shown raw below if it will not parse */
  }

  return (
    <div className="border-b border-ink-850 bg-warn/5 px-3 py-2.5">
      <div className="flex items-center gap-1.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-100">
          {approval.tool}
        </span>
        <Badge tone={approval.risk === 'destructive' ? 'danger' : 'warn'}>{approval.risk}</Badge>
      </div>

      <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[10.5px]">
        {Object.entries(args).map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-ink-600">{k}</dt>
            <dd className="truncate font-mono text-ink-300">{String(v)}</dd>
          </div>
        ))}
      </dl>

      <p className="mt-1.5 text-[11px] leading-relaxed text-ink-200">
        {approval.rationale || 'It gave no reason.'}
      </p>

      {approval.injectionFlag && (
        <p className="mt-1.5 flex items-start gap-1.5 text-[10.5px] leading-relaxed text-warn">
          <AlertTriangle size={11} className="mt-0.5 shrink-0" />
          Text shaped like an instruction appeared in what it read before proposing this. Worth a
          second look.
        </p>
      )}

      <div className="mt-2 flex items-center gap-1.5">
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Note (optional)"
          className={inputClass}
        />
        <Button size="sm" variant="primary" disabled={busy} onClick={() => void decide(true)}>
          <Check size={11} /> Allow
        </Button>
        <Button size="sm" disabled={busy} onClick={() => void decide(false)}>
          <X size={11} /> Refuse
        </Button>
      </div>
    </div>
  )
}

function RunView({ run, steps }: { run: Run; steps: Step[] }) {
  return (
    <div className="border-b border-ink-800 px-3 py-2">
      <div className="flex items-center gap-1.5">
        <span className="min-w-0 flex-1 truncate text-[11px] font-medium text-ink-100">
          {run.goal}
        </span>
        <Badge tone={runTone[run.status] ?? 'neutral'}>{run.status.replace('_', ' ')}</Badge>
        {run.trigger === 'schedule' && <Badge>patrol</Badge>}
      </div>
      <StepList steps={steps} />
      {run.summary && (
        <p className="mt-1.5 text-[11px] leading-relaxed text-ink-200">{run.summary}</p>
      )}
      {run.error && <p className="mt-1.5 text-[11px] text-danger">{run.error}</p>}
    </div>
  )
}

function StepList({ steps }: { steps: Step[] }) {
  if (steps.length === 0) return null
  return (
    <ol className="mt-1.5 space-y-1">
      {steps.map((s) => (
        <li key={s.id} className="flex gap-1.5 text-[10.5px] leading-relaxed">
          <span className="w-4 shrink-0 text-right text-ink-600">{s.seq}</span>
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-1">
              {s.tool ? (
                <span className="font-mono text-ink-200">{s.tool}</span>
              ) : (
                <span className="text-ink-500">{s.phase}</span>
              )}
              {s.outcome.startsWith('denied') && <Badge tone="danger">refused</Badge>}
              {s.outcome === 'rejected' && <Badge tone="warn">you said no</Badge>}
              {s.outcome === 'failed' && <Badge tone="danger">failed</Badge>}
              {s.injectionFlag && (
                <span title={s.outcome}>
                  <Badge tone="warn">suspicious input</Badge>
                </span>
              )}
              {s.skillId && <Badge tone="accent">skill</Badge>}
            </span>
            {s.reasoning && <span className="block text-ink-400">{s.reasoning}</span>}
            {s.result && (
              <span className="block truncate font-mono text-[10px] text-ink-600">{s.result}</span>
            )}
          </span>
        </li>
      ))}
    </ol>
  )
}

function PastRun({ run }: { run: Run }) {
  const toast = useAppStore((s) => s.toast)
  const [open, setOpen] = useState(false)
  const [steps, setSteps] = useState<Step[]>([])

  return (
    <div className="border-b border-ink-850 px-3 py-2">
      <button
        onClick={async () => {
          const next = !open
          setOpen(next)
          if (next && steps.length === 0) {
            try {
              setSteps(await orchApi.steps(run.id))
            } catch (e) {
              toast('error', errText(e))
            }
          }
        }}
        className="flex w-full items-center gap-1.5 text-left"
      >
        <ChevronRight
          size={11}
          className={clsx('shrink-0 text-ink-600 transition-transform', open && 'rotate-90')}
        />
        <span className="min-w-0 flex-1 truncate text-[11px] text-ink-200">{run.goal}</span>
        {run.status === 'cancelled' && <CircleSlash size={11} className="shrink-0 text-ink-600" />}
        <Badge tone={runTone[run.status] ?? 'neutral'}>{run.status.replace('_', ' ')}</Badge>
      </button>
      {open && (
        <div className="pl-4">
          <StepList steps={steps} />
          {run.summary && (
            <p className="mt-1.5 text-[11px] leading-relaxed text-ink-300">{run.summary}</p>
          )}
          {run.error && <p className="mt-1.5 text-[11px] text-danger">{run.error}</p>}
          <p className="mt-1 text-[10px] text-ink-600">
            {new Date(run.startedAt * 1000).toLocaleString()} · {run.model}
            {run.trigger === 'schedule' && ' · patrol'}
          </p>
        </div>
      )}
    </div>
  )
}
