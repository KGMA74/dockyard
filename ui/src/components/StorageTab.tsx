import { useState } from 'react'
import { toast } from 'sonner'
import { Trash2, Database, Boxes, Layers, Tags } from 'lucide-react'
import { runGC, RepoSummary, StorageStats } from '../api'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

interface Props {
  stats: StorageStats | null
  repos: RepoSummary[]
  onRefresh: () => void
}

export default function StorageTab({ stats, repos, onRefresh }: Props) {
  const [running, setRunning] = useState(false)

  async function handleGC() {
    setRunning(true)
    try {
      const result = await runGC()
      toast.success(`GC done — removed ${result.count} blob(s), freed ${result.freed_human}`)
      onRefresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'GC failed')
    } finally {
      setRunning(false)
    }
  }

  if (!stats) return null

  const unavailable = stats.total_size_bytes === -1
  const totalTags = repos.reduce((sum, r) => sum + r.total, 0)
  const avgTags = repos.length > 0 ? (totalTags / repos.length).toFixed(1) : '0'

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          Storage
        </h2>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <StatCard icon={Database} label="Storage used" value={unavailable ? '—' : stats.total_size_human} />
          <StatCard icon={Boxes} label="Repositories" value={unavailable ? '—' : String(stats.repo_count)} />
          <StatCard icon={Layers} label="Blobs" value={unavailable ? '—' : String(stats.blob_count)} />
          <StatCard icon={Tags} label="Tags" value={String(totalTags)} sub={`avg ${avgTags}/repo`} />
        </div>
      </div>

      {stats.storage_path && (
        <div>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
            Storage path
          </h3>
          <Card className="p-4 rounded-xl">
            <p className="font-mono text-xs text-muted-foreground break-all">{stats.storage_path}</p>
          </Card>
        </div>
      )}

      <div>
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          Garbage collection
        </h3>
        <Card className="p-4 rounded-xl gap-2">
          <p className="text-sm text-muted-foreground">
            Removes blobs that are no longer referenced by any manifest. Run this after
            deleting tags or repositories to reclaim disk space.
          </p>
          {unavailable ? (
            <p className="text-xs text-muted-foreground/70">Not available in proxy mode</p>
          ) : (
            <Button
              variant="secondary"
              size="sm"
              onClick={handleGC}
              disabled={running}
              className="self-start mt-1"
            >
              <Trash2 />
              {running ? 'Running…' : 'Run garbage collection'}
            </Button>
          )}
        </Card>
      </div>
    </div>
  )
}

function StatCard({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: typeof Database
  label: string
  value: string
  sub?: string
}) {
  return (
    <Card className="p-4 gap-1 rounded-xl">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        <Icon className="size-3.5" strokeWidth={1.5} />
        <p className="text-xs font-medium">{label}</p>
      </div>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      {sub && <p className="text-xs text-muted-foreground/70">{sub}</p>}
    </Card>
  )
}
