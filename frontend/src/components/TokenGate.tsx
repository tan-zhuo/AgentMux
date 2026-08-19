import { Lock } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import {
  AUTH_EVENT,
  getToken,
  reconnectStream,
  setToken,
  verifyToken,
} from '../lib/webTransport'
import { useT } from '../store/useI18n'
import { Button, inputClass } from './ui'

/**
 * Browser-mode door. The serve endpoint controls every server the user owns,
 * so nothing renders until the stored token has been proven against the
 * backend — and if the server starts rejecting it later, the door comes back
 * instead of the app failing call by call.
 */
export function TokenGate({ children }: { children: ReactNode }) {
  // unknown: still probing the stored token on first load.
  const [state, setState] = useState<'unknown' | 'locked' | 'open'>('unknown')
  const [value, setValue] = useState('')
  const [error, setError] = useState(false)
  const [busy, setBusy] = useState(false)
  const t = useT()

  useEffect(() => {
    const stored = getToken()
    if (!stored) {
      setState('locked')
      return
    }
    void verifyToken(stored).then((ok) => setState(ok ? 'open' : 'locked'))
  }, [])

  useEffect(() => {
    const relock = () => setState('locked')
    window.addEventListener(AUTH_EVENT, relock)
    return () => window.removeEventListener(AUTH_EVENT, relock)
  }, [])

  async function submit() {
    const token = value.trim()
    if (!token || busy) return
    setBusy(true)
    setError(false)
    try {
      if (await verifyToken(token)) {
        setToken(token)
        reconnectStream()
        setValue('')
        setState('open')
      } else {
        setError(true)
      }
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  if (state === 'open') return <>{children}</>
  if (state === 'unknown') return <div className="h-screen w-screen bg-ink-950" />

  return (
    <div className="flex h-screen w-screen items-center justify-center bg-ink-950 px-6">
      <div className="w-full max-w-xs">
        <div className="mb-4 flex items-center gap-2 text-ink-100">
          <Lock size={16} className="text-accent" />
          <span className="text-sm font-semibold">AgentMux</span>
        </div>
        <p className="mb-3 text-xs leading-relaxed text-ink-400">{t('web.gate.hint')}</p>
        <input
          type="password"
          value={value}
          autoFocus
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && void submit()}
          placeholder={t('web.gate.placeholder')}
          className={inputClass}
        />
        {error && <p className="mt-2 text-[11px] text-err">{t('web.gate.wrong')}</p>}
        <div className="mt-3 flex justify-end">
          <Button variant="primary" disabled={!value.trim() || busy} onClick={() => void submit()}>
            {busy ? t('web.gate.checking') : t('web.gate.connect')}
          </Button>
        </div>
      </div>
    </div>
  )
}
