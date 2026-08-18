import { Component, type ErrorInfo, type ReactNode } from 'react'
import { t } from '../store/useI18n'
import { Button } from './ui'

interface State {
  error: Error | null
  stack: string
}

/**
 * Catches a render crash and shows it.
 *
 * Without this, an exception thrown while rendering unmounts the whole tree and
 * leaves a blank window — the least diagnosable failure a desktop app can
 * have, and one the user has to describe from memory. Everything the app owns
 * is remote and survives, so reloading the view is a safe way out.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null, stack: '' }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.setState({ stack: info.componentStack ?? '' })
    // Still log it: a console binary or the dev server shows it in the terminal.
    console.error('AgentMux render error', error, info.componentStack)
  }

  render() {
    const { error, stack } = this.state
    if (!error) return this.props.children

    return (
      <div className="flex h-full w-full flex-col bg-ink-900 p-6 text-ink-200">
        <h1 className="text-sm font-semibold text-danger">{t('crash.title')}</h1>
        <p className="mt-1 max-w-2xl text-xs leading-relaxed text-ink-400">{t('crash.body')}</p>

        <pre className="mt-4 max-h-40 shrink-0 overflow-auto rounded-card border border-danger/30 bg-danger/5 px-3 py-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-danger">
          {error.message || String(error)}
        </pre>

        {stack && (
          <pre className="mt-2 min-h-0 flex-1 overflow-auto rounded-card border hairline bg-ink-850 px-3 py-2 font-mono text-[10.5px] leading-relaxed whitespace-pre-wrap text-ink-500">
            {stack.trim()}
          </pre>
        )}

        <div className="mt-4 flex gap-2">
          <Button variant="primary" onClick={() => window.location.reload()}>
            {t('crash.reload')}
          </Button>
          <Button onClick={() => this.setState({ error: null, stack: '' })}>
            {t('crash.continue')}
          </Button>
        </div>
      </div>
    )
  }
}
