import { Component, type ErrorInfo, type ReactNode } from 'react'

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
        <h1 className="text-sm font-semibold text-danger">Something in the interface crashed</h1>
        <p className="mt-1 max-w-2xl text-xs leading-relaxed text-ink-400">
          Your servers and agents are unaffected — everything long-lived runs in tmux on the remote
          side and is still going. Reloading rebuilds this view and reattaches.
        </p>

        <pre className="mt-4 max-h-40 shrink-0 overflow-auto rounded-card border border-danger/30 bg-danger/5 px-3 py-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-danger">
          {error.message || String(error)}
        </pre>

        {stack && (
          <pre className="mt-2 min-h-0 flex-1 overflow-auto rounded-card border hairline bg-ink-850 px-3 py-2 font-mono text-[10.5px] leading-relaxed whitespace-pre-wrap text-ink-500">
            {stack.trim()}
          </pre>
        )}

        <div className="mt-4 flex gap-2">
          <button
            onClick={() => window.location.reload()}
            className="rounded-control border border-accent-dim bg-accent/15 px-3 py-1.5 text-xs font-medium text-accent hover:bg-accent/25"
          >
            Reload the interface
          </button>
          <button
            onClick={() => this.setState({ error: null, stack: '' })}
            className="rounded-control border hairline bg-ink-800 px-3 py-1.5 text-xs font-medium text-ink-200 hover:bg-ink-750"
          >
            Try to continue
          </button>
        </div>
      </div>
    )
  }
}
