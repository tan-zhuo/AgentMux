import clsx from 'clsx'
import { Check, Download, Upload } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import {
  agents as agentApi,
  connect as connectApi,
  errText,
  getBackURL,
  getRemoteAddr,
  getShellOrigin,
  isDesktop,
  llm as llmApi,
  servers as serverApi,
  toolkit,
  tree as treeApi,
} from '../lib/api'
import {
  LANGUAGES,
  offsetLabel,
  systemTimeZone,
  timeZones,
  type Lang,
} from '../lib/i18n'
import { themes } from '../lib/themes'
import type {
  DesktopOS,
  Agent,
  AgentChoice,
  AuthType,
  LLMConfig,
  LLMStatus,
  LocalHost,
  Project,
  ServerInput,
  ServerKind,
  TrustLevel,
  Workspace,
} from '../lib/types'
import { isDesktopKind, isLocalKind } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useFmt, useI18n, useT } from '../store/useI18n'
import { useTheme } from '../store/useTheme'
import { DesktopDialog } from './DesktopDialog'
import { SkillDialog, SkillHistoryDialog } from './SkillDialogs'
import { SplitDialog } from './SplitDialog'
import { TransferDialog } from './TransferDialog'
import { UpdateCheckButton, UpdateMirrorField } from './UpdateBanner'
import { Button, Field, Modal, Segmented, inputClass, textareaClass } from './ui'

export function Dialogs() {
  const dialog = useDialogs((s) => s.dialog)
  if (!dialog) return null
  switch (dialog.kind) {
    case 'server':
      return <ServerDialog />
    case 'project':
      return <ProjectDialog />
    case 'workspace':
      return <WorkspaceDialog />
    case 'agent':
      return <AgentDialog />
    case 'settings':
      return <SettingsDialog />
    case 'skill':
      return <SkillDialog />
    case 'skillHistory':
      return <SkillHistoryDialog />
    case 'split':
      return <SplitDialog />
    case 'transfer':
      return <TransferDialog mode={dialog.mode} />
    case 'desktop':
      return <DesktopDialog server={dialog.server} />
  }
}

function useDialogPlumbing() {
  const close = useDialogs((s) => s.close)
  const refreshSnapshot = useAppStore((s) => s.refreshSnapshot)
  const toast = useAppStore((s) => s.toast)
  const [saving, setSaving] = useState(false)

  async function submit(fn: () => Promise<unknown>, okText: string) {
    setSaving(true)
    try {
      await fn()
      await refreshSnapshot()
      toast('ok', okText)
      close()
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setSaving(false)
    }
  }
  return { close, submit, saving, toast }
}

/** Where a system's own remote desktop listens, before anybody edits it. */
function desktopPort(os: DesktopOS): number {
  return os === 'windows' ? 3389 : 5900
}

