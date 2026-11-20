import { useTranslation } from 'react-i18next'
import type { ScanResult, Severity, TagScanSummary } from '@/api'
import { StatusBadge, type Tone } from './kit'

const STATUS_TONE: Record<ScanResult['status'], Tone> = {
  queued: 'neutral',
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
}

export function ScanStatusBadge({ status }: { status: ScanResult['status'] }) {
  const { t } = useTranslation()
  return <StatusBadge tone={STATUS_TONE[status]}>{t(`scan.status.${status}`)}</StatusBadge>
}

export const SEVERITY_TONE: Record<Severity, Tone> = {
  CRITICAL: 'danger',
  HIGH: 'warning',
  MEDIUM: 'info',
  LOW: 'neutral',
  UNKNOWN: 'neutral',
}

export function SeverityBadge({ severity, count }: { severity: Severity; count?: number }) {
  const { t } = useTranslation()
  return (
    <StatusBadge tone={SEVERITY_TONE[severity]}>
      {t(`scan.severity.${severity}`)}
      {count !== undefined && <span className="tabular-nums">{count}</span>}
    </StatusBadge>
  )
}

/** One table cell's worth of scan result. Critical and high are what decide
 *  whether an image ships, so they get badges; everything below is folded
 *  into a count, keeping a column of these readable down a long tag list. */
export function CompactScan({ scan }: { scan?: TagScanSummary }) {
  const { t } = useTranslation()
  if (!scan) return <span className="text-xs text-muted-foreground">{t('repository.notScanned')}</span>
  if (scan.status !== 'succeeded') return <ScanStatusBadge status={scan.status} />

  const lower = scan.medium_count + scan.low_count + scan.unknown_count
  if (scan.critical_count + scan.high_count + lower === 0) {
    return (
      <span title={t('scan.clean')}>
        <StatusBadge tone="success">{t('scan.cleanShort')}</StatusBadge>
      </span>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {scan.critical_count > 0 && <SeverityBadge severity="CRITICAL" count={scan.critical_count} />}
      {scan.high_count > 0 && <SeverityBadge severity="HIGH" count={scan.high_count} />}
      {lower > 0 && (
        <span className="text-xs text-muted-foreground tabular-nums">
          {scan.critical_count + scan.high_count > 0
            ? t('repository.lowerSeverities', { count: lower })
            : t('repository.mediumOrLower', { count: lower })}
        </span>
      )}
    </span>
  )
}

/** Severity counts of a finished scan; "No known vulnerabilities" when clean
 *  (an empty row of badges reads as "not scanned"). */
export function SeverityCounts({
  scan,
}: {
  scan: Pick<ScanResult, 'critical_count' | 'high_count' | 'medium_count' | 'low_count' | 'unknown_count'>
}) {
  const { t } = useTranslation()
  const counts: [Severity, number][] = [
    ['CRITICAL', scan.critical_count],
    ['HIGH', scan.high_count],
    ['MEDIUM', scan.medium_count],
    ['LOW', scan.low_count],
    ['UNKNOWN', scan.unknown_count],
  ]
  const present = counts.filter(([, count]) => count > 0)
  if (present.length === 0) return <StatusBadge tone="success">{t('scan.clean')}</StatusBadge>
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {present.map(([severity, count]) => (
        <SeverityBadge key={severity} severity={severity} count={count} />
      ))}
    </span>
  )
}
