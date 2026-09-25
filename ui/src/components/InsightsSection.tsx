import { useState } from 'react'
import { TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { RepoSize, StatsSample } from '../api'
import GrowthChart from './GrowthChart'
import { formatBytes } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

interface Props {
  topRepos: RepoSize[]
  history: StatsSample[]
}

// InsightsSection shows the largest repositories and storage growth; hidden
// for non-admins (403) and in proxy mode. Data is fetched once by the parent
// (StorageTab) and shared with the "Storage used" sparkline.
export default function InsightsSection({ topRepos, history }: Props) {
  const { t } = useTranslation()
  const [showTable, setShowTable] = useState(false)

  const maxSize = Math.max(1, ...topRepos.map(r => r.size_bytes))
  // One point per day at most, most recent last.
  const growth = history.slice(-14)

  return (
    <div>
      <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
        {t('insights.title')}
      </h3>
      <div className="grid gap-3 lg:grid-cols-2">
        <Card className="p-4 rounded-xl gap-3">
          <p className="text-sm font-medium">{t('insights.largestRepos')}</p>
          {topRepos.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('insights.noRepo')}</p>
          ) : (
            <div className="space-y-2">
              {topRepos.map(r => (
                <div key={r.name} className="text-xs">
                  <div className="flex justify-between mb-0.5">
                    <span className="font-mono truncate">{r.name}</span>
                    <span className="text-muted-foreground shrink-0 ml-2">
                      {r.size_human} · {t('insights.tagCount', { count: r.tags })}
                    </span>
                  </div>
                  <div className="h-1.5 rounded-full bg-muted overflow-hidden">
                    <div
                      className="h-full rounded-full bg-blue-500/70 dark:bg-blue-400/70"
                      style={{ width: `${Math.max(2, (r.size_bytes / maxSize) * 100)}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card className="p-4 rounded-xl gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <TrendingUp className="size-4 text-muted-foreground" strokeWidth={1.5} />
              <p className="text-sm font-medium">{t('insights.growth')}</p>
            </div>
            {growth.length >= 2 && (
              <Button variant="ghost" size="sm" className="text-xs text-muted-foreground -my-1" onClick={() => setShowTable(v => !v)}>
                {showTable ? t('insights.viewAsChart') : t('insights.viewAsTable')}
              </Button>
            )}
          </div>

          {growth.length < 2 ? (
            <p className="text-xs text-muted-foreground">
              {t('insights.notEnoughSamples')}
            </p>
          ) : showTable ? (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-left text-muted-foreground border-b">
                    <th className="py-1.5 pr-3 font-medium">{t('insights.date')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('insights.size')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('insights.blobs')}</th>
                    <th className="py-1.5 font-medium">{t('insights.repos')}</th>
                  </tr>
                </thead>
                <tbody>
                  {growth.map(s => (
                    <tr key={s.at} className="border-b last:border-0">
                      <td className="py-1.5 pr-3 whitespace-nowrap text-muted-foreground">
                        {new Date(s.at).toLocaleString()}
                      </td>
                      <td className="py-1.5 pr-3">{formatBytes(s.total_size)}</td>
                      <td className="py-1.5 pr-3">{s.blob_count}</td>
                      <td className="py-1.5">{s.repo_count}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <GrowthChart samples={growth} />
          )}
        </Card>
      </div>
    </div>
  )
}
