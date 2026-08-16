// Mirrors the Go structs in internal/store and internal/sshx.

export type AuthType = 'agent' | 'key' | 'password'

export interface Folder {
  id: string
  name: string
  parentId: string | null
  sort: number
}

export interface Project {
  id: string
  name: string
  description: string
  folderId: string | null
  sort: number
  createdAt: number
}

export interface Server {
  id: string
  name: string
  host: string
  port: number
  username: string
  authType: AuthType
  keyPath: string
  hasPassword: boolean
  hasPassphrase: boolean
  jumpServerId: string | null
  tags: string[]
  favorite: boolean
  hostKey: string
  createdAt: number
  lastOkAt: number | null
}

export interface ServerInput {
  id: string
  name: string
  host: string
  port: number
  username: string
  authType: AuthType
  keyPath: string
  /** null keeps the stored secret, '' clears it, anything else replaces it. */
  password: string | null
  passphrase: string | null
  jumpServerId: string | null
  tags: string[]
  favorite: boolean
}

export interface Workspace {
  id: string
  projectId: string
  serverId: string
  name: string
  remotePath: string
  defaultTmuxSession: string
  defaultAgentCommand: string
  env: Record<string, string>
  sort: number
}

export type AgentStatus = 'running' | 'idle' | 'error' | 'detached' | 'unknown'

export interface Agent {
  id: string
  workspaceId: string
  name: string
  command: string
  tmuxSession: string
  tmuxWindow: string
  tmuxPaneId: string
  status: AgentStatus
  lastSeen: number | null
  pid: number | null
  progressText: string
  createdAt: number
}

export interface TerminalTab {
  id: string
  title: string
  serverId: string
  workspaceId: string
  agentId: string
  tmuxSession: string
  kind: 'shell' | 'tmux' | 'agent' | 'command' | 'files' | 'editor'
  command: string
  sort: number
}

// --- toolkit: detecting and installing agent CLIs ---------------------------

export interface InstallMethod {
  id: string
  label: string
  requires: string
  needsRoot: boolean
  script: string
}

export interface Tool {
  id: string
  name: string
  vendor: string
  description: string
  binary: string
  runCommand: string
  versionArgs: string
  docs: string
  methods: InstallMethod[]
  kind: 'agent' | 'runtime'
}

export interface Presence {
  binary: string
  installed: boolean
  path: string
  version: string
}

export interface ToolStatus {
  tool: Tool
  installed: boolean
  path: string
  version: string
  available: InstallMethod[] | null
  blocked: string
}

export interface ToolReport {
  serverId: string
  os: string
  shell: string
  agents: ToolStatus[]
  runtimes: ToolStatus[]
  presence: Record<string, Presence>
  error: string
}

// --- remote file system -----------------------------------------------------

export interface FileEntry {
  name: string
  path: string
  isDir: boolean
  isLink: boolean
  size: number
  mode: string
  modTime: number
  target: string
  targetIsDir: boolean
}

export interface Listing {
  serverId: string
  path: string
  parent: string
  entries: FileEntry[]
}

export interface FileContent {
  path: string
  /** Empty in a write result — the caller already has what it just sent, and
   *  echoing a whole file back through the IPC layer costs real time. */
  content: string
  size: number
  modTime: number
  mode: string
  /** The file used CRLF line endings on the server; saving restores them. */
  crlf: boolean
}

export interface Transfer {
  id: string
  serverId: string
  kind: 'upload' | 'download'
  local: string
  remote: string
  size: number
  done: number
  status: 'running' | 'done' | 'error' | 'cancelled'
  error: string
  startedAt: number
}

// --- host metrics -----------------------------------------------------------

export interface DiskUsage {
  mount: string
  fs: string
  type: string
  totalBytes: number
  usedBytes: number
  usePercent: number
  inodePercent: number
}

export interface NetRate {
  name: string
  rxps: number
  txps: number
}

export interface GpuUsage {
  index: number
  name: string
  utilPercent: number
  memTotalMb: number
  memUsedMb: number
  tempC: number
  powerW: number
}

