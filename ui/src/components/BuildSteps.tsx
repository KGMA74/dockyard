import { useState } from 'react'
import { FolderOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { HistoryStep } from '@/api'
import { parseInstruction } from '@/lib/dockerfile'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

function Line({ step, onBrowse }: { step: HistoryStep; onBrowse?: (digest: string) => void }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const { keyword, args } = parseInstruction(step.created_by)
  const addsLayer = !!step.layer_digest

  return (
    <li className={cn('flex items-start gap-3 py-1.5 pr-1.5 pl-3', !addsLayer && 'text-muted-foreground')}>
      <button
        type="button"
        onClick={() => setExpanded(value => !value)}
        aria-expanded={expanded}
        className={cn(
          'min-w-0 flex-1 rounded-sm py-0.5 text-left font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring',
          expanded ? 'break-all whitespace-pre-wrap' : 'truncate',
        )}
        title={expanded ? undefined : step.created_by}
      >
        {keyword && <span className={cn('font-semibold', addsLayer && 'text-foreground')}>{keyword} </span>}
        {args}
      </button>
      <span className="shrink-0 py-0.5 text-xs tabular-nums">
        {addsLayer ? formatBytes(step.size_bytes ?? 0) : <span className="text-muted-foreground">{t('imageDetails.noLayerAdded')}</span>}
      </span>
      {onBrowse && (
        <span className="w-6 shrink-0">
          {addsLayer && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t('imageDetails.browseLayer')}
              title={t('imageDetails.browseLayer')}
              onClick={() => onBrowse(step.layer_digest!)}
              className="text-muted-foreground hover:text-foreground"
            >
              <FolderOpen />
            </Button>
          )}
        </span>
      )}
    </li>
  )
}

/**
 * The image's Dockerfile, reconstructed from its config: each instruction
 * next to the layer it produced. Steps that only change metadata (ENV, CMD…)
 * are folded away by default — what costs space is what people look for.
 */
export default function BuildSteps({ history, onBrowse }: { history: HistoryStep[]; onBrowse: (digest: string) => void }) {
  const { t } = useTranslation()
  const [showMetadata, setShowMetadata] = useState(false)
  const metadataCount = history.filter(step => !step.layer_digest).length
  const shown = showMetadata ? history : history.filter(step => step.layer_digest)

  return (
    <div className="space-y-2">
      <ol className="divide-y rounded-lg border">
        {shown.map((step, index) => (
          <Line key={`${index}-${step.created_by}`} step={step} onBrowse={onBrowse} />
        ))}
      </ol>
      {metadataCount > 0 && (
        <Button variant="ghost" size="xs" className="text-muted-foreground" onClick={() => setShowMetadata(value => !value)}>
          {showMetadata ? t('imageDetails.hideMetadataSteps') : t('imageDetails.showMetadataSteps', { count: metadataCount })}
        </Button>
      )}
    </div>
  )
}
