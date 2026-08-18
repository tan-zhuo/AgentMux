import clsx from 'clsx'
import { GripVertical, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { errText, skills as skillApi } from '../lib/api'
import type { Skill, SkillScope, SkillStep, SkillVersion, ToolMeta } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useFmt, useT } from '../store/useI18n'
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
  const t = useT()

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
      title={existing ? t('skillDialog.edit', { name: existing.name }) : t('skill.new')}
      onClose={close}
      wide
      footer={
        <>
          <Button onClick={close}>{t('common.cancel')}</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={async () => {
              setSaving(true)
              try {
                if (existing) {
                  await skillApi.update(
                    { ...existing, ...form } as Skill,
                    note || t('skillDialog.edited'),
                  )
                  toast('ok', t('skillDialog.updated'))
                } else {
                  await skillApi.create(form)
                  toast('ok', t('skillDialog.created'))
                }
                close()
              } catch (e) {
                toast('error', errText(e))
              } finally {
                setSaving(false)
              }
            }}
          >
            {saving
              ? t('common.saving')
              : existing
                ? t('skillDialog.saveVersion')
                : t('skillDialog.create')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="grid grid-cols-[2fr_1fr] gap-3">
          <Field label={t('skillDialog.name')}>
            <input
              autoFocus
              className={inputClass}
              value={form.name ?? ''}
              onChange={(e) => set('name', e.target.value)}
              placeholder={t('skillDialog.name.placeholder')}
            />
          </Field>
          <Field label={t('skillDialog.scope')}>
            <select
              className={inputClass}
              value={form.scope ?? 'global'}
              onChange={(e) => set('scope', e.target.value as SkillScope)}
            >
              <option value="global">{t('skillDialog.scope.global')}</option>
              <option value="project">{t('skillDialog.scope.project')}</option>
              <option value="agent_type">{t('skillDialog.scope.agentType')}</option>
            </select>
          </Field>
        </div>

        {form.scope === 'project' && (
          <Field label={t('skillDialog.projects')} hint={t('skillDialog.projects.hint')}>
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
                <span className="text-[11px] text-ink-500">{t('skillDialog.noProjects')}</span>
              )}
            </div>
          </Field>
        )}

        {form.scope === 'agent_type' && (
          <Field label={t('skillDialog.scope.agentType')} hint={t('skillDialog.agentTypes.hint')}>
            <input
              className={inputClass}
              value={(form.agentTypes ?? []).join(', ')}
              onChange={(e) =>
                set(
                  'agentTypes',
                  e.target.value
                    .split(',')
                    .map((type) => type.trim())
                    .filter(Boolean),
                )
              }
            />
          </Field>
        )}

        <Field label={t('skillDialog.trigger')} hint={t('skillDialog.trigger.hint')}>
          <textarea
            rows={2}
            className={textareaClass}
            value={form.trigger ?? ''}
            onChange={(e) => set('trigger', e.target.value)}
            placeholder={t('skillDialog.trigger.placeholder')}
          />
        </Field>

        <Field label={t('skillDialog.description')} hint={t('skillDialog.description.hint')}>
          <input
            className={inputClass}
            value={form.description ?? ''}
            onChange={(e) => set('description', e.target.value)}
          />
        </Field>

        <div>
          <div className="mb-1 flex items-center justify-between">
            <span className="text-[11px] font-semibold text-ink-300">
              {t('skillDialog.steps')}
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
              <Plus size={11} /> {t('skillDialog.addStep')}
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
                    placeholder={t('skillDialog.step.placeholder')}
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
                  {tools.map((tool) => {
                    const on = s.recommendedTools.includes(tool.name)
                    return (
                      <button
                        key={tool.name}
                        title={t('skillDialog.tool.title', {
                          description: tool.description,
                          risk: tool.risk,
                        })}
                        onClick={() =>
                          setStep(i, {
                            recommendedTools: on
                              ? s.recommendedTools.filter((n) => n !== tool.name)
                              : [...s.recommendedTools, tool.name],
                          })
                        }
                        className={clsx(
                          'rounded-control border px-1.5 py-0.5 font-mono text-[10px] transition-colors',
                          on
                            ? 'border-accent bg-accent/15 text-accent'
                            : 'hairline bg-ink-850 text-ink-500 hover:text-ink-300',
                          // Risk is shown even unselected: recommending a
                          // destructive tool should never be a casual click.
                          !on && tool.risk === 'destructive' && 'border-danger/30 text-danger/70',
                        )}
                      >
                        {tool.name}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
          <p className="mt-1 text-[10.5px] leading-relaxed text-ink-500">
            {t('skillDialog.toolsNote')}
          </p>
        </div>

        <Field label={t('skillDialog.constraints')} hint={t('skillDialog.constraints.hint')}>
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
            placeholder={t('skillDialog.constraints.placeholder')}
          />
        </Field>

        {existing && (
          <Field label={t('skillDialog.changed')} hint={t('skillDialog.changed.hint')}>
            <input
              className={inputClass}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder={t('skillDialog.changed.placeholder')}
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
  const t = useT()
  const fmt = useFmt()
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
      title={t('skillHistory.title', { name: skill.name })}
      onClose={close}
      wide
      footer={<Button onClick={close}>{t('common.close')}</Button>}
    >
      <div className="space-y-2">
        {versions.length === 0 && (
          <p className="text-[11px] text-ink-500">{t('skillHistory.empty')}</p>
        )}
        {versions.map((v) => (
          <div key={v.id} className="rounded-control border hairline bg-ink-800 p-2.5">
            <div className="flex items-center gap-2">
              <span className="text-[11px] font-medium text-ink-100">v{v.version}</span>
              <Badge>{v.changedBy}</Badge>
              <span className="min-w-0 flex-1 truncate text-[11px] text-ink-400">{v.note}</span>
              <span className="shrink-0 text-[10px] text-ink-600">
                {fmt.dateTime(v.createdAt)}
              </span>
              <Button
                size="sm"
                disabled={busy || v.version === skill.version}
                title={
                  v.version === skill.version
                    ? t('skillHistory.current')
                    : t('skillHistory.restore.title')
                }
                onClick={async () => {
                  setBusy(true)
                  try {
                    await skillApi.rollback(skill.id, v.version)
                    toast('ok', t('skillHistory.rolledBack', { version: v.version }))
                    close()
                  } catch (e) {
                    toast('error', errText(e))
                  } finally {
                    setBusy(false)
                  }
                }}
              >
                <RotateCcw size={11} /> {t('skillHistory.restore')}
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
        <p className="text-[10.5px] leading-relaxed text-ink-500">{t('skillHistory.note')}</p>
      </div>
    </Modal>
  )
}
