import { Call, Events } from '@wailsio/runtime'
import { httpCall, isDesktop, sseOn } from './webTransport'

export { isDesktop }
import type {
  Agent,
  ConfigImport,
  ConfigManifest,
  ConnStatus,
  DesktopEndpoint,
  DesktopOffer,
  DesktopSession,
  Diagnostics,
  ExportOptions,
  Folder,
  LocalHost,
  Probe,
  Project,
  Receipt,
  Server,
  ServerInput,
  ShellInfo,
  Snapshot,
  StartResult,
  TerminalTab,
  TmuxInfo,
  TmuxPane,
  TmuxServerView,
  TmuxSession,
  Tool,
  ToolReport,
  InstallStarted,
  Presence,
  Workspace,
  Listing,
  FileContent,
  Transfer,
  MetricSample,
  HardwareInfo,
  UpdateInfo,
  UpdateResult,
  DetachedTab,
  AgentChoice,
  QuickLaunch,
  BroadcastTarget,
  Memory,
  MemoryFilter,
  MemoryHit,
  MemoryQuery,
  MemoryStats,
  PutResult,
  LLMConfig,
  LLMModel,
  LLMStatus,
  ImportResult,
  Skill,
  SkillFilter,
  SkillMatch,
  SkillQuery,
  SkillStats,
  SkillVersion,
  ToolMeta,
  Approval,
  OrchConfig,
  OrchStatus,
  Run,
  Step,
} from './types'

// Wails binds methods as "<go package path>.<Type>.<Method>".
const PKG = 'agentmux/internal/app'

function call<T>(service: string, method: string, ...args: unknown[]): Promise<T> {
  const name = `${PKG}.${service}.${method}`
  if (isDesktop) return Call.ByName(name, ...args) as unknown as Promise<T>
  return httpCall<T>(name, args)
}

/** Subscribe to a backend event. Returns an unsubscribe function. */
export function on<T>(event: string, cb: (data: T) => void): () => void {
  if (isDesktop) return Events.On(event, (e: { data: T }) => cb(e.data))
  return sseOn(event, cb)
}

export const servers = {
  list: () => call<Server[]>('ServerService', 'List'),
  get: (id: string) => call<Server>('ServerService', 'Get', id),
  save: (input: ServerInput) => call<Server>('ServerService', 'Save', input),
  /** Whether this computer can be added as a host, and whether it already is. */
  localSupport: () => call<LocalHost>('ServerService', 'LocalSupport'),
  /** Adds this computer as a host. Idempotent: there is only one of it. */
  addLocal: (name: string) => call<Server>('ServerService', 'AddLocal', name),
  /** Whether this computer's native Windows side can be added as its own host. */
  localWinSupport: () => call<LocalHost>('ServerService', 'LocalWinSupport'),
  /** Adds the native Windows side of this computer as a host. Idempotent. */
  addLocalWin: (name: string) => call<Server>('ServerService', 'AddLocalWin', name),
  /** The OS this build runs on, for offering only the host flavours it has. */
  platform: () => call<string>('ServerService', 'Platform'),
  remove: (id: string) => call<void>('ServerService', 'Delete', id),
  test: (id: string) => call<Probe>('ServerService', 'Test', id),
  connect: (id: string) => call<void>('ServerService', 'Connect', id),
  disconnect: (id: string) => call<void>('ServerService', 'Disconnect', id),
  connections: () => call<ConnStatus[]>('ServerService', 'Connections'),
  clearHostKey: (id: string) => call<void>('ServerService', 'ClearHostKey', id),
  diagnostics: () => call<Diagnostics>('ServerService', 'Diagnostics'),
}

export const tree = {
  snapshot: () => call<Snapshot>('TreeService', 'Snapshot'),
  saveFolder: (f: Folder) => call<Folder>('TreeService', 'SaveFolder', f),
  deleteFolder: (id: string) => call<void>('TreeService', 'DeleteFolder', id),
  saveProject: (p: Project) => call<Project>('TreeService', 'SaveProject', p),
  deleteProject: (id: string) => call<void>('TreeService', 'DeleteProject', id),
  saveWorkspace: (w: Workspace) => call<Workspace>('TreeService', 'SaveWorkspace', w),
  deleteWorkspace: (id: string) => call<void>('TreeService', 'DeleteWorkspace', id),
  getSetting: (key: string, def: string) => call<string>('TreeService', 'GetSetting', key, def),
  setSetting: (key: string, value: string) => call<void>('TreeService', 'SetSetting', key, value),
}

