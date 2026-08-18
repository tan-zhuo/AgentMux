import type { MsgKey, TFunc } from './i18n'
import type { AgentStatus } from './types'

const KEYS: Record<AgentStatus, MsgKey> = {
  running: 'agent.status.running',
  idle: 'agent.status.idle',
  error: 'agent.status.error',
  detached: 'agent.status.detached',
  unknown: 'agent.status.unknown',
}

/**
 * The status a person reads, rather than the enum the backend sends. Kept in
 * one place because it is shown in the sidebar, the detail panel and the tmux
 * list, and those three drifting apart is how "running" ends up next to "実行中".
 */
export function agentStatusLabel(t: TFunc, status: AgentStatus): string {
  return t(KEYS[status] ?? 'agent.status.unknown')
}
