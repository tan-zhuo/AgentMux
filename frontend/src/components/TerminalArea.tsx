import clsx from 'clsx'
import { Bot, FolderTree, Layers, PackagePlus, TerminalSquare, X } from 'lucide-react'
import { useAppStore } from '../store/useAppStore'
import { FileBrowser } from './FileBrowser'
import { TerminalPane } from './TerminalPane'
import { Empty } from './ui'

const kindIcon = {
  shell: TerminalSquare,
  tmux: Layers,
  agent: Bot,
  command: PackagePlus,
  files: FolderTree,
}

export function TerminalArea() {
  const tabs = useAppStore((s) => s.tabs)
  const activeTabId = useAppStore((s) => s.activeTabId)
  const setActiveTab = useAppStore((s) => s.setActiveTab)
  const closeTab = useAppStore((s) => s.closeTab)

  return (
    <div className="flex min-w-0 flex-1 flex-col bg-ink-950">
      <div className="flex h-9 shrink-0 items-stretch gap-px overflow-x-auto border-b border-ink-800 bg-ink-900">
        {tabs.map((tab) => {
          const Icon = kindIcon[tab.kind]
          const active = tab.id === activeTabId
          return (
            <div
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={clsx(
                'group flex min-w-0 shrink-0 cursor-pointer items-center gap-1.5 border-r border-ink-800 px-3 text-xs',
                active
                  ? 'bg-ink-950 text-ink-100'
                  : 'bg-ink-900 text-ink-400 hover:bg-ink-850 hover:text-ink-200',
              )}
            >
              <Icon size={12} className="shrink-0 opacity-70" />
              <span className="max-w-[180px] truncate">{tab.title}</span>
              {tab.status === 'opening' && (
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warn" />
              )}
              {(tab.status === 'closed' || tab.status === 'error') && (
                <span className="h-1.5 w-1.5 rounded-full bg-ink-600" />
              )}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  void closeTab(tab.id)
                }}
                className="ml-0.5 rounded p-0.5 text-ink-500 opacity-0 group-hover:opacity-100 hover:bg-ink-750 hover:text-ink-100"
                title="Close tab"
              >
                <X size={11} />
              </button>
            </div>
          )
        })}
        {!tabs.length && (
          <div className="flex items-center px-3 text-[11px] text-ink-500">No open terminals</div>
        )}
      </div>

      <div className="relative min-h-0 flex-1">
        {tabs.length === 0 ? (
          <Empty
            title="Nothing attached yet"
            hint="Pick a server, workspace or agent on the left. Agent and tmux tabs reattach to work that is already running remotely."
          />
        ) : (
          tabs.map((tab) => (
            <div
              key={tab.id}
              className="absolute inset-0"
              style={{ visibility: tab.id === activeTabId ? 'visible' : 'hidden' }}
            >
              {tab.kind === 'files' ? (
                <FileBrowser tab={tab} />
              ) : (
                <TerminalPane tab={tab} active={tab.id === activeTabId} />
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
