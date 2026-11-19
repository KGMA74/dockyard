import { useQuery } from '@tanstack/react-query'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getTagDiff } from '../api'
import { formatBytes } from '../lib/format'
import { ErrorState, StatusBadge, TableSkeleton } from './kit'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'

interface Props {
  imageName: string
  tagA: string
  tagB: string
  onClose: () => void
}

export default function TagDiff({ imageName, tagA, tagB, onClose }: Props) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['tags', imageName, 'diff', tagA, tagB],
    queryFn: () => getTagDiff(imageName, tagA, tagB),
  })
  const diff = query.data

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-sm flex items-center gap-2 font-mono">
            {tagA} <ArrowRight className="size-3.5 text-muted-foreground shrink-0" /> {tagB}
          </DialogTitle>
          <DialogDescription className="truncate">{imageName}</DialogDescription>
        </DialogHeader>

        {query.isPending ? (
          <TableSkeleton rows={4} />
        ) : query.isError ? (
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        ) : diff ? (
          <div className="flex-1 min-h-0 overflow-y-auto space-y-5">
            <div className="grid grid-cols-2 gap-3">
              <ColumnField label={t('tagDiff.size')} value={diff.a.total_size_human} />
              <ColumnField label={t('tagDiff.size')} value={diff.b.total_size_human} />
              <ColumnField label={t('tagDiff.architecture')} value={diff.a.architecture} diff={diff.a.architecture !== diff.b.architecture} />
              <ColumnField label={t('tagDiff.architecture')} value={diff.b.architecture} diff={diff.a.architecture !== diff.b.architecture} />
              <ColumnField label={t('tagDiff.os')} value={diff.a.os} diff={diff.a.os !== diff.b.os} />
              <ColumnField label={t('tagDiff.os')} value={diff.b.os} diff={diff.a.os !== diff.b.os} />
              {diff.a.signed !== undefined && (
                <>
                  <ColumnField label={t('tagDiff.signed')} value={diff.a.signed ? t('common.yes') : t('common.no')} />
                  <ColumnField label={t('tagDiff.signed')} value={diff.b.signed ? t('common.yes') : t('common.no')} />
                </>
              )}
            </div>

            <div>
              <p className="text-xs text-muted-foreground font-medium mb-2">{t('tagDiff.sizeDelta')}</p>
              <StatusBadge
                tone={diff.size_delta_bytes > 0 ? 'warning' : diff.size_delta_bytes < 0 ? 'success' : 'neutral'}
                className="tabular-nums"
              >
                {diff.size_delta_bytes === 0
                  ? t('tagDiff.noSizeChange')
                  : `${diff.size_delta_bytes > 0 ? '+' : '−'}${formatBytes(Math.abs(diff.size_delta_bytes))}`}
              </StatusBadge>
            </div>

            <div>
              <p className="text-xs text-muted-foreground font-medium mb-2">
                {t('tagDiff.layersSummary', {
                  shared: diff.layers_common.length,
                  onlyACount: diff.layers_only_a.length,
                  tagA,
                  onlyBCount: diff.layers_only_b.length,
                  tagB,
                })}
              </p>
              {diff.layers_only_a.length === 0 && diff.layers_only_b.length === 0 ? (
                <p className="text-xs text-muted-foreground/70">{t('tagDiff.identical')}</p>
              ) : (
                <div className="space-y-1.5">
                  {diff.layers_only_a.map(d => (
                    <LayerRow key={`a-${d}`} digest={d} label={t('tagDiff.onlyIn', { tag: tagA })} side="a" />
                  ))}
                  {diff.layers_only_b.map(d => (
                    <LayerRow key={`b-${d}`} digest={d} label={t('tagDiff.onlyIn', { tag: tagB })} side="b" />
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function ColumnField({ label, value, diff }: { label: string; value?: string; diff?: boolean }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground font-medium">{label}</p>
      <p className={`text-sm mt-0.5 ${diff ? 'text-warning font-medium' : ''}`}>
        {value ?? '—'}
      </p>
    </div>
  )
}

// Which side a layer belongs to is a fact, not good or bad news: the two sides
// are told apart by a neutral marker (−/+, as in a diff), not red and green.
function LayerRow({ digest, label, side }: { digest: string; label: string; side: 'a' | 'b' }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
      <span className="flex min-w-0 items-center gap-2 font-mono text-xs">
        <span aria-hidden="true" className="w-2 shrink-0 text-muted-foreground">{side === 'a' ? '−' : '+'}</span>
        <span className="truncate text-muted-foreground">{digest.slice(0, 19)}…</span>
      </span>
      <StatusBadge>{label}</StatusBadge>
    </div>
  )
}