function ServerDialog() {
  const t = useT()
  const dialog = useDialogs((s) => s.dialog)
  // A host handed in with no id is a starting point rather than an edit: the
  // "add this machine over SSH too" offer fills the address in and leaves the
  // rest of the form to be answered.
  const handed = dialog?.kind === 'server' ? dialog.server : undefined
  const existing = handed?.id ? handed : undefined
  const preset = handed?.id ? undefined : handed
  const servers = useAppStore((s) => s.snapshot.servers)
  const { close, submit, saving, toast } = useDialogPlumbing()

  const [form, setForm] = useState<ServerInput>({
    id: existing?.id ?? '',
    name: existing?.name ?? preset?.name ?? '',
    host: existing?.host ?? preset?.host ?? '',
    port: existing?.port ?? preset?.port ?? 22,
    username: existing?.username ?? preset?.username ?? '',
    authType: existing?.authType ?? preset?.authType ?? 'agent',
    keyPath: existing?.keyPath ?? preset?.keyPath ?? '',
    password: null,
    passphrase: null,
    jumpServerId: existing?.jumpServerId ?? null,
    tags: existing?.tags ?? [],
    favorite: existing?.favorite ?? false,
    trustLevel: existing?.trustLevel ?? 'normal',
  })
  const [tagText, setTagText] = useState((existing?.tags ?? []).join(', '))
  const [testing, setTesting] = useState(false)

  // Which kind of host is being added. Editing never changes it: a machine does
  // not stop being this computer, and the fields it would need do not exist.
  const [kind, setKind] = useState<ServerKind>(existing?.kind ?? preset?.kind ?? 'ssh')
  const local = kind === 'local' || kind === 'localwin'
  // A desktop host is an address and a screen: no shell to log into, so none of
  // the fields that describe how to log in apply to it.
  const desk = kind === 'desktop'
  const [desktopOs, setDesktopOs] = useState<DesktopOS>(existing?.desktopOs || 'windows')
  const [posixSupport, setPosixSupport] = useState<LocalHost | null>(null)
  const [winSupport, setWinSupport] = useState<LocalHost | null>(null)
  const [platform, setPlatform] = useState('')
  useEffect(() => {
    // Asked once, before offering it: on Windows the POSIX answer is "is there
    // a WSL distribution to work in", and there is no point offering otherwise.
    // The native Windows flavour is only asked about — and only shown — where
    // it exists, which is Windows itself.
    let cancelled = false
    serverApi
      .localSupport()
      .then((s) => !cancelled && setPosixSupport(s))
      .catch(() => {})
    serverApi
      .platform()
      .then((p) => {
        if (cancelled) return
        setPlatform(p)
        if (p === 'windows')
          serverApi
            .localWinSupport()
            .then((s) => !cancelled && setWinSupport(s))
            .catch(() => {})
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])
  const support = kind === 'localwin' ? winSupport : posixSupport

  const set = <K extends keyof ServerInput>(k: K, v: ServerInput[K]) =>
    setForm((f) => ({ ...f, [k]: v }))

  return (
    <Modal
      title={existing ? t('dialog.editName', { name: existing.name }) : t('server.addHost')}
      onClose={close}
      footer={
        <>
          {existing && (
            <Button
              disabled={testing}
              onClick={async () => {
                setTesting(true)
                const p = await serverApi.test(existing.id)
                setTesting(false)
                if (p.ok)
                  toast(
                    'ok',
                    t('server.testResult', {
                      ms: p.latencyMs,
                      os: p.os || t('common.unknownOS'),
                      tmux: p.hasTmux ? p.tmuxVersion : t('common.noTmux'),
                    }),
                  )
                else toast('error', p.error)
              }}
            >
              {t('tree.testConnection')}
            </Button>
          )}
          <Button onClick={close}>{t('common.cancel')}</Button>
          <Button
            variant="primary"
            disabled={saving || (local && !existing && !support?.supported)}
            onClick={() =>
              submit(
                () => {
                  const tags = tagText
                    .split(',')
                    .map((t) => t.trim())
                    .filter(Boolean)
                  // A new local host has nothing to fill in, so the backend
                  // creates it: there is exactly one of this machine, and it
                  // knows what to call it.
                  if (local && !existing)
                    return kind === 'localwin'
                      ? serverApi.addLocalWin(form.name)
                      : serverApi.addLocal(form.name)
                  return serverApi.save({
                    ...form,
                    kind,
                    tags,
                    desktopOs: kind === 'desktop' ? desktopOs : '',
                  })
                },
                existing ? t('server.updated') : t('server.added'),
              )
            }
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {!existing && (
          <Field
            label={t('server.whereWorkRuns')}
            hint={
              kind === 'localwin'
                ? t('server.kind.localwinHint')
                : local
                ? t('server.kind.localHint')
                : kind === 'sshwin'
                ? t('server.kind.sshwinHint')
                : kind === 'desktop'
                  ? t('server.kind.desktopHint')
                  : t('server.kind.sshHint')
            }
          >
            <Segmented<ServerKind>
              size="sm"
              value={kind}
              onChange={(v) => {
                setKind(v)
                const s = v === 'localwin' ? winSupport : posixSupport
                if ((v === 'local' || v === 'localwin') && s?.name && !form.name)
                  set('name', s.name)
                if (v === 'desktop') set('port', desktopPort(desktopOs))
                else if (form.port === 3389 || form.port === 5900) set('port', 22)
              }}
              options={[
                {
                  value: 'ssh',
                  label: t('server.kind.remote'),
                  title: t('server.kind.remoteTitle'),
                },
                {
                  value: 'sshwin',
                  label: t('server.kind.sshwin'),
                  title: t('server.kind.sshwinTitle'),
                },
                {
                  value: 'desktop',
                  label: t('server.kind.desktop'),
                  title: t('server.kind.desktopTitle'),
                },
                {
                  value: 'local',
                  // On Windows the POSIX local host is WSL, and saying so is
                  // what makes the difference from the native option legible.
                  label:
                    platform === 'windows'
                      ? t('server.kind.localWsl')
                      : t('server.kind.local'),
                  title: t('server.kind.localTitle'),
                },
                ...(platform === 'windows'
                  ? [
                      {
                        value: 'localwin' as ServerKind,
                        label: t('server.kind.localwin'),
                        title: t('server.kind.localwinTitle'),
                      },
                    ]
                  : []),
              ]}
            />
          </Field>
        )}

        {local && !existing && support && !support.supported && (
          <p className="rounded-control border hairline bg-ink-800 px-2 py-1.5 text-[11px] leading-relaxed text-warn">
            {support.reason}
          </p>
        )}
        {local && !existing && support?.existingId && (
          <p className="rounded-control border hairline bg-ink-800 px-2 py-1.5 text-[11px] leading-relaxed text-ink-400">
            {t('server.alreadyInTree')}
          </p>
        )}

        <div className={local ? '' : 'grid grid-cols-2 gap-3'}>
          <Field label={t('server.name')}>
            <input
              autoFocus
              className={inputClass}
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder={local ? support?.name || t('tree.thisComputer') : 'gpu-box-01'}
            />
          </Field>
          {!local && (
            <Field label={t('server.tags')} hint={t('server.tags.hint')}>
              <input
                className={inputClass}
                value={tagText}
                onChange={(e) => setTagText(e.target.value)}
                placeholder="gpu, prod"
              />
            </Field>
          )}
        </div>

        {desk && (
          <Field label={t('server.desktopOs')} hint={t('server.desktopOs.hint')}>
            <Segmented<DesktopOS>
              size="sm"
              value={desktopOs}
              onChange={(v) => {
                setDesktopOs(v)
                // The port follows the system until somebody types over it,
                // the same way the desktop dialog's does.
                if (form.port === 0 || form.port === 3389 || form.port === 5900)
                  set('port', desktopPort(v))
              }}
              options={[
                { value: 'windows', label: t('server.os.windows'), title: t('server.os.windowsTitle') },
                { value: 'macos', label: t('server.os.macos'), title: t('server.os.macosTitle') },
                { value: 'linux', label: t('server.os.linux'), title: t('server.os.linuxTitle') },
              ]}
            />
          </Field>
        )}

        {local ? (
          <p className="rounded-control border hairline bg-ink-800 px-2 py-1.5 text-[11px] leading-relaxed text-ink-400">
            {t('server.localBlurb')}
          </p>
        ) : desk ? (
          <div className="grid grid-cols-[2fr_1fr] gap-3">
            <Field label={t('server.host')}>
              <input
                className={inputClass}
                value={form.host}
                onChange={(e) => set('host', e.target.value)}
                placeholder="10.0.0.12"
              />
            </Field>
            <Field label={t('server.port')}>
              <input
                type="number"
                className={inputClass}
                value={form.port}
                onChange={(e) => set('port', Number(e.target.value) || desktopPort(desktopOs))}
              />
            </Field>
          </div>
        ) : (
        <div className="grid grid-cols-[2fr_1fr_1.5fr] gap-3">
          <Field label={t('server.host')}>
            <input
              className={inputClass}
              value={form.host}
              onChange={(e) => set('host', e.target.value)}
              placeholder="10.0.0.12"
            />
          </Field>
          <Field label={t('server.port')}>
            <input
              type="number"
              className={inputClass}
              value={form.port}
              onChange={(e) => set('port', Number(e.target.value) || 22)}
            />
          </Field>
          <Field label={t('server.username')}>
            <input
              className={inputClass}
              value={form.username}
              onChange={(e) => set('username', e.target.value)}
              placeholder="ubuntu"
            />
          </Field>
        </div>
        )}

        {!desk && (
        <Field label={t('server.trust')} hint={t('server.trust.hint')}>
          <Segmented<TrustLevel>
            size="sm"
            value={form.trustLevel ?? 'normal'}
            onChange={(v) => set('trustLevel', v)}
            options={[
              {
                value: 'trusted',
                label: t('server.trust.trusted'),
                title: t('server.trust.trustedTitle'),
              },
              {
                value: 'normal',
                label: t('server.trust.normal'),
                title: t('server.trust.normalTitle'),
              },
              {
                value: 'production',
                label: t('server.trust.production'),
                title: t('server.trust.productionTitle'),
              },
            ]}
          />
        </Field>
        )}

        {!local && !desk && (
        <Field label={t('server.auth')}>
          <Segmented<AuthType>
            size="sm"
            value={form.authType}
            onChange={(v) => set('authType', v)}
            options={[
              { value: 'agent', label: 'ssh-agent' },
              { value: 'key', label: t('server.auth.key') },
              { value: 'password', label: t('server.auth.password') },
            ]}
          />
        </Field>
        )}

        {!local && !desk && form.authType === 'key' && (
          <>
            <Field label={t('server.keyPath')} hint={t('server.keyPath.hint')}>
              <input
                className={inputClass}
                value={form.keyPath}
                onChange={(e) => set('keyPath', e.target.value)}
                placeholder="~/.ssh/id_ed25519"
              />
            </Field>
            <Field
              label={t('server.passphrase')}
              hint={
                existing?.hasPassphrase
                  ? t('server.secret.stored')
                  : t('server.secret.encrypted')
              }
            >
              <input
                type="password"
                className={inputClass}
                value={form.passphrase ?? ''}
                onChange={(e) => set('passphrase', e.target.value)}
                placeholder={existing?.hasPassphrase ? '••••••••' : ''}
              />
            </Field>
          </>
        )}

        {!local && !desk && form.authType === 'password' && (
          <Field
            label={t('server.auth.password')}
            hint={existing?.hasPassword ? t('server.secret.stored') : t('server.secret.encrypted')}
          >
            <input
              type="password"
              className={inputClass}
              value={form.password ?? ''}
              onChange={(e) => set('password', e.target.value)}
              placeholder={existing?.hasPassword ? '••••••••' : ''}
            />
          </Field>
        )}

        {!local && !desk && (
        <Field label={t('server.jumpHost')} hint={t('server.jumpHost.hint')}>
          <select
            className={inputClass}
            value={form.jumpServerId ?? ''}
            onChange={(e) => set('jumpServerId', e.target.value || null)}
          >
            <option value="">{t('server.jumpHost.none')}</option>
            {servers
              // A jump host is a machine an SSH connection is made through,
              // which rules out this computer and anything with no SSH on it.
              .filter((s) => s.id !== form.id && !isLocalKind(s.kind) && !isDesktopKind(s.kind))
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} ({s.host})
                </option>
              ))}
          </select>
        </Field>
        )}

        {existing?.hostKey && (
          <p className="rounded-control border hairline bg-ink-800 px-2 py-1.5 text-[11px] text-ink-400">
            {t('server.hostKeyPinned')}
          </p>
        )}
      </div>
    </Modal>
  )
}

