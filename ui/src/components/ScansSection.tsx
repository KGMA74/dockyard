import { useState } from 'react'
import { FileSearch, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ScanResult } from '@/api'
import { useScans } from '@/lib/queries'
import { Link, repoHref } from '@/lib/router'
import { Digest, EmptyState, Panel, QueryPanel, RowAction, Section, Timestamp } from './kit'
import { ScanStatusBadge, SeverityCounts } from './ScanBadges'
import VulnerabilityReport from './VulnerabilityReport'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

/** History of vulnerability scans across the registry. Scans are started
 *  from an image's detail panel; this is where their results are compared. */
export default function ScansSection() {
  const { t } = useTranslation()
  const scans = useScans({ limit: 50 }, { pollWhileRunning: true })
  const [report, setReport] = useState<ScanResult | null>(null)

  return (
    <Section id="scans" title={t('scans.title')} description={t('scans.description')}>
      <QueryPanel
        query={scans}
        unavailable={{
          title: t('scans.disabledTitle'),
          body: (
            <>
              {t('scans.disabledPrefix')} <code className="font-medium text-foreground">SCAN_ENABLED=true</code> {t('scans.disabledSuffix')}
            </>
          ),
        }}
        isEmpty={data => data.scans.length === 0}
        empty={<EmptyState icon={ShieldAlert} title={t('scans.emptyTitle')} description={t('scans.emptyBody')} />}
        skeletonRows={4}
      >
        {data => (
          <Panel>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="px-4">{t('columns.image')}</TableHead>
                  <TableHead className="hidden lg:table-cell">{t('columns.digest')}</TableHead>
                  <TableHead>{t('scans.findings')}</TableHead>
                  <TableHead className="hidden md:table-cell">{t('scans.requestedBy')}</TableHead>
                  <TableHead className="hidden text-right sm:table-cell">{t('scans.scanned')}</TableHead>
                  <TableHead className="w-12 px-4" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.scans.map(scan => {
                  // `reference` is whatever the scan was requested with; only a tag makes a useful link.
                  const tag = scan.reference.startsWith('sha256:') ? undefined : scan.reference
                  return (
                    <TableRow key={scan.id}>
                      <TableCell className="max-w-64 px-4">
                        <Link to={repoHref(scan.name, tag)} className="block truncate font-mono text-[13px] hover:underline">
                          {scan.name}
                          {tag && <span className="text-muted-foreground">:{tag}</span>}
                        </Link>
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        <Digest value={scan.digest} />
                      </TableCell>
                      <TableCell>
                        {scan.status === 'succeeded' ? (
                          <SeverityCounts scan={scan} />
                        ) : (
                          <span title={scan.error || undefined}>
                            <ScanStatusBadge status={scan.status} />
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground md:table-cell">{scan.requested_by || '—'}</TableCell>
                      <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                        <Timestamp value={scan.finished_at ?? scan.created_at} />
                      </TableCell>
                      <TableCell className="px-4 text-right">
                        {scan.status === 'succeeded' && (
                          <RowAction label={t('imageDetails.viewReport')} onClick={() => setReport(scan)}>
                            <FileSearch />
                          </RowAction>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </Panel>
        )}
      </QueryPanel>

      {report && <VulnerabilityReport scan={report} image={`${report.name}@${report.digest}`} onClose={() => setReport(null)} />}
    </Section>
  )
}
