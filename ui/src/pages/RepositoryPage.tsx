import { useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, FolderSearch, GitCompare, Info, RefreshCw, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { deleteManifest, deleteRepository, registryHost, type TagInfo } from '@/api'
import { keys, useInvalidate, useTags } from '@/lib/queries'
import { Link, navigate, repoHref, setQueryParam, useLocation } from '@/lib/router'
import { isArtifactTag, useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import DeleteModal from '@/components/DeleteModal'
import ImageDetailsPanel from '@/components/ImageDetailsPanel'
import {
  Checkbox,
  Command,
  CopyButton,
  Digest,
  EmptyState,
  ErrorState,
  Page,
  PageHeader,
  Panel,
  StatusBadge,
  TableSkeleton,
  TagBadge,
  Timestamp,
} from '@/components/kit'
import PushGuide from '@/components/PushGuide'
import TagDiff from '@/components/TagDiff'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

type SortKey = 'pushed' | 'tag'
type SortState = { key: SortKey; descending: boolean }

// Version-aware comparison: v1.10.0 sorts after v1.9.0.
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function sortTags(tags: TagInfo[], sort: SortState): TagInfo[] {
  const direction = sort.descending ? -1 : 1
  return [...tags].sort((a, b) => {
    if (sort.key === 'pushed') {
      const byDate = (a.pushed_at ?? '').localeCompare(b.pushed_at ?? '')
      if (byDate !== 0) return byDate * direction
    }
    return collator.compare(a.tag, b.tag) * direction
  })
}

function SortHeader({
  label,
  sortKey,
  sort,
  onSort,
  align = 'left',
}: {
  label: string
  sortKey: SortKey
  sort: SortState
  onSort: (key: SortKey) => void
  align?: 'left' | 'right'
}) {
  const active = sort.key === sortKey
  const Icon = sort.descending ? ArrowDown : ArrowUp
  return (
    <button
      onClick={() => onSort(sortKey)}
      className={cn(
        'inline-flex items-center gap-1 rounded-sm outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring',
        align === 'right' && 'flex-row-reverse',
        !active && 'text-muted-foreground',
      )}
    >
      {label}
      <Icon className={cn('size-3', !active && 'opacity-0')} aria-hidden="true" />
    </button>
  )
}

export default function RepositoryPage({ repo }: { repo: string }) {
  const { t } = useTranslation()
  const session = useSession()
  const { query } = useLocation()
  const tags = useTags(repo)
  const invalidate = useInvalidate()

  const [filter, setFilter] = useState('')
  const [showArtifacts, setShowArtifacts] = useState(false)
  const [sort, setSort] = useState<SortState>({ key: 'pushed', descending: true })
  const [selected, setSelected] = useState<string[]>([])
  const [comparing, setComparing] = useState(false)
  const [tagToDelete, setTagToDelete] = useState<TagInfo | null>(null)
  const [deletingRepo, setDeletingRepo] = useState(false)

  const all = useMemo(() => tags.data ?? [], [tags.data])
  const images = useMemo(() => all.filter(tag => !isArtifactTag(tag.tag)), [all])
  const artifactCount = all.length - images.length
  const hasPushDates = all.some(tag => tag.pushed_at)

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const pool = showArtifacts ? all : images
    const effectiveSort = !hasPushDates && sort.key === 'pushed' ? { key: 'tag' as const, descending: true } : sort
    return sortTags(needle ? pool.filter(tag => tag.tag.toLowerCase().includes(needle)) : pool, effectiveSort)
  }, [all, images, filter, showArtifacts, sort, hasPushDates])

  // The tag the "pull" example uses: latest when it exists, else the newest.
  const featured = images.find(tag => tag.tag === 'latest') ?? sortTags(images, { key: 'pushed', descending: true })[0]
  const lastPushed = images.reduce<string | undefined>(
    (latest, tag) => (tag.pushed_at && (!latest || tag.pushed_at > latest) ? tag.pushed_at : latest),
    undefined,
  )

  const openTagName = query.get('tag')
  const openTag = openTagName ? all.find(tag => tag.tag === openTagName) : undefined

  function toggleSort(key: SortKey) {
    setSort(current => (current.key === key ? { key, descending: !current.descending } : { key, descending: true }))
  }

  function toggleSelected(tag: string) {
    setSelected(current => {
      if (current.includes(tag)) return current.filter(name => name !== tag)
      return current.length >= 2 ? [current[1], tag] : [...current, tag] // keep the two latest picks
    })
  }

  /** Other tags pointing at the same manifest: deleting one deletes them all. */
  function siblingsOf(tag: TagInfo): string[] {
    return all.filter(other => other.digest === tag.digest && other.tag !== tag.tag).map(other => other.tag)
  }

  async function handleDeleteTag(tag: TagInfo) {
    await deleteManifest(repo, tag.digest)
    setTagToDelete(null)
    setSelected([])
    toast.success(t('repository.tagDeleted', { ref: `${repo}:${tag.tag}` }))
    const wasLast = all.every(other => other.digest === tag.digest)
    await invalidate(keys.tags(repo), keys.repositories, keys.stats)
    if (wasLast) navigate('/images', { replace: true })
  }

  async function handleDeleteRepo() {
    await deleteRepository(repo)
    toast.success(t('images.deleted', { name: repo }))
    await invalidate(keys.repositories, keys.stats)
    navigate('/images', { replace: true })
  }

  const missing = tags.isSuccess && all.length === 0

  return (
    <Page>
      <PageHeader
        title={<span className="font-mono break-all">{repo}</span>}
        description={
          tags.isSuccess && !missing ? (
            <span className="flex flex-wrap items-center gap-x-1.5">
              {t('repository.tagCount', { count: images.length })}
              {lastPushed && (
                <>
                  <span aria-hidden="true">·</span>
                  {t('repository.lastPushed')} <Timestamp value={lastPushed} />
                </>
              )}
            </span>
          ) : undefined
        }
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => tags.refetch()} disabled={tags.isFetching}>
              <RefreshCw className={cn(tags.isFetching && 'animate-spin')} />
              {t('kit.refresh')}
            </Button>
            {session.isAdmin && !missing && (
              <Button variant="destructive" size="sm" onClick={() => setDeletingRepo(true)}>
                <Trash2 />
                {t('repository.delete')}
              </Button>
            )}
          </>
        }
      />

      {tags.isPending ? (
        <Panel>
          <TableSkeleton rows={5} />
        </Panel>
      ) : tags.isError ? (
        <Panel>
          <ErrorState error={tags.error} onRetry={() => tags.refetch()} />
        </Panel>
      ) : missing ? (
        <Panel>
          <EmptyState icon={FolderSearch} title={t('repository.missingTitle')} description={t('repository.missingBody')}>
            {session.canPush && <PushGuide repo={repo} className="mt-2 w-full max-w-xl text-left" />}
            <Button asChild variant="outline" size="sm">
              <Link to="/images">{t('shell.backToImages')}</Link>
            </Button>
          </EmptyState>
        </Panel>
      ) : (
        <>
          {featured && (
            <div className="max-w-2xl space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">{t('repository.pull')}</p>
              <Command>{`docker pull ${registryHost()}/${repo}:${featured.tag}`}</Command>
            </div>
          )}

          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <div className="relative min-w-56 flex-1 sm:max-w-xs">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  value={filter}
                  onChange={event => setFilter(event.target.value)}
                  placeholder={t('repository.filterTags')}
                  aria-label={t('repository.filterTags')}
                  className="bg-card pl-8"
                />
              </div>
              {artifactCount > 0 && (
                <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Checkbox checked={showArtifacts} onChange={event => setShowArtifacts(event.target.checked)} />
                  {t('repository.showArtifacts', { count: artifactCount })}
                </label>
              )}
              <div className="ml-auto flex min-h-7 items-center gap-2 text-xs text-muted-foreground" aria-live="polite">
                {selected.length === 0 && images.length > 1 && <span>{t('repository.compareHint')}</span>}
                {selected.length === 1 && <span>{t('repository.pickOneMore', { tag: selected[0] })}</span>}
                {selected.length === 2 && (
                  <Button variant="outline" size="sm" onClick={() => setComparing(true)} className="text-foreground">
                    <GitCompare />
                    {t('repository.compare', { a: selected[0], b: selected[1] })}
                  </Button>
                )}
                {selected.length > 0 && (
                  <Button variant="ghost" size="sm" onClick={() => setSelected([])}>
                    {t('repository.clearSelection')}
                  </Button>
                )}
              </div>
            </div>

            <Panel>
              {visible.length === 0 ? (
                <EmptyState title={t('repository.noMatch', { filter })} />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-10 pl-4">
                        <span className="sr-only">{t('repository.selectColumn')}</span>
                      </TableHead>
                      <TableHead>
                        <SortHeader label={t('columns.tag')} sortKey="tag" sort={sort} onSort={toggleSort} />
                      </TableHead>
                      <TableHead className="hidden sm:table-cell">{t('columns.digest')}</TableHead>
                      {hasPushDates && (
                        <TableHead className="hidden text-right md:table-cell">
                          <SortHeader label={t('columns.pushed')} sortKey="pushed" sort={sort} onSort={toggleSort} align="right" />
                        </TableHead>
                      )}
                      <TableHead className="w-28 pr-4" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visible.map(tag => {
                      const artifact = isArtifactTag(tag.tag)
                      const isSelected = selected.includes(tag.tag)
                      return (
                        <TableRow key={tag.tag} data-state={isSelected ? 'selected' : undefined} className="group/row">
                          <TableCell className="pl-4">
                            {!artifact && (
                              <Checkbox
                                checked={isSelected}
                                onChange={() => toggleSelected(tag.tag)}
                                aria-label={t('repository.selectForCompare', { tag: tag.tag })}
                                className="align-middle"
                              />
                            )}
                          </TableCell>
                          <TableCell className="max-w-0 sm:max-w-64">
                            <span className="flex items-center gap-1.5">
                              <Link
                                to={repoHref(repo, tag.tag)}
                                className="inline-flex min-w-0 rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
                              >
                                <TagBadge className={artifact ? 'opacity-60' : undefined}>{tag.tag}</TagBadge>
                              </Link>
                              {artifact && <StatusBadge>{t('repository.artifact')}</StatusBadge>}
                            </span>
                          </TableCell>
                          <TableCell className="hidden sm:table-cell">
                            <Digest value={tag.digest} />
                          </TableCell>
                          {hasPushDates && (
                            <TableCell className="hidden text-right text-xs text-muted-foreground md:table-cell">
                              <Timestamp value={tag.pushed_at} />
                            </TableCell>
                          )}
                          <TableCell className="pr-4">
                            <span className="flex items-center justify-end gap-0.5">
                              {!artifact && (
                                <CopyButton
                                  value={`docker pull ${registryHost()}/${repo}:${tag.tag}`}
                                  label={t('repository.copyPull')}
                                />
                              )}
                              <Button
                                variant="ghost"
                                size="icon-xs"
                                onClick={() => setQueryParam('tag', tag.tag)}
                                aria-label={t('repository.detailsOf', { tag: tag.tag })}
                                title={t('repository.details')}
                                className="text-muted-foreground hover:text-foreground"
                              >
                                <Info />
                              </Button>
                              {session.isAdmin && (
                                <Button
                                  variant="ghost"
                                  size="icon-xs"
                                  onClick={() => setTagToDelete(tag)}
                                  aria-label={t('repository.deleteTagNamed', { tag: tag.tag })}
                                  title={t('repository.deleteTag')}
                                  className="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                >
                                  <Trash2 />
                                </Button>
                              )}
                            </span>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              )}
            </Panel>
          </div>
        </>
      )}

      {openTag && <ImageDetailsPanel imageName={repo} tag={openTag} onClose={() => setQueryParam('tag', null)} />}

      {comparing && selected.length === 2 && (
        <TagDiff imageName={repo} tagA={selected[0]} tagB={selected[1]} onClose={() => setComparing(false)} />
      )}

      {tagToDelete && (
        <DeleteModal
          title={t('repository.deleteTagTitle')}
          resourceName={`${repo}:${tagToDelete.tag}`}
          confirmLabel={t('repository.deleteTag')}
          description={t('repository.deleteTagBody')}
          warning={
            siblingsOf(tagToDelete).length > 0
              ? t('repository.deleteTagSiblings', { tags: siblingsOf(tagToDelete).join(', ') })
              : undefined
          }
          detail={tagToDelete.digest}
          onConfirm={() => handleDeleteTag(tagToDelete)}
          onCancel={() => setTagToDelete(null)}
        />
      )}

      {deletingRepo && (
        <DeleteModal
          title={t('images.deleteTitle')}
          resourceName={repo}
          confirmLabel={t('images.deleteConfirm')}
          description={t('images.deleteBody', { count: all.length })}
          onConfirm={handleDeleteRepo}
          onCancel={() => setDeletingRepo(false)}
        />
      )}
    </Page>
  )
}
