import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import type { ScanResult } from '../api'

const STATUS_STYLE: Record<ScanResult['status'], string> = {
  queued: 'text-muted-foreground',
  running: 'text-blue-600 dark:text-blue-400 border-blue-500/30 bg-blue-500/10',
  succeeded: 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10',
  failed: 'text-destructive border-destructive/30 bg-destructive/10',
}

export function ScanStatusBadge({ status }: { status: ScanResult['status'] }) {
  const { t } = useTranslation()
  return (
    <Badge variant="outline" className={`capitalize ${STATUS_STYLE[status]}`}>
      {t(`scanBadges.status.${status}`)}
    </Badge>
  )
}

const SEVERITY_STYLE = {
  critical: 'text-destructive border-destructive/30 bg-destructive/10',
  high: 'text-orange-600 dark:text-orange-400 border-orange-500/30 bg-orange-500/10',
  medium: 'text-amber-600 dark:text-amber-400 border-amber-500/30 bg-amber-500/10',
  low: 'text-muted-foreground',
} as const

export function SeverityBadge({
  count,
  tone,
}: {
  count: number
  tone: keyof typeof SEVERITY_STYLE
}) {
  const { t } = useTranslation()
  if (count === 0) return null
  return (
    <Badge variant="outline" className={SEVERITY_STYLE[tone]}>
      {t(`scanBadges.severity.${tone}`)} {count}
    </Badge>
  )
}
