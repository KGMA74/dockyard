import { useEffect, useState } from 'react'
import { getManifestDetails, ManifestDetails, TagInfo } from '../api'

interface Props {
  imageName: string
  tag: TagInfo
  onClose: () => void
}

export default function ImageDetailsPanel({ imageName, tag, onClose }: Props) {
  const [details, setDetails] = useState<ManifestDetails | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    getManifestDetails(imageName, tag.digest)
      .then(d => { if (!cancelled) setDetails(d) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load details') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [imageName, tag.digest])

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />

      <div className="relative w-full max-w-md h-full bg-zinc-950 border-l border-zinc-800 shadow-2xl overflow-y-auto">
        <div className="sticky top-0 bg-zinc-950/95 backdrop-blur border-b border-zinc-800 px-5 py-4 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="font-mono text-sm text-zinc-100 truncate">{imageName}</p>
            <span className="inline-block mt-1 font-mono text-xs text-blue-400 bg-blue-950/30 px-2 py-0.5 rounded-md border border-blue-900/30">
              {tag.tag}
            </span>
          </div>
          <button
            onClick={onClose}
            className="text-zinc-500 hover:text-zinc-200 transition-colors p-1.5 rounded-lg hover:bg-zinc-900 shrink-0"
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="p-5 space-y-5">
          {loading ? (
            <p className="text-sm text-zinc-600 py-10 text-center">Loading…</p>
          ) : error ? (
            <p className="text-sm text-red-400 py-10 text-center">{error}</p>
          ) : details ? (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Total size" value={details.total_size_human} />
                <Field label="Layers" value={String(details.layers.length)} />
                {details.os && <Field label="OS" value={details.os} />}
                {details.architecture && <Field label="Architecture" value={details.architecture} />}
                {details.created && (
                  <Field label="Created" value={new Date(details.created).toLocaleString()} wide />
                )}
              </div>

              <div>
                <p className="text-xs text-zinc-500 font-medium mb-2">Digest</p>
                <p className="font-mono text-xs text-zinc-400 bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 break-all">
                  {details.digest}
                </p>
              </div>

              <div>
                <p className="text-xs text-zinc-500 font-medium mb-2">
                  Layers ({details.layers.length})
                </p>
                <div className="space-y-1.5">
                  {details.layers.map((layer, i) => (
                    <div
                      key={layer.digest + i}
                      className="flex items-center justify-between gap-3 bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2"
                    >
                      <span className="font-mono text-xs text-zinc-500 truncate">
                        {layer.digest.slice(0, 19)}…
                      </span>
                      <span className="text-xs text-zinc-400 shrink-0 tabular-nums">{layer.size_human}</span>
                    </div>
                  ))}
                </div>
              </div>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function Field({ label, value, wide }: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={wide ? 'col-span-2' : undefined}>
      <p className="text-xs text-zinc-500 font-medium">{label}</p>
      <p className="text-sm text-zinc-200 mt-0.5">{value}</p>
    </div>
  )
}