export interface BlockIoRate {
  name: string
  readps: number
  writeps: number
}

export interface ProcRow {
  cpu: number
  mem: number
  pid: number
  user: string
  command: string
}

export interface MetricSample {
  serverId: string
  at: number
  ok: boolean
  error: string

  distro: string
  kernel: string
  arch: string
  hostname: string

  cores: number
  cpuPercent: number
  cpuUser: number
  cpuSystem: number
  cpuIowait: number
  cpuSteal: number
  perCore: number[]
  load1: number
  load5: number
  load15: number
  loadPerCore: number
  contextRate: number
  procsRunning: number
  procsBlocked: number

  memTotalBytes: number
  memUsedBytes: number
  memCachedBytes: number
  memPercent: number
  swapTotalBytes: number
  swapUsedBytes: number

  uptimeSeconds: number
  processes: number
  users: number
  connections: number
  openFds: number
  maxFds: number
  tempC: number

  disks: DiskUsage[]
  nets: NetRate[]
  blockIo: BlockIoRate[]
  topCpu: ProcRow[]
  topMem: ProcRow[]
  gpus: GpuUsage[]
}

/**
 * A broadcast recipient: either a registered agent, or a tmux session addressed
 * directly. Sessions matter because launching into a directory produces a
 * running session and no agent record.
 */
export interface BroadcastTarget {
  agentId: string
  serverId: string
  session: string
}

/** An agent CLI a server can run right now. */
export interface AgentChoice {
  id: string
  name: string
  command: string
  version: string
}

/** The outcome of launching an agent straight from a directory. */
export interface QuickLaunch {
  serverId: string
  dir: string
  session: string
  command: string
  createdSession: boolean
  reusedSession: boolean
  agentId: string
}

/** A tab handed to a new window when it is torn out of the tab strip. */
export interface DetachedTab {
  token?: string
  title: string
  kind: 'shell' | 'tmux' | 'agent' | 'command' | 'files' | 'editor'
  serverId: string
  workspaceId: string
  agentId: string
  tmuxSession: string
  command: string
  /** An already-open PTY the new window takes over. */
  shellId: string
}

export interface InstallStarted {
  serverId: string
  toolId: string
  toolName: string
  methodId: string
  script: string
  session: string
  usesTmux: boolean
  needsRoot: boolean
  command: string
}

export interface Snapshot {
  folders: Folder[]
  projects: Project[]
  servers: Server[]
  workspaces: Workspace[]
  agents: Agent[]
}

export interface Probe {
  ok: boolean
  latencyMs: number
  os: string
  uptime: string
  load: string
  hasTmux: boolean
  tmuxVersion: string
  error: string
}

export interface ConnState {
  serverId: string
  state: 'connecting' | 'connected' | 'disconnected' | 'error'
  detail: string
  at: number
}

export interface ConnStatus {
  serverId: string
  connected: boolean
  leases: number
}

export interface Diagnostics {
  /** The build's identity: a release tag, or 'dev' for a local build. */
  version: string
  dataDir: string
  keyInFile: boolean
  keyLocationOk: boolean
}

export interface ShellInfo {
  id: string
  serverId: string
  cols: number
  rows: number
  openedAt: number
  alive: boolean
}

export interface TmuxSession {
  name: string
  windows: number
  attached: boolean
  created: number
  activity: number
}

export interface TmuxPane {
  sessionName: string
  windowIndex: string
  windowName: string
  paneIndex: string
  paneId: string
  pid: number
  command: string
  path: string
  active: boolean
  title: string
}

export interface TmuxInfo {
  available: boolean
  version: string
  error: string
}

export interface TmuxServerView {
  serverId: string
  info: TmuxInfo
  sessions: TmuxSession[]
  panes: TmuxPane[]
  error: string
}

export interface StartResult {
  agentId: string
  status: AgentStatus
  session: string
  target: string
  createdSession: boolean
  alreadyRunning: boolean
  error: string
}

export interface Receipt {
  agentId: string
  agentName: string
  ok: boolean
  target: string
  error: string
  at: number
}
