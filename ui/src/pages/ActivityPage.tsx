import { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, RefreshCw, ScrollText, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { auditTarget, describeAction, resultLabel, resultTone } from '@/lib/audit'
import { formatDateTime } from '@/lib/format'
import { useAudit } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { EmptyState, ErrorState, Field, Page, PageHeader, Panel, StatusBadge, TableSkeleton } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const PAGE_SIZE = 50

function useDebounced(value: string, delay = 300): string {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay)
    return () => clearTimeout(id)
  }, [value, delay])
  return debounced
}

export default function ActivityPage() {
  const { t } = useTranslation()
  const [actor, setActor] = useState('')
  const [repo, setRepo] = useState('')
  const actorQuery = useDebounced(actor.trim())
  const repoQuery = useDebounced(repo.trim())

  // Paging restarts whenever the filters change.
  const filterKey = `${actorQuery}|${repoQuery}`
  const [paging, setPaging] = useState({ filterKey, offset: 0 })
  const offset = paging.filterKey === filterKey ? paging.offset : 0
  const setOffset = (next: number) => setPaging({ filterKey, offset: next })

  const audit = useAudit({ limit: PAGE_SIZE, offset, actor: actorQuery || undefined, repo: repoQuery || undefined })
  const entries = audit.data?.entries ?? []
  const total = audit.data?.total ?? 0
  const filtered = actorQuery !== '' || repoQuery !== ''

  return (
    <Page>
      <PageHeader
        title={t('activity.title')}
        description={t('activity.description')}
        actions={
          <Button variant="outline" size="sm" onClick={() => audit.refetch()} disabled={audit.isFetching}>
            <RefreshCw className={cn(audit.isFetching && 'animate-spin')} />
            {t('kit.refresh')}
          </Button>
        }
      />

      <div className="space-y-3">
        <div className="flex flex-wrap items-end gap-3">
          <Field label={t('activity.filterActor')} className="w-full sm:w-48">
            {ids => <Input {...ids} value={actor} onChange={e => setActor(e.target.value)} placeholder="alice" className="bg-card" />}
          </Field>
          <Field label={t('activity.filterRepo')} className="w-full sm:w-64">
            {ids => (
              <Input {...ids} value={repo} onChange={e => setRepo(e.target.value)} placeholder="team/app" className="bg-card font-mono" />
            )}
          </Field>
          {filtered && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setActor('')
                setRepo('')
              }}
              className="text-muted-foreground"
            >
              <X />
              {t('activity.clearFilters')}
            </Button>
          )}
        </div>

        <Panel>
          {audit.isPending ? (
            <TableSkeleton rows={8} />
          ) : audit.isError ? (
            <ErrorState error={audit.error} onRetry={() => audit.refetch()} />
          ) : entries.length === 0 ? (
            <EmptyState
              icon={ScrollText}
              title={filtered ? t('activity.noMatch') : t('activity.emptyTitle')}
              description={filtered ? undefined : t('activity.emptyBody')}
            />
          ) : (
            <Table className={audit.isPlaceholderData ? 'opacity-60 transition-opacity' : undefined}>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="px-4">{t('activity.when')}</TableHead>
                  <TableHead>{t('activity.actor')}</TableHead>
                  <TableHead>{t('activity.action')}</TableHead>
                  <TableHead className="hidden md:table-cell">{t('activity.target')}</TableHead>
                  <TableHead className="hidden lg:table-cell">{t('activity.sourceIp')}</TableHead>
                  <TableHead className="px-4 text-right">{t('activity.result')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map(entry => {
                  const action = describeAction(entry.action, t)
                  const target = auditTarget(entry)
                  const at = entry.at.endsWith('Z') ? entry.at : entry.at + 'Z'
                  return (
                    <TableRow key={entry.id}>
                      <TableCell className="px-4 text-xs text-muted-foreground">
                        <time dateTime={at}>{formatDateTime(at)}</time>
                      </TableCell>
                      <TableCell className="max-w-40 truncate font-medium">
                        {entry.actor || <span className="font-normal text-muted-foreground">{t('audit.anonymous')}</span>}
                      </TableCell>
                      <TableCell className="max-w-56 truncate" title={action.raw ?? entry.details}>
                        {action.label}
                      </TableCell>
                      <TableCell className="hidden max-w-64 truncate font-mono text-xs md:table-cell" title={entry.details}>
                        {target || <span className="font-sans text-muted-foreground">—</span>}
                      </TableCell>
                      <TableCell className="hidden font-mono text-xs text-muted-foreground lg:table-cell">
                        {entry.source_ip || '—'}
                      </TableCell>
                      <TableCell className="px-4 text-right">
                        <StatusBadge tone={resultTone(entry.result)}>{resultLabel(entry.result, t)}</StatusBadge>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </Panel>

        {total > 0 && (
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span className="tabular-nums">
              {t('kit.range', { from: offset + 1, to: Math.min(offset + PAGE_SIZE, total), total })}
            </span>
            {total > PAGE_SIZE && (
              <div className="flex gap-1">
                <Button
                  variant="outline"
                  size="icon-sm"
                  disabled={offset === 0}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                  aria-label={t('kit.previousPage')}
                >
                  <ChevronLeft />
                </Button>
                <Button
                  variant="outline"
                  size="icon-sm"
                  disabled={offset + PAGE_SIZE >= total}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                  aria-label={t('kit.nextPage')}
                >
                  <ChevronRight />
                </Button>
              </div>
            )}
          </div>
        )}
      </div>
    </Page>
  )
}
