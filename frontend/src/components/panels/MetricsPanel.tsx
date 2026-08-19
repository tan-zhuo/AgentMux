import clsx from 'clsx'
import { Activity, RefreshCw, Table2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { errText, metrics as metricsApi } from '../../lib/api'
import type { TFunc } from '../../lib/i18n'
import type { HardwareInfo, MemModule, MetricSample } from '../../lib/types'
import { useFmt, useT } from '../../store/useI18n'
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

/** "28C / 56T · 2 sockets · 3.5 GHz" — symbols, so no per-language variant. */
function cpuTopoLine(t: TFunc, hw: HardwareInfo): string {
  const parts: string[] = []
  if (hw.cpuCores > 0 || hw.cpuThreads > 0) parts.push(`${hw.cpuCores}C / ${hw.cpuThreads}T`)
  if (hw.cpuSockets > 1) parts.push(t('metrics.hw.sockets', { n: hw.cpuSockets }))
  if (hw.cpuMaxMhz > 0) parts.push(`${(hw.cpuMaxMhz / 1000).toFixed(1)} GHz`)
  return parts.join(' · ')
}

/** Identical DIMMs collapse into one entry: "4 × 16 GB DDR4-3200". */
function memModuleLine(mods: MemModule[]): string {
  const groups = new Map<string, number>()
  for (const m of mods) {
    const gen = [m.type, m.speedMts > 0 ? String(m.speedMts) : ''].filter(Boolean).join('-')
    const label = [bytes(m.sizeBytes), gen].filter(Boolean).join(' ')
    groups.set(label, (groups.get(label) ?? 0) + 1)
  }
  return [...groups].map(([label, n]) => `${n} × ${label}`).join(' · ')
}

/** Per-slot detail for the row's tooltip, where part numbers can be long. */
function memModuleDetail(mods: MemModule[]): string {
  return mods
    .map((m) =>
      [m.slot, bytes(m.sizeBytes), m.type, m.speedMts > 0 ? `${m.speedMts} MT/s` : '', m.manufacturer, m.partNumber]
        .filter(Boolean)
        .join(' · '),
    )
    .join('\n')
}

function duration(t: TFunc, seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return t('metrics.dh', { d, h })
  if (h > 0) return t('metrics.hm', { h, m })
  return t('metrics.m', { m })
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
  label,
  height = 34,
}: {
  values: number[]
  color: string
  /** Shown until there are two points to draw a line between. */
  label: string
  height?: number
}) {
  const [hover, setHover] = useState<number | null>(null)
  const ref = useRef<SVGSVGElement | null>(null)
  const w = 100
  const h = height

  if (values.length < 2) {
    return (
      <div style={{ height }} className="flex items-end text-[10px] text-ink-600">
        {label}
      </div>
    )
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
        <span className="pointer-events-none absolute -top-1 right-0 rounded-control bg-ink-800 px-1.5 py-0.5 text-[10px] text-ink-200 tabular-nums">
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
      className="h-1.5 w-full overflow-hidden rounded-capsule"
      style={{ background: `color-mix(in oklab, ${color} 18%, transparent)` }}
    >
      <div
        className="h-full rounded-capsule transition-[width] duration-500"
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
    <div className="rounded-card border hairline bg-ink-850 px-2.5 py-2">
      <p className="text-[10px] font-medium text-ink-500">{label}</p>
      <p className="mt-0.5 text-lg leading-none font-semibold text-ink-100">{value}</p>
      {sub && <p className="mt-1 text-[10.5px] text-ink-500">{sub}</p>}
      {children}
    </div>
  )
}

