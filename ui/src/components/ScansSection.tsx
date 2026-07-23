import { useCallback, useEffect, useState } from 'react'
import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { listScans, ScanResult } from '../api'
import { ScanStatusBadge, SeverityBadge } from './ScanBadges'
import { Card } from '@/components/ui/card'

// ScansSection lives in the Storage tab, alongside Retention/GC. Hidden when
// the feature is off (SCAN_ENABLED=false) or the caller isn't admin — the
// list call 403s/404s in that case.
export default function ScansSection() {
  const { t } = useTranslation()
  const [scans, setScans] = useState<ScanResult[] | null>(null)

  const load = useCallback(() => {
    listScans({ limit: 20 })
      .then(r => setScans(r.scans))
      .catch(() => setScans(null))
  }, [])

  useEffect(load, [load])

  if (scans === null) return null

  return (
    <div>
      <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
        {t('scansSection.title')}
      </h3>
      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <ShieldAlert className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('scansSection.description')}
          </p>
        </div>

        {scans.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('scansSection.noScan')}</p>
        ) : (
          <div className="space-y-2">
            {scans.map(s => (
              <div key={s.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <span className="font-mono text-xs truncate flex-1" title={`${s.name}@${s.digest}`}>
                  {s.name}
                </span>
                <ScanStatusBadge status={s.status} />
                {s.status === 'succeeded' && (
                  <div className="flex items-center gap-1.5 shrink-0">
                    <SeverityBadge count={s.critical_count} tone="critical" />
                    <SeverityBadge count={s.high_count} tone="high" />
                    <SeverityBadge count={s.medium_count} tone="medium" />
                    <SeverityBadge count={s.low_count} tone="low" />
                  </div>
                )}
                <span className="text-xs text-muted-foreground shrink-0 tabular-nums">
                  {new Date(s.created_at).toLocaleString()}
                </span>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
