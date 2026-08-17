import {
  AlertTriangle,
  Archive,
  Check,
  Download,
  FlaskConical,
  History,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { errText, skills as skillApi } from '../../lib/api'
import type { Skill, SkillMatch, SkillStats, SkillStatus } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { useDialogs } from '../../store/useDialogs'
import { Badge, Button, Empty, Segmented, inputClass, textareaClass } from '../ui'

const statusTone: Record<SkillStatus, 'ok' | 'accent' | 'neutral' | 'warn' | 'danger'> = {
  active: 'ok',
  draft: 'accent',
  disabled: 'neutral',
  archived: 'neutral',
  rejected: 'danger',
}

type Tab = 'active' | 'draft' | 'other'

const tabFilter: Record<Tab, string[]> = {
  active: ['active'],
  draft: ['draft'],
  other: ['disabled', 'archived', 'rejected'],
}

/**
 * The skill library: procedures the orchestrator may follow, and the queue of
 * ones it has proposed but nobody has approved.
 *
 * Skills are managed here before anything can generate them, which is the order
 * the whole feature was built in: a person has to be able to write, read and
 * revoke this material before a model is allowed to add to it.
 */
export function SkillPanel() {
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)
  const dialog = useDialogs((s) => s.dialog)

  const [tab, setTab] = useState<Tab>('active')
  const [rows, setRows] = useState<Skill[]>([])
  const [stats, setStats] = useState<SkillStats | null>(null)
  const [text, setText] = useState('')
  const [loading, setLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [scenario, setScenario] = useState('')
  const [matches, setMatches] = useState<SkillMatch[] | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [list, s] = await Promise.all([
        skillApi.list({ statuses: tabFilter[tab], text }),
        skillApi.stats(),
      ])
      setRows(list ?? [])
      setStats(s)
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setLoading(false)
    }
  }, [tab, text, toast])

  useEffect(() => {
    const t = setTimeout(() => void load(), 180)
    return () => clearTimeout(t)
  }, [load])

  // The editor lives in a modal; reload when it closes so an edit shows up.
  useEffect(() => {
    if (!dialog) void load()
  }, [dialog, load])

  async function act(id: string, event: string, label: string) {
    try {
      await skillApi.apply(id, event)
      toast('ok', label)
      await load()
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b hairline px-3 py-2">
        <span className="text-[11px] font-semibold text-ink-300">
          Skills {stats ? `(${stats.total})` : ''}
        </span>
        <div className="flex gap-1">
          <Button
            size="sm"
            variant={testing ? 'primary' : 'subtle'}
            onClick={() => {
              setTesting((v) => !v)
              setMatches(null)
            }}
            title="Try a situation and see which skills would be matched"
          >
            <FlaskConical size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={() => void importBundle(toast, load)} title="Import">
            <Upload size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={() => void exportBundle(toast)} title="Export">
            <Download size={11} />
          </Button>
          <Button
            size="sm"
            variant="subtle"
            onClick={() => openDialog({ kind: 'skill' })}
            title="New skill"
          >
            <Plus size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={() => void load()} disabled={loading} title="Refresh">
            <RefreshCw size={11} className={loading ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      <div className="border-b hairline px-3 py-2">
        <Segmented<Tab>
          className="w-full"
          value={tab}
          onChange={setTab}
          options={[
            { value: 'active', label: 'Active' },
            {
              value: 'draft',
              label: (
                <>
                  Draft
                  {!!stats?.draft && <Badge tone="accent">{stats.draft}</Badge>}
                </>
              ),
            },
            { value: 'other', label: 'Retired' },
          ]}
        />
      </div>

      {testing ? (
        <TestBench
          scenario={scenario}
          setScenario={setScenario}
          matches={matches}
          setMatches={setMatches}
        />
      ) : (
        <div className="border-b hairline px-3 py-2">
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Find by name or trigger…"
            className={inputClass}
          />
        </div>
      )}

      {/* The banner is about active skills, so it belongs on the tab where it
          can be acted on. On the draft queue it would just be noise about
          something else. */}
      {tab === 'active' && stats && stats.pending > 0 && (
        <div className="flex items-start gap-2 border-b hairline bg-warn/5 px-3 py-2">
          <AlertTriangle size={12} className="mt-0.5 shrink-0 text-warn" />
          <div className="min-w-0 flex-1">
            <p className="text-[11px] leading-relaxed text-ink-200">
              {stats.pending} active skill{stats.pending === 1 ? ' has' : 's have'} no vector for{' '}
              <span className="font-mono">{stats.model}</span>. They will not be matched until they
              are embedded.
            </p>
            <Button
              size="sm"
              variant="primary"
              className="mt-1.5"
              onClick={async () => {
                try {
                  await skillApi.embed()
                  toast('ok', 'Skills embedded')
                  await load()
                } catch (e) {
                  toast('error', errText(e))
                }
              }}
            >
              Embed now
            </Button>
          </div>
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {rows.length === 0 && !loading && (
          <Empty
            title={tab === 'draft' ? 'No drafts waiting' : 'No skills here'}
            hint={
              tab === 'draft'
                ? 'Skills the orchestrator proposes land here for review before they can be used.'
                : 'A skill is a procedure worth repeating: when it applies, what to do, what never to do.'
            }
          />
        )}
        {rows.map((s) => (
          <SkillRow key={s.id} skill={s} onAct={act} onChanged={load} />
        ))}
      </div>
    </div>
  )
}

function SkillRow({
  skill,
  onAct,
  onChanged,
}: {
  skill: Skill
  onAct: (id: string, event: string, label: string) => Promise<void>
  onChanged: () => Promise<void>
}) {
  const toast = useAppStore((s) => s.toast)
  const openDialog = useDialogs((s) => s.open)
  const [open, setOpen] = useState(false)

  return (
    <div className="border-b hairline px-3 py-2">
      <div className="flex items-center gap-1.5">
        <button
          onClick={() => setOpen((v) => !v)}
          className="min-w-0 flex-1 truncate text-left text-[11px] font-medium text-ink-100 hover:text-accent"
        >
          {skill.name}
        </button>
        <Badge tone={statusTone[skill.status]}>{skill.status}</Badge>
        {skill.createdBy === 'orchestrator' && <Badge tone="accent">proposed</Badge>}
        {skill.status === 'active' && !skill.hasVector && <Badge tone="warn">no vector</Badge>}
        <span className="text-[10px] text-ink-600">v{skill.version}</span>
      </div>

      <p className="mt-0.5 line-clamp-2 text-[11px] leading-relaxed text-ink-400">
        {skill.trigger}
      </p>

      {open && (
        <div className="mt-2 space-y-2">
          {skill.description && (
            <p className="text-[11px] leading-relaxed text-ink-300">{skill.description}</p>
          )}
          <ol className="space-y-1">
            {skill.steps.map((st) => (
              <li key={st.order} className="flex gap-1.5 text-[11px] text-ink-300">
                <span className="shrink-0 text-ink-600">{st.order}.</span>
                <span className="min-w-0">
                  {st.description}
                  {st.recommendedTools.length > 0 && (
                    <span className="ml-1 font-mono text-[10px] text-ink-500">
                      {st.recommendedTools.join(' ')}
                    </span>
                  )}
                  {st.notes && <span className="block text-[10.5px] text-ink-500">{st.notes}</span>}
                </span>
              </li>
            ))}
          </ol>
          {skill.constraints.length > 0 && (
            <ul className="space-y-0.5">
              {skill.constraints.map((c) => (
                <li key={c} className="flex gap-1.5 text-[11px] text-warn">
                  <span className="shrink-0">·</span>
                  <span>{c}</span>
                </li>
              ))}
            </ul>
          )}
          <p className="text-[10px] text-ink-600">
            {skill.scope}
            {skill.usageCount > 0 && ` · used ${skill.usageCount}×`}
            {skill.confidence !== null && ` · confidence ${skill.confidence.toFixed(2)}`}
            {` · updated ${new Date(skill.updatedAt * 1000).toLocaleDateString()}`}
          </p>

          <div className="flex flex-wrap gap-1.5">
            {skill.status === 'draft' && (
              <>
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => void onAct(skill.id, 'approve', 'Approved')}
                >
                  <Check size={11} /> Approve
                </Button>
                <Button size="sm" onClick={() => void onAct(skill.id, 'reject', 'Rejected')}>
                  <X size={11} /> Reject
                </Button>
              </>
            )}
            {skill.status === 'active' && (
              <Button size="sm" onClick={() => void onAct(skill.id, 'disable', 'Disabled')}>
                <Pause size={11} /> Disable
              </Button>
            )}
            {skill.status === 'disabled' && (
              <Button size="sm" onClick={() => void onAct(skill.id, 'enable', 'Enabled')}>
                <Play size={11} /> Enable
              </Button>
            )}
            {skill.status === 'archived' && (
              <Button size="sm" onClick={() => void onAct(skill.id, 'restore', 'Restored')}>
                <Play size={11} /> Restore
              </Button>
            )}
            {(skill.status === 'active' || skill.status === 'disabled') && (
              <Button size="sm" onClick={() => void onAct(skill.id, 'archive', 'Archived')}>
                <Archive size={11} /> Archive
              </Button>
            )}
            <Button size="sm" onClick={() => openDialog({ kind: 'skill', skill })}>
              <Pencil size={11} /> Edit
            </Button>
            <Button size="sm" onClick={() => openDialog({ kind: 'skillHistory', skill })}>
              <History size={11} /> v{skill.version}
            </Button>
            <Button
              size="sm"
              variant="danger"
              onClick={async () => {
                const ok = await confirmAction({
                  title: `Delete ${skill.name}`,
                  message: 'The skill and its whole version history are removed.',
                  points: ['Archiving keeps the record; deleting does not'],
                  confirmLabel: 'Delete',
                })
                if (!ok) return
                try {
                  await skillApi.remove(skill.id)
                  await onChanged()
                } catch (e) {
                  toast('error', errText(e))
                }
              }}
            >
              <Trash2 size={11} />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * Describe a situation, see what would be matched and why.
 *
 * Without this, a trigger's real meaning is only discovered the first time it
 * fires — or fails to — inside a plan, which is the worst possible place to
 * find out that it says something other than what its author meant.
 */
function TestBench({
  scenario,
  setScenario,
  matches,
  setMatches,
}: {
  scenario: string
  setScenario: (v: string) => void
  matches: SkillMatch[] | null
  setMatches: (m: SkillMatch[] | null) => void
}) {
  const toast = useAppStore((s) => s.toast)
  const [busy, setBusy] = useState(false)

  async function run() {
    if (!scenario.trim()) return
    setBusy(true)
    try {
      setMatches(await skillApi.match({ text: scenario, topK: 5 }))
    } catch (e) {
      toast('error', errText(e))
      setMatches(null)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="border-b hairline px-3 py-2">
      <textarea
        value={scenario}
        onChange={(e) => setScenario(e.target.value)}
        rows={2}
        placeholder="Describe a situation: the payment service is slow again…"
        className={textareaClass}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void run()
        }}
      />
      <div className="mt-1.5 flex items-center gap-2">
        <Button size="sm" variant="primary" disabled={busy || !scenario.trim()} onClick={() => void run()}>
          Match
        </Button>
        <span className="text-[10px] text-ink-600">Nothing is executed. This only matches.</span>
      </div>

      {matches !== null && (
        <div className="mt-2 space-y-1.5">
          {matches.length === 0 && (
            <p className="text-[11px] text-ink-500">
              No skill matched. Either none applies, or a trigger does not describe this situation
              in words close enough to yours.
            </p>
          )}
          {matches.map((m) => (
            <div key={m.skill.id} className="rounded-control border hairline bg-ink-850 px-2.5 py-1.5">
              <div className="flex items-center gap-1.5">
                <span className="min-w-0 flex-1 truncate text-[11px] font-medium text-ink-100">
                  {m.skill.name}
                </span>
                <Badge tone="accent">{m.score.toFixed(2)}</Badge>
              </div>
              <p className="mt-0.5 text-[10.5px] leading-relaxed text-ink-500">{m.reason}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// Export and import go through the clipboard rather than the filesystem: the
// app has no file dialog of its own, and a bundle is small enough to paste into
// a gist, a pull request or a chat window.
async function exportBundle(toast: (tone: 'ok' | 'error', text: string) => void) {
  try {
    const json = await skillApi.exportJson([])
    await navigator.clipboard.writeText(json)
    toast('ok', 'Skill bundle copied to the clipboard')
  } catch (e) {
    toast('error', errText(e))
  }
}

async function importBundle(
  toast: (tone: 'ok' | 'error', text: string) => void,
  reload: () => Promise<void>,
) {
  try {
    const text = await navigator.clipboard.readText()
    if (!text.trim()) {
      toast('error', 'The clipboard is empty — copy a skill bundle first')
      return
    }
    const res = await skillApi.importJson(text)
    if (res.skipped.length > 0) {
      // Naming what was refused is the difference between a fixable file and a
      // mystery. The count alone would say nothing about which skill to fix.
      toast('error', `Imported ${res.imported}; skipped ${res.skipped.length}: ${res.skipped[0]}`)
    } else {
      toast('ok', `Imported ${res.imported} skill${res.imported === 1 ? '' : 's'}`)
    }
    await reload()
  } catch (e) {
    toast('error', errText(e))
  }
}
