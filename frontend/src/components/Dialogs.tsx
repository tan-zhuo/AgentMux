import clsx from 'clsx'
import { Check } from 'lucide-react'
import { useEffect, useState } from 'react'
import { agents as agentApi, errText, servers as serverApi, tree as treeApi } from '../lib/api'
import { themes } from '../lib/themes'
import type { Agent, AuthType, Project, ServerInput, Workspace } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useTheme } from '../store/useTheme'
import { Button, Field, Modal, inputClass } from './ui'

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

function ServerDialog() {
  const dialog = useDialogs((s) => s.dialog)
  const existing = dialog?.kind === 'server' ? dialog.server : undefined
  const servers = useAppStore((s) => s.snapshot.servers)
  const { close, submit, saving, toast } = useDialogPlumbing()

  const [form, setForm] = useState<ServerInput>({
    id: existing?.id ?? '',
    name: existing?.name ?? '',
    host: existing?.host ?? '',
    port: existing?.port ?? 22,
    username: existing?.username ?? '',
    authType: existing?.authType ?? 'agent',
    keyPath: existing?.keyPath ?? '',
    password: null,
    passphrase: null,
    jumpServerId: existing?.jumpServerId ?? null,
    tags: existing?.tags ?? [],
    favorite: existing?.favorite ?? false,
  })
  const [tagText, setTagText] = useState((existing?.tags ?? []).join(', '))
  const [testing, setTesting] = useState(false)

  const set = <K extends keyof ServerInput>(k: K, v: ServerInput[K]) =>
    setForm((f) => ({ ...f, [k]: v }))

  return (
    <Modal
      title={existing ? `Edit ${existing.name}` : 'Add server'}
      onClose={close}
      footer={
        <>
          {existing && (
            <Button
              size="sm"
              disabled={testing}
              onClick={async () => {
                setTesting(true)
                const p = await serverApi.test(existing.id)
                setTesting(false)
                if (p.ok)
                  toast('ok', `${p.latencyMs} ms · ${p.os || '?'} · ${p.hasTmux ? p.tmuxVersion : 'no tmux'}`)
                else toast('error', p.error)
              }}
            >
              Test connection
            </Button>
          )}
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() =>
              submit(
                () =>
                  serverApi.save({
                    ...form,
                    tags: tagText
                      .split(',')
                      .map((t) => t.trim())
                      .filter(Boolean),
                  }),
                existing ? 'Server updated' : 'Server added',
              )
            }
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name">
            <input
              autoFocus
              className={inputClass}
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder="gpu-box-01"
            />
          </Field>
          <Field label="Tags" hint="comma separated">
            <input
              className={inputClass}
              value={tagText}
              onChange={(e) => setTagText(e.target.value)}
              placeholder="gpu, prod"
            />
          </Field>
        </div>

        <div className="grid grid-cols-[2fr_1fr_1.5fr] gap-3">
          <Field label="Host">
            <input
              className={inputClass}
              value={form.host}
              onChange={(e) => set('host', e.target.value)}
              placeholder="10.0.0.12"
            />
          </Field>
          <Field label="Port">
            <input
              type="number"
              className={inputClass}
              value={form.port}
              onChange={(e) => set('port', Number(e.target.value) || 22)}
            />
          </Field>
          <Field label="Username">
            <input
              className={inputClass}
              value={form.username}
              onChange={(e) => set('username', e.target.value)}
              placeholder="ubuntu"
            />
          </Field>
        </div>

        <Field label="Authentication">
          <div className="flex gap-1.5">
            {(['agent', 'key', 'password'] as AuthType[]).map((t) => (
              <Button
                key={t}
                size="sm"
                variant={form.authType === t ? 'primary' : 'ghost'}
                onClick={() => set('authType', t)}
              >
                {t === 'agent' ? 'ssh-agent' : t}
              </Button>
            ))}
          </div>
        </Field>

        {form.authType === 'key' && (
          <>
            <Field label="Private key path" hint="Leave blank to try ~/.ssh/id_ed25519, id_ecdsa, id_rsa">
              <input
                className={inputClass}
                value={form.keyPath}
                onChange={(e) => set('keyPath', e.target.value)}
                placeholder="~/.ssh/id_ed25519"
              />
            </Field>
            <Field
              label="Key passphrase"
              hint={
                existing?.hasPassphrase
                  ? 'Stored. Leave blank to keep it, or type a new one.'
                  : 'Encrypted with a key held in your OS keychain.'
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

        {form.authType === 'password' && (
          <Field
            label="Password"
            hint={
              existing?.hasPassword
                ? 'Stored. Leave blank to keep it, or type a new one.'
                : 'Encrypted with a key held in your OS keychain.'
            }
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

        <Field label="Jump host" hint="Route the connection through another configured server">
          <select
            className={inputClass}
            value={form.jumpServerId ?? ''}
            onChange={(e) => set('jumpServerId', e.target.value || null)}
          >
            <option value="">None (direct)</option>
            {servers
              .filter((s) => s.id !== form.id)
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} ({s.host})
                </option>
              ))}
          </select>
        </Field>

        {existing?.hostKey && (
          <p className="rounded border border-ink-700 bg-ink-800 px-2 py-1.5 text-[11px] text-ink-400">
            Host key pinned on first connection. Clear it from the server detail panel if you
            rotated the server's key.
          </p>
        )}
      </div>
    </Modal>
  )
}

function ProjectDialog() {
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
      title={existing ? `Edit ${existing.name}` : 'New project'}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() => submit(() => treeApi.saveProject(form), 'Project saved')}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Name">
          <input
            autoFocus
            className={inputClass}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="checkout-service"
          />
        </Field>
        <Field label="Description">
          <textarea
            rows={3}
            className={`${inputClass} resize-none`}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </Field>
      </div>
    </Modal>
  )
}

