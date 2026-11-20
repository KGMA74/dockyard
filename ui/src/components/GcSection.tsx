import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Eye, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { runGC, type GCResult } from '@/api'
import { keys, useInvalidate } from '@/lib/queries'
import { Checkbox, ConfirmDialog, errorMessage, Panel, Section, Spinner, StatusBadge } from './kit'
import { Button } from '@/components/ui/button'

/** Result of the last run, kept on screen: a toast that vanishes after four
 *  seconds is not where "freed 12 GB, skipped 3 recent blobs" belongs. */
function Outcome({ result }: { result: GCResult }) {
  const { t } = useTranslation()
  const graceMinutes = Math.round((result.grace_period_seconds ?? 0) / 60)
  return (
    <div className="space-y-2 border-t px-4 py-3 text-sm" aria-live="polite">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge tone={result.dry_run ? 'info' : 'success'}>{result.dry_run ? t('gc.preview') : t('gc.completed')}</StatusBadge>
        <span>
          {result.count === 0
            ? t('gc.nothing')
            : result.dry_run
              ? t('gc.wouldRemove', { count: result.count, freed: result.freed_human })
              : t('gc.removed', { count: result.count, freed: result.freed_human })}
        </span>
      </div>
      <ul className="space-y-1 text-xs text-muted-foreground">
        <li className="tabular-nums">{t('gc.scanned', { count: result.scanned_blobs ?? 0 })}</li>
        {(result.skipped_recent ?? 0) > 0 && (
          <li>{t('gc.skippedRecent', { count: result.skipped_recent, minutes: graceMinutes })}</li>
        )}
        {(result.failed ?? 0) > 0 && <li className="text-destructive">{t('gc.failedCount', { count: result.failed })}</li>}
      </ul>
    </div>
  )
}

export default function GcSection({ repositoryCount }: { repositoryCount: number }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [force, setForce] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [result, setResult] = useState<GCResult | null>(null)

  const preview = useMutation({
    mutationFn: () => runGC({ dryRun: true, force }),
    onSuccess: setResult,
    onError: err => toast.error(errorMessage(err, t('gc.failed'))),
  })

  async function collect() {
    const outcome = await runGC({ force })
    setResult(outcome)
    setConfirming(false)
    // Zero blobs listed while repositories exist: the backend could not
    // enumerate storage (typically a missing s3:ListBucket permission).
    if (outcome.scanned_blobs === 0 && repositoryCount > 0) toast.warning(t('gc.nothingListed'))
    await invalidate(keys.stats, keys.insights)
  }

  return (
    <Section id="gc" title={t('gc.title')} description={t('gc.description')}>
      <Panel>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-3 px-4 py-3">
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => preview.mutate()} disabled={preview.isPending}>
              {preview.isPending ? <Spinner /> : <Eye />}
              {t('gc.previewAction')}
            </Button>
            <Button variant="secondary" size="sm" onClick={() => setConfirming(true)}>
              <Trash2 />
              {t('gc.runAction')}
            </Button>
          </div>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Checkbox checked={force} onChange={event => setForce(event.target.checked)} />
            {t('gc.force')}
          </label>
        </div>
        {result && <Outcome result={result} />}
      </Panel>

      {confirming && (
        <ConfirmDialog
          title={t('gc.confirmTitle')}
          description={force ? t('gc.confirmForceBody') : t('gc.confirmBody')}
          confirmLabel={t('gc.runAction')}
          destructive
          onConfirm={collect}
          onCancel={() => setConfirming(false)}
        />
      )}
    </Section>
  )
}
