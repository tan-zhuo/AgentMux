import clsx from 'clsx'
import { Bot, Rocket, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { agents as agentApi, errText, toolkit } from '../lib/api'
import type { AgentChoice } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { Button, inputClass } from './ui'

/** The panel's fixed width, needed before it is measured to keep the first
 *  frame from being drawn off the edge of the window. */
const PANEL_W = 320

/**
 * Starts an agent in a directory in one step.
 *
 * The whole point is to skip the setup: no project, no workspace, no agent
 * record. It names a tmux session after the folder, creates it there, runs the
 * command, and attaches — which is the sequence people were doing by hand.
 *
 * It opens at a point rather than under a trigger, because the thing being
 * launched into is usually a folder that was just clicked, and a panel that
 * appears somewhere else means crossing the window to answer a question raised
 * where the pointer already is.
 */
export function AgentPicker({
  serverId,
  dir,
  x,
  y,
  onClose,
  onLaunched,
}: {
  serverId: string
  dir: string
  x: number
  y: number
  onClose: () => void
  onLaunched: (session: string, title: string) => void
}) {
  const toast = useAppStore((s) => s.toast)
  const [choices, setChoices] = useState<AgentChoice[] | null>(null)
  const [custom, setCustom] = useState('')
  const [busy, setBusy] = useState(false)
  const [cursor, setCursor] = useState(0)
  const boxRef = useRef<HTMLDivElement | null>(null)
  const [pos, setPos] = useState({ left: x, top: y })

  const folder = dir.split('/').filter(Boolean).pop() || dir

  // Placed at the pointer, then nudged back inside the window — the list grows
  // when the detected agents arrive, so this re-runs on that too.
  useLayoutEffect(() => {
    const el = boxRef.current
    const w = el?.offsetWidth ?? PANEL_W
    const h = el?.offsetHeight ?? 260
    const pad = 8
    const next = {
      left: Math.max(pad, Math.min(x, window.innerWidth - w - pad)),
      top: Math.max(pad, Math.min(y, window.innerHeight - h - pad)),
    }
    setPos((prev) => (prev.left === next.left && prev.top === next.top ? prev : next))
  }, [x, y, choices])

  useEffect(() => {
    let cancelled = false
    toolkit
      .installedAgents(serverId)
      .then((c) => !cancelled && setChoices(c))
      .catch(() => !cancelled && setChoices([]))
    return () => {
      cancelled = true
    }
  }, [serverId])

  // Takes focus the way a menu does, so the arrow keys reach the list rather
  // than the terminal underneath, and hands it back on the way out.
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null
    boxRef.current?.focus()
    return () => {
      if (before && document.contains(before)) before.focus()
    }
  }, [])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [onClose])

  async function launch(command: string) {
    const cmd = command.trim()
    if (!cmd) return
    setBusy(true)
    try {
      const r = await agentApi.launchInDir(serverId, dir, cmd)
      onClose()
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

  const list = choices ?? []

  return (
    <div
      ref={boxRef}
      style={{ left: pos.left, top: pos.top, width: PANEL_W }}
      // Arrow keys and Enter, so a picker that opened under the pointer can also
      // be answered from the keyboard. Keys typed into the command field below
      // are its own business.
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setCursor((c) => Math.min(c + 1, list.length - 1))
        } else if (e.key === 'ArrowUp') {
          e.preventDefault()
          setCursor((c) => Math.max(c - 1, 0))
        } else if (e.key === 'Enter') {
          e.preventDefault()
          const pick = list[cursor]
          if (pick) void launch(pick.command)
        }
      }}
      tabIndex={-1}
      className="fixed z-50 overflow-hidden rounded-card border hairline bg-ink-850 shadow-2xl outline-none"
    >
      <div className="flex items-start justify-between gap-2 border-b hairline px-3 py-2">
        <div className="min-w-0">
          <p className="text-xs font-semibold text-ink-100">Run an agent in {folder}</p>
          <p className="mt-0.5 truncate font-mono text-[10.5px] text-ink-500">{dir}</p>
          <p className="mt-1 text-[10.5px] text-ink-500">
            Creates the tmux session{' '}
            <span className="font-mono text-ink-400">agentmux/{slug(folder)}</span>
          </p>
        </div>
        <button
          onClick={onClose}
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
        {list.map((c, i) => (
          <button
            key={c.id}
            disabled={busy}
            onMouseEnter={() => setCursor(i)}
            onClick={() => void launch(c.command)}
            className={clsx(
              'flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs disabled:opacity-50',
              i === cursor ? 'bg-accent text-white' : 'text-ink-200 hover:bg-ink-800',
            )}
          >
            <Bot size={13} className={clsx('shrink-0', i === cursor ? 'text-white' : 'text-accent')} />
            <span className="min-w-0 flex-1 truncate">{c.name}</span>
            <span
              className={clsx(
                'shrink-0 font-mono text-[10.5px]',
                i === cursor ? 'text-white/70' : 'text-ink-600',
              )}
            >
              {c.command}
            </span>
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
  )
}

/**
 * The toolbar button for the directory currently open.
 *
 * It opens the same picker as a folder row does, anchored under itself: a button
 * that answers where it was pressed, rather than in a bar somewhere above.
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
  const [at, setAt] = useState<{ x: number; y: number } | null>(null)
  const anchorRef = useRef<HTMLSpanElement | null>(null)
  const folder = dir.split('/').filter(Boolean).pop() || dir

  return (
    <>
      {/* The picker closes on any mousedown outside itself, and this button is
          outside it. Stopping the event here keeps the button a toggle instead
          of a close followed immediately by a reopen. */}
      <span ref={anchorRef} onMouseDown={(e) => e.stopPropagation()}>
        <Button
          size="sm"
          variant="primary"
          onClick={() => {
            if (at) {
              setAt(null)
              return
            }
            // Right-aligned to the button, which sits at the right end of the
            // toolbar; the picker then clamps itself inside the window.
            const r = anchorRef.current?.getBoundingClientRect()
            setAt({ x: (r?.right ?? PANEL_W) - PANEL_W, y: (r?.bottom ?? 0) + 4 })
          }}
          title={`Start an agent in ${folder}`}
        >
          <Rocket size={11} /> Run agent here
        </Button>
      </span>

      {at && (
        <AgentPicker
          serverId={serverId}
          dir={dir}
          x={at.x}
          y={at.y}
          onClose={() => setAt(null)}
          onLaunched={onLaunched}
        />
      )}
    </>
  )
}

/** Mirrors the backend's session naming so the panel can show it up front. */
function slug(s: string): string {
  const out = s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return (out || 'untitled').slice(0, 40)
}
