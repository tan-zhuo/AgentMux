import { create } from 'zustand'
import { agents as agentApi, errText, servers, terminal, tree, windows } from '../lib/api'
import type {
  Agent,
  ConnState,
  ConnStatus,
  Diagnostics,
  Snapshot,
  TerminalTab,
} from '../lib/types'

export type TabKind = 'shell' | 'tmux' | 'agent' | 'command' | 'files'
export type TabStatus = 'pending' | 'opening' | 'open' | 'closed' | 'error'

/** A terminal tab in the UI. shellId is the live backend PTY, absent when the
 *  tab is restored from disk but not yet attached. */
export interface Tab {
  id: string
  title: string
  kind: TabKind
  serverId: string
  workspaceId: string
  agentId: string
  tmuxSession: string
  /** For kind 'command' the remote command this PTY runs; for kind 'files' the
   *  directory the browser is showing. */
  command?: string
  /** Take over this already-open PTY instead of starting a new one. Set when a
   *  tab is torn out of another window. */
  adoptShellId?: string
  shellId?: string
  status: TabStatus
  error?: string
}

export type Selection =
  | { kind: 'none' }
  | { kind: 'server'; id: string }
  | { kind: 'project'; id: string }
  | { kind: 'workspace'; id: string }
  | { kind: 'agent'; id: string }

export interface Toast {
  id: string
  tone: 'info' | 'ok' | 'warn' | 'error'
  text: string
}

export type RightPanel = 'detail' | 'broadcast' | 'tmux' | 'toolkit' | 'metrics'

const emptySnapshot: Snapshot = {
  folders: [],
  projects: [],
  servers: [],
  workspaces: [],
  agents: [],
}

/** Pane width limits. The maxima leave room for a usable terminal even on a
 *  small window; the exact figure is re-clamped against the real window width
 *  while dragging. */
export const SIDEBAR_DEFAULT = 288
export const SIDEBAR_MIN = 200
export const SIDEBAR_MAX = 620
export const RIGHT_DEFAULT = 384
export const RIGHT_MIN = 300
export const RIGHT_MAX = 760

function clampPane(w: number, min: number, max: number): number {
  return Math.round(Math.max(min, Math.min(max, w)))
}

let tabSeq = 0
const nextTabId = () => `tab-${Date.now().toString(36)}-${tabSeq++}`

/**
 * Compares the agent fields the UI actually renders. `lastSeen` is excluded on
 * purpose: the backend stamps it on every poll, so including it would report a
 * change every few seconds and make the comparison pointless.
 */
function sameAgents(a: Agent[], b: Agent[]): boolean {
  if (a === b) return true
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    const x = a[i]
    const y = b[i]
    if (
      x.id !== y.id ||
      x.status !== y.status ||
      x.pid !== y.pid ||
      x.progressText !== y.progressText ||
      x.name !== y.name ||
      x.command !== y.command ||
      x.tmuxSession !== y.tmuxSession ||
      x.workspaceId !== y.workspaceId
    ) {
      return false
    }
  }
  return true
}

interface AppState {
  snapshot: Snapshot
  connections: Record<string, ConnStatus>
  connState: Record<string, ConnState>
  diagnostics: Diagnostics | null
  loading: boolean

  tabs: Tab[]
  activeTabId: string | null
  /** True in a window holding a single torn-out tab. Such a window must not
   *  write the persisted layout: its one tab is not the main window's list. */
  detached: boolean

  selection: Selection
  rightPanel: RightPanel
  sidebarOpen: boolean
  rightOpen: boolean
  sidebarWidth: number
  rightWidth: number
  search: string
  expanded: Record<string, boolean>
  broadcastTargets: string[]
  toasts: Toast[]
  paletteOpen: boolean

  loadAll: () => Promise<void>
  refreshSnapshot: () => Promise<void>
  refreshConnections: () => Promise<void>
  applyAgents: (agents: Agent[]) => void
  applyConnState: (s: ConnState) => void

  select: (s: Selection) => void
  setRightPanel: (p: RightPanel) => void
  toggleSidebar: () => void
  toggleRight: () => void
  setSidebarWidth: (w: number, commit?: boolean) => void
  setRightWidth: (w: number, commit?: boolean) => void
  setSearch: (q: string) => void
  toggleExpanded: (key: string) => void
  setExpanded: (key: string, open: boolean) => void
  setPaletteOpen: (open: boolean) => void

  toggleBroadcastTarget: (agentId: string) => void
  setBroadcastTargets: (ids: string[]) => void

  openTab: (spec: Omit<Tab, 'id' | 'status'> & { id?: string }) => string
  setTabState: (id: string, patch: Partial<Tab>) => void
  moveTab: (from: number, to: number) => void
  detachTab: (id: string, x: number, y: number, width: number, height: number) => Promise<void>
  closeTab: (id: string) => Promise<void>
  setActiveTab: (id: string) => void
  persistTabs: () => Promise<void>
  restoreTabs: () => Promise<void>