export const terminal = {
  openShell: (serverId: string, cols: number, rows: number) =>
    call<ShellInfo>('TerminalService', 'OpenShell', serverId, cols, rows),
  openWorkspace: (workspaceId: string, cols: number, rows: number) =>
    call<ShellInfo>('TerminalService', 'OpenWorkspace', workspaceId, cols, rows),
  openCommand: (serverId: string, command: string, cols: number, rows: number) =>
    call<ShellInfo>('TerminalService', 'OpenCommand', serverId, command, cols, rows),
  attachTmux: (serverId: string, session: string, cols: number, rows: number) =>
    call<ShellInfo>('TerminalService', 'AttachTmux', serverId, session, cols, rows),
  attachAgent: (agentId: string, cols: number, rows: number) =>
    call<ShellInfo>('TerminalService', 'AttachAgent', agentId, cols, rows),
  write: (id: string, b64: string) => call<void>('TerminalService', 'Write', id, b64),
  resize: (id: string, cols: number, rows: number) =>
    call<void>('TerminalService', 'Resize', id, cols, rows),
  scrollback: (id: string) => call<string>('TerminalService', 'Scrollback', id),
  close: (id: string) => call<void>('TerminalService', 'Close', id),
  list: () => call<ShellInfo[]>('TerminalService', 'List'),
  loadTabs: () => call<TerminalTab[]>('TerminalService', 'LoadTabs'),
  saveTabs: (tabs: TerminalTab[]) => call<void>('TerminalService', 'SaveTabs', tabs),
}

export const tmux = {
  info: (serverId: string) => call<TmuxInfo>('TmuxService', 'Info', serverId),
  view: (serverId: string) => call<TmuxServerView>('TmuxService', 'View', serverId),
  sessions: (serverId: string) => call<TmuxSession[]>('TmuxService', 'Sessions', serverId),
  panes: (serverId: string) => call<TmuxPane[]>('TmuxService', 'Panes', serverId),
  createSession: (serverId: string, name: string, cwd: string) =>
    call<void>('TmuxService', 'CreateSession', serverId, name, cwd),
  killSession: (serverId: string, name: string) =>
    call<void>('TmuxService', 'KillSession', serverId, name),
  renameSession: (serverId: string, from: string, to: string) =>
    call<void>('TmuxService', 'RenameSession', serverId, from, to),
  sendText: (serverId: string, target: string, text: string, pressEnter: boolean) =>
    call<void>('TmuxService', 'SendText', serverId, target, text, pressEnter),
  sendKey: (serverId: string, target: string, key: string) =>
    call<void>('TmuxService', 'SendKey', serverId, target, key),
  capture: (serverId: string, target: string, lines: number) =>
    call<string>('TmuxService', 'Capture', serverId, target, lines),
}

export const toolkit = {
  catalog: () => call<Tool[]>('ToolkitService', 'Catalog'),
  detect: (serverId: string) => call<ToolReport>('ToolkitService', 'Detect', serverId),
  install: (serverId: string, toolId: string, methodId: string) =>
    call<InstallStarted>('ToolkitService', 'Install', serverId, toolId, methodId),
  installCustom: (serverId: string, label: string, script: string) =>
    call<InstallStarted>('ToolkitService', 'InstallCustom', serverId, label, script),
  verify: (serverId: string, toolId: string) =>
    call<Presence>('ToolkitService', 'Verify', serverId, toolId),
  installedAgents: (serverId: string) =>
    call<AgentChoice[]>('ToolkitService', 'InstalledAgents', serverId),
  installSessionName: () => call<string>('ToolkitService', 'InstallSessionName'),
}

