import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Trash2, Database, Boxes, Layers, Tags, Eye, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getHealth, runGC, HealthInfo, RepoSummary, StorageStats } from '../api'
import InsightsSection from './InsightsSection'
import RetentionSection from './RetentionSection'
import ScansSection from './ScansSection'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

interface Props {
  stats: StorageStats | null
  repos: RepoSummary[]
  onRefresh: () => void
}

export default function StorageTab({ stats, repos, onRefresh }: Props) {
  const { t } = useTranslation()
  const [running, setRunning] = useState(false)
  const [health, setHealth] = useState<HealthInfo | null>(null)

  useEffect(() => {
    getHealth().then(setHealth).catch(() => setHealth(null))
  }, [])

  async function handleGC(dryRun: boolean) {
    setRunning(true)
    try {
      const result = await runGC(dryRun)
      if (dryRun) {
        toast.info(
          result.count === 0
            ? t('storageTab.gcNothing')
            : t('storageTab.gcPreview', { count: result.count, freed: result.freed_human }),
        )
      } else {
        toast.success(t('storageTab.gcDone', { count: result.count, freed: result.freed_human }))
        onRefresh()
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('storageTab.gcFailed'))
    } finally {
      setRunning(false)
    }
  }

  if (!stats) return null

  const unavailable = stats.total_size_bytes === -1
  const totalTags = repos.reduce((sum, r) => sum + r.total, 0)
  const avgTags = repos.length > 0 ? (totalTags / repos.length).toFixed(1) : '0'

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          {t('storageTab.title')}
        </h2>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <StatCard icon={Database} label={t('storageTab.storageUsed')} value={unavailable ? '—' : stats.total_size_human} />
          <StatCard icon={Boxes} label={t('storageTab.repositories')} value={unavailable ? '—' : String(stats.repo_count)} />
          <StatCard icon={Layers} label={t('storageTab.blobs')} value={unavailable ? '—' : String(stats.blob_count)} />
          <StatCard icon={Tags} label={t('storageTab.tags')} value={String(totalTags)} sub={t('storageTab.avgPerRepo', { avg: avgTags })} />
        </div>
      </div>

      {health?.mirror && (
        <div>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
            {t('storageTab.pullThroughCache')}
          </h3>
          <div className="grid grid-cols-2 gap-3">
            <StatCard icon={Zap} label={t('storageTab.cacheHits')} value={String(health.mirror.hits)} />
            <StatCard
              icon={Zap}
              label={t('storageTab.cacheMisses')}
              value={String(health.mirror.misses)}
              sub={health.registry ? t('storageTab.upstream', { url: health.registry.replace(/^https?:\/\//, '') }) : undefined}
            />
          </div>
        </div>
      )}

      {stats.storage_path && (
        <div>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
            {t('storageTab.storagePath')}
          </h3>
          <Card className="p-4 rounded-xl">
            <p className="font-mono text-xs text-muted-foreground break-all">{stats.storage_path}</p>
          </Card>
        </div>
      )}

      <div>
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          {t('storageTab.gcTitle')}
        </h3>
        <Card className="p-4 rounded-xl gap-2">
          <p className="text-sm text-muted-foreground">
            {t('storageTab.gcDescription')}
          </p>
          {unavailable ? (
            <p className="text-xs text-muted-foreground/70">{t('storageTab.notAvailableProxy')}</p>
          ) : (
            <div className="flex gap-2 mt-1">
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleGC(true)}
                disabled={running}
              >
                <Eye />
                {t('storageTab.previewGC')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => handleGC(false)}
                disabled={running}
              >
                <Trash2 />
                {running ? t('storageTab.running') : t('storageTab.runGC')}
              </Button>
            </div>
          )}
        </Card>
      </div>

      {!unavailable && <InsightsSection />}

      {!unavailable && <RetentionSection />}

      {!unavailable && <ScansSection />}
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