  toast: (tone: Toast['tone'], text: string) => void
  dismissToast: (id: string) => void
}

export const useAppStore = create<AppState>((set, get) => ({
  snapshot: emptySnapshot,
  connections: {},
  connState: {},
  diagnostics: null,
  loading: true,

  tabs: [],
  activeTabId: null,
  detached: false,

  selection: { kind: 'none' },
  rightPanel: 'detail',
  sidebarOpen: true,
  rightOpen: true,
  sidebarWidth: SIDEBAR_DEFAULT,
  rightWidth: RIGHT_DEFAULT,
  search: '',
  expanded: {},
  broadcastTargets: [],
  toasts: [],
  paletteOpen: false,

  async loadAll() {
    set({ loading: true })
    try {
      const [snapshot, diagnostics, panel, sideW, rightW] = await Promise.all([
        tree.snapshot(),
        servers.diagnostics(),
        tree.getSetting('rightPanel', 'detail'),
        tree.getSetting('sidebarWidth', String(SIDEBAR_DEFAULT)),
        tree.getSetting('rightWidth', String(RIGHT_DEFAULT)),
      ])
      const known: RightPanel[] = ['detail', 'broadcast', 'tmux', 'toolkit', 'metrics']
      set({
        snapshot,
        diagnostics,
        loading: false,
        rightPanel: known.includes(panel as RightPanel) ? (panel as RightPanel) : 'detail',
        sidebarWidth: clampPane(Number(sideW) || SIDEBAR_DEFAULT, SIDEBAR_MIN, SIDEBAR_MAX),
        rightWidth: clampPane(Number(rightW) || RIGHT_DEFAULT, RIGHT_MIN, RIGHT_MAX),
      })
      await get().refreshConnections()
      await get().restoreTabs()
    } catch (e) {
      set({ loading: false })
      get().toast('error', `Could not load local data: ${errText(e)}`)
    }
  },

  async refreshSnapshot() {
    try {
      set({ snapshot: await tree.snapshot() })
    } catch (e) {
      get().toast('error', errText(e))
    }
  },

  async refreshConnections() {
    try {
      const list = await servers.connections()
      const map: Record<string, ConnStatus> = {}
      for (const c of list) map[c.serverId] = c
      set({ connections: map })
    } catch {
      /* connection view is best-effort */
    }
  },

  applyAgents(list) {
    set((s) => {
      // Replacing the array unconditionally would hand every subscriber a new
      // snapshot object on each poll, even when nothing about the agents moved.
      if (sameAgents(s.snapshot.agents, list)) return s
      return { snapshot: { ...s.snapshot, agents: list } }
    })
  },

  applyConnState(cs) {
    set((s) => {
      const connected = cs.state === 'connected'
      const prev = s.connections[cs.serverId]
      const prevState = s.connState[cs.serverId]
      const stateUnchanged =
        prevState?.state === cs.state && prevState?.detail === cs.detail
      if (stateUnchanged && prev?.connected === connected) return s

      return {
        connState: { ...s.connState, [cs.serverId]: cs },
        connections: {
          ...s.connections,
          [cs.serverId]: {
            serverId: cs.serverId,
            connected,
            leases: prev?.leases ?? 0,
          },
        },
      }
    })
  },

  select(selection) {
    set({ selection })
  },
  setRightPanel(rightPanel) {
    set({ rightPanel, rightOpen: true })
    // Remembered across restarts: whichever panel you work in is almost always
    // the one you want back.
    void tree.setSetting('rightPanel', rightPanel).catch(() => {})
  },
  toggleSidebar() {
    set((s) => ({ sidebarOpen: !s.sidebarOpen }))
  },
  toggleRight() {
    set((s) => ({ rightOpen: !s.rightOpen }))
  },

  // While a splitter is being dragged the width changes on every pointer move,
  // so only the final value is written to the settings table.
  setSidebarWidth(w, commit) {
    const next = clampPane(w, SIDEBAR_MIN, SIDEBAR_MAX)
    set({ sidebarWidth: next })
    if (commit) void tree.setSetting('sidebarWidth', String(next)).catch(() => {})
  },
  setRightWidth(w, commit) {
    const next = clampPane(w, RIGHT_MIN, RIGHT_MAX)
    set({ rightWidth: next })
    if (commit) void tree.setSetting('rightWidth', String(next)).catch(() => {})
  },
  setSearch(search) {
    set({ search })
  },
  toggleExpanded(key) {
    set((s) => ({ expanded: { ...s.expanded, [key]: !s.expanded[key] } }))
  },
  setExpanded(key, open) {
    set((s) => ({ expanded: { ...s.expanded, [key]: open } }))
  },
  setPaletteOpen(paletteOpen) {
    set({ paletteOpen })
  },

  toggleBroadcastTarget(agentId) {
    set((s) => ({
      broadcastTargets: s.broadcastTargets.includes(agentId)
        ? s.broadcastTargets.filter((x) => x !== agentId)
        : [...s.broadcastTargets, agentId],
    }))
  },
  setBroadcastTargets(broadcastTargets) {
    set({ broadcastTargets })
  },

  openTab(spec) {
    // Re-focus an equivalent tab instead of stacking duplicates.
    const existing = get().tabs.find(
      (t) =>
        t.kind === spec.kind &&
        t.serverId === spec.serverId &&
        t.workspaceId === spec.workspaceId &&
        t.agentId === spec.agentId &&
        t.tmuxSession === spec.tmuxSession &&
        t.status !== 'closed',
    )
    if (existing && spec.kind !== 'shell') {
      set({ activeTabId: existing.id })
      return existing.id
    }

    const id = spec.id ?? nextTabId()
    const tab: Tab = { ...spec, id, status: 'pending' }
    set((s) => ({ tabs: [...s.tabs, tab], activeTabId: id }))
    void get().persistTabs()
    return id
  },

  setTabState(id, patch) {
    set((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? { ...t, ...patch } : t)) }))
  },

  moveTab(from, to) {
    set((s) => {
      if (from === to || from < 0 || to < 0 || from >= s.tabs.length || to >= s.tabs.length) {
        return s
      }
      const tabs = [...s.tabs]
      const [moved] = tabs.splice(from, 1)
      tabs.splice(to, 0, moved)
      return { tabs }
    })
    void get().persistTabs()
  },

  async detachTab(id, x, y, width, height) {
    const tab = get().tabs.find((t) => t.id === id)
    if (!tab) return
    try {
      await windows.detach(
        {
          title: tab.title,
          kind: tab.kind,
          serverId: tab.serverId,
          workspaceId: tab.workspaceId,
          agentId: tab.agentId,
          tmuxSession: tab.tmuxSession,
          command: tab.command ?? '',
          // Hand over the live PTY so the new window continues the same
          // session rather than opening a second one beside it.
          shellId: tab.shellId ?? '',
        },
        x,
        y,
        width,
        height,
      )
    } catch (e) {
      get().toast('error', errText(e))
      return
    }
    // The tab now lives in the other window. Drop it here without closing the
    // shell, which the new window has taken over.
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== id)
      let activeTabId = s.activeTabId
      if (activeTabId === id) activeTabId = tabs.length ? tabs[tabs.length - 1].id : null
      return { tabs, activeTabId }
    })
    void get().persistTabs()
  },

  async closeTab(id) {
    const tab = get().tabs.find((t) => t.id === id)
    if (tab?.shellId) {
      try {
        await terminal.close(tab.shellId)
      } catch {
        /* already gone */
      }
    }
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== id)
      let activeTabId = s.activeTabId
      if (activeTabId === id) activeTabId = tabs.length ? tabs[tabs.length - 1].id : null
      return { tabs, activeTabId }
    })
    void get().persistTabs()
  },

  setActiveTab(activeTabId) {
    set({ activeTabId })
  },

  async persistTabs() {
    // A detached window holds one tab. Writing that as "the layout" would wipe
    // everything the main window has open the next time it starts.
    if (get().detached) return
    const tabs = get().tabs
    const payload: TerminalTab[] = tabs.map((t, i) => ({
      id: t.id,
      title: t.title,
      serverId: t.serverId,
      workspaceId: t.workspaceId,
      agentId: t.agentId,
      tmuxSession: t.tmuxSession,
      kind: t.kind,
      command: t.command ?? '',
      sort: i,
    }))
    try {
      await terminal.saveTabs(payload)
    } catch {
      /* layout persistence is best-effort */
    }
  },

  async restoreTabs() {
    try {
      const saved = await terminal.loadTabs()
      // tmux and agent tabs reattach to work that is still running remotely; a
      // file browser reopens at the directory you left it in. A plain shell has
      // no state to come back to, so it is not restored.
      const restorable = saved.filter(
        (t) => t.kind === 'tmux' || t.kind === 'agent' || t.kind === 'files',
      )
      if (!restorable.length) return
      set({
        tabs: restorable.map((t) => ({
          id: t.id,
          title: t.title,
          kind: t.kind as TabKind,
          serverId: t.serverId,
          workspaceId: t.workspaceId,
          agentId: t.agentId,
          tmuxSession: t.tmuxSession,
          command: t.command,
          status: 'pending' as TabStatus,
        })),
        activeTabId: restorable[0].id,
      })
    } catch {
      /* nothing to restore */
    }
  },

  toast(tone, text) {
    const id = `t-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`
    set((s) => ({ toasts: [...s.toasts, { id, tone, text }] }))
    setTimeout(() => get().dismissToast(id), tone === 'error' ? 9000 : 4500)
  },
  dismissToast(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
  },
}))

/** Refresh agent status for a server after an action that changes it. */
export async function refreshServerAgents(serverId: string) {
  try {
    const list = await agentApi.refresh(serverId)
    useAppStore.getState().applyAgents(list)
  } catch (e) {
    useAppStore.getState().toast('error', errText(e))
  }
}
