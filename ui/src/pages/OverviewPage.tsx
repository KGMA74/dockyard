import { ArrowRight, Box, Boxes, Database, Layers, PackageOpen, Tags } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { registryHost } from '@/api'
import { auditTarget, describeAction, resultLabel, resultTone } from '@/lib/audit'
import { formatBytes } from '@/lib/format'
import { useAudit, useHealth, useInsights, useRepositories, useStorageStats } from '@/lib/queries'
import { Link, repoHref } from '@/lib/router'
import { useSession } from '@/lib/session'
import {
  EmptyState,
  ErrorState,
  Fact,
  Facts,
  Page,
  PageHeader,
  Panel,
  Section,
  StatusBadge,
  TableSkeleton,
  Timestamp,
} from '@/components/kit'
import PushGuide from '@/components/PushGuide'
import SetupChecklist from '@/components/SetupChecklist'
import StatTile from '@/components/StatTile'
import { Button } from '@/components/ui/button'

const RECENT_LIMIT = 8

function RecentlyPushed() {
  const { t } = useTranslation()
  const repositories = useRepositories()

  if (repositories.isPending) return <TableSkeleton rows={5} />
  if (repositories.isError) return <ErrorState error={repositories.error} onRetry={() => repositories.refetch()} />
  if (repositories.data.length === 0) {
    return <EmptyState icon={PackageOpen} title={t('overview.noImagesTitle')} description={t('overview.noImagesBody')} />
  }

  // Proxy mode has no push timestamps: fall back to alphabetical order.
  const recent = [...repositories.data]
    .sort((a, b) => (b.last_pushed ?? '').localeCompare(a.last_pushed ?? '') || a.name.localeCompare(b.name))
    .slice(0, RECENT_LIMIT)

  return (
    <ul className="divide-y">
      {recent.map(repo => (
        <li key={repo.name}>
          <Link
            to={repoHref(repo.name)}
            className="flex items-center gap-3 px-4 py-2.5 outline-none transition-colors hover:bg-muted/50 focus-visible:bg-muted/50"
          >
            <Box className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.5} />
            <span className="min-w-0 flex-1 truncate font-mono text-[13px]">{repo.name}</span>
            <span className="shrink-0 text-xs text-muted-foreground">{t('overview.tagCount', { count: repo.total })}</span>
            <Timestamp value={repo.last_pushed} className="w-28 shrink-0 text-right text-xs text-muted-foreground" />
          </Link>
        </li>
      ))}
    </ul>
  )
}

