import { useState } from 'react'
import { runGC, StorageStats } from '../api'

interface Props {
  stats: StorageStats
  onRefresh: () => void
}

export default function StatsBar({ stats, onRefresh }: Props) {
  const [running, setRunning] = useState(false)
  const [gcMsg, setGcMsg] = useState<{ text: string; ok: boolean } | null>(null)

  async function handleGC() {
    setRunning(true)
    setGcMsg(null)
    try {
      const result = await runGC()
      setGcMsg({ text: `Removed ${result.count} blob(s) · freed ${result.freed_human}`, ok: true })
      onRefresh()
    } catch (err) {
      setGcMsg({ text: err instanceof Error ? err.message : 'GC failed', ok: false })
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      <StatCard label="Storage used" value={stats.total_size_human} />
      <StatCard label="Repositories" value={String(stats.repo_count)} />
      <StatCard label="Blobs" value={String(stats.blob_count)} />

      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-4 flex flex-col gap-2">
        <span className="text-xs text-zinc-500 font-medium">Garbage collection</span>
        {gcMsg && (
          <p className={`text-xs ${gcMsg.ok ? 'text-emerald-400' : 'text-red-400'}`}>
            {gcMsg.text}
          </p>
        )}
        <button
          onClick={handleGC}
          disabled={running}
          className="mt-auto text-xs bg-zinc-800 hover:bg-zinc-700 active:bg-zinc-600 disabled:opacity-40 disabled:cursor-not-allowed text-zinc-300 font-medium py-1.5 px-3 rounded-lg border border-zinc-700/50 transition-colors"
        >
          {running ? 'Running…' : 'Run GC'}
        </button>
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
