import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownAZ, PackageOpen, RefreshCw, Search, SearchX, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { deleteRepository, type RepoSummary } from '@/api'
import { keys, useInvalidate, useRepositories } from '@/lib/queries'
import { Link, repoHref, setQueryParam, useLocation } from '@/lib/router'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import DeleteModal from '@/components/DeleteModal'
import { EmptyState, ErrorState, NativeSelect, Page, PageHeader, Panel, TableSkeleton, Timestamp } from '@/components/kit'
import PushGuide from '@/components/PushGuide'
import TagSearch from '@/components/TagSearch'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

type SortKey = 'name' | 'tags' | 'pushed'
type View = 'repositories' | 'tags'

function sortRepositories(repos: RepoSummary[], sort: SortKey): RepoSummary[] {
  return [...repos].sort((a, b) => {
    if (sort === 'tags') return b.total - a.total || a.name.localeCompare(b.name)
    if (sort === 'pushed') return (b.last_pushed ?? '').localeCompare(a.last_pushed ?? '') || a.name.localeCompare(b.name)
    return a.name.localeCompare(b.name)
  })
}

/** `team/sub/app` with the namespace dimmed, so the image name stands out
 *  in a long list that shares the same prefixes. */
function RepositoryName({ name }: { name: string }) {
  const cut = name.lastIndexOf('/') + 1
  return (
    <span className="truncate font-mono text-[13px]">
      {cut > 0 && <span className="text-muted-foreground">{name.slice(0, cut)}</span>}
      <span className="font-medium">{name.slice(cut)}</span>
    </span>
  )
}

