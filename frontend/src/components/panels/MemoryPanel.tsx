import clsx from 'clsx'
import {
  AlertTriangle,
  Brain,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  Trash2,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { errText, memory as memoryApi, on } from '../../lib/api'
import type { Memory, MemoryKind, MemoryStats, ReindexStatus } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { confirmAction } from '../../store/useConfirm'
import { Badge, Button, Empty, Segmented, inputClass, textareaClass } from '../ui'

const kindLabels: Record<MemoryKind, string> = {
  project_fact: 'project',
  agent_event: 'agent',
  user_pref: 'preference',
  session_ctx: 'session',
  system_log: 'log',
}

/**
 * The memory library: what the orchestrator has been told and what it has
 * worked out, in one list that can be read, searched and pruned by hand.
 *
 * Making this visible is not a debugging affordance. A system that remembers
 * things about you and cannot show you what it remembers is one you have no way
 * to correct.
 */
export function MemoryPanel() {
  const toast = useAppStore((s) => s.toast)

  const [rows, setRows] = useState<Memory[]>([])
  const [scores, setScores] = useState<Record<string, number>>({})
  const [stats, setStats] = useState<MemoryStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [query, setQuery] = useState('')
  const [semantic, setSemantic] = useState(false)
  const [adding, setAdding] = useState(false)
  const [reindex, setReindex] = useState<ReindexStatus | null>(null)

  // Guards against an older search landing after a newer one and overwriting
  // it, which is what makes a fast typist see results for a prefix of what they
  // typed.
  const seq = useRef(0)

  const load = useCallback(async () => {
    const mine = ++seq.current
    setLoading(true)
    try {
      const [list, s] = await Promise.all([
        memoryApi.list({ text: semantic ? '' : query, limit: 200 }),
        memoryApi.stats(),
      ])
      if (mine !== seq.current) return
      setRows(list ?? [])
      setScores({})
      setStats(s)
    } catch (e) {
      if (mine === seq.current) toast('error', errText(e))
    } finally {
      if (mine === seq.current) setLoading(false)
    }
  }, [query, semantic, toast])

  const runSemantic = useCallback(async () => {
    const text = query.trim()
    if (!text) return
    const mine = ++seq.current
    setLoading(true)
    try {
      const hits = await memoryApi.search({ text, topK: 20 })
      if (mine !== seq.current) return
      setRows((hits ?? []).map((h) => h.memory))
      setScores(Object.fromEntries((hits ?? []).map((h) => [h.memory.id, h.score])))
    } catch (e) {
      // Semantic search needs the embedder, so this is where "Ollama is not
      // running" surfaces. Saying so beats an empty list, which reads as
      // "nothing was remembered".
      if (mine === seq.current) toast('error', errText(e))
    } finally {
      if (mine === seq.current) setLoading(false)
    }
  }, [query, toast])

  useEffect(() => {
    if (semantic) return
    // Debounced so typing does not run a query per keystroke.
    const t = setTimeout(() => void load(), 200)
    return () => clearTimeout(t)
  }, [load, semantic])

  useEffect(() => {
    const off = on<ReindexStatus>('memory:reindex', (s) => {
      setReindex(s?.running ? s : null)
      if (s && !s.running) {
        if (s.error) toast('error', s.error)
        else toast('ok', 'Memory index rebuilt')
        void load()
      }
    })
    return off
  }, [load, toast])

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b hairline px-3 py-2">
        <span className="text-[11px] font-semibold text-ink-300">
          Memory {stats ? `(${stats.total})` : ''}
        </span>
        <div className="flex gap-1">
          <Button size="sm" variant="subtle" onClick={() => setAdding((v) => !v)} title="Add a memory">
            <Plus size={11} />
          </Button>
          <Button
            size="sm"
            variant="subtle"
            onClick={() => void load()}
            disabled={loading}
            title="Refresh"
          >
            <RefreshCw size={11} className={loading ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      <div className="flex gap-1.5 border-b hairline px-3 py-2">
        <div className="relative min-w-0 flex-1">
          <Search
            size={11}
            className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2 text-ink-500"
          />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && semantic) void runSemantic()
              if (e.key === 'Escape') setQuery('')
            }}
            placeholder={semantic ? 'Describe it, then press Enter' : 'Find text…'}
            className={`${inputClass} pl-6`}
          />
        </div>
        {/* Two ways of searching, not a mode flag on one of them. A segmented
            control says they are alternatives; a highlighted button did not. */}
        <Segmented<'text' | 'meaning'>
          size="sm"
          value={semantic ? 'meaning' : 'text'}
          onChange={(next) => {
            const on = next === 'meaning'
            setSemantic(on)
            if (!on) void load()
          }}
          options={[
            { value: 'text', label: 'Text', title: 'Find these exact characters' },
            {
              value: 'meaning',
              label: (
                <>
                  <Sparkles size={10} /> Meaning
                </>
              ),
              title: 'Find memories about this, using the embedding model',
            },
          ]}
        />
      </div>

      {stats?.needsRebuild && !reindex && (
        <div className="flex items-start gap-2 border-b hairline bg-warn/5 px-3 py-2">
          <AlertTriangle size={12} className="mt-0.5 shrink-0 text-warn" />
          <div className="min-w-0 flex-1">
            <p className="text-[11px] leading-relaxed text-ink-200">
              {stats.pending} of {stats.total} memories have no vector for{' '}
              <span className="font-mono">{stats.model || 'the current model'}</span>. They cannot be
              found by meaning until the index is rebuilt.
            </p>
            <Button
              size="sm"
              variant="primary"
              className="mt-1.5"
              onClick={async () => {
                try {
                  await memoryApi.reindex()
                  setReindex({ running: true, done: 0, total: stats.pending, error: '' })
                } catch (e) {
                  toast('error', errText(e))
                }
              }}
            >
              Rebuild index
            </Button>
          </div>
        </div>
      )}

      {reindex?.running && (
        <div className="border-b hairline px-3 py-2">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 text-[11px] text-ink-300">
              Rebuilding {reindex.done}/{reindex.total}
            </span>
            <Button size="sm" variant="subtle" onClick={() => void memoryApi.cancelReindex()}>
              <X size={11} /> Stop
            </Button>
          </div>
          <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-ink-800">
            <div
              className="h-full bg-accent transition-[width] duration-300"
              style={{
                width: `${reindex.total ? Math.round((reindex.done / reindex.total) * 100) : 0}%`,
              }}
            />
          </div>
        </div>
      )}

      {adding && <AddForm onDone={() => { setAdding(false); void load() }} />}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {rows.length === 0 && !loading && (
          <Empty
            title={query ? 'Nothing matched' : 'No memories yet'}
            hint={
              query
                ? 'Try fewer words, or switch between text and meaning.'
                : 'Facts about a project, preferences you have stated, and what agents did are remembered here.'
            }
          />
        )}
        {rows.map((m) => (
          <Row key={m.id} memory={m} score={scores[m.id]} onDeleted={() => void load()} />
        ))}
      </div>
    </div>
  )
}