export const files = {
  list: (serverId: string, dir: string) => call<Listing>('FileService', 'List', serverId, dir),
  listWorkspace: (workspaceId: string) => call<Listing>('FileService', 'ListWorkspace', workspaceId),
  home: (serverId: string) => call<string>('FileService', 'Home', serverId),
  mkdir: (serverId: string, dir: string) => call<void>('FileService', 'Mkdir', serverId, dir),
  rename: (serverId: string, from: string, to: string) =>
    call<void>('FileService', 'Rename', serverId, from, to),
  remove: (serverId: string, target: string, recursive: boolean) =>
    call<void>('FileService', 'Remove', serverId, target, recursive),
  read: (serverId: string, remote: string) =>
    call<FileContent>('FileService', 'Read', serverId, remote),
  write: (serverId: string, remote: string, content: string, expectedModTime: number, crlf: boolean) =>
    call<FileContent>('FileService', 'Write', serverId, remote, content, expectedModTime, crlf),
  download: (serverId: string, remote: string, local: string) =>
    call<Transfer>('FileService', 'Download', serverId, remote, local),
  upload: (serverId: string, local: string, remoteDir: string) =>
    call<Transfer>('FileService', 'Upload', serverId, local, remoteDir),
  cancel: (id: string) => call<void>('FileService', 'Cancel', id),
  transfers: () => call<Transfer[]>('FileService', 'Transfers'),
  clearFinished: () => call<void>('FileService', 'ClearFinished'),
}

export const windows = {
  detach: (tab: DetachedTab, x: number, y: number, width: number, height: number) =>
    call<string>('WindowService', 'Detach', tab, x, y, width, height),
  claim: (token: string) => call<DetachedTab>('WindowService', 'Claim', token),
}

export const update = {
  // The desktop's service also raises the upgrade banner; everywhere else —
  // browser, phone — only the check-only service exists.
  check: () =>
    isDesktop
      ? call<UpdateInfo>('UpdateService', 'Check')
      : call<UpdateInfo>('UpdateCheckService', 'Check'),
  apply: () => call<UpdateResult>('UpdateService', 'Apply'),
  openReleasePage: () => call<void>('UpdateService', 'OpenReleasePage'),
}

export const metrics = {
  sample: (serverId: string) => call<MetricSample>('MetricsService', 'Sample', serverId),
  sampleMany: (serverIds: string[]) => call<MetricSample[]>('MetricsService', 'SampleMany', serverIds),
  hardware: (serverId: string) => call<HardwareInfo>('MetricsService', 'Hardware', serverId),
}

export const agents = {
  list: () => call<Agent[]>('AgentService', 'List'),
  save: (a: Agent) => call<Agent>('AgentService', 'Save', a),
  remove: (id: string) => call<void>('AgentService', 'Delete', id),
  suggestSession: (workspaceId: string, name: string) =>
    call<string>('AgentService', 'SuggestSession', workspaceId, name),
  start: (id: string) => call<StartResult>('AgentService', 'Start', id),
  stop: (id: string) => call<void>('AgentService', 'Stop', id),
  kill: (id: string) => call<void>('AgentService', 'Kill', id),
  restart: (id: string) => call<StartResult>('AgentService', 'Restart', id),
  send: (id: string, message: string, execute: boolean) =>
    call<Receipt>('AgentService', 'Send', id, message, execute),
  broadcast: (ids: string[], message: string, execute: boolean) =>
    call<Receipt[]>('AgentService', 'Broadcast', ids, message, execute),
  broadcastTo: (targets: BroadcastTarget[], message: string, execute: boolean) =>
    call<Receipt[]>('AgentService', 'BroadcastTo', targets, message, execute),
  logs: (id: string, lines: number) => call<string>('AgentService', 'Logs', id, lines),
  launchInDir: (serverId: string, dir: string, command: string) =>
    call<QuickLaunch>('AgentService', 'LaunchInDir', serverId, dir, command),
  refresh: (serverId: string) => call<Agent[]>('AgentService', 'Refresh', serverId),
  refreshAll: () => call<Agent[]>('AgentService', 'RefreshAll'),
  acknowledge: (id: string) => call<void>('AgentService', 'Acknowledge', id),
}

/**
 * Carrying this installation to another machine.
 *
 * The file is written by the backend rather than assembled here: the secrets it
 * holds are decrypted for exactly as long as it takes to seal them under the
 * passphrase, and that moment belongs on the side of the app that already holds
 * them.
 */
export const config = {
  suggestFilename: () => call<string>('ConfigService', 'SuggestFilename'),
  peek: (path: string) => call<{ recognised: boolean; exportedAt: number }>('ConfigService', 'Peek', path),
  export: (path: string, passphrase: string, options: ExportOptions) =>
    call<ConfigManifest>('ConfigService', 'Export', path, passphrase, options),
  inspect: (path: string, passphrase: string) =>
    call<ConfigManifest>('ConfigService', 'Inspect', path, passphrase),
  import: (path: string, passphrase: string) =>
    call<ConfigImport>('ConfigService', 'Import', path, passphrase),
}