function ProjectDialog() {
  const t = useT()
  const dialog = useDialogs((s) => s.dialog)
  const existing = dialog?.kind === 'project' ? dialog.project : undefined
  const { close, submit, saving } = useDialogPlumbing()

  const [form, setForm] = useState<Project>({
    id: existing?.id ?? '',
    name: existing?.name ?? '',
    description: existing?.description ?? '',
    folderId: existing?.folderId ?? null,
    sort: existing?.sort ?? 0,
    createdAt: existing?.createdAt ?? 0,
  })

  return (
    <Modal
      title={existing ? t('dialog.editName', { name: existing.name }) : t('project.new')}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>{t('common.cancel')}</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() => submit(() => treeApi.saveProject(form), t('project.saved'))}
          >
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label={t('server.name')}>
          <input
            autoFocus
            className={inputClass}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="checkout-service"
          />
        </Field>
        <Field label={t('project.description')}>
          <textarea
            rows={3}
            className={`${textareaClass} resize-none`}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </Field>
      </div>
    </Modal>
  )
}

function WorkspaceDialog() {
  const t = useT()
  const dialog = useDialogs((s) => s.dialog)
  const existing = dialog?.kind === 'workspace' ? dialog.workspace : undefined
  const presetProject = dialog?.kind === 'workspace' ? dialog.projectId : undefined
  const snapshot = useAppStore((s) => s.snapshot)
  const { close, submit, saving } = useDialogPlumbing()

  const [form, setForm] = useState<Workspace>({
    id: existing?.id ?? '',
    projectId: existing?.projectId ?? presetProject ?? snapshot.projects[0]?.id ?? '',
    serverId:
      existing?.serverId ??
      // A workspace is a directory somebody works in, which a host with no
      // shell does not have. Defaulting to one would put the wrong answer in
      // the field before the question was read.
      snapshot.servers.find((s) => !isDesktopKind(s.kind))?.id ??
      '',
    name: existing?.name ?? '',
    remotePath: existing?.remotePath ?? '',
    defaultTmuxSession: existing?.defaultTmuxSession ?? '',
    defaultAgentCommand: existing?.defaultAgentCommand ?? '',
    env: existing?.env ?? {},
    sort: existing?.sort ?? 0,
  })
  const [envText, setEnvText] = useState(
    Object.entries(existing?.env ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join('\n'),
  )

  return (
    <Modal
      title={existing ? t('dialog.editName', { name: existing.name }) : t('workspace.new')}
      onClose={close}
      wide
      footer={
        <>
          <Button onClick={close}>{t('common.cancel')}</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() =>
              submit(
                () => treeApi.saveWorkspace({ ...form, env: parseEnv(envText) }),
                t('workspace.saved'),
              )
            }
          >
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('workspace.project')}>
            <select
              className={inputClass}
              value={form.projectId}
              onChange={(e) => setForm({ ...form, projectId: e.target.value })}
            >
              {snapshot.projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('workspace.server')}>
            <select
              className={inputClass}
              value={form.serverId}
              onChange={(e) => setForm({ ...form, serverId: e.target.value })}
            >
              {snapshot.servers
                .filter((s) => !isDesktopKind(s.kind))
                .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} (
                  {s.kind === 'local'
                    ? t('tree.thisComputer')
                    : s.kind === 'localwin'
                    ? t('tree.thisComputerWin')
                    : s.host}
                  )
                </option>
              ))}
            </select>
          </Field>
        </div>

        <div className="grid grid-cols-[1fr_2fr] gap-3">
          <Field label={t('server.name')}>
            <input
              autoFocus
              className={inputClass}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="prod checkout"
            />
          </Field>
          <Field label={t('workspace.remotePath')} hint={t('workspace.remotePath.hint')}>
            <input
              className={inputClass}
              value={form.remotePath}
              onChange={(e) => setForm({ ...form, remotePath: e.target.value })}
              placeholder="/home/ubuntu/work/checkout"
            />
          </Field>
        </div>

        <Field label={t('workspace.defaultCommand')} hint={t('workspace.defaultCommand.hint')}>
          <input
            className={inputClass}
            value={form.defaultAgentCommand}
            onChange={(e) => setForm({ ...form, defaultAgentCommand: e.target.value })}
            placeholder="claude"
          />
        </Field>

        <Field label={t('workspace.env')} hint={t('workspace.env.hint')}>
          <textarea
            rows={4}
            className={`${textareaClass} resize-none font-mono`}
            value={envText}
            onChange={(e) => setEnvText(e.target.value)}
            placeholder={'ANTHROPIC_API_KEY=sk-...\nNODE_ENV=development'}
          />
        </Field>
      </div>
    </Modal>
  )
}