export function MetricsPanel({ serverId }: { serverId: string }) {
  const t = useT()
  const fmt = useFmt()
  const [sample, setSample] = useState<MetricSample | null>(null)
  const [hw, setHw] = useState<HardwareInfo | null>(null)
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
        setError(s.error || t('metrics.readFailed'))
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
  }, [serverId, t])

  // Hardware is what the machine is, not what it is doing: one read per host,
  // cached on the backend, never on the polling ticker. A failure here only
  // hides the inventory card; the vitals poll reports connection problems.
  useEffect(() => {
    let live = true
    setHw(null)
    metricsApi
      .hardware(serverId)
      .then((h) => {
        if (live && h.ok) setHw(h)
      })
      .catch(() => {})
    return () => {
      live = false
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
      <div className="flex items-center justify-between border-b hairline px-3 py-2">
        <span className="flex items-center gap-1.5 text-[11px] font-semibold text-ink-300">
          <Activity size={11} /> {t('metrics.title')}
        </span>
        <div className="flex gap-1">
          <Button
            size="sm"
            variant="subtle"
            onClick={() => setShowTable((v) => !v)}
            title={t('metrics.tableView')}
          >
            <Table2 size={11} />
          </Button>
          <Button size="sm" variant="subtle" onClick={() => void poll()} disabled={busy} title={t('status.refreshNow')}>
            <RefreshCw size={11} className={busy ? 'animate-spin' : undefined} />
          </Button>
        </div>
      </div>

      {error && (
        <p className="border-b hairline px-3 py-2 text-[11px] leading-relaxed text-danger">{error}</p>
      )}

      {!sample && !error ? (
        <Empty title={t('metrics.reading')} hint={t('metrics.reading.hint')} />
      ) : (
        sample && (
          <div className={clsx('min-h-0 flex-1 overflow-y-auto px-3 py-2.5', busy && 'opacity-95')}>
            {(sample.distro || sample.kernel) && (
              <p className="mb-2 truncate font-mono text-[10.5px] text-ink-500">
                {[sample.distro, sample.kernel, sample.arch].filter(Boolean).join(' · ')}
              </p>
            )}

            {hw && (
              <div className="mb-2 rounded-card border hairline bg-ink-850 px-2.5 py-2">
                <p className="text-[10px] font-medium text-ink-500">{t('metrics.hardware')}</p>
                <dl className="mt-1.5 space-y-1.5">
                  {(hw.vendor || hw.product) && (
                    <HwRow
                      k={t('metrics.hw.machine')}
                      v={[hw.vendor, hw.product].filter(Boolean).join(' ')}
                    />
                  )}
                  {hw.cpuModel && (
                    <HwRow k={t('metrics.cpu')} v={hw.cpuModel} sub={cpuTopoLine(t, hw)} />
                  )}
                  {(hw.memTotalBytes > 0 || hw.memModules.length > 0) && (
                    <HwRow
                      k={t('metrics.memory')}
                      v={bytes(
                        hw.memTotalBytes || hw.memModules.reduce((a, m) => a + m.sizeBytes, 0),
                      )}
                      sub={memModuleLine(hw.memModules)}
                      hint={memModuleDetail(hw.memModules)}
                    />
                  )}
                  {hw.disks.map((d) => (
                    <HwRow
                      key={d.name}
                      k={t('metrics.hw.disk')}
                      v={d.model || d.name}
                      sub={[
                        bytes(d.sizeBytes),
                        d.kind,
                        d.transport ? d.transport.toUpperCase() : '',
                        d.model ? d.name : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    />
                  ))}
                  {hw.gpus.map((g, i) => (
                    <HwRow
                      key={`${g.name}-${i}`}
                      k={t('metrics.hw.gpu')}
                      v={g.name}
                      sub={[
                        g.memTotalMb > 0 ? bytes(g.memTotalMb * 1024 * 1024) : '',
                        g.driver ? t('metrics.hw.driver', { v: g.driver }) : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    />
                  ))}
                </dl>
              </div>
            )}

            <div className="grid grid-cols-2 gap-2">
              <Tile
                label={t('metrics.cpu')}
                value={`${sample.cpuPercent.toFixed(0)}%`}
                sub={t(
                  sample.cpuSteal >= 1 ? 'metrics.cpu.subSteal' : 'metrics.cpu.sub',
                  {
                    usr: sample.cpuUser.toFixed(0),
                    sys: sample.cpuSystem.toFixed(0),
                    io: sample.cpuIowait.toFixed(0),
                    steal: sample.cpuSteal.toFixed(0),
                  },
                )}
              >
                <div className="mt-1.5">
                  <Sparkline
                    values={cpuHistory}
                    color={severityVar[severity(sample.cpuPercent)]}
                    label={t('metrics.collecting')}
                  />
                </div>
              </Tile>
              <Tile
                label={t('metrics.memory')}
                value={`${sample.memPercent.toFixed(0)}%`}
                sub={t('metrics.usedOf', {
                  used: bytes(sample.memUsedBytes),
                  total: bytes(sample.memTotalBytes),
                })}
              >
                <div className="mt-1.5">
                  <Sparkline
                    values={memHistory}
                    color={severityVar[severity(sample.memPercent)]}
                    label={t('metrics.collecting')}
                  />
                </div>
              </Tile>
            </div>

            {/* Per-core utilisation. Height carries the value; a single hue
                keeps it from double-encoding what the bars already show. */}
            {sample.perCore.length > 1 && (
              <div className="mt-2 rounded-card border hairline bg-ink-850 px-2.5 py-2">
                <div className="flex items-baseline justify-between">
                  <span className="text-[10px] font-medium text-ink-500">
                    {t('metrics.perCore', { n: sample.perCore.length })}
                  </span>
                  <span className="text-[10.5px] tabular-nums text-ink-500">
                    {t('metrics.busiest', { pct: Math.max(...sample.perCore).toFixed(0) })}
                  </span>
                </div>
                {/* A hairline baseline so an idle machine reads as bars at zero
                    rather than a row of floating dashes. */}
                <div className="mt-2 flex h-8 items-end gap-[2px] border-b hairline">
                  {sample.perCore.map((v, i) => (
                    <span
                      key={i}
                      title={t('metrics.core', { i, pct: v.toFixed(0) })}
                      className="min-w-0 flex-1 rounded-t-[2px] bg-accent"
                      style={{ height: `${Math.max(3, v)}%` }}
                    />
                  ))}
                </div>
              </div>
            )}

            <div className="mt-2 grid grid-cols-2 gap-2">
              <Tile
                label={t('metrics.uptime')}
                value={duration(t, sample.uptimeSeconds)}
                sub={t('metrics.processes', { n: sample.processes })}
              />
              <Tile
                label={t('metrics.network')}
                value={rate(sample.nets.reduce((a, n) => a + n.rxps, 0))}
                sub={t('metrics.upRate', {
                  rate: rate(sample.nets.reduce((a, n) => a + n.txps, 0)),
                })}
              />
              <Tile
                label={t('metrics.diskIo')}
                value={rate(sample.blockIo.reduce((a, d) => a + d.readps, 0))}
                sub={t('metrics.writeRate', {
                  rate: rate(sample.blockIo.reduce((a, d) => a + d.writeps, 0)),
                })}
              />
              <Tile
                label={t('metrics.load')}
                value={sample.load1.toFixed(2)}
                sub={t('metrics.load.sub', {
                  l5: sample.load5.toFixed(2),
                  l15: sample.load15.toFixed(2),
                  perCore: sample.loadPerCore.toFixed(2),
                })}
              />
            </div>

            {/* Facts that are one number each, so they belong in a row rather
                than in tiles of their own. */}
            <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 rounded-card border hairline bg-ink-850 px-2.5 py-2 text-[11px]">
              <Fact
                k={t('metrics.runBlock')}
                v={`${sample.procsRunning} / ${sample.procsBlocked}`}
              />
              <Fact
                k={t('metrics.contextSw')}
                v={t('metrics.perSecond', { n: fmt.number(Math.round(sample.contextRate)) })}
              />
              <Fact
                k={t('metrics.connections')}
                v={t('metrics.established', { n: sample.connections })}
              />
              <Fact
                k={t('metrics.loggedIn')}
                v={sample.users === 1 ? t('metrics.user') : t('metrics.users', { n: sample.users })}
              />
              <Fact
                k={t('metrics.openFiles')}
                v={
                  sample.maxFds > 0
                    ? t('metrics.usedOf', {
                        used: fmt.number(sample.openFds),
                        total: fmt.number(sample.maxFds),
                      })
                    : fmt.number(sample.openFds)
                }
              />
              {sample.tempC > 0 && (
                <Fact
                  k={t('metrics.temperature')}
                  v={t('metrics.degrees', { n: sample.tempC.toFixed(0) })}
                />
              )}
              <Fact k={t('metrics.cached')} v={bytes(sample.memCachedBytes)} />
            </dl>

            {sample.swapTotalBytes > 0 && (
              <div className="mt-2 rounded-card border hairline bg-ink-850 px-2.5 py-2">
                <div className="flex items-baseline justify-between">
                  <span className="text-[10px] font-medium text-ink-500">{t('metrics.swap')}</span>
                  <span className="text-[11px] tabular-nums text-ink-300">
                    {bytes(sample.swapUsedBytes)} / {bytes(sample.swapTotalBytes)}
                  </span>
                </div>
                <div className="mt-1.5">
                  <Meter pct={(100 * sample.swapUsedBytes) / sample.swapTotalBytes} />
                </div>
              </div>
            )}

            <Section title={t('metrics.disks', { n: sample.disks.length })} />
            {sample.disks.map((d) => (
              <div key={d.mount} className="mb-2 rounded-card border hairline bg-ink-850 px-2.5 py-2">
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
                  {t('metrics.disk.sub', {
                    used: bytes(d.usedBytes),
                    total: bytes(d.totalBytes),
                    type: d.type,
                  })}
                  {d.inodePercent > 0 &&
                    ` · ${t('metrics.disk.inodes', { pct: d.inodePercent.toFixed(0) })}`}
                </p>
              </div>
            ))}

            {sample.blockIo.length > 0 && (
              <>
                <Section title={t('metrics.diskActivity')} />
                <ProcTable
                  head={[t('metrics.col.device'), t('metrics.col.read'), t('metrics.col.write')]}
                  rows={sample.blockIo.map((d) => [d.name, rate(d.readps), rate(d.writeps)])}
                />
              </>
            )}

            {sample.nets.length > 0 && (
              <>
                <Section title={t('metrics.interfaces')} />
                <ProcTable
                  head={[t('metrics.col.interface'), t('metrics.col.down'), t('metrics.col.up')]}
                  rows={sample.nets.map((n) => [n.name, rate(n.rxps), rate(n.txps)])}
                />
              </>
            )}

            {sample.topCpu.length > 0 && (
              <>
                <Section title={t('metrics.topCpu')} />
                <ProcTable
                  head={[t('metrics.col.process'), t('metrics.col.cpu'), t('metrics.col.mem')]}
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
                <Section title={t('metrics.topMem')} />
                <ProcTable
                  head={[t('metrics.col.process'), t('metrics.col.cpu'), t('metrics.col.mem')]}
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
                <Section title={t('metrics.gpus', { n: sample.gpus.length })} />
                {sample.gpus.map((g) => (
                  <div key={g.index} className="mb-2 rounded-card border hairline bg-ink-850 px-2.5 py-2">
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
                      {t('metrics.gpu.sub', {
                        used: Math.round(g.memUsedMb),
                        total: Math.round(g.memTotalMb),
                        temp: g.tempC.toFixed(0),
                        power: g.powerW.toFixed(0),
                      })}
                    </p>
                  </div>
                ))}
              </>
            )}

            {showTable && (
              <>
                <Section title={t('metrics.recentSamples')} />
                <table className="w-full text-[10.5px] tabular-nums">
                  <thead>
                    <tr className="text-ink-500">
                      <th className="py-0.5 text-left font-medium">{t('metrics.col.time')}</th>
                      <th className="py-0.5 text-right font-medium">{t('metrics.cpu')}</th>
                      <th className="py-0.5 text-right font-medium">{t('metrics.memory')}</th>
                    </tr>
                  </thead>
                  <tbody className="text-ink-300">
                    {[...rows].reverse().map((r, i) => (
                      <tr key={`${r.at}-${i}`}>
                        <td className="py-0.5">{fmt.time(r.at)}</td>
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
    <p className="mt-3 mb-1.5 text-[11px] font-semibold text-ink-300">
      {title}
    </p>
  )
}

/** One inventory row: label above, model below, spec line under that. Model
 *  strings are long, so they truncate with the full text on hover. */
function HwRow({ k, v, sub, hint }: { k: string; v: string; sub?: string; hint?: string }) {
  return (
    <div className="min-w-0" title={hint}>
      <dt className="text-[10px] text-ink-500">{k}</dt>
      <dd className="truncate text-[11px] text-ink-200" title={v}>
        {v}
      </dd>
      {sub && <dd className="text-[10px] text-ink-600">{sub}</dd>}
    </div>
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
