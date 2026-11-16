import { useTranslation } from 'react-i18next'
import type { ScanResult, Severity } from '@/api'
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

/** Severity counts of a finished scan; "No known vulnerabilities" when clean
 *  (an empty row of badges reads as "not scanned"). */
export function SeverityCounts({ scan }: { scan: ScanResult }) {
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