function WorkspaceDialog() {
  const dialog = useDialogs((s) => s.dialog)
  const existing = dialog?.kind === 'workspace' ? dialog.workspace : undefined
  const presetProject = dialog?.kind === 'workspace' ? dialog.projectId : undefined
  const snapshot = useAppStore((s) => s.snapshot)
  const { close, submit, saving } = useDialogPlumbing()

  const [form, setForm] = useState<Workspace>({
    id: existing?.id ?? '',
    projectId: existing?.projectId ?? presetProject ?? snapshot.projects[0]?.id ?? '',
    serverId: existing?.serverId ?? snapshot.servers[0]?.id ?? '',
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
      title={existing ? `Edit ${existing.name}` : 'New workspace'}
      onClose={close}
      wide
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() => submit(() => treeApi.saveWorkspace({ ...form, env: parseEnv(envText) }), 'Workspace saved')}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Project">
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
          <Field label="Server">
            <select
              className={inputClass}
              value={form.serverId}
              onChange={(e) => setForm({ ...form, serverId: e.target.value })}
            >
              {snapshot.servers.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} ({s.host})
                </option>
              ))}
            </select>
          </Field>
        </div>

        <div className="grid grid-cols-[1fr_2fr] gap-3">
          <Field label="Name">
            <input
              autoFocus
              className={inputClass}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="prod checkout"
            />
          </Field>
          <Field label="Remote path" hint="Absolute path on the server">
            <input
              className={inputClass}
              value={form.remotePath}
              onChange={(e) => setForm({ ...form, remotePath: e.target.value })}
              placeholder="/home/ubuntu/work/checkout"
            />
          </Field>
        </div>

        <Field label="Default agent command" hint="e.g. claude, opencode, or your own launcher">
          <input
            className={inputClass}
            value={form.defaultAgentCommand}
            onChange={(e) => setForm({ ...form, defaultAgentCommand: e.target.value })}
            placeholder="claude"
          />
        </Field>

        <Field label="Environment" hint="One KEY=value per line; applied before the agent starts">
          <textarea
            rows={4}
            className={`${inputClass} resize-none font-mono`}
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

