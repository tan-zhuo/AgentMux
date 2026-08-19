import type { MsgKey, TFunc } from './i18n'
import type { Agent, AgentStatus } from './types'

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

/**
 * The one-line answer to "what is this agent doing right now". For a running
 * agent the pane's activity beats the raw status: "waiting for your input" and
 * "idle — no task running" are the two facts a person scanning a list of
 * agents actually wants, and the progress line only matters while it works.
 */
export function agentActivityLabel(t: TFunc, agent: Agent): string {
  if (agent.status === 'running') {
    if (agent.activity === 'input') return t('agent.activity.input')
    if (agent.activity === 'quiet') return t('agent.activity.quiet')
    return agent.progressText || t('agent.status.running')
  }
  return agentStatusLabel(t, agent.status)
}
