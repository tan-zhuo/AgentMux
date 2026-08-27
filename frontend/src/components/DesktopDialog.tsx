import clsx from 'clsx'
import { ExternalLink, Monitor, MonitorX } from 'lucide-react'
import { useEffect, useState } from 'react'
import { desktop as desktopApi, errText, isDesktop } from '../lib/api'
import type { DesktopEndpoint, DesktopOffer, DesktopProtocol, Server } from '../lib/types'
import { useAppStore } from '../store/useAppStore'
import { useDialogs } from '../store/useDialogs'
import { useT } from '../store/useI18n'
import { Button, Field, Modal, inputClass } from './ui'

/** The port each protocol is usually behind, for the row somebody types into. */
const defaultPort: Record<DesktopProtocol, number> = { rdp: 3389, vnc: 5900 }

const same = (a: DesktopEndpoint, b: DesktopEndpoint) =>
  a.protocol === b.protocol && a.port === b.port

/**
 * Asks which desktop to open, when there is a question worth asking.
 *
 * The host is probed as this opens, so the usual answer is already selected by
 * the time anybody reads the dialog: the one used last time, or the one thing
 * that answered. Typing a protocol and a port is for the machines that serve a
 * desktop somewhere other than where everyone else does — and once used, that
 * choice is what the next opening starts from.
 */
