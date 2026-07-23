import { useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { searchRepositories, SearchResult, TagInfo } from '../api'
import { relativeTime } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const PAGE_SIZE = 50

interface Props {
  query: string
  onShowDetails: (name: string, tag: TagInfo) => void
}

// DenseRepoView is a flat, server-searched alternative to the per-repo card
// list — useful once a repo has many tags (no expand-per-repo needed) or
// when filtering by signed status across the whole registry.
export default function DenseRepoView({ query, onShowDetails }: Props) {
  const { t } = useTranslation()
  const [items, setItems] = useState<SearchResult[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [signedFilter, setSignedFilter] = useState<'any' | 'true' | 'false'>('any')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => setOffset(0), [query, signedFilter])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    const id = setTimeout(() => {
      searchRepositories({
        q: query,
        signed: signedFilter === 'any' ? undefined : signedFilter === 'true',
        limit: PAGE_SIZE,
        offset,
      })
        .then(r => { if (!cancelled) { setItems(r.items); setTotal(r.total) } })
        .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : t('denseRepoView.searchFailed')) })
        .finally(() => { if (!cancelled) setLoading(false) })
    }, 250) // debounce
    return () => { cancelled = true; clearTimeout(id) }
  }, [query, signedFilter, offset, t])

  const showingSigned = items.some(i => i.signed !== undefined)

  return (
    <div className="space-y-3">
      {showingSigned && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{t('denseRepoView.signed')}</span>
          <Select value={signedFilter} onValueChange={v => setSignedFilter(v as typeof signedFilter)}>
            <SelectTrigger size="sm" className="w-32 text-xs bg-card">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="any">{t('denseRepoView.any')}</SelectItem>
              <SelectItem value="true">{t('denseRepoView.signedOpt')}</SelectItem>
              <SelectItem value="false">{t('denseRepoView.unsigned')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      )}

      {error ? (
        <div className="text-center py-20 text-destructive text-sm">{error}</div>
      ) : !loading && items.length === 0 ? (
        <div className="text-center py-20 text-muted-foreground text-sm">
          {query ? t('denseRepoView.noMatchQuery', { query }) : t('denseRepoView.noMatchFilters')}
        </div>
      ) : (
        <div className="bg-card border rounded-xl overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4 text-xs">{t('denseRepoView.repository')}</TableHead>
                <TableHead className="px-4 text-xs">{t('denseRepoView.tag')}</TableHead>
                <TableHead className="px-4 text-xs hidden sm:table-cell">{t('denseRepoView.digest')}</TableHead>
                <TableHead className="px-4 text-xs hidden md:table-cell">{t('denseRepoView.signed')}</TableHead>
                <TableHead className="px-4 text-xs hidden md:table-cell">{t('denseRepoView.scan')}</TableHead>
                <TableHead className="px-4 text-xs hidden lg:table-cell">{t('denseRepoView.pushed')}</TableHead>
                <TableHead className="px-4 w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map(item => (
                <TableRow key={item.name + ':' + item.tag}>
                  <TableCell className="px-4 py-2.5 font-mono text-xs truncate max-w-60">{item.name}</TableCell>
                  <TableCell className="px-4 py-2.5">
                    <Badge
                      variant="outline"
                      className="font-mono text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/30 border-blue-200 dark:border-blue-900/30"
                    >
                      {item.tag}
                    </Badge>
                  </TableCell>
                  <TableCell className="px-4 py-2.5 hidden sm:table-cell font-mono text-xs text-muted-foreground/70">
                    {item.digest.slice(0, 7)}…{item.digest.slice(-6)}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 hidden md:table-cell">
                    {item.signed !== undefined && (
                      <Badge
                        variant="outline"
                        className={item.signed ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10' : 'text-muted-foreground'}
                      >
                        {item.signed ? t('denseRepoView.signedOpt') : t('denseRepoView.unsigned')}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 hidden md:table-cell">
                    {item.scan && (
                      <Badge
                        variant="outline"
                        className={
                          item.scan.status === 'succeeded' && item.scan.critical_count === 0 && item.scan.high_count === 0
                            ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10'
                            : item.scan.status === 'succeeded'
                              ? 'text-destructive border-destructive/30 bg-destructive/10'
                              : 'text-muted-foreground'
                        }
                      >
                        {item.scan.status === 'succeeded'
                          ? t('denseRepoView.highPlus', { count: item.scan.critical_count + item.scan.high_count })
                          : t(`scanBadges.status.${item.scan.status}`)}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 hidden lg:table-cell text-xs text-muted-foreground/70">
                    {item.pushed_at ? relativeTime(item.pushed_at) : '—'}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-right">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      title={t('denseRepoView.viewDetails')}
                      onClick={() => onShowDetails(item.name, { tag: item.tag, digest: item.digest, pushed_at: item.pushed_at })}
                      className="text-muted-foreground/60 hover:text-blue-500 dark:hover:text-blue-400"
                    >
                      <Info strokeWidth={1.5} />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {total > PAGE_SIZE && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            {t('denseRepoView.rangeOfTotal', { from: offset + 1, to: Math.min(offset + PAGE_SIZE, total), total })}
          </span>
          <div className="flex gap-1">
            <Button variant="outline" size="icon-sm" disabled={offset === 0} onClick={() => setOffset(o => Math.max(0, o - PAGE_SIZE))}>
              <ChevronLeft />
            </Button>
            <Button variant="outline" size="icon-sm" disabled={offset + PAGE_SIZE >= total} onClick={() => setOffset(o => o + PAGE_SIZE)}>
              <ChevronRight />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
