import { create } from 'zustand'
import { agents as agentApi, errText, servers, terminal, tree, windows } from '../lib/api'
import { confirmAction } from './useConfirm'
import { useDialogs } from './useDialogs'
import type {
  Agent,
  BroadcastTarget,
  ConnState,
  ConnStatus,
  Diagnostics,
  Snapshot,
  TerminalTab,
} from '../lib/types'

export type TabKind = 'shell' | 'tmux' | 'agent' | 'command' | 'files' | 'editor'
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
   *  directory the browser is showing; for kind 'editor' the file being edited. */
  command?: string
  /** An editor tab with edits that are not on the server yet. Closing one asks
   *  first, because the alternative is losing work to a stray click on an X. */
  dirty?: boolean
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

/** How a two-or-three-way split is laid out. Four panes are always a 2×2 grid,
 *  where an axis would mean nothing. */
export type SplitAxis = 'cols' | 'rows'

/** The most panes worth having. Past four, each one is too small to read a
 *  terminal in, and the tab strip is the better tool. */
export const MAX_PANES = 4

export type RightPanel =
  | 'detail'
  | 'broadcast'
  | 'tmux'
  | 'toolkit'
  | 'metrics'
  | 'memory'
  | 'skills'
  | 'orchestrator'

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

/** Identity for a broadcast recipient, which is either an agent or a session. */
export function targetKey(t: BroadcastTarget): string {
  return t.agentId ? `agent:${t.agentId}` : `session:${t.serverId}:${t.session}`
}

let tabSeq = 0
const nextTabId = () => `tab-${Date.now().toString(36)}-${tabSeq++}`

/**
 * Show `id` in the focused pane and focus it.
 *
 * Every path that changes the focused tab goes through this — clicking the
 * strip, opening a tab, restoring a layout — because the alternative is four
 * copies of the same three lines, and the one that gets missed leaves a pane
 * showing a tab that no longer exists.
 */
function focusIntoPane(paneIds: string[], activeTabId: string | null, id: string): string[] {
  if (paneIds.includes(id)) return paneIds
  const at = paneIds.indexOf(activeTabId ?? '')
  if (at === -1) return [id]
  const next = [...paneIds]
  next[at] = id
  return next
}

/** Put `id` in a pane of its own, alongside the panes already on screen.
 *  Returns the list unchanged when the tab is already visible or the grid is
 *  full, so every caller can hand its result straight to `set`. */
function addPane(paneIds: string[], id: string): string[] {
  if (paneIds.includes(id) || paneIds.length >= MAX_PANES) return paneIds
  return [...paneIds, id]
}

/** The pane count is remembered, not the tabs in them: on the next start the
 *  same shape comes back, filled by whatever reattaches. */
function savePaneCount(n: number) {
  // Never zero: a window with no tabs still opens with one pane next time.
  void tree.setSetting('paneCount', String(Math.max(1, n))).catch(() => {})
}

