import { useState } from 'react'
import { TagInfo } from '../api'

interface Props {
  imageName: string
  tag: TagInfo
  onConfirm: () => Promise<void>
  onCancel: () => void
}

export default function DeleteModal({ imageName, tag, onConfirm, onCancel }: Props) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function handleConfirm() {
    setLoading(true)
    setError('')
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
      setLoading(false)
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50 p-4"
      onClick={e => { if (e.target === e.currentTarget) onCancel() }}
    >
      <div className="bg-zinc-900 border border-zinc-800 rounded-2xl p-6 max-w-sm w-full shadow-2xl">
        <div className="flex items-start gap-3 mb-4">
          <div className="w-8 h-8 rounded-lg bg-red-950/50 border border-red-900/50 flex items-center justify-center shrink-0 mt-0.5">
            <svg className="w-4 h-4 text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          </div>
          <div>
            <h3 className="font-semibold text-zinc-100 text-sm">Delete tag</h3>
            <p className="text-sm text-zinc-400 mt-1">
              This removes the manifest for{' '}
              <span className="font-mono text-zinc-200 text-xs">
                {imageName}:{tag.tag}
              </span>
              . Unreferenced blobs are freed on the next GC run.
            </p>
          </div>
        </div>

        <p className="font-mono text-xs text-zinc-700 mb-4 truncate px-1" title={tag.digest}>
          {tag.digest}
        </p>

        {error && (
          <p className="text-xs text-red-400 mb-3">{error}</p>
        )}

        <div className="flex gap-2 justify-end">
          <button
            onClick={onCancel}
            disabled={loading}
            className="text-sm text-zinc-400 hover:text-zinc-200 disabled:opacity-50 px-4 py-2 rounded-lg transition-colors hover:bg-zinc-800"
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            disabled={loading}
            className="text-sm bg-red-600 hover:bg-red-500 active:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed text-white font-medium px-4 py-2 rounded-lg transition-colors"
          >
            {loading ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </div>
  )
}
