import { useEffect, useState } from 'react'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getTagDiff, TagDiff as TagDiffResult } from '../api'
import { Badge } from '@/components/ui/badge'
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
  const [diff, setDiff] = useState<TagDiffResult | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    getTagDiff(imageName, tagA, tagB)
      .then(d => { if (!cancelled) setDiff(d) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : t('tagDiff.loadFailed')) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [imageName, tagA, tagB, t])

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-sm flex items-center gap-2 font-mono">
            {tagA} <ArrowRight className="size-3.5 text-muted-foreground shrink-0" /> {tagB}
          </DialogTitle>
          <DialogDescription className="truncate">{imageName}</DialogDescription>
        </DialogHeader>

        {loading ? (
          <p className="text-sm text-muted-foreground py-10 text-center">{t('common.loading')}</p>
        ) : error ? (
          <p className="text-sm text-destructive py-10 text-center">{error}</p>
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
              <Badge
                variant="outline"
                className={
                  diff.size_delta_bytes > 0
                    ? 'text-destructive border-destructive/30 bg-destructive/10'
                    : diff.size_delta_bytes < 0
                      ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10'
                      : 'text-muted-foreground'
                }
              >
                {diff.size_delta_bytes > 0 ? '+' : ''}
                {t('tagDiff.bytes', { count: diff.size_delta_bytes.toLocaleString() })}
              </Badge>
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
                    <LayerRow key={`a-${d}`} digest={d} label={t('tagDiff.onlyIn', { tag: tagA })} tone="destructive" />
                  ))}
                  {diff.layers_only_b.map(d => (
                    <LayerRow key={`b-${d}`} digest={d} label={t('tagDiff.onlyIn', { tag: tagB })} tone="success" />
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
      <p className={`text-sm mt-0.5 ${diff ? 'text-amber-600 dark:text-amber-400 font-medium' : ''}`}>
        {value ?? '—'}
      </p>
    </div>
  )
}

function LayerRow({ digest, label, tone }: { digest: string; label: string; tone: 'destructive' | 'success' }) {
  return (
    <div className="flex items-center justify-between gap-3 bg-muted/50 border rounded-lg px-3 py-2">
      <span className="font-mono text-xs text-muted-foreground truncate">{digest.slice(0, 19)}…</span>
      <Badge
        variant="outline"
        className={
          tone === 'destructive'
            ? 'text-destructive border-destructive/30 bg-destructive/10 shrink-0'
            : 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10 shrink-0'
        }
      >
        {label}
      </Badge>
    </div>
  )
}
