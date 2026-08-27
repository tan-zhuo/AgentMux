import { Copy, Monitor, Plug, RefreshCw, TerminalSquare } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { servers as serverApi } from '../../lib/api'
import { copyText } from '../../lib/clipboard'
import type { Server } from '../../lib/types'
import { useAppStore } from '../../store/useAppStore'
import { useDialogs } from '../../store/useDialogs'
import { useT } from '../../store/useI18n'
import { Button } from '../ui'

/**
 * The one-line install that puts an SSH server on a Windows machine.
 *
 * Microsoft's own: the capability ships with Windows and only has to be turned
 * on. Started and set to start again, because a service that stops at the next
 * reboot is a machine that goes quiet on a Monday.
 */
const WINDOWS_OPENSSH_INSTALL =
  'Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0; ' +
  'Start-Service sshd; Set-Service sshd -StartupType Automatic'

/**
 * What a host added for its screen can say about itself.
 *
 * It has no shell, so the panels that ask a machine questions have nothing to
 * show — but there is one question worth asking without one: does it also
 * answer on the SSH port. A machine that does can be worked on as well as
 * watched, and saying so is better than leaving somebody to wonder why their
 * Windows box is a picture and not a host.
 *
 * A machine that does not can be told what to install. Not by this app: RDP
 * carries pixels and keystrokes, not commands, and typing an elevated install
 * blind into somebody's desktop is not automation. The command goes to the
 * clipboard instead, for the session already open in the next pane.
 */
export function DesktopHostPanel({ server }: { server: Server }) {
  const t = useT()
  const toast = useAppStore((s) => s.toast)
  const openTab = useAppStore((s) => s.openTab)
  const openDialog = useDialogs((s) => s.open)

  const [ssh, setSSH] = useState<{ open: boolean; error: string } | null>(null)
  const [checking, setChecking] = useState(false)

  const check = useCallback(async () => {
    setChecking(true)
    try {
      setSSH(await serverApi.sshOpen(server.id))
    } catch {
      setSSH({ open: false, error: '' })
    } finally {
      setChecking(false)
    }
  }, [server.id])

  // Asked once per host, when it is looked at. It is one connection attempt to
  // one port, and the answer changes only when somebody installs something.
  useEffect(() => {
    setSSH(null)
    void check()
  }, [check])

  const protocol = server.desktopOs === 'windows' ? 'rdp' : 'vnc'
  const port = server.port || (server.desktopOs === 'windows' ? 3389 : 5900)

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto p-3">
      <div>
        <p className="flex items-center gap-1.5 text-xs font-semibold text-ink-100">
          <Monitor size={13} /> {server.name}
        </p>
        <p className="mt-1 font-mono text-[10.5px] text-ink-500">
          {protocol.toUpperCase()} · {server.host}:{port}
        </p>
      </div>

      <Button
        variant="primary"
        onClick={() =>
          openTab({
            title: server.name,
            kind: 'desktop',
            serverId: server.id,
            workspaceId: '',
            agentId: '',
            tmuxSession: '',
            command: `${protocol}:${port}`,
          })
        }
      >
        <Monitor size={11} /> {t('tree.openDesktop')}
      </Button>

      <div className="rounded-card border hairline bg-ink-850 px-2.5 py-2">
        <p className="flex items-center gap-1.5 text-[10px] font-medium text-ink-500">
          <TerminalSquare size={11} /> {t('deskPanel.shellTitle')}
          <span className="flex-1" />
          <button
            type="button"
            onClick={() => void check()}
            title={t('status.refreshNow')}
            className="text-ink-500 hover:text-ink-200"
          >
            <RefreshCw size={10} className={checking ? 'animate-spin' : undefined} />
          </button>
        </p>

        {ssh === null ? (
          <p className="mt-1.5 text-[11px] text-ink-500">{t('deskPanel.checking')}</p>
        ) : ssh.open ? (
          <>
            <p className="mt-1.5 text-[11px] leading-relaxed text-ink-300">
              {t('deskPanel.sshOpen')}
            </p>
            <Button
              size="sm"
              className="mt-2 w-full"
              onClick={() =>
                openDialog({
                  kind: 'server',
                  // No id: a starting point for a new host rather than an edit
                  // of this one. The screen stays; the shell is added beside it.
                  server: {
                    ...server,
                    id: '',
                    kind: 'ssh',
                    port: 22,
                    name: `${server.name} (SSH)`,
                  },
                })
              }
            >
              <Plug size={11} /> {t('deskPanel.addAsSSH')}
            </Button>
          </>
        ) : (
          <>
            <p className="mt-1.5 text-[11px] leading-relaxed text-ink-400">
              {t('deskPanel.sshClosed')}
            </p>
            {server.desktopOs === 'windows' && (
              <>
                <p className="mt-1.5 text-[10.5px] leading-relaxed text-ink-500">
                  {t('deskPanel.installHint')}
                </p>
                <pre className="mt-1.5 overflow-x-auto rounded-control border hairline bg-ink-900 px-2 py-1.5 font-mono text-[10px] leading-relaxed whitespace-pre-wrap text-ink-300">
                  {WINDOWS_OPENSSH_INSTALL}
                </pre>
                <Button
                  size="sm"
                  className="mt-1.5 w-full"
                  onClick={async () => {
                    const ok = await copyText(WINDOWS_OPENSSH_INSTALL)
                    toast(ok ? 'ok' : 'error', ok ? t('deskPanel.copied') : t('term.copyFailed'))
                  }}
                >
                  <Copy size={11} /> {t('deskPanel.copyInstall')}
                </Button>
              </>
            )}
          </>
        )}
      </div>
    </div>
  )
}