function Row({
  memory,
  score,
  onDeleted,
}: {
  memory: Memory
  score?: number
  onDeleted: () => void
}) {
  const toast = useAppStore((s) => s.toast)
  const [open, setOpen] = useState(false)

  return (
    <div className="border-b hairline px-3 py-2">
      <div className="flex items-center gap-1.5">
        <Brain size={11} className="shrink-0 text-ink-600" />
        <button
          onClick={() => setOpen((v) => !v)}
          className="min-w-0 flex-1 truncate text-left text-[11px] text-ink-100 hover:text-accent"
        >
          {memory.title || memory.body.slice(0, 80)}
        </button>
        {score !== undefined && <Badge tone="accent">{score.toFixed(2)}</Badge>}
        <Badge>{kindLabels[memory.kind] ?? memory.kind}</Badge>
        {!memory.hasVector && (
          <span title="Not embedded — this memory cannot be found by meaning yet">
            <Badge tone="warn">no vector</Badge>
          </span>
        )}
        {memory.redacted && (
          <span title="A secret was removed from this text before it was stored">
            <ShieldCheck size={11} className="shrink-0 text-ok" />
          </span>
        )}
        <button
          title="Forget this"
          onClick={async () => {
            const ok = await confirmAction({
              title: 'Forget this memory',
              message: 'It is removed from the library and will not be retrieved again.',
              confirmLabel: 'Forget',
            })
            if (!ok) return
            try {
              await memoryApi.remove(memory.id)
              onDeleted()
            } catch (e) {
              toast('error', errText(e))
            }
          }}
          className="rounded p-1 text-ink-400 hover:bg-ink-750 hover:text-danger"
        >
          <Trash2 size={12} />
        </button>
      </div>
      <p
        className={clsx(
          'mt-0.5 pl-4 text-[11px] leading-relaxed whitespace-pre-wrap text-ink-400',
          !open && 'line-clamp-2',
        )}
      >
        {memory.body}
      </p>
      {open && (
        <p className="mt-1 pl-4 text-[10px] text-ink-600">
          {new Date(memory.createdAt * 1000).toLocaleString()}
          {memory.source && ` · ${memory.source}`}
          {memory.useCount > 0 && ` · used ${memory.useCount}×`}
          {memory.embeddingModel && ` · ${memory.embeddingModel}`}
        </p>
      )}
    </div>
  )
}

function AddForm({ onDone }: { onDone: () => void }) {
  const toast = useAppStore((s) => s.toast)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [kind, setKind] = useState<MemoryKind>('user_pref')
  const [saving, setSaving] = useState(false)

  return (
    <div className="space-y-1.5 border-b hairline px-3 py-2">
      <div className="flex gap-1.5">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as MemoryKind)}
          className={`${inputClass} w-32 shrink-0`}
        >
          {Object.entries(kindLabels).map(([k, label]) => (
            <option key={k} value={k}>
              {label}
            </option>
          ))}
        </select>
        <input
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Title"
          className={inputClass}
        />
      </div>
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={3}
        placeholder="What should be remembered?"
        className={textareaClass}
      />
      <div className="flex justify-end gap-1.5">
        <Button size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button
          size="sm"
          variant="primary"
          disabled={saving || !body.trim()}
          onClick={async () => {
            setSaving(true)
            try {
              const res = await memoryApi.add({ kind, title: title.trim(), body: body.trim() })
              // Stored but unsearchable is a real state worth naming, not a
              // silent success: it happens whenever Ollama is not running.
              if (res.embedError) {
                toast('error', `Saved, but not embedded: ${res.embedError}`)
              } else {
                toast('ok', 'Remembered')
              }
              onDone()
            } catch (e) {
              toast('error', errText(e))
            } finally {
              setSaving(false)
            }
          }}
        >
          Remember
        </Button>
      </div>
    </div>
  )
}
