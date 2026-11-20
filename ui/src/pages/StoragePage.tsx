import { Boxes, Database, HardDrive, Layers } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatBytes } from '@/lib/format'
import { useHealth, useInsights, useRepositories, useStorageStats } from '@/lib/queries'
import GcSection from '@/components/GcSection'
import InsightsSection from '@/components/InsightsSection'
import { isUnavailable, Page, PageHeader, Unavailable } from '@/components/kit'
import QuotasSection from '@/components/QuotasSection'
import RetentionSection from '@/components/RetentionSection'
import StatTile from '@/components/StatTile'

export default function StoragePage() {
  const { t } = useTranslation()
  const stats = useStorageStats()
  const health = useHealth()
  const repositories = useRepositories()
  const insights = useInsights()

  // Proxy mode: the upstream owns storage, so there is nothing to size or
  // collect from here (stats come back as -1).
  const proxied = stats.data !== undefined && stats.data.total_size_bytes < 0
  const trend = insights.data?.history.slice(-14).map(sample => sample.total_size)
  const freeBytes = health.data?.storage?.free_bytes

  return (
    <Page>
      <PageHeader title={t('storage.title')} description={t('storage.description')} />

      {proxied ? (
        <Unavailable title={t('storage.proxyTitle')}>{t('storage.proxyBody')}</Unavailable>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile icon={Database} label={t('overview.storageUsed')} value={stats.data?.total_size_human} trend={trend} />
            <StatTile
              icon={Layers}
              label={t('overview.blobs')}
              value={stats.data ? String(stats.data.blob_count) : undefined}
              hint={t('overview.blobsHint')}
            />
            <StatTile icon={Boxes} label={t('overview.repositories')} value={stats.data ? String(stats.data.repo_count) : undefined} />
            {stats.data?.storage_path ? (
              <StatTile
                icon={HardDrive}
                label={t('storage.diskFree')}
                value={freeBytes !== undefined ? formatBytes(freeBytes) : '—'}
                hint={
                  <code className="block truncate" title={stats.data.storage_path}>
                    {stats.data.storage_path}
                  </code>
                }
              />
            ) : (
              <StatTile
                icon={HardDrive}
                label={t('storage.backend')}
                value={stats.data ? 'S3' : undefined}
                hint={t('storage.objectStorage')}
              />
            )}
          </div>

          {insights.data && <InsightsSection topRepos={insights.data.top_repos} history={insights.data.history} />}
          {insights.isError && !isUnavailable(insights.error) && (
            <p className="text-sm text-destructive">{t('storage.insightsFailed')}</p>
          )}

          <GcSection repositoryCount={repositories.data?.length ?? 0} />
          <RetentionSection />
        </>
      )}

      <QuotasSection />
    </Page>
  )
}