function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const i = trimmed.indexOf('=')
    if (i <= 0) continue
    out[trimmed.slice(0, i).trim()] = trimmed.slice(i + 1).trim()
  }
  return out
}

/** The select's value for "none of these — I'll type the command myself". */
const CUSTOM_AGENT = '__custom__'

function AgentDialog() {
  const t = useT()
  const dialog = useDialogs((s) => s.dialog)
  const existing = dialog?.kind === 'agent' ? dialog.agent : undefined
  const presetWorkspace = dialog?.kind === 'agent' ? dialog.workspaceId : undefined
  const presetCommand = dialog?.kind === 'agent' ? dialog.presetCommand : undefined
  const snapshot = useAppStore((s) => s.snapshot)
  const { close, submit, saving } = useDialogPlumbing()

  const initialWs =
    existing?.workspaceId ?? presetWorkspace ?? snapshot.workspaces[0]?.id ?? ''
  const [form, setForm] = useState<Agent>({
    id: existing?.id ?? '',
    workspaceId: initialWs,
    name: existing?.name ?? '',
    command:
      existing?.command ??
      presetCommand ??
      snapshot.workspaces.find((w) => w.id === initialWs)?.defaultAgentCommand ??
      '',
    tmuxSession: existing?.tmuxSession ?? '',
    tmuxWindow: existing?.tmuxWindow ?? '',
    tmuxPaneId: existing?.tmuxPaneId ?? '',
    status: existing?.status ?? 'unknown',
    activity: existing?.activity ?? '',
    attention: existing?.attention ?? '',
    lastSeen: existing?.lastSeen ?? null,
    pid: existing?.pid ?? null,
    progressText: existing?.progressText ?? '',
    createdAt: existing?.createdAt ?? 0,
  })

  // The agent CLIs the workspace's server can actually run — the same list the
  // file browser's launcher offers. Picking a name people know beats recalling
  // the command that starts it, and the two places should ask the same way.
  const serverId = snapshot.workspaces.find((w) => w.id === form.workspaceId)?.serverId ?? ''
  const [choices, setChoices] = useState<AgentChoice[] | null>(null)
  const [custom, setCustom] = useState(false)

  useEffect(() => {
    if (!serverId) {
      setChoices([])
      setCustom(true)
      return
    }
    let cancelled = false
    setChoices(null)
    toolkit
      .installedAgents(serverId)
      .then((found) => {
        if (cancelled) return
        setChoices(found)
        // A command already in hand — an agent being edited, a workspace
        // default — keeps the field it came from unless one of the detected
        // agents owns it. An empty one starts on the first thing installed, so
        // the common case is a name, a Save, and nothing to remember.
        setForm((f) => {
          if (f.command.trim()) {
            setCustom(!found.some((c) => c.command === f.command))
            return f
          }
          const first = found[0]
          setCustom(!first)
          return first ? { ...f, command: first.command } : f
        })
      })
      .catch(() => {
        if (cancelled) return
        setChoices([])
        setCustom(true)
      })
    return () => {
      cancelled = true
    }
  }, [serverId])

  const selected = choices?.find((c) => c.command === form.command)

  // Keep the suggested session name in step with the name until the user edits it.
  const [sessionTouched, setSessionTouched] = useState(!!existing?.tmuxSession)

  // A session the app named itself goes on following the agent's name: the save
  // renames the live tmux session to match, so watching the field change as the
  // name is typed is the truth rather than a suggestion. A session the user
  // typed is theirs and stays where it is.
  useEffect(() => {
    if (!existing?.tmuxSession) return
    let cancelled = false
    agentApi
      .suggestSession(existing.workspaceId, existing.name)
      .then((s) => {
        if (!cancelled && s === existing.tmuxSession) setSessionTouched(false)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [existing?.workspaceId, existing?.name, existing?.tmuxSession])
  useEffect(() => {
    if (sessionTouched || !form.workspaceId || !form.name.trim()) return
    let cancelled = false
    agentApi
      .suggestSession(form.workspaceId, form.name)
      .then((s) => {
        if (!cancelled) setForm((f) => ({ ...f, tmuxSession: s }))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [form.workspaceId, form.name, sessionTouched])

  return (
    <Modal
      title={existing ? t('dialog.editName', { name: existing.name }) : t('agent.new')}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>{t('common.cancel')}</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() => submit(() => agentApi.save(form), t('agent.saved'))}
          >
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label={t('agent.workspace')}>
          <select
            className={inputClass}
            value={form.workspaceId}
            onChange={(e) => {
              const ws = snapshot.workspaces.find((w) => w.id === e.target.value)
              setForm((f) => ({
                ...f,
                workspaceId: e.target.value,
                command: f.command || ws?.defaultAgentCommand || '',
              }))
            }}
          >
            {snapshot.workspaces.map((w) => {
              const project = snapshot.projects.find((p) => p.id === w.projectId)
              return (
                <option key={w.id} value={w.id}>
                  {project?.name ?? '?'} / {w.name}
                </option>
              )
            })}
          </select>
        </Field>

        <Field
          label={t('agent.type')}
          hint={
            choices === null
              ? t('agent.type.detecting')
              : choices.length === 0
                ? t('agent.type.none')
                : selected
                  ? selected.command
                  : undefined
          }
        >
          <select
            className={inputClass}
            disabled={choices === null}
            value={custom || !selected ? CUSTOM_AGENT : selected.id}
            onChange={(e) => {
              const pick = choices?.find((c) => c.id === e.target.value)
              if (!pick) {
                setCustom(true)
                return
              }
              setCustom(false)
              // The agent's own id is the name people would have typed here
              // anyway, and an empty name is the only one worth guessing at.
              setForm((f) => ({ ...f, command: pick.command, name: f.name || pick.id }))
            }}
          >
            {choices === null && <option value={CUSTOM_AGENT}>{t('agent.type.detecting')}</option>}
            {choices?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
                {c.version ? ` · ${c.version}` : ''}
              </option>
            ))}
            {choices !== null && <option value={CUSTOM_AGENT}>{t('agent.type.custom')}</option>}
          </select>
        </Field>

        {(custom || (choices !== null && !selected)) && (
          <Field label={t('agent.command')} hint={t('agent.command.hint')}>
            <input
              className={`${inputClass} font-mono`}
              value={form.command}
              onChange={(e) => setForm({ ...form, command: e.target.value })}
              placeholder="claude"
            />
          </Field>
        )}

        <Field label={t('server.name')}>
          <input
            autoFocus
            className={inputClass}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="claude-backend"
          />
        </Field>

        <Field label={t('agent.tmuxSession')} hint={t('agent.tmuxSession.hint')}>
          <input
            className={`${inputClass} font-mono`}
            value={form.tmuxSession}
            onChange={(e) => {
              setSessionTouched(true)
              setForm({ ...form, tmuxSession: e.target.value })
            }}
            placeholder="agentmux/project/agent"
          />
        </Field>
      </div>
    </Modal>
  )
}

function SettingsDialog() {
  const { close } = useDialogPlumbing()
  const t = useT()
  const diagnostics = useAppStore((s) => s.diagnostics)
  const themeId = useTheme((s) => s.themeId)
  const setTheme = useTheme((s) => s.setTheme)

  return (
    <Modal
      title={t('settings.title')}
      onClose={close}
      wide
      footer={<Button onClick={close}>{t('common.close')}</Button>}
    >
      <LanguageSettings />

      <ConnectSettings />

      <p className="mb-2 text-[11px] font-semibold text-ink-300">{t('settings.theme')}</p>
      <div className="mb-5 grid grid-cols-2 gap-2">
        {themes.map((theme) => {
          const selected = theme.id === themeId
          return (
            <button
              key={theme.id}
              type="button"
              onClick={() => setTheme(theme.id)}
              className={clsx(
                'flex flex-col gap-2 rounded-card border p-2.5 text-left transition-colors',
                selected
                  ? 'border-accent bg-accent/10'
                  : 'hairline bg-ink-800 hover:border-ink-600',
              )}
            >
              <span className="flex items-center gap-2">
                <span className="text-xs font-medium text-ink-100">{theme.name}</span>
                {selected && <Check size={12} className="text-accent" />}
                <span className="ml-auto text-[10px] text-ink-500">
                  {theme.mode === 'dark' ? t('theme.dark') : t('theme.light')}
                </span>
              </span>
              <span className="text-[10.5px] leading-snug text-ink-500">{t(theme.blurb)}</span>
              {/* A miniature of the real layout reads better than loose swatches. */}
              <span
                className="flex h-9 overflow-hidden rounded-control border"
                style={{ borderColor: theme.colors['ink-700'], background: theme.colors['ink-900'] }}
              >
                <span
                  className="h-full w-1/4 border-r"
                  style={{
                    background: theme.colors['ink-900'],
                    borderColor: theme.colors['ink-800'],
                  }}
                />
                <span className="h-full flex-1" style={{ background: theme.terminal.background }} />
                <span className="flex h-full w-8 flex-col justify-center gap-1 px-1.5">
                  <span
                    className="block h-1 rounded-capsule"
                    style={{ background: theme.colors.accent }}
                  />
                  <span
                    className="block h-1 rounded-capsule"
                    style={{ background: theme.colors.ok }}
                  />
                  <span
                    className="block h-1 rounded-capsule"
                    style={{ background: theme.colors.danger }}
                  />
                </span>
              </span>
            </button>
          )
        })}
      </div>

      <LocalModelSettings />

      <TransferSettings />

      <p className="mb-2 text-[11px] font-semibold text-ink-300">{t('settings.diagnostics')}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-[11px]">
        <dt className="text-ink-500">{t('settings.version')}</dt>
        <dd className="flex items-center gap-2 font-mono text-ink-200">
          {diagnostics?.version ?? t('common.none')}
          <UpdateCheckButton current={diagnostics?.version ?? ''} />
        </dd>
        <dt className="self-center text-ink-500">{t('update.mirror')}</dt>
        <dd>
          <UpdateMirrorField />
        </dd>
        <dt className="text-ink-500">{t('settings.dataDir')}</dt>
        <dd className="font-mono break-all text-ink-200">
          {diagnostics?.dataDir ?? t('common.none')}
        </dd>
        <dt className="text-ink-500">{t('settings.secretStorage')}</dt>
        <dd className={diagnostics?.keyLocationOk ? 'text-ok' : 'text-warn'}>
          {diagnostics?.keyLocationOk
            ? t('settings.secret.keychain')
            : t('settings.secret.file')}
        </dd>
      </dl>
      <p className="mt-4 text-[11px] leading-relaxed text-ink-400">{t('settings.security.blurb')}</p>
    </Modal>
  )
}

/** Mirrors the launch page's normalize: scheme defaulted, trailing slash off. */
function normalizeAddr(v: string): string {
  v = v.trim().replace(/\/+$/, '')
  if (!v) return ''
  if (!/^https?:\/\//.test(v)) v = 'http://' + v
  return v
}

/**
 * Which core this window is looking at: the one running inside this app, or a
 * remote `agentmux --serve`.
 *
 * It only appears where a shell exists to do the switching — the Android app,
 * which routes the choice through its launch page, and the desktop app, whose
 * Go side re-points the window: reached over the Wails bridge from the local
 * page, and over the loopback control URL it handed us from a remote one. A
 * plain browser switches with its address bar and sees nothing here.
 */
function ConnectSettings() {
  const t = useT()
  const shell = getShellOrigin()
  const back = getBackURL()
  // What this page is right now. A desktop remote page carries the back-URL;
  // the phone shell boots its own core on 127.0.0.1 and serves from anywhere
  // else; the Wails-served page is by definition the local core.
  const remoteNow =
    back !== '' ||
    (shell !== '' &&
      !isDesktop &&
      window.location.hostname !== '127.0.0.1' &&
      window.location.hostname !== 'localhost')

  // What the current remote actually is: a page behind the desktop's pinned
  // proxy has a loopback origin, and the real address travels in the hash.
  const currentAddr = (back && getRemoteAddr()) || window.location.origin

  const [mode, setMode] = useState<'local' | 'remote'>(remoteNow ? 'remote' : 'local')
  const [addr, setAddr] = useState(remoteNow ? currentAddr : '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  // A self-signed serve's certificate fingerprint, waiting for the user to
  // compare it with the one the server printed and say "trust it".
  const [pinFp, setPinFp] = useState('')

  // The desktop's local page prefills the last remote address it was pointed
  // at, so switching back and forth does not mean retyping it.
  useEffect(() => {
    if (!isDesktop) return
    connectApi
      .remoteAddr()
      .then((a) => {
        if (a) setAddr((cur) => cur || a)
      })
      .catch(() => {})
  }, [])

  if (!shell && !back && !isDesktop) return null

  const target = normalizeAddr(addr)
  const changed =
    mode === 'remote' ? target !== '' && (!remoteNow || target !== currentAddr) : remoteNow

  // accepted is the fingerprint the user just agreed to trust, '' otherwise.
  const apply = async (accepted: string) => {
    setErr('')
    setBusy(true)
    try {
      if (shell) {
        // The launch page owns the choice on the phone: hand it over and let
        // it persist the mode, then boot or connect accordingly — including
        // the certificate-trust conversation for a self-signed serve.
        const q = mode === 'remote' ? `?mode=remote&addr=${encodeURIComponent(target)}` : '?mode=local'
        window.location.href = `${shell}/${q}`
        return
      }
      if (back) {
        // A remote page cannot reach the local Go over the Wails bridge — its
        // runtime posts to this page's origin — so the desktop app listens on
        // its loopback for exactly this request.
        const q =
          mode === 'remote'
            ? `&mode=remote&addr=${encodeURIComponent(target)}` +
              (accepted ? `&pin=${encodeURIComponent(accepted)}` : '')
            : '&mode=local'
        const res = await fetch(back + q)
        if (res.status === 428) {
          // The address answered TLS with a certificate nobody vouches for;
          // the answer is its fingerprint, and the decision is the user's.
          const probe = (await res.json()) as { needsPin?: boolean; fingerprint?: string; error?: string }
          if (probe.needsPin && probe.fingerprint) {
            setPinFp(probe.fingerprint)
            setBusy(false)
            return
          }
          throw new Error(probe.error || t('connect.control.error'))
        }
        if (!res.ok) throw new Error((await res.text().catch(() => '')) || t('connect.control.error'))
        return
      }
      if (!accepted) {
        const probe = await connectApi.probe(target)
        if (probe.needsPin) {
          setPinFp(probe.fingerprint)
          setBusy(false)
          return
        }
        if (!probe.ok) throw new Error(probe.error)
      }
      await connectApi.remote(target, accepted)
    } catch (e) {
      setErr(errText(e))
      setBusy(false)
    }
  }

  return (
    <>
      <p className="mb-2 text-[11px] font-semibold text-ink-300">{t('settings.connect')}</p>
      <div className="mb-5 space-y-2">
        <Segmented
          size="sm"
          value={mode}
          onChange={(m) => {
            setMode(m)
            setPinFp('')
          }}
          options={[
            { value: 'local', label: t('connect.local') },
            { value: 'remote', label: t('connect.remote') },
          ]}
        />
        {mode === 'remote' && (
          <Field label={t('connect.addr')} hint={t('connect.addr.hint')}>
            <input
              className={`${inputClass} font-mono`}
              value={addr}
              onChange={(e) => {
                setAddr(e.target.value)
                setPinFp('')
              }}
              placeholder="http://192.168.1.10:8642"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
            />
          </Field>
        )}
        <p className="text-[11px] leading-relaxed text-ink-400">
          {mode === 'remote' ? t('connect.remote.blurb') : t('connect.local.blurb')}
        </p>
        {pinFp ? (
          <div className="space-y-2 rounded-card border hairline bg-ink-800 p-2.5">
            <p className="text-[11px] font-semibold text-ink-200">{t('connect.fingerprint')}</p>
            <p className="font-mono text-[10.5px] leading-relaxed break-all text-ink-100">{pinFp}</p>
            <p className="text-[11px] leading-relaxed text-ink-400">{t('connect.fingerprint.blurb')}</p>
            <Button variant="primary" disabled={busy} onClick={() => void apply(pinFp)}>
              {busy ? t('connect.applying') : t('connect.trust')}
            </Button>
          </div>
        ) : (
          changed && (
            <Button variant="primary" disabled={busy} onClick={() => void apply('')}>
              {busy ? t('connect.applying') : t('connect.apply')}
            </Button>
          )
        )}
        {err && <p className="text-[11px] text-danger">{err}</p>}
      </div>
    </>
  )
}

/**
 * Moving this installation somewhere else.
 *
 * It sits in Settings rather than in the tree's own menus because it is about
 * the installation rather than about any one host: what leaves here is every
 * host at once, and where it lands is a machine that has none.
 */
function TransferSettings() {
  const t = useT()
  const open = useDialogs((s) => s.open)

  return (
    <div className="mb-5">
      <p className="mb-2 text-[11px] font-semibold text-ink-300">{t('settings.transfer')}</p>
      <p className="mb-2 text-[11px] leading-relaxed text-ink-400">{t('settings.transfer.blurb')}</p>
      <div className="flex gap-1.5">
        <Button size="sm" onClick={() => open({ kind: 'transfer', mode: 'export' })}>
          <Download size={11} /> {t('settings.transfer.export')}
        </Button>
        <Button size="sm" onClick={() => open({ kind: 'transfer', mode: 'import' })}>
          <Upload size={11} /> {t('settings.transfer.import')}
        </Button>
      </div>
    </div>
  )
}

/**
 * Language and time zone.
 *
 * They sit together because they answer the same question — how this app should
 * read to the person in front of it — and because the zone is the half people
 * forget: an agent's "seen 09:14" is a lie if the host is in Tokyo and you are
 * not. The zone is chosen, not inferred from the language, since working in
 * English from Shanghai is at least as common as the reverse.
 */
function LanguageSettings() {
  const t = useT()
  const fmt = useFmt()
  const lang = useI18n((s) => s.lang)
  const setLang = useI18n((s) => s.setLang)
  const tz = useI18n((s) => s.tz)
  const setTz = useI18n((s) => s.setTz)
  // Built once: on a runtime that knows every zone this is a 400-entry list.
  const zones = useMemo(() => timeZones(), [])
  const here = systemTimeZone()

  return (
    <div className="mb-5 grid grid-cols-2 gap-3">
      <Field label={t('settings.language')} hint={t('settings.language.hint')}>
        <select
          className={inputClass}
          value={lang}
          onChange={(e) => setLang(e.target.value as Lang)}
        >
          {LANGUAGES.map((l) => (
            <option key={l.id} value={l.id}>
              {l.native}
            </option>
          ))}
        </select>
      </Field>
      <Field
        label={t('settings.timezone')}
        hint={t('settings.timezone.now', { time: fmt.dateTime(Math.floor(Date.now() / 1000)) })}
      >
        <select className={inputClass} value={tz} onChange={(e) => setTz(e.target.value)}>
          <option value="">
            {t('settings.timezone.system')} — {here} {offsetLabel(here)}
          </option>
          {zones.map((z) => (
            <option key={z} value={z}>
              {z} {offsetLabel(z)}
            </option>
          ))}
        </select>
      </Field>
    </div>
  )
}

/**
 * The local model runtime.
 *
 * AgentMux does not manage Ollama — it does not start it or pull models. What
 * it owes the user is an honest account of what is there, because "connected,
 * but the model you named is not installed" is the usual way this is broken and
 * it is invisible from anywhere else in the app.
 */
function LocalModelSettings() {
  const t = useT()
  const toast = useAppStore((s) => s.toast)
  const [cfg, setCfg] = useState<LLMConfig | null>(null)
  const [status, setStatus] = useState<LLMStatus | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const c = await llmApi.config()
        if (!alive) return
        setCfg(c)
        const s = await llmApi.status()
        if (alive) setStatus(s)
      } catch (e) {
        if (alive) toast('error', errText(e))
      }
    })()
    return () => {
      alive = false
    }
  }, [toast])

  if (!cfg) return null

  const set = <K extends keyof LLMConfig>(k: K, v: LLMConfig[K]) =>
    setCfg((c) => (c ? { ...c, [k]: v } : c))

  // Only embedding models are offered for the embedding slot, by name: picking
  // a chat model there produces vectors that are technically valid and useless.
  const embedCandidates = (status?.models ?? []).filter((m) =>
    /embed|bge|nomic|gte|minilm/i.test(m.name),
  )
  const chatCandidates = (status?.models ?? []).filter((m) => !embedCandidates.includes(m))

  return (
    <>
      <p className="mb-2 text-[11px] font-semibold text-ink-300">{t('llm.title')}</p>
      <div className="mb-2 space-y-2">
        <Field label={t('llm.address')} hint={t('llm.address.hint')}>
          <input
            value={cfg.baseUrl}
            onChange={(e) => set('baseUrl', e.target.value)}
            placeholder="http://127.0.0.1:11434"
            className={inputClass}
          />
        </Field>
        <div className="grid grid-cols-2 gap-2">
          <Field label={t('llm.chatModel')}>
            <input
              list="agentmux-chat-models"
              value={cfg.chatModel}
              onChange={(e) => set('chatModel', e.target.value)}
              className={inputClass}
            />
            <datalist id="agentmux-chat-models">
              {chatCandidates.map((m) => (
                <option key={m.name} value={m.name} />
              ))}
            </datalist>
          </Field>
          <Field label={t('llm.embedModel')} hint={t('llm.embedModel.hint')}>
            <input
              list="agentmux-embed-models"
              value={cfg.embedModel}
              onChange={(e) => set('embedModel', e.target.value)}
              className={inputClass}
            />
            <datalist id="agentmux-embed-models">
              {embedCandidates.map((m) => (
                <option key={m.name} value={m.name} />
              ))}
            </datalist>
          </Field>
        </div>

        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="primary"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              try {
                setStatus(await llmApi.saveConfig(cfg))
                toast('ok', t('llm.saved'))
              } catch (e) {
                toast('error', errText(e))
              } finally {
                setBusy(false)
              }
            }}
          >
            {t('llm.saveAndTest')}
          </Button>
          {status && (
            <span
              className={clsx(
                'text-[11px]',
                status.reachable ? 'text-ok' : 'text-danger',
              )}
            >
              {status.reachable ? `Ollama ${status.version}` : t('llm.notReachable')}
            </span>
          )}
          {status?.reachable && (
            <span className="flex gap-1.5">
              <span
                className={clsx(
                  'text-[11px]',
                  status.chatModelReady ? 'text-ink-500' : 'text-warn',
                )}
              >
                {status.chatModelReady ? t('llm.planningOk') : t('llm.planningMissing')}
              </span>
              <span
                className={clsx(
                  'text-[11px]',
                  status.embedModelReady ? 'text-ink-500' : 'text-warn',
                )}
              >
                {status.embedModelReady ? t('llm.embeddingOk') : t('llm.embeddingMissing')}
              </span>
            </span>
          )}
        </div>

        {status?.hint && (
          <p className="rounded-control border hairline bg-ink-800 px-2.5 py-2 font-mono text-[11px] text-ink-300">
            {status.hint}
          </p>
        )}
      </div>
      <p className="mb-5 text-[11px] leading-relaxed text-ink-400">{t('llm.privacy')}</p>
    </>
  )
}