export default function ImagesPage() {
  const { t } = useTranslation()
  const session = useSession()
  const { query } = useLocation()
  const repositories = useRepositories()
  const invalidate = useInvalidate()
  const searchRef = useRef<HTMLInputElement>(null)
  const [toDelete, setToDelete] = useState<RepoSummary | null>(null)

  // Filter, sort and view live in the URL: a filtered list is shareable and
  // survives a reload or the back button.
  const search = query.get('q') ?? ''
  const view: View = query.get('view') === 'tags' ? 'tags' : 'repositories'
  const sort = (['name', 'tags', 'pushed'].includes(query.get('sort') ?? '') ? query.get('sort') : 'name') as SortKey

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    const all = repositories.data ?? []
    return sortRepositories(needle ? all.filter(repo => repo.name.toLowerCase().includes(needle)) : all, sort)
  }, [repositories.data, search, sort])

  // "/" focuses the filter, Escape clears it.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      const target = event.target as HTMLElement
      const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable
      if (event.key === '/' && !typing) {
        event.preventDefault()
        searchRef.current?.focus()
      } else if (event.key === 'Escape' && target === searchRef.current) {
        setQueryParam('q', null, { replace: true })
        searchRef.current?.blur()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  async function handleDelete(repo: RepoSummary) {
    await deleteRepository(repo.name)
    setToDelete(null)
    toast.success(t('images.deleted', { name: repo.name }))
    await invalidate(keys.repositories, keys.stats)
  }

  const total = repositories.data?.length ?? 0
  const hasPushDates = repositories.data?.some(repo => repo.last_pushed) ?? false

  return (
    <Page>
      <PageHeader
        title={t('images.title')}
        description={
          repositories.data
            ? `${t('images.repoCount', { count: total })} · ${t('overview.tagCount', {
                count: repositories.data.reduce<number>((sum, repo) => sum + repo.total, 0),
              })}`
            : undefined
        }
        actions={
          <Button variant="outline" size="sm" onClick={() => repositories.refetch()} disabled={repositories.isFetching}>
            <RefreshCw className={cn(repositories.isFetching && 'animate-spin')} />
            {t('kit.refresh')}
          </Button>
        }
      />

      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-56 flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              ref={searchRef}
              type="search"
              value={search}
              onChange={event => setQueryParam('q', event.target.value, { replace: true })}
              placeholder={view === 'tags' ? t('images.searchTags') : t('images.filterRepositories')}
              aria-label={view === 'tags' ? t('images.searchTags') : t('images.filterRepositories')}
              className="bg-card pl-8"
            />
            <kbd className="pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 rounded border bg-muted px-1 text-[10px] font-medium text-muted-foreground sm:block">
              /
            </kbd>
          </div>

          {view === 'repositories' && (
            <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <ArrowDownAZ className="size-3.5" aria-hidden="true" />
              <span className="sr-only">{t('images.sort')}</span>
              <NativeSelect
                value={sort}
                onChange={event => setQueryParam('sort', event.target.value === 'name' ? null : event.target.value, { replace: true })}
                className="w-auto bg-card"
              >
                <option value="name">{t('images.sortName')}</option>
                <option value="tags">{t('images.sortTags')}</option>
                {hasPushDates && <option value="pushed">{t('images.sortPushed')}</option>}
              </NativeSelect>
            </label>
          )}

          <div role="tablist" aria-label={t('images.view')} className="flex rounded-lg border bg-card p-0.5 text-xs">
            {(['repositories', 'tags'] as const).map(option => (
              <button
                key={option}
                role="tab"
                aria-selected={view === option}
                onClick={() => setQueryParam('view', option === 'tags' ? 'tags' : null, { replace: true })}
                className={cn(
                  'rounded-md px-2.5 py-1 font-medium outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring',
                  view === option ? 'bg-muted text-foreground' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {t(`images.view_${option}`)}
              </button>
            ))}
          </div>
        </div>

        {view === 'tags' ? (
          <TagSearch query={search} />
        ) : (
          <Panel>
            {repositories.isPending ? (
              <TableSkeleton rows={6} />
            ) : repositories.isError ? (
              <ErrorState error={repositories.error} onRetry={() => repositories.refetch()} />
            ) : total === 0 ? (
              <EmptyState icon={PackageOpen} title={t('images.emptyTitle')} description={t('images.emptyBody')}>
                <PushGuide className="mt-2 w-full max-w-xl text-left" />
              </EmptyState>
            ) : filtered.length === 0 ? (
              <EmptyState icon={SearchX} title={t('images.noMatch', { search })} description={t('images.noMatchHint')}>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setQueryParam('q', null, { replace: true })}>
                    {t('images.clearFilter')}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setQueryParam('view', 'tags', { replace: true })}>
                    {t('images.searchTagsInstead')}
                  </Button>
                </div>
              </EmptyState>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="px-4">{t('columns.repository')}</TableHead>
                    <TableHead className="w-24 text-right">{t('columns.tags')}</TableHead>
                    {hasPushDates && <TableHead className="hidden w-40 text-right sm:table-cell">{t('columns.lastPushed')}</TableHead>}
                    {session.isAdmin && <TableHead className="w-12 px-4" />}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map(repo => (
                    <TableRow key={repo.name} className="group/row">
                      <TableCell className="max-w-0 px-4">
                        <Link
                          to={repoHref(repo.name)}
                          className="flex min-w-0 rounded-sm py-1 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
                        >
                          <RepositoryName name={repo.name} />
                        </Link>
                      </TableCell>
                      <TableCell className="text-right text-muted-foreground">{repo.total}</TableCell>
                      {hasPushDates && (
                        <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                          <Timestamp value={repo.last_pushed} />
                        </TableCell>
                      )}
                      {session.isAdmin && (
                        <TableCell className="px-4 text-right">
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => setToDelete(repo)}
                            aria-label={t('images.deleteNamed', { name: repo.name })}
                            className="text-muted-foreground opacity-0 group-hover/row:opacity-100 hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100"
                          >
                            <Trash2 />
                          </Button>
                        </TableCell>
                      )}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Panel>
        )}

        {view === 'repositories' && search && filtered.length > 0 && (
          <p className="text-xs text-muted-foreground tabular-nums">{t('images.showing', { shown: filtered.length, total })}</p>
        )}
      </div>

      {toDelete && (
        <DeleteModal
          title={t('images.deleteTitle')}
          resourceName={toDelete.name}
          confirmLabel={t('images.deleteConfirm')}
          description={t('images.deleteBody', { count: toDelete.total })}
          onConfirm={() => handleDelete(toDelete)}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Page>
  )
}