function AgentDialog() {
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
    lastSeen: existing?.lastSeen ?? null,
    pid: existing?.pid ?? null,
    progressText: existing?.progressText ?? '',
    createdAt: existing?.createdAt ?? 0,
  })

  // Keep the suggested session name in step with the name until the user edits it.
  const [sessionTouched, setSessionTouched] = useState(!!existing?.tmuxSession)
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
      title={existing ? `Edit ${existing.name}` : 'New agent'}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="primary"
            disabled={saving}
            onClick={() => submit(() => agentApi.save(form), 'Agent saved')}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Workspace">
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

        <Field label="Name">
          <input
            autoFocus
            className={inputClass}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="claude-backend"
          />
        </Field>

        <Field label="Command" hint="Runs inside the tmux pane, in the workspace directory">
          <input
            className={`${inputClass} font-mono`}
            value={form.command}
            onChange={(e) => setForm({ ...form, command: e.target.value })}
            placeholder="claude"
          />
        </Field>

        <Field label="tmux session" hint="Must not contain ':' or '.'">
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
  const diagnostics = useAppStore((s) => s.diagnostics)
  const themeId = useTheme((s) => s.themeId)
  const setTheme = useTheme((s) => s.setTheme)

  return (
    <Modal title="Settings" onClose={close} wide footer={<Button onClick={close}>Close</Button>}>
      <p className="mb-2 text-[11px] font-medium tracking-wide text-ink-300 uppercase">Theme</p>
      <div className="mb-5 grid grid-cols-2 gap-2">
        {themes.map((t) => {
          const selected = t.id === themeId
          return (
            <button
              key={t.id}
              type="button"
              onClick={() => setTheme(t.id)}
              className={clsx(
                'flex flex-col gap-2 rounded-lg border p-2.5 text-left transition-colors',
                selected
                  ? 'border-accent bg-accent/10'
                  : 'border-ink-700 bg-ink-800 hover:border-ink-600',
              )}
            >
              <span className="flex items-center gap-2">
                <span className="text-xs font-medium text-ink-100">{t.name}</span>
                {selected && <Check size={12} className="text-accent" />}
                <span className="ml-auto text-[10px] text-ink-500">{t.mode}</span>
              </span>
              <span className="text-[10.5px] leading-snug text-ink-500">{t.blurb}</span>
              {/* A miniature of the real layout reads better than loose swatches. */}
              <span
                className="flex h-9 overflow-hidden rounded border"
                style={{ borderColor: t.colors['ink-700'], background: t.colors['ink-900'] }}
              >
                <span
                  className="h-full w-1/4 border-r"
                  style={{
                    background: t.colors['ink-900'],
                    borderColor: t.colors['ink-800'],
                  }}
                />
                <span className="h-full flex-1" style={{ background: t.terminal.background }} />
                <span className="flex h-full w-8 flex-col justify-center gap-1 px-1.5">
                  <span
                    className="block h-1 rounded-full"
                    style={{ background: t.colors.accent }}
                  />
                  <span className="block h-1 rounded-full" style={{ background: t.colors.ok }} />
                  <span
                    className="block h-1 rounded-full"
                    style={{ background: t.colors.danger }}
                  />
                </span>
              </span>
            </button>
          )
        })}
      </div>

      <p className="mb-2 text-[11px] font-medium tracking-wide text-ink-300 uppercase">
        Diagnostics
      </p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-[11px]">
        <dt className="text-ink-500">Data directory</dt>
        <dd className="font-mono break-all text-ink-200">{diagnostics?.dataDir ?? '—'}</dd>
        <dt className="text-ink-500">Secret storage</dt>
        <dd className={diagnostics?.keyLocationOk ? 'text-ok' : 'text-warn'}>
          {diagnostics?.keyLocationOk
            ? 'Master key held in the OS keychain'
            : 'OS keychain unavailable — master key is in a 0600 file'}
        </dd>
      </dl>
      <p className="mt-4 text-[11px] leading-relaxed text-ink-400">
        Passwords and key passphrases are encrypted with AES-256-GCM before they are written to the
        local database. Host keys are pinned on first connection and a later mismatch aborts the
        connection.
      </p>
    </Modal>
  )
}