function RegistryStatus() {
  const { t } = useTranslation()
  const health = useHealth()

  if (health.isPending) return <TableSkeleton rows={4} />
  if (health.isError) return <ErrorState error={health.error} onRetry={() => health.refetch()} />

  const h = health.data
  const upstreamDown = h.registry?.startsWith('unreachable')
  const mirrorTotal = h.mirror ? h.mirror.hits + h.mirror.misses : 0

  return (
    <Facts>
      <Fact label={t('overview.status')}>
        <StatusBadge tone={h.status === 'ok' ? 'success' : 'warning'}>
          {h.status === 'ok' ? t('overview.statusOk') : t('overview.statusDegraded')}
        </StatusBadge>
      </Fact>
      <Fact label={t('overview.mode')}>{t(`modes.${h.mode}`)}</Fact>
      {h.storage && (
        <Fact label={t('overview.storageBackend')}>
          {h.storage.ok ? (
            <span className="tabular-nums">{t('overview.respondsIn', { ms: h.storage.latency_ms })}</span>
          ) : (
            <StatusBadge tone="danger">{t('overview.unreachable')}</StatusBadge>
          )}
        </Fact>
      )}
      {h.storage?.free_bytes !== undefined && (
        <Fact label={t('overview.diskFree')}>
          <span className="tabular-nums">{formatBytes(h.storage.free_bytes)}</span>
        </Fact>
      )}
      {h.registry && (
        <Fact label={t('overview.upstream')}>
          {upstreamDown ? (
            <StatusBadge tone="danger">{t('overview.unreachable')}</StatusBadge>
          ) : (
            <code className="text-xs">{h.registry.replace(/^https?:\/\//, '')}</code>
          )}
        </Fact>
      )}
      {h.mirror && (
        <Fact label={t('overview.cacheHitRate')}>
          <span className="tabular-nums">
            {mirrorTotal === 0 ? '—' : `${Math.round((h.mirror.hits / mirrorTotal) * 100)}%`}
            <span className="text-muted-foreground"> · {t('overview.requests', { count: mirrorTotal })}</span>
          </span>
        </Fact>
      )}
      <Fact label={t('overview.version')}>
        <code className="text-xs">{h.version ?? '—'}</code>
      </Fact>
    </Facts>
  )
}

function RecentActivity() {
  const { t } = useTranslation()
  const audit = useAudit({ limit: 6 })

  if (audit.isPending) return <TableSkeleton rows={4} />
  if (audit.isError) return <ErrorState error={audit.error} onRetry={() => audit.refetch()} />
  if (audit.data.entries.length === 0) {
    return <p className="px-4 py-8 text-center text-sm text-muted-foreground">{t('overview.noActivity')}</p>
  }

  return (
    <ul className="divide-y">
      {audit.data.entries.map(entry => {
        const target = auditTarget(entry)
        return (
          <li key={entry.id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
            <span className="min-w-0 flex-1 truncate">
              <span className="font-medium">{entry.actor || t('audit.anonymous')}</span>{' '}
              <span className="text-muted-foreground">{describeAction(entry.action, t).label.toLowerCase()}</span>
              {target && <code className="ml-1.5 text-xs">{target}</code>}
            </span>
            {resultTone(entry.result) !== 'success' && (
              <StatusBadge tone={resultTone(entry.result)}>{resultLabel(entry.result, t)}</StatusBadge>
            )}
            <Timestamp value={entry.at} className="shrink-0 text-xs text-muted-foreground" />
          </li>
        )
      })}
    </ul>
  )
}

export default function OverviewPage() {
  const { t } = useTranslation()
  const session = useSession()
  const repositories = useRepositories()
  const stats = useStorageStats()
  const insights = useInsights(session.isAdmin)

  const repos = repositories.data
  const totalTags = repos?.reduce((sum, repo) => sum + repo.total, 0)
  // Proxy mode reports -1: the upstream owns the storage.
  const hasStorage = stats.data !== undefined && stats.data.total_size_bytes >= 0
  const trend = insights.data?.history.slice(-14).map(sample => sample.total_size)

  return (
    <Page>
      <PageHeader
        title={t('overview.title')}
        description={
          <>
            {t('overview.subtitle')} <code className="font-medium text-foreground">{registryHost()}</code>
          </>
        }
        actions={
          <Button asChild variant="outline" size="sm">
            <Link to="/images">
              {t('overview.browseImages')}
              <ArrowRight />
            </Link>
          </Button>
        }
      />

      <SetupChecklist />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile icon={Boxes} label={t('overview.repositories')} value={repos ? String(repos.length) : undefined} />
        <StatTile
          icon={Tags}
          label={t('overview.tags')}
          value={totalTags !== undefined ? String(totalTags) : undefined}
          hint={
            repos && repos.length > 0
              ? t('overview.avgPerRepo', { avg: ((totalTags ?? 0) / repos.length).toFixed(1) })
              : undefined
          }
        />
        {(stats.isPending || hasStorage) && (
          <>
            <StatTile icon={Database} label={t('overview.storageUsed')} value={stats.data?.total_size_human} trend={trend} />
            <StatTile
              icon={Layers}
              label={t('overview.blobs')}
              value={stats.data ? String(stats.data.blob_count) : undefined}
              hint={t('overview.blobsHint')}
            />
          </>
        )}
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Section
            title={t('overview.recentlyPushed')}
            actions={
              <Button asChild variant="ghost" size="sm" className="text-muted-foreground">
                <Link to="/images">
                  {t('overview.viewAll')}
                  <ArrowRight />
                </Link>
              </Button>
            }
          >
            <Panel>
              <RecentlyPushed />
            </Panel>
          </Section>
        </div>
        <Section title={t('overview.registry')}>
          <Panel>
            <RegistryStatus />
          </Panel>
        </Section>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Section
          id="push-guide"
          title={session.canPush ? t('overview.pushTitle') : t('overview.pullTitle')}
          description={t('overview.pushBody')}
        >
          <Panel className="p-4">
            <PushGuide />
          </Panel>
        </Section>

        {session.isAdmin && (
          <Section
            title={t('overview.recentActivity')}
            description={t('overview.recentActivityBody')}
            actions={
              <Button asChild variant="ghost" size="sm" className="text-muted-foreground">
                <Link to="/activity">
                  {t('overview.viewAll')}
                  <ArrowRight />
                </Link>
              </Button>
            }
          >
            <Panel>
              <RecentActivity />
            </Panel>
          </Section>
        )}
      </div>
    </Page>
  )
}
