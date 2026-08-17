import clsx from 'clsx'
import { Bot, Rocket, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { agents as agentApi, errText, toolkit } from '../lib/api'
import type { AgentChoice } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { Button, inputClass } from './ui'

/**
 * Starts an agent in a directory in one step.
 *
 * The whole point is to skip the setup: no project, no workspace, no agent
 * record. It names a tmux session after the folder, creates it there, runs the
 * command, and attaches — which is the sequence people were doing by hand.
 */
export function LaunchHere({
  serverId,
  dir,
  onLaunched,
}: {
  serverId: string
  dir: string
  onLaunched: (session: string, title: string) => void
}) {
  const toast = useAppStore((s) => s.toast)
  const [open, setOpen] = useState(false)
  const [choices, setChoices] = useState<AgentChoice[] | null>(null)
  const [custom, setCustom] = useState('')
  const [busy, setBusy] = useState(false)
  const boxRef = useRef<HTMLDivElement | null>(null)

  const folder = dir.split('/').filter(Boolean).pop() || dir

  useEffect(() => {
    if (!open) return
    setChoices(null)
    let cancelled = false
    toolkit
      .installedAgents(serverId)
      .then((c) => !cancelled && setChoices(c))
      .catch(() => !cancelled && setChoices([]))
    return () => {
      cancelled = true
    }
  }, [open, serverId])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  async function launch(command: string) {
    const cmd = command.trim()
    if (!cmd) return
    setBusy(true)
    try {
      const r = await agentApi.launchInDir(serverId, dir, cmd)
      setOpen(false)
      onLaunched(r.session, folder)
      if (r.reusedSession) {
        toast('info', `${r.session} already had something running — attached to it instead`)
      } else {
        toast('ok', `${cmd} started in ${r.session}`)
      }
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative" ref={boxRef}>
      <Button
        size="sm"
        variant="primary"
        onClick={() => setOpen((v) => !v)}
        title={`Start an agent in ${folder}`}
      >
        <Rocket size={11} /> Run agent here
      </Button>

      {open && (
        <div className="absolute right-0 z-40 mt-1 w-80 overflow-hidden rounded-card border hairline bg-ink-850 shadow-2xl">
          <div className="flex items-start justify-between gap-2 border-b hairline px-3 py-2">
            <div className="min-w-0">
              <p className="text-xs font-semibold text-ink-100">Run an agent in {folder}</p>
              <p className="mt-0.5 truncate font-mono text-[10.5px] text-ink-500">{dir}</p>
              <p className="mt-1 text-[10.5px] text-ink-500">
                Creates the tmux session <span className="font-mono text-ink-400">agentmux/{slug(folder)}</span>
              </p>
            </div>
            <button
              onClick={() => setOpen(false)}
              className="rounded-control p-1 text-ink-500 hover:bg-ink-800 hover:text-ink-100"
            >
              <X size={12} />
            </button>
          </div>

          <div className="max-h-56 overflow-y-auto py-1">
            {choices === null && (
              <p className="px-3 py-2 text-[11px] text-ink-500">Checking what this server has…</p>
            )}
            {choices?.length === 0 && (
              <p className="px-3 py-2 text-[11px] leading-relaxed text-ink-500">
                No agent CLI detected on this server. Type a command below, or install one from the
                Install panel.
              </p>
            )}
            {choices?.map((c) => (
              <button
                key={c.id}
                disabled={busy}
                onClick={() => void launch(c.command)}
                className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-ink-200 hover:bg-ink-800 disabled:opacity-50"
              >
                <Bot size={13} className="shrink-0 text-accent" />
                <span className="min-w-0 flex-1 truncate">{c.name}</span>
                <span className="shrink-0 font-mono text-[10.5px] text-ink-600">{c.command}</span>
              </button>
            ))}
          </div>

          <div className="border-t hairline px-3 py-2">
            <label className="mb-1 block text-[10.5px] text-ink-500">Or run something else</label>
            <div className="flex gap-1.5">
              <input
                value={custom}
                onChange={(e) => setCustom(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void launch(custom)
                }}
                placeholder="claude --resume"
                className={clsx(inputClass, 'font-mono')}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={busy || !custom.trim()}
                onClick={() => void launch(custom)}
              >
                Run
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

/** Mirrors the backend's session naming so the popover can show it up front. */
function slug(s: string): string {
  const out = s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return (out || 'untitled').slice(0, 40)
}
