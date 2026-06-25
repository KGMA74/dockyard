import { useState } from 'react'
import { runGC, StorageStats } from '../api'
import { useToast } from './Toast'

interface Props {
  stats: StorageStats | null
  onRefresh: () => void
}

export default function StatsBar({ stats, onRefresh }: Props) {
  const { toast } = useToast()
  const [running, setRunning] = useState(false)

  async function handleGC() {
    setRunning(true)
    try {
      const result = await runGC()
      toast(`GC done — removed ${result.count} blob(s), freed ${result.freed_human}`)
      onRefresh()
    } catch (err) {
      toast(err instanceof Error ? err.message : 'GC failed', 'error')
    } finally {
      setRunning(false)
    }
  }

  if (!stats) return null

  const unavailable = stats.total_size_bytes === -1

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      <StatCard label="Storage used" value={unavailable ? '—' : stats.total_size_human} />
      <StatCard label="Repositories"  value={unavailable ? '—' : String(stats.repo_count)} />
      <StatCard label="Blobs"          value={unavailable ? '—' : String(stats.blob_count)} />

      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-4 flex flex-col gap-2">
        <span className="text-xs text-zinc-500 font-medium">Garbage collection</span>
        {unavailable ? (
          <p className="text-xs text-zinc-600 mt-auto">Not available in proxy mode</p>
        ) : (
          <button
            onClick={handleGC}
            disabled={running}
            className="mt-auto text-xs bg-zinc-800 hover:bg-zinc-700 active:bg-zinc-600 disabled:opacity-40 disabled:cursor-not-allowed text-zinc-300 font-medium py-1.5 px-3 rounded-lg border border-zinc-700/50 transition-colors"
          >
            {running ? 'Running…' : 'Run GC'}
          </button>
        )}
      </div>
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-4">
      <p className="text-xs text-zinc-500 font-medium">{label}</p>
      <p className="text-2xl font-semibold text-zinc-100 mt-1 tabular-nums">{value}</p>
    </div>
  )
}