/**
 * A host's screen, opened in this computer's own viewer.
 *
 * The backend forwards a port through the SSH connection it already holds and
 * starts whichever client this machine has, so nothing here knows anything
 * about RDP or VNC beyond their names.
 */
export const desktop = {
  probe: (serverId: string) => call<DesktopOffer>('DesktopService', 'Probe', serverId),
  open: (serverId: string, endpoint: DesktopEndpoint) =>
    call<DesktopSession>('DesktopService', 'Open', serverId, endpoint),
  close: (serverId: string) => call<void>('DesktopService', 'Close', serverId),
  forget: (serverId: string) => call<void>('DesktopService', 'Forget', serverId),
}

export const llm = {
  config: () => call<LLMConfig>('LLMService', 'Config'),
  saveConfig: (cfg: LLMConfig) => call<LLMStatus>('LLMService', 'SaveConfig', cfg),
  status: () => call<LLMStatus>('LLMService', 'Status'),
  models: () => call<LLMModel[]>('LLMService', 'Models'),
}

export const memory = {
  list: (filter: MemoryFilter) => call<Memory[]>('MemoryService', 'List', filter),
  count: (filter: MemoryFilter) => call<number>('MemoryService', 'Count', filter),
  search: (query: MemoryQuery) => call<MemoryHit[]>('MemoryService', 'Search', query),
  add: (m: Partial<Memory>) => call<PutResult>('MemoryService', 'Add', m),
  remove: (id: string) => call<void>('MemoryService', 'Delete', id),
  stats: () => call<MemoryStats>('MemoryService', 'Stats'),
  reindex: () => call<void>('MemoryService', 'Reindex'),
  cancelReindex: () => call<void>('MemoryService', 'CancelReindex'),
}

export const skills = {
  list: (filter: SkillFilter) => call<Skill[]>('SkillService', 'List', filter),
  get: (id: string) => call<Skill>('SkillService', 'Get', id),
  create: (s: Partial<Skill>) => call<Skill>('SkillService', 'Create', s),
  update: (s: Skill, note: string) => call<Skill>('SkillService', 'Update', s, note),
  remove: (id: string) => call<void>('SkillService', 'Delete', id),
  apply: (id: string, event: string) => call<Skill>('SkillService', 'Apply', id, event),
  versions: (id: string) => call<SkillVersion[]>('SkillService', 'Versions', id),
  rollback: (id: string, version: number) => call<Skill>('SkillService', 'Rollback', id, version),
  match: (q: SkillQuery) => call<SkillMatch[]>('SkillService', 'Match', q),
  stats: () => call<SkillStats>('SkillService', 'Stats'),
  embed: () => call<void>('SkillService', 'Embed'),
  exportJson: (ids: string[]) => call<string>('SkillService', 'ExportJSON', ids),
  exportMarkdown: (ids: string[]) => call<string>('SkillService', 'ExportMarkdown', ids),
  importJson: (data: string) => call<ImportResult>('SkillService', 'ImportJSON', data),
  tools: () => call<ToolMeta[]>('SkillService', 'Tools'),
}

export const orch = {
  config: () => call<OrchConfig>('OrchService', 'Config'),
  saveConfig: (cfg: OrchConfig) => call<OrchConfig>('OrchService', 'SaveConfig', cfg),
  status: () => call<OrchStatus>('OrchService', 'Status'),
  start: (goal: string, projectId: string) => call<Run>('OrchService', 'Start', goal, projectId),
  stop: () => call<void>('OrchService', 'Stop'),
  decide: (id: string, approved: boolean, note: string) =>
    call<void>('OrchService', 'Decide', id, approved, note),
  runs: (limit: number) => call<Run[]>('OrchService', 'Runs', limit),
  steps: (runId: string) => call<Step[]>('OrchService', 'Steps', runId),
  pending: () => call<Approval[]>('OrchService', 'Pending'),
}

/** Turns a backend error into something worth showing a user. */
export function errText(e: unknown): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  return JSON.stringify(e)
}
