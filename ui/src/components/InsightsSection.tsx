import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { RepoSize, StatsSample } from '@/api'
import { formatBytes, formatDateTime } from '@/lib/format'
import { Link, repoHref } from '@/lib/router'
import GrowthChart from './GrowthChart'
import { Panel, Section } from './kit'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

interface Props {
  topRepos: RepoSize[]
  history: StatsSample[]
}

/** Where the space goes and how fast it grows. */
export default function InsightsSection({ topRepos, history }: Props) {
  const { t } = useTranslation()
  const [showTable, setShowTable] = useState(false)

  const maxSize = Math.max(1, ...topRepos.map(repo => repo.size_bytes))
  const growth = history.slice(-28)

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Section title={t('insights.largest')} description={t('insights.largestBody')}>
        <Panel className="p-4">
          {topRepos.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">{t('insights.noRepo')}</p>
          ) : (
            <ul className="space-y-3">
              {topRepos.map(repo => (
                <li key={repo.name} className="space-y-1 text-xs">
                  <div className="flex items-baseline justify-between gap-3">
                    <Link to={repoHref(repo.name)} className="min-w-0 truncate font-mono text-[13px] hover:underline">
                      {repo.name}
                    </Link>
                    <span className="shrink-0 tabular-nums">
                      {repo.size_human}
                      <span className="text-muted-foreground"> · {t('insights.tagCount', { count: repo.tags })}</span>
                    </span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-primary/70"
                      style={{ width: `${Math.max(2, (repo.size_bytes / maxSize) * 100)}%` }}
                    />
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </Section>

      <Section
        title={t('insights.growth')}
        description={t('insights.growthBody')}
        actions={
          growth.length >= 2 && (
            <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => setShowTable(v => !v)}>
              {showTable ? t('insights.viewAsChart') : t('insights.viewAsTable')}
            </Button>
          )
        }
      >
        <Panel className={showTable && growth.length >= 2 ? undefined : 'p-4'}>
          {growth.length < 2 ? (
            <p className="py-6 text-center text-sm text-muted-foreground text-pretty">{t('insights.notEnoughSamples')}</p>
          ) : showTable ? (
            <div className="max-h-72 overflow-y-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="px-4">{t('insights.date')}</TableHead>
                    <TableHead className="text-right">{t('insights.size')}</TableHead>
                    <TableHead className="text-right">{t('insights.blobs')}</TableHead>
                    <TableHead className="px-4 text-right">{t('insights.repos')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {[...growth].reverse().map(sample => (
                    <TableRow key={sample.at}>
                      <TableCell className="px-4 text-xs text-muted-foreground">{formatDateTime(sample.at)}</TableCell>
                      <TableCell className="text-right">{formatBytes(sample.total_size)}</TableCell>
                      <TableCell className="text-right">{sample.blob_count}</TableCell>
                      <TableCell className="px-4 text-right">{sample.repo_count}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <GrowthChart samples={growth} />
          )}
        </Panel>
      </Section>
    </div>
  )
}