export function DesktopDialog({ server }: { server: Server }) {
  const close = useDialogs((s) => s.close)
  const toast = useAppStore((s) => s.toast)
  const openTab = useAppStore((s) => s.openTab)
  const setDesktopSupport = useAppStore((s) => s.setDesktopSupport)
  const t = useT()

  const [offer, setOffer] = useState<DesktopOffer | null>(null)
  const [error, setError] = useState('')
  const [choice, setChoice] = useState<DesktopEndpoint | null>(null)
  const [manual, setManual] = useState<DesktopEndpoint>({ protocol: 'rdp', port: 3389 })
  const [typing, setTyping] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    desktopApi
      .probe(server.id)
      .then((o) => {
        if (cancelled) return
        setOffer(o)
        // The list can arrive empty, and from an older core as null; either
        // way it is "nothing answered", and neither may be read as a list
        // without checking first.
        const found = o.found ?? []
        // What the host is known to serve, so the menu that opened this can
        // grey itself out rather than offering a door onto nothing again.
        setDesktopSupport(server.id, found.length > 0 || !!o.saved)
        // What was chosen before wins over what happens to answer now: a
        // machine serving two desktops should not change which one it opens
        // between one day and the next.
        const first = o.saved ?? found[0] ?? null
        setChoice(first)
        if (first) setManual(first)
        setTyping(!first)
      })
      .catch((e) => {
        if (cancelled) return
        setError(errText(e))
        setTyping(true)
      })
    return () => {
      cancelled = true
    }
    // The store's setter is stable; the probe belongs to the host, and rerunning
    // it for anything else would ask the same question twice.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server.id])

  const endpoint = typing ? manual : choice
  const ready = !!endpoint && endpoint.port > 0 && !busy

  /** Open it here, in a pane, next to the terminals. */
  function openHere() {
    if (!endpoint) return
    setDesktopSupport(server.id, true)
    openTab({
      title: `${server.name} · ${endpoint.protocol.toUpperCase()}`,
      kind: 'desktop',
      serverId: server.id,
      workspaceId: '',
      agentId: '',
      tmuxSession: '',
      command: `${endpoint.protocol}:${endpoint.port}`,
    })
    close()
  }

  async function open() {
    if (!endpoint) return
    setBusy(true)
    try {
      const session = await desktopApi.open(server.id, endpoint)
      // Opening by hand on a port the probe does not try is the proof the
      // probe could not get: this host has a desktop.
      setDesktopSupport(server.id, true)
      if (session.client) {
        toast('ok', t('desktop.opened', { client: session.client, name: server.name }))
        close()
      } else {
        // The door is open even though no viewer was: the address is worth
        // handing over rather than swallowing with the error.
        toast(
          'warn',
          t('desktop.noClient', { protocol: endpoint.protocol.toUpperCase(), local: session.local }),
        )
      }
    } catch (e) {
      toast('error', errText(e))
    } finally {
      setBusy(false)
    }
  }

  const rows = offer
    ? [
        ...(offer.saved ? [{ ep: offer.saved, why: t('desktop.saved') }] : []),
        ...(offer.found ?? [])
          .filter((f) => !offer.saved || !same(f, offer.saved))
          .map((ep) => ({ ep, why: t('desktop.found') })),
      ]
    : []

  return (
    <Modal
      title={t('desktop.title', { name: server.name })}
      onClose={close}
      footer={
        <>
          {offer?.saved && (
            <Button
              size="sm"
              className="mr-auto"
              onClick={async () => {
                await desktopApi.forget(server.id)
                toast('info', t('desktop.forgotten'))
                setOffer({ ...offer, saved: null })
              }}
            >
              <MonitorX size={11} /> {t('desktop.forget')}
            </Button>
          )}
          <Button onClick={close}>{t('common.cancel')}</Button>
          {isDesktop && (
            <Button disabled={!ready} onClick={() => void open()} title={t('desktop.openSystem.hint')}>
              <ExternalLink size={11} /> {busy ? t('desktop.opening') : t('desktop.openSystem')}
            </Button>
          )}
          <Button variant="primary" disabled={!ready} onClick={openHere}>
            <Monitor size={11} /> {t('desktop.openHere')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-[11px] leading-relaxed text-ink-400">{t('desktop.blurb')}</p>

        {!offer && !error && (
          <p className="text-[11px] text-ink-500">{t('desktop.looking')}</p>
        )}
        {error && <p className="text-[11px] leading-relaxed text-danger">{error}</p>}
        {offer && !offer.reachable && !error && (
          <p className="text-[11px] leading-relaxed text-warn">{t('desktop.unreachable')}</p>
        )}
        {offer?.reachable && rows.length === 0 && (
          <p className="text-[11px] leading-relaxed text-warn">{t('desktop.none')}</p>
        )}

        {rows.map(({ ep, why }) => {
          const selected = !typing && !!choice && same(choice, ep)
          return (
            <button
              key={`${ep.protocol}:${ep.port}`}
              onClick={() => {
                setChoice(ep)
                setManual(ep)
                setTyping(false)
              }}
              className={clsx(
                'flex w-full items-center gap-2 rounded-control border px-2.5 py-2 text-left',
                selected ? 'border-accent bg-accent/10' : 'hairline bg-ink-800 hover:border-ink-600',
              )}
            >
              <Monitor size={13} className={selected ? 'text-accent' : 'text-ink-500'} />
              <span className="min-w-0 flex-1">
                <span className="block text-[11px] text-ink-100">
                  {t(ep.protocol === 'rdp' ? 'desktop.rdp' : 'desktop.vnc')}
                  <span className="ml-1.5 font-mono text-ink-500">{ep.port}</span>
                </span>
                <span className="block text-[10.5px] text-ink-500">
                  {t(ep.protocol === 'rdp' ? 'desktop.rdp.hint' : 'desktop.vnc.hint')}
                </span>
              </span>
              <span className="shrink-0 text-[10px] text-ink-600">{why}</span>
            </button>
          )
        })}

        <label className="flex cursor-default items-center gap-2">
          <input
            type="checkbox"
            checked={typing}
            onChange={(e) => setTyping(e.target.checked)}
            className="h-3 w-3 shrink-0 accent-[#4c8dff]"
          />
          <span className="text-[11px] text-ink-200">{t('desktop.other')}</span>
        </label>

        {typing && (
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('desktop.protocol')}>
              <select
                className={inputClass}
                value={manual.protocol}
                onChange={(e) => {
                  const protocol = e.target.value as DesktopProtocol
                  setManual((m) => ({
                    protocol,
                    // The port follows the protocol until it has been typed
                    // over, so switching does not leave 3389 next to VNC.
                    port: m.port === defaultPort[m.protocol] ? defaultPort[protocol] : m.port,
                  }))
                }}
              >
                <option value="rdp">{t('desktop.rdp')}</option>
                <option value="vnc">{t('desktop.vnc')}</option>
              </select>
            </Field>
            <Field label={t('desktop.port')}>
              <input
                type="number"
                min={1}
                max={65535}
                className={inputClass}
                value={manual.port || ''}
                onChange={(e) => setManual((m) => ({ ...m, port: Number(e.target.value) || 0 }))}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && ready) void open()
                }}
              />
            </Field>
          </div>
        )}
      </div>
    </Modal>
  )
}
