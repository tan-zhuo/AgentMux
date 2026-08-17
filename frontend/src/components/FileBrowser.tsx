import { Clipboard, Dialogs } from '@wailsio/runtime'
import clsx from 'clsx'
import {
  ArrowUp,
  Boxes,
  ChevronRight,
  ClipboardCopy,
  CornerUpLeft,
  Download,
  File as FileIcon,
  FileCode2,
  Folder,
  FolderPlus,
  Home,
  Link2,
  Pencil,
  RefreshCw,
  Rocket,
  TerminalSquare,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { errText, files as filesApi, on, tree as treeApi, windows as windowsApi } from '../lib/api'
import type { FileEntry, Listing, Transfer } from '../lib/types'
import { useAppStore, type Tab } from '../store/useAppStore'
import { confirmAction } from '../store/useConfirm'
import { openContextMenu, separator } from '../store/useContextMenu'
import { AgentPicker, LaunchHere } from './LaunchHere'
import { Button, Empty, inputClass } from './ui'

function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / Math.pow(1024, i)
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/** Quotes a path for a /bin/sh command line on the host. */
function shellQuote(p: string): string {
  return `'${p.replaceAll("'", `'\\''`)}'`
}

/** Splits a POSIX path into clickable breadcrumb segments. */
function crumbs(p: string): Array<{ label: string; path: string }> {
  const parts = p.split('/').filter(Boolean)
  const out = [{ label: '/', path: '/' }]
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    out.push({ label: part, path: acc })
  }
  return out
}

/**
 * File browser for one host, remote or this computer.
 *
 * It lives in the centre area rather than the side panel because a file manager
 * needs width: a path, a name, a size and a time do not fit in 384 pixels
 * without truncating the only column that matters.
 */
export function FileBrowser({ tab }: { tab: Tab }) {
  const toast = useAppStore((s) => s.toast)
  const setTabState = useAppStore((s) => s.setTabState)

  const [listing, setListing] = useState<Listing | null>(null)
  const [cwd, setCwd] = useState(tab.command ?? '')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string>('')
  const [transfers, setTransfers] = useState<Transfer[]>([])
  const [newDir, setNewDir] = useState<string | null>(null)
  const [renaming, setRenaming] = useState<{ path: string; name: string } | null>(null)
  // Set when the rocket on a row is clicked or a folder's menu asks to run an
  // agent: the picker targets that folder instead of the one currently open, and
  // opens where the pointer already is.
  const [launch, setLaunch] = useState<{ dir: string; x: number; y: number } | null>(null)
  // On this computer there is nothing to transfer: the file is already here, and
  // copying it to another local path is a file manager's job.
  const isLocalHost = useAppStore(
    (s) => s.snapshot.servers.find((x) => x.id === tab.serverId)?.kind === 'local',
  )

  const openTab = useAppStore((s) => s.openTab)
  const detached = useAppStore((s) => s.detached)

  /** Attach to the session the launcher just started. */
  function openAgentTab(session: string, title: string) {
    setLaunch(null)
    if (detached) {
      // This window shows one tab and has no strip to add another to, so the
      // new session gets a window of its own.
      void windowsApi
        .detach(
          {
            title,
            kind: 'tmux',
            serverId: tab.serverId,
            workspaceId: tab.workspaceId,
            agentId: '',
            tmuxSession: session,
            command: '',
            shellId: '',
          },
          Math.round(window.screenX + 60),
          Math.round(window.screenY + 60),
          1000,
          680,
        )
        .catch((e) => toast('error', errText(e)))
      return
    }
    openTab({
      title,
      kind: 'tmux',
      serverId: tab.serverId,
      workspaceId: tab.workspaceId,
      agentId: '',
      tmuxSession: session,
    })
  }

  /** Open a file in the editor. A detached window has no tab strip, so the file
   *  gets a window of its own there instead. */
  function openEditor(entry: FileEntry) {
    const spec = {
      title: entry.name,
      kind: 'editor' as const,
      serverId: tab.serverId,
      workspaceId: tab.workspaceId,
      agentId: '',
      tmuxSession: '',
      command: entry.path,
    }
    if (detached) {
      void windowsApi
        .detach(
          { ...spec, shellId: '' },
          Math.round(window.screenX + 60),
          Math.round(window.screenY + 60),
          1000,
          680,
        )
        .catch((e) => toast('error', errText(e)))
      return
    }
    openTab(spec)
  }

  const load = useCallback(
    async (dir: string) => {
      setLoading(true)
      try {
        const l = await filesApi.list(tab.serverId, dir)
        setListing(l)
        setCwd(l.path)
        setError('')
        // Remember where we were so the tab title and a later reopen make sense.
        setTabState(tab.id, { command: l.path, title: l.path.split('/').pop() || l.path })
      } catch (e) {
        setError(errText(e))
      } finally {
        setLoading(false)
      }
    },
    [tab.serverId, tab.id, setTabState],
  )

  useEffect(() => {
    void load(tab.command ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.serverId])

  useEffect(() => {
    let cancelled = false
    filesApi
      .transfers()
      .then((t) => !cancelled && setTransfers(t))
      .catch(() => {})
    const off = on<Transfer>('transfer:update', (t) => {
      setTransfers((list) => {
        const i = list.findIndex((x) => x.id === t.id)
        if (i === -1) return [t, ...list]
        const copy = [...list]
        copy[i] = t
        return copy
      })
    })
    return () => {
      cancelled = true
      off()
    }
  }, [])

  const entries = listing?.entries ?? []
  const active = transfers.filter((t) => t.status === 'running')

  async function upload() {
    try {
      const picked = await Dialogs.OpenFile({
        Title: `Upload to ${cwd}`,
        AllowsMultipleSelection: true,
        CanChooseFiles: true,
      })
      const list = Array.isArray(picked) ? picked : picked ? [picked] : []
      for (const local of list) {
        await filesApi.upload(tab.serverId, local, cwd)
      }
      if (list.length) toast('info', `Uploading ${list.length} file(s) to ${cwd}`)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  async function download(entry: FileEntry) {
    try {
      const local = await Dialogs.SaveFile({ Title: `Save ${entry.name}`, Filename: entry.name })
      if (!local) return
      await filesApi.download(tab.serverId, entry.path, local)
      toast('info', `Downloading ${entry.name}`)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  /** Creates the folder named in the new-folder bar. */
  async function createDir() {
    const name = (newDir ?? '').trim()
    if (!name) return
    try {
      await filesApi.mkdir(tab.serverId, `${cwd}/${name}`)
      setNewDir(null)
      await load(cwd)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  /**
   * Registers a folder as a project with a workspace in it.
   *
   * The two dialogs ask for what is already on screen — the name is the folder,
   * the path is the path, the server is the one being browsed — so the honest
   * amount of typing here is none. Adding the same folder twice selects what is
   * already there instead of duplicating it, and a folder whose name matches an
   * existing project joins that project rather than standing up a rival copy,
   * which is what the same checkout on a second host should do.
   */
  async function addAsProject(path: string) {
    const { snapshot, refreshSnapshot, select } = useAppStore.getState()
    const name = path.split('/').filter(Boolean).pop() || path
    const already = snapshot.workspaces.find(
      (w) => w.serverId === tab.serverId && w.remotePath === path,
    )
    if (already) {
      select({ kind: 'workspace', id: already.id })
      toast('info', `${already.name} already points at this folder`)
      return
    }
    try {
      const twin = snapshot.projects.find((p) => p.name === name)
      const project =
        twin ??
        (await treeApi.saveProject({
          id: '',
          name,
          description: '',
          folderId: null,
          sort: 0,
          createdAt: 0,
        }))
      const ws = await treeApi.saveWorkspace({
        id: '',
        projectId: project.id,
        serverId: tab.serverId,
        name,
        remotePath: path,
        defaultTmuxSession: '',
        defaultAgentCommand: '',
        env: {},
        sort: 0,
      })
      await refreshSnapshot()
      select({ kind: 'workspace', id: ws.id })
      toast(
        'ok',
        twin
          ? `${name} added to the project ${project.name} — add an agent to it on the left`
          : `${name} is now a project — add an agent to it on the left`,
      )
    } catch (e) {
      toast('error', errText(e))
    }
  }

  /**
   * Applies the inline rename, which only Enter does.
   *
   * Escape and clicking away abandon it instead: a rename is a change to a file
   * an agent may be working in, and a stray click somewhere else is not somebody
   * asking for it. Typed-and-discarded says so, because silence there reads as
   * having been saved.
   */
  async function commitRename() {
    if (!renaming) return
    const name = renaming.name.trim()
    if (!name || name === renaming.path.split('/').pop()) {
      setRenaming(null)
      return
    }
    try {
      await filesApi.rename(tab.serverId, renaming.path, `${cwd}/${name}`)
      setRenaming(null)
      await load(cwd)
    } catch (e) {
      // Left open with the name still in it, so it can be corrected.
      toast('error', errText(e))
    }
  }

  function cancelRename() {
    if (!renaming) return
    if (renaming.name.trim() !== renaming.path.split('/').pop()) {
      toast('info', `${renaming.path.split('/').pop()} was not renamed — Enter applies a new name`)
    }
    setRenaming(null)
  }

  async function remove(entry: FileEntry) {
    const isDir = entry.isDir
    const ok = await confirmAction({
      title: `Delete ${entry.name}`,
      message: isDir
        ? 'The directory and everything inside it is removed from the server.'
        : 'The file is removed from the server.',
      points: [`${entry.path} on this server`, 'There is no undo and no trash to recover it from'],
      confirmLabel: isDir ? 'Delete directory' : 'Delete file',
      requireText: isDir ? entry.name : undefined,
    })
    if (!ok) return
    try {
      await filesApi.remove(tab.serverId, entry.path, entry.isDir)
      await load(cwd)
    } catch (e) {
      toast('error', errText(e))
    }
  }

  return (
    <div className="flex h-full w-full flex-col bg-ink-950">
      {/* Path + actions */}
      <div className="flex items-center gap-1.5 border-b hairline bg-ink-900 px-2.5 py-1.5">
        <Button
          size="sm"
          variant="subtle"
          title="Home"
          onClick={() => void load('')}
        >
          <Home size={12} />
        </Button>
        <Button
          size="sm"
          variant="subtle"
          title="Up one level"
          disabled={!listing?.parent}
          onClick={() => listing?.parent && void load(listing.parent)}
        >
          <ArrowUp size={12} />
        </Button>
        <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto">
          {crumbs(cwd).map((c, i) => (
            <span key={c.path} className="flex shrink-0 items-center">
              {i > 0 && <ChevronRight size={11} className="text-ink-600" />}
              <button
                onClick={() => void load(c.path)}
                className="rounded-control px-1 py-0.5 font-mono text-[11px] text-ink-300 hover:bg-ink-800 hover:text-ink-100"
              >
                {c.label}
              </button>
            </span>
          ))}
        </div>
        <Button size="sm" variant="subtle" title="New folder" onClick={() => setNewDir('')}>
          <FolderPlus size={12} />
        </Button>
        <Button
          size="sm"
          variant="subtle"
          title={`Add ${cwd.split('/').filter(Boolean).pop() || cwd} as a project — a project and a workspace, without the forms`}
          onClick={() => void addAsProject(cwd)}
        >
          <Boxes size={12} />
        </Button>
        <Button size="sm" variant="subtle" title="Refresh" onClick={() => void load(cwd)} disabled={loading}>
          <RefreshCw size={12} className={loading ? 'animate-spin' : undefined} />
        </Button>
        {!isLocalHost && (
          <Button size="sm" onClick={() => void upload()}>
            <Upload size={11} /> Upload
          </Button>
        )}
        <LaunchHere serverId={tab.serverId} dir={cwd} onLaunched={openAgentTab} />
      </div>

      {newDir !== null && (
        <div className="flex gap-1.5 border-b hairline bg-ink-900 px-2.5 py-2">
          <input
            autoFocus
            className={inputClass}
            value={newDir}
            placeholder="new folder name"
            onChange={(e) => setNewDir(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setNewDir(null)
              if (e.key === 'Enter') void createDir()
            }}
          />
          <Button size="sm" variant="primary" onClick={() => void createDir()}>
            Create
          </Button>
          <Button size="sm" onClick={() => setNewDir(null)}>
            Cancel
          </Button>
        </div>
      )}

      {/* Asking which agent to run belongs at the folder that was clicked, not
          in a bar above the listing that has to be found and then cancelled. */}
      {launch && (
        <AgentPicker
          serverId={tab.serverId}
          dir={launch.dir}
          x={launch.x}
          y={launch.y}
          onClose={() => setLaunch(null)}
          onLaunched={openAgentTab}
        />
      )}

      {error && (
        <p className="border-b hairline px-3 py-2 text-[11px] leading-relaxed text-danger">{error}</p>
      )}

      {/* Listing */}
      <div className="min-h-0 flex-1 overflow-auto">
        {!error && entries.length === 0 && !loading && (
          <Empty title="Empty directory" hint="Nothing here. Upload a file or go up a level." />
        )}
        <table className="w-full text-xs">
          <tbody>
            {entries.map((e) => {
              const isDir = e.isDir || e.targetIsDir
              return (
                <tr
                  key={e.path}
                  onClick={() => setSelected(e.path)}
                  onDoubleClick={() => (isDir ? void load(e.path) : openEditor(e))}
                  onContextMenu={(ev) => {
                    setSelected(e.path)
                    // Where the menu was opened, so anything it opens in turn
                    // appears in the same place rather than jumping.
                    const at = { x: ev.clientX, y: ev.clientY }
                    openContextMenu(ev, [
                      isDir
                        ? { label: 'Open', icon: Folder, onSelect: () => void load(e.path) }
                        : { label: 'Edit', icon: FileCode2, onSelect: () => openEditor(e) },
                      isDir || isLocalHost
                        ? {}
                        : {
                            label: 'Download',
                            icon: Download,
                            onSelect: () => void download(e),
                          },
                      isDir
                        ? {
                            label: 'Run agent here',
                            icon: Rocket,
                            onSelect: () => setLaunch({ dir: e.path, ...at }),
                          }
                        : {},
                      isDir
                        ? {
                            label: 'Add as a project',
                            icon: Boxes,
                            hint: 'project and workspace',
                            onSelect: () => void addAsProject(e.path),
                          }
                        : {},
                      isDir
                        ? {
                            label: 'Open terminal here',
                            icon: TerminalSquare,
                            onSelect: () => {
                              openTab({
                                title: e.name,
                                kind: 'command',
                                serverId: tab.serverId,
                                workspaceId: '',
                                agentId: '',
                                tmuxSession: '',
                                command: `cd ${shellQuote(e.path)} && exec "\${SHELL:-/bin/sh}" -l`,
                              })
                            },
                          }
                        : {},
                      separator,
                      {
                        label: 'Copy path',
                        icon: ClipboardCopy,
                        onSelect: () => void Clipboard.SetText(e.path),
                      },
                      {
                        label: 'Copy name',
                        icon: ClipboardCopy,
                        onSelect: () => void Clipboard.SetText(e.name),
                      },
                      separator,
                      {
                        label: 'Rename',
                        icon: Pencil,
                        onSelect: () => setRenaming({ path: e.path, name: e.name }),
                      },
                      {
                        label: isDir ? 'Delete directory' : 'Delete file',
                        icon: Trash2,
                        danger: true,
                        onSelect: () => void remove(e),
                      },
                    ])
                  }}
                  className={clsx(
                    'group cursor-default border-b hairline',
                    // Finder fills the selected row and turns everything in it
                    // white. The cells carry their own greys, so they are
                    // overridden by descendant rules rather than by hoping each
                    // one happens to be legible on system blue.
                    selected === e.path
                      ? 'bg-accent [&_button:hover]:bg-white/20 [&_svg]:text-current ' +
                        '[&_span]:text-white [&_td]:text-white'
                      : 'hover:bg-ink-900',
                  )}
                >
                  <td className="w-6 py-1 pl-3">
                    {e.isLink ? (
                      <Link2 size={13} className="text-ink-500" />
                    ) : isDir ? (
                      <Folder size={13} className="text-accent" />
                    ) : (
                      <FileIcon size={13} className="text-ink-500" />
                    )}
                  </td>
                  <td className="py-1 pl-1.5">
                    {renaming?.path === e.path ? (
                      <input
                        autoFocus
                        className={inputClass}
                        value={renaming.name}
                        onClick={(ev) => ev.stopPropagation()}
                        onChange={(ev) => setRenaming({ path: e.path, name: ev.target.value })}
                        // Clicking anywhere else is how a rename is taken back.
                        // Losing the window is not a decision, though, so
                        // switching apps and back finds the field as it was.
                        onBlur={() => {
                          if (document.hasFocus()) cancelRename()
                        }}
                        onKeyDown={(ev) => {
                          if (ev.key === 'Escape') setRenaming(null)
                          if (ev.key === 'Enter') void commitRename()
                        }}
                      />
                    ) : (
                      <span className={clsx('truncate', isDir ? 'text-ink-100' : 'text-ink-200')}>
                        {e.name}
                        {e.isLink && e.target && (
                          <span className="ml-1.5 font-mono text-[10.5px] text-ink-600">→ {e.target}</span>
                        )}
                      </span>
                    )}
                  </td>
                  <td className="w-24 py-1 pr-2 text-right tabular-nums text-ink-500">
                    {isDir ? '' : bytes(e.size)}
                  </td>
                  <td className="w-36 py-1 pr-2 text-right tabular-nums text-ink-600">
                    {e.modTime ? new Date(e.modTime * 1000).toLocaleString() : ''}
                  </td>
                  <td className="w-24 py-1 pr-2 font-mono text-[10.5px] text-ink-600">{e.mode}</td>
                  <td className="w-28 py-1 pr-3">
                    <span className="flex items-center justify-end gap-0.5 opacity-0 group-hover:opacity-100">
                      {isDir && (
                        <button
                          title={`Add ${e.name} as a project`}
                          onClick={(ev) => {
                            ev.stopPropagation()
                            void addAsProject(e.path)
                          }}
                          className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-accent"
                        >
                          <Boxes size={12} />
                        </button>
                      )}
                      {isDir && (
                        <button
                          title={`Run an agent in ${e.name}`}
                          onClick={(ev) => {
                            ev.stopPropagation()
                            setLaunch({ dir: e.path, x: ev.clientX, y: ev.clientY })
                          }}
                          className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-accent"
                        >
                          <Rocket size={12} />
                        </button>
                      )}
                      {!isDir && (
                        <button
                          title="Edit"
                          onClick={(ev) => {
                            ev.stopPropagation()
                            openEditor(e)
                          }}
                          className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-accent"
                        >
                          <FileCode2 size={12} />
                        </button>
                      )}
                      {!isDir && !isLocalHost && (
                        <button
                          title="Download"
                          onClick={(ev) => {
                            ev.stopPropagation()
                            void download(e)
                          }}
                          className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
                        >
                          <Download size={12} />
                        </button>
                      )}
                      <button
                        title="Rename"
                        onClick={(ev) => {
                          ev.stopPropagation()
                          setRenaming({ path: e.path, name: e.name })
                        }}
                        className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
                      >
                        <Pencil size={12} />
                      </button>
                      <button
                        title="Delete"
                        onClick={(ev) => {
                          ev.stopPropagation()
                          void remove(e)
                        }}
                        className="rounded-control p-1 text-ink-400 hover:bg-ink-800 hover:text-danger"
                      >
                        <Trash2 size={12} />
                      </button>
                    </span>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Transfers */}
      {transfers.length > 0 && (
        <div className="max-h-40 shrink-0 overflow-y-auto border-t hairline bg-ink-900">
          <div className="flex items-center justify-between px-3 py-1.5">
            <span className="text-[11px] font-semibold text-ink-300">
              Transfers {active.length > 0 && `(${active.length} running)`}
            </span>
            <Button
              size="sm"
              variant="subtle"
              onClick={async () => {
                await filesApi.clearFinished()
                setTransfers(await filesApi.transfers())
              }}
            >
              Clear finished
            </Button>
          </div>
          {transfers.map((t) => {
            const pct = t.size > 0 ? (100 * t.done) / t.size : 0
            return (
              <div key={t.id} className="px-3 py-1">
                <div className="flex items-center gap-2 text-[11px]">
                  {t.kind === 'upload' ? (
                    <Upload size={11} className="shrink-0 text-ink-500" />
                  ) : (
                    <Download size={11} className="shrink-0 text-ink-500" />
                  )}
                  <span className="min-w-0 flex-1 truncate font-mono text-ink-300">
                    {t.kind === 'upload' ? t.remote : t.local}
                  </span>
                  <span className="shrink-0 tabular-nums text-ink-500">
                    {bytes(t.done)} / {bytes(t.size)}
                  </span>
                  <span
                    className={clsx(
                      'w-16 shrink-0 text-right',
                      t.status === 'done' && 'text-ok',
                      t.status === 'error' && 'text-danger',
                      t.status === 'cancelled' && 'text-ink-500',
                      t.status === 'running' && 'text-ink-300',
                    )}
                  >
                    {t.status === 'running' ? `${pct.toFixed(0)}%` : t.status}
                  </span>
                  {t.status === 'running' && (
                    <button
                      title="Cancel"
                      onClick={() => void filesApi.cancel(t.id)}
                      className="rounded-control p-0.5 text-ink-500 hover:text-danger"
                    >
                      <X size={11} />
                    </button>
                  )}
                </div>
                {t.status === 'running' && (
                  <div className="mt-1 h-1 w-full overflow-hidden rounded-capsule bg-accent/20">
                    <div className="h-full rounded-capsule bg-accent" style={{ width: `${pct}%` }} />
                  </div>
                )}
                {t.error && <p className="mt-0.5 text-[10.5px] text-danger">{t.error}</p>}
              </div>
            )
          })}
        </div>
      )}

      <div className="flex shrink-0 items-center gap-2 border-t hairline bg-ink-900 px-3 py-1 text-[10.5px] text-ink-600">
        <CornerUpLeft size={10} />
        Double-click a folder to open it. Drops and directory transfers are not supported yet.
      </div>
    </div>
  )
}
