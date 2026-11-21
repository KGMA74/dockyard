import { useMemo, useState } from 'react'
import {
  Ban,
  Bug,
  ExternalLink,
  FileSearch,
  Search,
  ShieldAlert,
  ShieldCheck,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { VulnCVE, VulnImage } from '@/api'
import { useVulnerabilityOverview } from '@/lib/queries'
import { Link, repoHref } from '@/lib/router'
import {
  Checkbox,
  EmptyState,
  ErrorState,
  isUnavailable,
  Page,
  PageHeader,
  Panel,
  Section,
  StatusBadge,
  TableSkeleton,
  Timestamp,
  Unavailable,
} from '@/components/kit'
import { CompactScan, SeverityBadge } from '@/components/ScanBadges'
import StatTile from '@/components/StatTile'
import VulnerabilityReport from '@/components/VulnerabilityReport'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const CVE_PAGE = 50

/** Where to read about a vulnerability: Aqua's database for CVEs (what
 *  Trivy links to), GitHub for GHSA advisories. */
function advisoryURL(id: string): string | undefined {
  if (/^CVE-\d{4}-\d+$/i.test(id)) return `https://avd.aquasec.com/nvd/${id.toLowerCase()}`
  if (/^GHSA-/i.test(id)) return `https://github.com/advisories/${id}`
  return undefined
}

function imageRef(image: VulnImage): string {
  return image.reference.startsWith('sha256:')
    ? `${image.name}@${image.reference}`
    : `${image.name}:${image.reference}`
}

/** One page for the whole registry's vulnerabilities: which images to fix
 *  first, and which vulnerabilities are everywhere — one base image update
 *  often clears a CVE from dozens of images. */
export default function VulnerabilitiesPage() {
  const { t } = useTranslation()
  const overview = useVulnerabilityOverview()
  const [filter, setFilter] = useState('')
  const [blockedOnly, setBlockedOnly] = useState(false)
  const [cve, setCve] = useState<VulnCVE | null>(null)
  const [report, setReport] = useState<VulnImage | null>(null)
  const [cveLimit, setCveLimit] = useState(CVE_PAGE)

  const images = useMemo(() => overview.data?.images ?? [], [overview.data])
  const cves = useMemo(() => overview.data?.cves ?? [], [overview.data])
  const byScan = useMemo(() => new Map(images.map(image => [image.scan_id, image])), [images])

  const visibleImages = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    const inCve = cve ? new Set(cve.scan_ids) : null
    return images.filter(
      image =>
        (!inCve || inCve.has(image.scan_id)) &&
        (!blockedOnly || image.pull_blocked) &&
        (!needle || imageRef(image).toLowerCase().includes(needle)),
    )
  }, [images, filter, blockedOnly, cve])

  const visibleCves = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    return needle ? cves.filter(c => c.id.toLowerCase().includes(needle)) : cves
  }, [cves, filter])

  if (overview.isError && isUnavailable(overview.error)) {
    return (
      <Page>
        <PageHeader title={t('vulnPage.title')} description={t('vulnPage.description')} />
        <Unavailable title={t('vulnPage.offTitle')}>{t('vulnPage.offBody')}</Unavailable>
      </Page>
    )
  }

  const critical = images.filter(image => image.critical_count > 0).length
  const blocked = images.filter(image => image.pull_blocked).length
  const clean = images.filter(
    image =>
      image.critical_count +
        image.high_count +
        image.medium_count +
        image.low_count +
        image.unknown_count ===
      0,
  ).length

  return (
    <Page>
      <PageHeader title={t('vulnPage.title')} description={t('vulnPage.description')} />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile
          icon={ShieldCheck}
          label={t('vulnPage.scanned')}
          value={overview.data ? String(images.length) : undefined}
          hint={overview.data ? t('vulnPage.cleanHint', { count: clean }) : undefined}
        />
        <StatTile
          icon={ShieldAlert}
          label={t('vulnPage.withCritical')}
          value={overview.data ? String(critical) : undefined}
        />
        <StatTile
          icon={Ban}
          label={t('vulnPage.blocked')}
          value={overview.data ? String(blocked) : undefined}
          hint={t('vulnPage.blockedHint')}
        />
        <StatTile
          icon={Bug}
          label={t('vulnPage.distinct')}
          value={overview.data ? String(overview.data.cve_count) : undefined}
        />
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="relative min-w-56 flex-1 sm:max-w-sm">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={filter}
            onChange={event => setFilter(event.target.value)}
            placeholder={t('vulnPage.filter')}
            aria-label={t('vulnPage.filter')}
            className="bg-card pl-8"
          />
        </div>
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Checkbox
            checked={blockedOnly}
            onChange={event => setBlockedOnly(event.target.checked)}
          />
          {t('vulnPage.blockedOnly')}
        </label>
        {cve && (
          <Button variant="outline" size="sm" onClick={() => setCve(null)}>
            {t('vulnPage.affectedBy', { id: cve.id })}
            <X />
          </Button>
        )}
      </div>

      <Section title={t('vulnPage.imagesTitle')} description={t('vulnPage.imagesBody')}>
        <Panel>
          {overview.isPending ? (
            <TableSkeleton rows={5} />
          ) : overview.isError ? (
            <ErrorState error={overview.error} onRetry={() => overview.refetch()} />
          ) : images.length === 0 ? (
            <EmptyState
              icon={ShieldCheck}
              title={t('vulnPage.noScansTitle')}
              description={t('vulnPage.noScansBody')}
            />
          ) : visibleImages.length === 0 ? (
            <p className="py-10 text-center text-sm text-muted-foreground">
              {t('vulnPage.noMatch')}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="px-4">{t('vulnPage.image')}</TableHead>
                  <TableHead>{t('columns.vulnerabilities')}</TableHead>
                  <TableHead className="hidden text-right md:table-cell">
                    {t('vulnPage.scannedAt')}
                  </TableHead>
                  <TableHead className="w-12 pr-4" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleImages.map(image => (
                  <TableRow key={image.scan_id}>
                    <TableCell className="max-w-0 px-4 sm:max-w-80">
                      <Link
                        to={repoHref(
                          image.name,
                          image.reference.startsWith('sha256:') ? undefined : image.reference,
                        )}
                        className="block truncate font-mono text-[13px] hover:underline"
                        title={imageRef(image)}
                      >
                        {imageRef(image)}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <CompactScan
                        scan={{ id: image.scan_id, status: 'succeeded', ...image }}
                        blocked={image.pull_blocked}
                      />
                    </TableCell>
                    <TableCell className="hidden text-right text-xs text-muted-foreground md:table-cell">
                      <Timestamp value={image.finished_at} />
                    </TableCell>
                    <TableCell className="pr-4 text-right">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => setReport(image)}
                        aria-label={t('repository.openReportOf', { tag: imageRef(image) })}
                        title={t('repository.openReport')}
                        className="text-muted-foreground hover:text-foreground"
                      >
                        <FileSearch />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Panel>
      </Section>

      {cves.length > 0 && (
        <Section
          title={t('vulnPage.cvesTitle')}
          description={t('vulnPage.cvesBody', {
            shown: cves.length,
            total: overview.data?.cve_count ?? 0,
          })}
        >
          <Panel>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="px-4">{t('vulns.id')}</TableHead>
                  <TableHead>{t('vulns.severity')}</TableHead>
                  <TableHead className="text-right">{t('vulnPage.affected')}</TableHead>
                  <TableHead className="hidden pr-4 sm:table-cell" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleCves.slice(0, cveLimit).map(item => {
                  const url = advisoryURL(item.id)
                  const affected = item.scan_ids.filter(id => byScan.has(id)).length
                  return (
                    <TableRow
                      key={item.id}
                      data-state={cve?.id === item.id ? 'selected' : undefined}
                    >
                      <TableCell className="px-4 font-mono text-xs">
                        {url ? (
                          <a
                            href={url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 hover:underline"
                          >
                            {item.id}
                            <ExternalLink className="size-3 text-muted-foreground" />
                          </a>
                        ) : (
                          item.id
                        )}
                      </TableCell>
                      <TableCell>
                        <SeverityBadge severity={item.severity} />
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="xs"
                          onClick={() => setCve(cve?.id === item.id ? null : item)}
                          aria-pressed={cve?.id === item.id}
                          title={t('vulnPage.showAffected')}
                          className="tabular-nums"
                        >
                          {t('vulnPage.imageCount', { count: affected })}
                        </Button>
                      </TableCell>
                      <TableCell className="hidden pr-4 text-right sm:table-cell">
                        {item.allowlisted ? (
                          <StatusBadge tone="success">{t('vulnPage.accepted')}</StatusBadge>
                        ) : (
                          <Button
                            asChild
                            variant="ghost"
                            size="xs"
                            className="text-muted-foreground"
                          >
                            <Link to="/security">{t('vulnPage.accept')}</Link>
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            {visibleCves.length > cveLimit && (
              <div className="border-t p-2 text-center">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setCveLimit(limit => limit + CVE_PAGE)}
                >
                  {t('vulnPage.showMore', {
                    count: Math.min(CVE_PAGE, visibleCves.length - cveLimit),
                  })}
                </Button>
              </div>
            )}
          </Panel>
        </Section>
      )}

      {report && (
        <VulnerabilityReport
          scan={{ id: report.scan_id, trivy_version: report.trivy_version ?? '' }}
          image={imageRef(report)}
          onClose={() => setReport(null)}
        />
      )}
    </Page>
  )
}
