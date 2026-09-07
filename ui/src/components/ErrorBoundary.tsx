import { Component, ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

// Last-resort boundary so a render crash shows a recoverable message instead of
// a blank page.
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: unknown) {
    console.error('Unhandled UI error:', error, info)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <div className="max-w-md space-y-3 text-center">
          <h1 className="text-lg font-semibold">Something went wrong</h1>
          <p className="text-sm text-muted-foreground break-words">{this.state.error.message}</p>
          <button
            onClick={() => window.location.reload()}
            className="rounded-md border px-3 py-1.5 text-sm hover:bg-muted"
          >
            Reload
          </button>
        </div>
      </div>
    )
  }
}
