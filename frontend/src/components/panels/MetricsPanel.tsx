import clsx from 'clsx'
import { Activity, RefreshCw, Table2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { errText, metrics as metricsApi } from '../../lib/api'
import type { MetricSample } from '../../lib/types'
import { Button, Empty } from '../ui'

const POLL_MS = 3000
const HISTORY = 40

/** Severity thresholds shared by every usage meter, so 80% means the same thing
 *  whether it is CPU, memory or a disk. */
function severity(pct: number): 'ok' | 'warn' | 'danger' {
  if (pct >= 90) return 'danger'
  if (pct >= 75) return 'warn'
  return 'ok'
}

const severityVar: Record<'ok' | 'warn' | 'danger', string> = {
  ok: 'var(--color-accent)',
  warn: 'var(--color-warn)',
  danger: 'var(--color-danger)',
}

function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / Math.pow(1024, i)
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

function rate(n: number): string {
  return `${bytes(n)}/s`
}

function duration(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/**
 * A single-series trend for one stat tile.
 *
 * One series, so no legend: the tile's label already says what is plotted. The
 * hover layer reports the value under the pointer, and the table view below the
 * panel carries the same numbers so nothing is reachable only by hovering.
 */
function Sparkline({
  values,
  color,
  height = 34,
}: {
  values: number[]
  color: string
  height?: number
}) {
  const [hover, setHover] = useState<number | null>(null)
  const ref = useRef<SVGSVGElement | null>(null)
  const w = 100
  const h = height

  if (values.length < 2) {
    return <div style={{ height }} className="flex items-end text-[10px] text-ink-600">collecting…</div>
  }

  // Percentages share a fixed 0-100 domain so the line's height means the same
  // thing between refreshes; a self-scaling axis would make 2% look alarming.
  const max = 100
  const step = w / (values.length - 1)
  const pt = (v: number, i: number) => [i * step, h - (Math.min(v, max) / max) * (h - 3) - 1.5] as const
  const line = values.map((v, i) => pt(v, i).join(',')).join(' ')
  const area = `0,${h} ${line} ${w},${h}`
  const last = values[values.length - 1]
  const [lx, ly] = pt(last, values.length - 1)

  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = ref.current?.getBoundingClientRect()
    if (!rect) return
    const x = ((e.clientX - rect.left) / rect.width) * w
    setHover(Math.max(0, Math.min(values.length - 1, Math.round(x / step))))
  }

  const hx = hover === null ? 0 : hover * step

  return (
    <div className="relative">
      <svg
        ref={ref}
        viewBox={`0 0 ${w} ${h}`}
        preserveAspectRatio="none"
        className="w-full"
        style={{ height }}
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        {/* Area wash at ~10%, never a saturated block. */}
        <polygon points={area} fill={color} opacity={0.1} />
        <polyline
          points={line}
          fill="none"
          stroke={color}
          strokeWidth={2}
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
        {hover !== null && (
          <line
            x1={hx}
            y1={0}
            x2={hx}
            y2={h}
            stroke="var(--color-ink-500)"
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
        )}
        {/* End marker: 2px ring in the surface colour keeps it legible over the line. */}
        <circle cx={lx} cy={ly} r={2.6} fill={color} stroke="var(--color-ink-850)" strokeWidth={2}
          vectorEffect="non-scaling-stroke" />
      </svg>
      {hover !== null && (
        <span className="pointer-events-none absolute -top-1 right-0 rounded bg-ink-800 px-1.5 py-0.5 text-[10px] text-ink-200 tabular-nums">
          {values[hover].toFixed(0)}%
        </span>
      )}
    </div>
  )
}

/** Meter: fill carries severity, track is a lighter step of the same hue so the
 *  state reads across the whole bar. */
function Meter({ pct, tone }: { pct: number; tone?: 'ok' | 'warn' | 'danger' }) {
  const sev = tone ?? severity(pct)
  const color = severityVar[sev]
  return (
    <div
      className="h-1.5 w-full overflow-hidden rounded-full"
      style={{ background: `color-mix(in oklab, ${color} 18%, transparent)` }}
    >
      <div
        className="h-full rounded-full transition-[width] duration-500"
        style={{ width: `${Math.max(0, Math.min(100, pct))}%`, background: color }}
      />
    </div>
  )
}

function Tile({
  label,
  value,
  sub,
  children,
}: {
  label: string
  value: string
  sub?: string
  children?: React.ReactNode
}) {
  return (
    <div className="rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2">
      <p className="text-[10px] tracking-wide text-ink-500 uppercase">{label}</p>
      <p className="mt-0.5 text-lg leading-none font-semibold text-ink-100">{value}</p>
      {sub && <p className="mt-1 text-[10.5px] text-ink-500">{sub}</p>}
      {children}
    </div>
  )
}

export function MetricsPanel({ serverId }: { serverId: string }) {
  const [sample, setSample] = useState<MetricSample | null>(null)
  const [cpuHistory, setCpuHistory] = useState<number[]>([])
  const [memHistory, setMemHistory] = useState<number[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [showTable, setShowTable] = useState(false)
  const [rows, setRows] = useState<Array<{ at: number; cpu: number; mem: number }>>([])
  const alive = useRef(true)

  const poll = useCallback(async () => {
    setBusy(true)
    try {
      const s = await metricsApi.sample(serverId)
      if (!alive.current) return
      if (!s.ok) {
        setError(s.error || 'could not read host metrics')
        return
      }
      setError('')
      // Previous values stay on screen while a refresh is in flight, so the
      // panel never flashes an empty skeleton every three seconds.
      setSample(s)
      setCpuHistory((h) => [...h, s.cpuPercent].slice(-HISTORY))
      setMemHistory((h) => [...h, s.memPercent].slice(-HISTORY))
      setRows((r) => [...r, { at: s.at, cpu: s.cpuPercent, mem: s.memPercent }].slice(-HISTORY))
    } catch (e) {
      if (alive.current) setError(errText(e))
    } finally {
      if (alive.current) setBusy(false)
    }
  }, [serverId])

  useEffect(() => {
    alive.current = true
    setSample(null)
    setCpuHistory([])
    setMemHistory([])
    setRows([])

    // Each sample is a real command on a real server. Polling every three
    // seconds while the window is minimised would keep every watched host busy
    // for nobody's benefit, so the timer stops when the window is hidden and
    // takes a fresh sample the moment it comes back.
    let timer = 0
    const start = () => {
      if (timer) return
      void poll()
      timer = window.setInterval(() => void poll(), POLL_MS)
    }
    const stop = () => {
      window.clearInterval(timer)
      timer = 0
    }
    const onVisibility = () => (document.hidden ? stop() : start())

    if (!document.hidden) start()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      alive.current = false
      document.removeEventListener('visibilitychange', onVisibility)
      stop()
    }
  }, [poll])

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-ink-800 px-3 py-2">
        <span className="flex items-center gap-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
          <Activity size={11} /> Host metrics
        </span>
        <div className="flex gap-1">
          <Button
            size="sm"
            variant="subtle"
            onClick={() => setShowTable((v) => !v)}
            title="Table view"
          >
            <Table2 size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={() => void poll()} disabled={busy} title="Refresh now">
            <RefreshCw size={11} className={busy ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {error && (
        <p className="border-b border-ink-800 px-3 py-2 text-[11px] leading-relaxed text-danger">{error}</p>
      )}

      {!sample && !error ? (
        <Empty title="Reading host" hint="Sampling CPU, memory, disks, I/O and processes." />
      ) : (
        sample && (
          <div className={clsx('min-h-0 flex-1 overflow-y-auto px-3 py-2.5', busy && 'opacity-95')}>
            {(sample.distro || sample.kernel) && (
              <p className="mb-2 truncate font-mono text-[10.5px] text-ink-500">
                {[sample.distro, sample.kernel, sample.arch].filter(Boolean).join(' · ')}
              </p>
            )}
            <div className="grid grid-cols-2 gap-2">
              <Tile
                label="CPU"
                value={`${sample.cpuPercent.toFixed(0)}%`}
                sub={`usr ${sample.cpuUser.toFixed(0)} · sys ${sample.cpuSystem.toFixed(0)} · io ${sample.cpuIowait.toFixed(0)}${
                  sample.cpuSteal >= 1 ? ` · steal ${sample.cpuSteal.toFixed(0)}` : ''
                }`}
              >
                <div className="mt-1.5">
                  <Sparkline values={cpuHistory} color={severityVar[severity(sample.cpuPercent)]} />
                </div>
              </Tile>
              <Tile
                label="Memory"
                value={`${sample.memPercent.toFixed(0)}%`}
                sub={`${bytes(sample.memUsedBytes)} of ${bytes(sample.memTotalBytes)}`}
              >
                <div className="mt-1.5">
                  <Sparkline values={memHistory} color={severityVar[severity(sample.memPercent)]} />
                </div>
              </Tile>
            </div>

            {/* Per-core utilisation. Height carries the value; a single hue
                keeps it from double-encoding what the bars already show. */}
            {sample.perCore.length > 1 && (
              <div className="mt-2 rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2">
                <div className="flex items-baseline justify-between">
                  <span className="text-[10px] tracking-wide text-ink-500 uppercase">
                    Per core ({sample.perCore.length})
                  </span>
                  <span className="text-[10.5px] tabular-nums text-ink-500">
                    busiest {Math.max(...sample.perCore).toFixed(0)}%
                  </span>
                </div>
                {/* A hairline baseline so an idle machine reads as bars at zero
                    rather than a row of floating dashes. */}
                <div className="mt-2 flex h-8 items-end gap-[2px] border-b border-ink-700">
                  {sample.perCore.map((v, i) => (
                    <span
                      key={i}
                      title={`core ${i}: ${v.toFixed(0)}%`}
                      className="min-w-0 flex-1 rounded-t-[2px] bg-accent"
                      style={{ height: `${Math.max(3, v)}%` }}
                    />
                  ))}
                </div>
              </div>
            )}

            <div className="mt-2 grid grid-cols-2 gap-2">
              <Tile label="Uptime" value={duration(sample.uptimeSeconds)} sub={`${sample.processes} processes`} />
              <Tile
                label="Network"
                value={rate(sample.nets.reduce((a, n) => a + n.rxps, 0))}
                sub={`up ${rate(sample.nets.reduce((a, n) => a + n.txps, 0))}`}
              />
              <Tile
                label="Disk I/O"
                value={rate(sample.blockIo.reduce((a, d) => a + d.readps, 0))}
                sub={`write ${rate(sample.blockIo.reduce((a, d) => a + d.writeps, 0))}`}
              />
              <Tile
                label="Load"
                value={sample.load1.toFixed(2)}
                sub={`${sample.load5.toFixed(2)} · ${sample.load15.toFixed(2)} · ${sample.loadPerCore.toFixed(2)}/core`}
              />
            </div>

            {/* Facts that are one number each, so they belong in a row rather
                than in tiles of their own. */}
            <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2 text-[11px]">
              <Fact k="Run / block" v={`${sample.procsRunning} / ${sample.procsBlocked}`} />
              <Fact k="Context sw" v={`${Math.round(sample.contextRate).toLocaleString()}/s`} />
              <Fact k="Connections" v={`${sample.connections} established`} />
              <Fact k="Logged in" v={`${sample.users} user${sample.users === 1 ? '' : 's'}`} />
              <Fact
                k="Open files"
                v={sample.maxFds > 0
                  ? `${sample.openFds.toLocaleString()} of ${sample.maxFds.toLocaleString()}`
                  : sample.openFds.toLocaleString()}
              />
              {sample.tempC > 0 && <Fact k="Temperature" v={`${sample.tempC.toFixed(0)} °C`} />}
              <Fact k="Cached" v={bytes(sample.memCachedBytes)} />
            </dl>

            {sample.swapTotalBytes > 0 && (
              <div className="mt-2 rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2">
                <div className="flex items-baseline justify-between">
                  <span className="text-[10px] tracking-wide text-ink-500 uppercase">Swap</span>
                  <span className="text-[11px] tabular-nums text-ink-300">
                    {bytes(sample.swapUsedBytes)} / {bytes(sample.swapTotalBytes)}
                  </span>
                </div>
                <div className="mt-1.5">
                  <Meter pct={(100 * sample.swapUsedBytes) / sample.swapTotalBytes} />
                </div>
              </div>
            )}

            <Section title={`Disks (${sample.disks.length})`} />
            {sample.disks.map((d) => (
              <div key={d.mount} className="mb-2 rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="min-w-0 truncate font-mono text-[11px] text-ink-200">{d.mount}</span>
                  <span className="shrink-0 text-[11px] tabular-nums text-ink-300">
                    {d.usePercent.toFixed(0)}%
                  </span>
                </div>
                <div className="mt-1.5">
                  <Meter pct={d.usePercent} />
                </div>
                <p className="mt-1 text-[10px] text-ink-600">
                  {bytes(d.usedBytes)} of {bytes(d.totalBytes)} · {d.type}
                  {d.inodePercent > 0 && ` · inodes ${d.inodePercent.toFixed(0)}%`}
                </p>
              </div>
            ))}

            {sample.blockIo.length > 0 && (
              <>
                <Section title="Disk activity" />
                <ProcTable
                  head={['Device', 'Read', 'Write']}
                  rows={sample.blockIo.map((d) => [d.name, rate(d.readps), rate(d.writeps)])}
                />
              </>
            )}

            {sample.nets.length > 0 && (
              <>
                <Section title="Interfaces" />
                <ProcTable
                  head={['Interface', 'Down', 'Up']}
                  rows={sample.nets.map((n) => [n.name, rate(n.rxps), rate(n.txps)])}
                />
              </>
            )}

            {sample.topCpu.length > 0 && (
              <>
                <Section title="Top by CPU" />
                <ProcTable
                  head={['Process', 'CPU', 'Mem']}
                  rows={sample.topCpu.map((p) => [
                    `${p.command}  ${p.pid}`,
                    `${p.cpu.toFixed(1)}%`,
                    `${p.mem.toFixed(1)}%`,
                  ])}
                />
              </>
            )}

            {sample.topMem.length > 0 && (
              <>
                <Section title="Top by memory" />
                <ProcTable
                  head={['Process', 'CPU', 'Mem']}
                  rows={sample.topMem.map((p) => [
                    `${p.command}  ${p.pid}`,
                    `${p.cpu.toFixed(1)}%`,
                    `${p.mem.toFixed(1)}%`,
                  ])}
                />
              </>
            )}

            {sample.gpus.length > 0 && (
              <>
                <Section title={`GPUs (${sample.gpus.length})`} />
                {sample.gpus.map((g) => (
                  <div key={g.index} className="mb-2 rounded-lg border border-ink-800 bg-ink-850 px-2.5 py-2">
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="min-w-0 truncate text-[11px] text-ink-200">
                        {g.index}: {g.name}
                      </span>
                      <span className="shrink-0 text-[11px] tabular-nums text-ink-300">
                        {g.utilPercent.toFixed(0)}%
                      </span>
                    </div>
                    <div className="mt-1.5">
                      <Meter pct={g.utilPercent} />
                    </div>
                    <p className="mt-1 text-[10px] text-ink-600">
                      {Math.round(g.memUsedMb)} / {Math.round(g.memTotalMb)} MB · {g.tempC.toFixed(0)}°C ·{' '}
                      {g.powerW.toFixed(0)} W
                    </p>
                  </div>
                ))}
              </>
            )}

            {showTable && (
              <>
                <Section title="Recent samples" />
                <table className="w-full text-[10.5px] tabular-nums">
                  <thead>
                    <tr className="text-ink-500">
                      <th className="py-0.5 text-left font-medium">Time</th>
                      <th className="py-0.5 text-right font-medium">CPU</th>
                      <th className="py-0.5 text-right font-medium">Memory</th>
                    </tr>
                  </thead>
                  <tbody className="text-ink-300">
                    {[...rows].reverse().map((r, i) => (
                      <tr key={`${r.at}-${i}`}>
                        <td className="py-0.5">{new Date(r.at * 1000).toLocaleTimeString()}</td>
                        <td className="py-0.5 text-right">{r.cpu.toFixed(1)}%</td>
                        <td className="py-0.5 text-right">{r.mem.toFixed(1)}%</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            )}
          </div>
        )
      )}
    </div>
  )
}

function Section({ title }: { title: string }) {
  return (
    <p className="mt-3 mb-1.5 text-[10px] font-semibold tracking-widest text-ink-500 uppercase">
      {title}
    </p>
  )
}

function Fact({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex min-w-0 items-baseline justify-between gap-2">
      <dt className="shrink-0 text-ink-500">{k}</dt>
      <dd className="min-w-0 truncate text-right tabular-nums text-ink-300">{v}</dd>
    </div>
  )
}

/** A compact three-column table. Numbers align, so tabular figures are right
 *  here even though the stat tiles use proportional ones. */
function ProcTable({ head, rows }: { head: [string, string, string]; rows: string[][] }) {
  return (
    <table className="w-full table-fixed text-[10.5px]">
      <thead>
        <tr className="text-ink-500">
          <th className="w-1/2 pb-0.5 text-left font-medium">{head[0]}</th>
          <th className="pb-0.5 text-right font-medium">{head[1]}</th>
          <th className="pb-0.5 text-right font-medium">{head[2]}</th>
        </tr>
      </thead>
      <tbody className="text-ink-300">
        {rows.map((r, i) => (
          <tr key={`${r[0]}-${i}`}>
            <td className="truncate py-0.5 pr-1 font-mono" title={r[0]}>
              {r[0]}
            </td>
            <td className="py-0.5 text-right tabular-nums">{r[1]}</td>
            <td className="py-0.5 text-right tabular-nums">{r[2]}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