/** Drop panes whose tab is gone, and make sure the focused tab is on screen. */
function reconcilePanes(
  tabs: Tab[],
  paneIds: string[],
  activeTabId: string | null,
): { paneIds: string[]; activeTabId: string | null } {
  const live = new Set(tabs.map((t) => t.id))
  const panes = paneIds.filter((id) => live.has(id))
  const active =
    activeTabId && live.has(activeTabId) ? activeTabId : (panes[0] ?? tabs.at(-1)?.id ?? null)
  if (!active) return { paneIds: [], activeTabId: null }
  return { paneIds: panes.includes(active) ? panes : [active], activeTabId: active }
}

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
  /** Tabs currently on screen, in pane order. Always contains activeTabId,
   *  which is the pane holding keyboard focus. One entry means no split. */
  paneIds: string[]
  splitAxis: SplitAxis
  rightPanel: RightPanel
  sidebarOpen: boolean
  rightOpen: boolean
  sidebarWidth: number
  rightWidth: number
  search: string
  expanded: Record<string, boolean>
  broadcastTargets: BroadcastTarget[]
  toasts: Toast[]
  paletteOpen: boolean

  loadAll: () => Promise<void>
  refreshSnapshot: () => Promise<void>
  refreshConnections: () => Promise<void>
  applyAgents: (agents: Agent[]) => void
  applyConnState: (s: ConnState) => void

  select: (s: Selection) => void
  /** Split into the next tab that is not on screen. False when there is no
   *  such tab, or the grid is already full. */
  splitPane: () => boolean
  /** What ⌘\ and the split control do: split instantly when there is an
   *  obvious tab for the new pane, and otherwise ask what to put in it. */
  requestSplit: () => void
  /** Show an already-open tab in a pane of its own and focus it. */
  splitWith: (tabId: string) => void
  closePane: (tabId: string) => void
  /** Move keyboard focus to the next (+1) or previous (-1) pane. */
  focusPane: (delta: number) => void
  setSplitAxis: (axis: SplitAxis) => void
  setRightPanel: (p: RightPanel) => void
  toggleSidebar: () => void
  toggleRight: () => void
  setSidebarWidth: (w: number, commit?: boolean) => void
  setRightWidth: (w: number, commit?: boolean) => void
  setSearch: (q: string) => void
  toggleExpanded: (key: string) => void
  setExpanded: (key: string, open: boolean) => void
  setPaletteOpen: (open: boolean) => void

  toggleBroadcastTarget: (target: BroadcastTarget) => void
  setBroadcastTargets: (targets: BroadcastTarget[]) => void
  isBroadcastTarget: (target: BroadcastTarget) => boolean

  openTab: (
    spec: Omit<Tab, 'id' | 'status'> & { id?: string },
    opts?: { newPane?: boolean },
  ) => string
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
  paneIds: [],
  splitAxis: 'cols',
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
      const known: RightPanel[] = [
        'detail', 'broadcast', 'tmux', 'toolkit', 'metrics', 'memory', 'skills', 'orchestrator',
      ]
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

  toggleBroadcastTarget(target) {
    const key = targetKey(target)
    set((s) => ({
      broadcastTargets: s.broadcastTargets.some((t) => targetKey(t) === key)
        ? s.broadcastTargets.filter((t) => targetKey(t) !== key)
        : [...s.broadcastTargets, target],
    }))
  },
  setBroadcastTargets(broadcastTargets) {
    set({ broadcastTargets })
  },
  isBroadcastTarget(target) {
    const key = targetKey(target)
    return get().broadcastTargets.some((t) => targetKey(t) === key)
  },

  openTab(spec, opts) {
    // `newPane` is how the split picker opens things: the tab arrives beside
    // what is already on screen instead of replacing it. Silently ignored once
    // the grid is full, because the tab is still worth opening.
    const newPane = !!opts?.newPane && get().paneIds.length < MAX_PANES
    // Re-focus an equivalent tab instead of stacking duplicates.
    const existing = get().tabs.find(
      (t) =>
        t.kind === spec.kind &&
        t.serverId === spec.serverId &&
        t.workspaceId === spec.workspaceId &&
        t.agentId === spec.agentId &&
        t.tmuxSession === spec.tmuxSession &&
        // Two open files on the same server are two tabs, not one.
        (spec.kind !== 'editor' || t.command === spec.command) &&
        t.status !== 'closed',
    )
    if (existing && spec.kind !== 'shell') {
      set((s) => ({
        activeTabId: existing.id,
        paneIds: newPane
          ? addPane(s.paneIds, existing.id)
          : focusIntoPane(s.paneIds, s.activeTabId, existing.id),
      }))
      if (newPane) savePaneCount(get().paneIds.length)
      return existing.id
    }

    const id = spec.id ?? nextTabId()
    const tab: Tab = { ...spec, id, status: 'pending' }
    set((s) => ({
      tabs: [...s.tabs, tab],
      activeTabId: id,
      paneIds: newPane ? addPane(s.paneIds, id) : focusIntoPane(s.paneIds, s.activeTabId, id),
    }))
    if (newPane) savePaneCount(get().paneIds.length)
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
    const panesBefore = get().paneIds.length
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== id)
      const active = s.activeTabId === id ? null : s.activeTabId
      return { tabs, ...reconcilePanes(tabs, s.paneIds, active) }
    })
    // Closing the tab a pane was showing collapses the split, and the collapsed
    // layout is the one worth coming back to.
    if (get().paneIds.length !== panesBefore) savePaneCount(get().paneIds.length)
    void get().persistTabs()
  },

  async closeTab(id) {
    const tab = get().tabs.find((t) => t.id === id)
    if (tab?.dirty) {
      set({ activeTabId: id })
      const ok = await confirmAction({
        title: `Close ${tab.title.replace(/ •$/, '')} without saving`,
        message: 'The changes you made here have not been written to the server.',
        tone: 'warning',
        confirmLabel: 'Discard changes',
        cancelLabel: 'Keep editing',
      })
      if (!ok) return
    }
    if (tab?.shellId) {
      try {
        await terminal.close(tab.shellId)
      } catch {
        /* already gone */
      }
    }
    const panesBefore = get().paneIds.length
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== id)
      const active = s.activeTabId === id ? null : s.activeTabId
      return { tabs, ...reconcilePanes(tabs, s.paneIds, active) }
    })
    // Closing the tab a pane was showing collapses the split, and the collapsed
    // layout is the one worth coming back to.
    if (get().paneIds.length !== panesBefore) savePaneCount(get().paneIds.length)
    void get().persistTabs()
  },

  setActiveTab(activeTabId) {
    // Choosing a tab that is already on screen focuses its pane. Choosing any
    // other one replaces what the focused pane is showing — which is what makes
    // a split usable with one tab strip rather than one per pane.
    set((s) => ({
      activeTabId,
      paneIds: activeTabId ? focusIntoPane(s.paneIds, s.activeTabId, activeTabId) : [],
    }))
  },

  splitPane() {
    const s = get()
    if (s.paneIds.length >= MAX_PANES) return false
    // A pane shows a tab, and there is one shell per tab, so a split cannot
    // conjure a second view of the same terminal. The next tab not already on
    // screen is the one that wants showing.
    const next = s.tabs.find((t) => !s.paneIds.includes(t.id))
    if (!next) return false
    get().splitWith(next.id)
    return true
  },

  requestSplit() {
    if (get().paneIds.length >= MAX_PANES) {
      get().toast('info', 'Four panes is the limit — close one before adding another')
      return
    }
    // Nothing spare to show means the pane needs a session started for it, and
    // that is a question rather than a default: which host, which directory.
    if (!get().splitPane()) useDialogs.getState().open({ kind: 'split' })
  },

  splitWith(tabId) {
    set((s) => {
      const paneIds = addPane(s.paneIds, tabId)
      // A full grid cannot take another pane, and focusing a tab that is not on
      // screen would leave the keyboard talking to something invisible. Show it
      // in the focused pane instead.
      if (!paneIds.includes(tabId)) {
        return { paneIds: focusIntoPane(s.paneIds, s.activeTabId, tabId), activeTabId: tabId }
      }
      return { paneIds, activeTabId: tabId }
    })
    savePaneCount(get().paneIds.length)
  },

  closePane(tabId) {
    set((s) => {
      if (s.paneIds.length <= 1) return {}
      const paneIds = s.paneIds.filter((id) => id !== tabId)
      // Closing a pane hides a tab; it does not close it. The shell stays
      // attached and the tab stays in the strip.
      const activeTabId = s.activeTabId === tabId ? paneIds[0] : s.activeTabId
      return { paneIds, activeTabId }
    })
    savePaneCount(get().paneIds.length)
  },

  focusPane(delta) {
    const s = get()
    if (s.paneIds.length < 2) return
    const at = s.paneIds.indexOf(s.activeTabId ?? '')
    const next = s.paneIds[(at + delta + s.paneIds.length) % s.paneIds.length]
    if (next) set({ activeTabId: next })
  },

  setSplitAxis(splitAxis) {
    void tree.setSetting('splitAxis', splitAxis).catch(() => {})
    set({ splitAxis })
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
      // file browser reopens at the directory you left it in, and an editor at
      // the file. A plain shell has no state to come back to, so it is not
      // restored.
      const restorable = saved.filter(
        (t) =>
          t.kind === 'tmux' || t.kind === 'agent' || t.kind === 'files' || t.kind === 'editor',
      )
      if (!restorable.length) return

      const [axis, count] = await Promise.all([
        tree.getSetting('splitAxis', 'cols'),
        tree.getSetting('paneCount', '1'),
      ])
      const panes = Math.min(Math.max(Number(count) || 1, 1), MAX_PANES)

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
        // The split comes back with the same shape, filled by whatever
        // reattached. Restoring which tab was in which pane would be restoring
        // a snapshot of attention, which nobody misses.
        paneIds: restorable.slice(0, panes).map((t) => t.id),
        splitAxis: axis === 'rows' ? 'rows' : 'cols',
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
