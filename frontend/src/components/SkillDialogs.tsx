import clsx from 'clsx'
import { GripVertical, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { errText, skills as skillApi } from '../lib/api'
import type { Skill, SkillScope, SkillStep, SkillVersion, ToolMeta } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { Badge, Button, Field, Modal, inputClass, textareaClass } from './ui'

const blank: Partial<Skill> = {
  name: '',
  description: '',
  trigger: '',
  scope: 'global',
  projectIds: [],
  agentTypes: [],
  steps: [{ order: 1, description: '', recommendedTools: [], notes: '' }],
  constraints: [],
  examples: { success: '', failure: '' },
}

/**
 * The skill editor.
 *
 * The trigger gets the most space and the plainest label, because it is what a
 * situation is actually matched against — a skill with perfect steps and a
 * vague trigger will never be found, and nothing else in the interface would
 * explain why.
 */
export function SkillDialog() {
  const dialog = useDialogs((s) => s.dialog)
  const close = useDialogs((s) => s.close)
  const toast = useAppStore((s) => s.toast)
  const projects = useAppStore((s) => s.snapshot.projects)

  const existing = dialog?.kind === 'skill' ? dialog.skill : undefined
  const [form, setForm] = useState<Partial<Skill>>(existing ?? blank)
  const [note, setNote] = useState('')
  const [tools, setTools] = useState<ToolMeta[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    void skillApi
      .tools()
      .then(setTools)
      .catch(() => setTools([]))
  }, [])

  const set = <K extends keyof Skill>(k: K, v: Skill[K]) => setForm((f) => ({ ...f, [k]: v }))
  const steps = form.steps ?? []

  const setStep = (i: number, patch: Partial<SkillStep>) =>
    set(
      'steps',
      steps.map((s, j) => (j === i ? { ...s, ...patch } : s)),
    )

  return (
    <Modal
      title={existing ? `Edit ${existing.name}` : 'New skill'}
      onClose={close}
      wide
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={async () => {
              setSaving(true)
              try {
                if (existing) {
                  await skillApi.update({ ...existing, ...form } as Skill, note || 'edited')
                  toast('ok', 'Skill updated')
                } else {
                  await skillApi.create(form)
                  toast('ok', 'Skill created')
                }
                close()
              } catch (e) {
                toast('error', errText(e))
              } finally {
                setSaving(false)
              }
            }}
          >
            {saving ? 'Saving…' : existing ? 'Save as a new version' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="grid grid-cols-[2fr_1fr] gap-3">
          <Field label="Name">
            <input
              autoFocus
              className={inputClass}
              value={form.name ?? ''}
              onChange={(e) => set('name', e.target.value)}
              placeholder="Payment latency triage"
            />
          </Field>
          <Field label="Scope">
            <select
              className={inputClass}
              value={form.scope ?? 'global'}
              onChange={(e) => set('scope', e.target.value as SkillScope)}
            >
              <option value="global">Everywhere</option>
              <option value="project">One or more projects</option>
              <option value="agent_type">Agent types</option>
            </select>
          </Field>
        </div>

        {form.scope === 'project' && (
          <Field label="Projects" hint="A project-scoped skill is only offered inside these.">
            <div className="flex flex-wrap gap-1.5">
              {projects.map((p) => {
                const on = (form.projectIds ?? []).includes(p.id)
                return (
                  <Button
                    key={p.id}
                    size="sm"
                    variant={on ? 'primary' : 'ghost'}
                    onClick={() =>
                      set(
                        'projectIds',
                        on
                          ? (form.projectIds ?? []).filter((id) => id !== p.id)
                          : [...(form.projectIds ?? []), p.id],
                      )
                    }
                  >
                    {p.name}
                  </Button>
                )
              })}
              {projects.length === 0 && (
                <span className="text-[11px] text-ink-500">No projects yet.</span>
              )}
            </div>
          </Field>
        )}

        {form.scope === 'agent_type' && (
          <Field label="Agent types" hint="Comma separated, e.g. claude, codex">
            <input
              className={inputClass}
              value={(form.agentTypes ?? []).join(', ')}
              onChange={(e) =>
                set(
                  'agentTypes',
                  e.target.value
                    .split(',')
                    .map((t) => t.trim())
                    .filter(Boolean),
                )
              }
            />
          </Field>
        )}

        <Field
          label="When it applies"
          hint="This is what a situation gets matched against. Describe the situation, not the fix."
        >
          <textarea
            rows={2}
            className={textareaClass}
            value={form.trigger ?? ''}
            onChange={(e) => set('trigger', e.target.value)}
            placeholder="the payment service slows down under load and latency alerts fire"
          />
        </Field>

        <Field label="Description" hint="One line, for the list.">
          <input
            className={inputClass}
            value={form.description ?? ''}
            onChange={(e) => set('description', e.target.value)}
          />
        </Field>

        <div>
          <div className="mb-1 flex items-center justify-between">
            <span className="text-[11px] font-semibold text-ink-300">
              Steps
            </span>
            <Button
              size="sm"
              variant="subtle"
              onClick={() =>
                set('steps', [
                  ...steps,
                  { order: steps.length + 1, description: '', recommendedTools: [], notes: '' },
                ])
              }
            >
              <Plus size={11} /> Step
            </Button>
          </div>
          <div className="space-y-2">
            {steps.map((s, i) => (
              <div key={i} className="rounded-control border hairline bg-ink-800 p-2">
                <div className="flex items-center gap-1.5">
                  <GripVertical size={12} className="shrink-0 text-ink-600" />
                  <span className="shrink-0 text-[11px] text-ink-500">{i + 1}</span>
                  <input
                    className={inputClass}
                    value={s.description}
                    onChange={(e) => setStep(i, { description: e.target.value })}
                    placeholder="What to do at this step"
                  />
                  <Button
                    size="sm"
                    variant="subtle"
                    onClick={() => set('steps', steps.filter((_, j) => j !== i))}
                  >
                    <Trash2 size={11} />
                  </Button>
                </div>
                <div className="mt-1.5 flex flex-wrap gap-1 pl-6">
                  {tools.map((t) => {
                    const on = s.recommendedTools.includes(t.name)
                    return (
                      <button
                        key={t.name}
                        title={`${t.description} (${t.risk})`}
                        onClick={() =>
                          setStep(i, {
                            recommendedTools: on
                              ? s.recommendedTools.filter((n) => n !== t.name)
                              : [...s.recommendedTools, t.name],
                          })
                        }
                        className={clsx(
                          'rounded-control border px-1.5 py-0.5 font-mono text-[10px] transition-colors',
                          on
                            ? 'border-accent bg-accent/15 text-accent'
                            : 'hairline bg-ink-850 text-ink-500 hover:text-ink-300',
                          // Risk is shown even unselected: recommending a
                          // destructive tool should never be a casual click.
                          !on && t.risk === 'destructive' && 'border-danger/30 text-danger/70',
                        )}
                      >
                        {t.name}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
          <p className="mt-1 text-[10.5px] leading-relaxed text-ink-500">
            Naming a tool is a recommendation, not permission. Whether it runs is still decided by
            its own risk level and the server it would touch.
          </p>
        </div>

        <Field label="Constraints" hint="One per line. Things that must not happen.">
          <textarea
            rows={2}
            className={textareaClass}
            value={(form.constraints ?? []).join('\n')}
            onChange={(e) =>
              set(
                'constraints',
                e.target.value.split('\n').map((c) => c.trim()).filter(Boolean),
              )
            }
            placeholder="never restart during business hours"
          />
        </Field>

        {existing && (
          <Field label="What changed" hint="Kept with the previous version, for the history.">
            <input
              className={inputClass}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="added the index check"
            />
          </Field>
        )}
      </div>
    </Modal>
  )
}

/** The version history of one skill, with a way back to any of it. */
export function SkillHistoryDialog() {
  const dialog = useDialogs((s) => s.dialog)
  const close = useDialogs((s) => s.close)
  const toast = useAppStore((s) => s.toast)
  const skill = dialog?.kind === 'skillHistory' ? dialog.skill : undefined

  const [versions, setVersions] = useState<SkillVersion[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!skill) return
    void skillApi
      .versions(skill.id)
      .then(setVersions)
      .catch((e) => toast('error', errText(e)))
  }, [skill, toast])

  if (!skill) return null

  return (
    <Modal
      title={`${skill.name} — history`}
      onClose={close}
      wide
      footer={<Button onClick={close}>Close</Button>}
    >
      <div className="space-y-2">
        {versions.length === 0 && <p className="text-[11px] text-ink-500">No history yet.</p>}
        {versions.map((v) => (
          <div key={v.id} className="rounded-control border hairline bg-ink-800 p-2.5">
            <div className="flex items-center gap-2">
              <span className="text-[11px] font-medium text-ink-100">v{v.version}</span>
              <Badge>{v.changedBy}</Badge>
              <span className="min-w-0 flex-1 truncate text-[11px] text-ink-400">{v.note}</span>
              <span className="shrink-0 text-[10px] text-ink-600">
                {new Date(v.createdAt * 1000).toLocaleString()}
              </span>
              <Button
                size="sm"
                disabled={busy || v.version === skill.version}
                title={
                  v.version === skill.version
                    ? 'This is the current content'
                    : 'Restore this content as a new version'
                }
                onClick={async () => {
                  setBusy(true)
                  try {
                    await skillApi.rollback(skill.id, v.version)
                    toast('ok', `Rolled back to v${v.version}`)
                    close()
                  } catch (e) {
                    toast('error', errText(e))
                  } finally {
                    setBusy(false)
                  }
                }}
              >
                <RotateCcw size={11} /> Restore
              </Button>
            </div>
            <p className="mt-1 text-[11px] leading-relaxed text-ink-400">{v.snapshot.trigger}</p>
            <ol className="mt-1 space-y-0.5">
              {v.snapshot.steps.map((s) => (
                <li key={s.order} className="text-[10.5px] text-ink-500">
                  {s.order}. {s.description}
                </li>
              ))}
            </ol>
          </div>
        ))}
        <p className="text-[10.5px] leading-relaxed text-ink-500">
          Restoring adds a new version rather than erasing the ones after it, so a rollback can
          itself be rolled back.
        </p>
      </div>
    </Modal>
  )
}
