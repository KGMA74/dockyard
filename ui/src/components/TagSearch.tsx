import { useEffect, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, SearchX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { searchRepositories, type SearchResult } from '@/api'
import { Link, repoHref } from '@/lib/router'
import { Digest, EmptyState, ErrorState, NativeSelect, Panel, StatusBadge, TableSkeleton, TagBadge, Timestamp } from './kit'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const PAGE_SIZE = 50

function ScanSummary({ scan }: { scan: NonNullable<SearchResult['scan']> }) {
  const { t } = useTranslation()
  if (scan.status !== 'succeeded') {
    return <StatusBadge tone={scan.status === 'failed' ? 'danger' : 'neutral'}>{t(`scan.status.${scan.status}`)}</StatusBadge>
  }
  const serious = scan.critical_count + scan.high_count
  if (serious === 0) return <StatusBadge tone="success">{t('scan.clean')}</StatusBadge>
  return (
    <StatusBadge tone={scan.critical_count > 0 ? 'danger' : 'warning'}>{t('scan.highPlus', { count: serious })}</StatusBadge>
  )
}

/** Flat, server-side search across every repository *and* tag — the view for
 *  "where is 2.4.1 deployed from?" or "which images are unsigned?". */
export default function TagSearch({ query }: { query: string }) {
  const { t } = useTranslation()
  const [signed, setSigned] = useState<'any' | 'true' | 'false'>('any')
  const [debounced, setDebounced] = useState(query)

  useEffect(() => {
    const id = setTimeout(() => setDebounced(query), 250)
    return () => clearTimeout(id)
  }, [query])

  // The page offset only means something for the filters it was reached
  // with: changing them starts again from the first page.
  const filterKey = `${debounced}|${signed}`
  const [paging, setPaging] = useState({ filterKey, offset: 0 })
  const offset = paging.filterKey === filterKey ? paging.offset : 0
  const setOffset = (next: number) => setPaging({ filterKey, offset: next })

  const params = {
    q: debounced,
    signed: signed === 'any' ? undefined : signed === 'true',
    limit: PAGE_SIZE,
    offset,
  }
  const search = useQuery({
    queryKey: ['tags', 'search', params],
    queryFn: () => searchRepositories(params),
    placeholderData: keepPreviousData,
  })

  const items = search.data?.items ?? []
  const total = search.data?.total ?? 0
  // The signed column only exists when cosign keys are configured server-side.
  const signingKnown = items.some(item => item.signed !== undefined) || signed !== 'any'

  return (
    <div className="space-y-3">
      {signingKnown && (
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          {t('tagSearch.signature')}
          <NativeSelect value={signed} onChange={e => setSigned(e.target.value as typeof signed)} className="h-7 w-36 bg-card text-xs">
            <option value="any">{t('tagSearch.any')}</option>
            <option value="true">{t('tagSearch.signed')}</option>
            <option value="false">{t('tagSearch.unsigned')}</option>
          </NativeSelect>
        </label>
      )}

      <Panel>
        {search.isPending ? (
          <TableSkeleton rows={6} />
        ) : search.isError ? (
          <ErrorState error={search.error} onRetry={() => search.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title={debounced ? t('tagSearch.noMatch', { query: debounced }) : t('tagSearch.noMatchFilters')}
            description={t('tagSearch.noMatchHint')}
          />
        ) : (
          <Table className={search.isPlaceholderData ? 'opacity-60 transition-opacity' : undefined}>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="px-4">{t('columns.repository')}</TableHead>
                <TableHead>{t('columns.tag')}</TableHead>
                <TableHead className="hidden sm:table-cell">{t('columns.digest')}</TableHead>
                {signingKnown && <TableHead className="hidden md:table-cell">{t('columns.signature')}</TableHead>}
                <TableHead className="hidden md:table-cell">{t('columns.scan')}</TableHead>
                <TableHead className="hidden px-4 text-right lg:table-cell">{t('columns.pushed')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map(item => (
                <TableRow key={`${item.name}:${item.tag}`}>
                  <TableCell className="max-w-64 px-4">
                    <Link to={repoHref(item.name)} className="block truncate font-mono text-[13px] hover:underline">
                      {item.name}
                    </Link>
                  </TableCell>
                  <TableCell className="max-w-48">
                    <Link to={repoHref(item.name, item.tag)} className="inline-flex max-w-full rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring">
                      <TagBadge>{item.tag}</TagBadge>
                    </Link>
                  </TableCell>
                  <TableCell className="hidden sm:table-cell">
                    <Digest value={item.digest} />
                  </TableCell>
                  {signingKnown && (
                    <TableCell className="hidden md:table-cell">
                      {item.signed === undefined ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        <StatusBadge tone={item.signed ? 'success' : 'neutral'}>
                          {item.signed ? t('tagSearch.signed') : t('tagSearch.unsigned')}
                        </StatusBadge>
                      )}
                    </TableCell>
                  )}
                  <TableCell className="hidden md:table-cell">
                    {item.scan ? <ScanSummary scan={item.scan} /> : <span className="text-muted-foreground">—</span>}
                  </TableCell>
                  <TableCell className="hidden px-4 text-right text-xs text-muted-foreground lg:table-cell">
                    <Timestamp value={item.pushed_at} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>

      {total > PAGE_SIZE && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span className="tabular-nums">
            {t('kit.range', { from: offset + 1, to: Math.min(offset + PAGE_SIZE, total), total })}
          </span>
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
        </div>
      )}
    </div>
  )
}
