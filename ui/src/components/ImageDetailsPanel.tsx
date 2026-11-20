import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { FileSearch, FolderOpen, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { registryHost, triggerScan, type ScanResult, type TagInfo } from '@/api'
import { formatDateTime } from '@/lib/format'
import { useManifest, useScans } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { historyIsAttributed } from '@/lib/dockerfile'
import BuildSteps from './BuildSteps'
import LayerBrowser from './LayerBrowser'
import {
  Command,
  CopyButton,
  Digest,
  ErrorState,
  errorMessage,
  isUnavailable,
  Spinner,
  StatusBadge,
  TableSkeleton,
  TagBadge,
} from './kit'
import { ScanStatusBadge, SeverityCounts } from './ScanBadges'
import VulnerabilityReport from './VulnerabilityReport'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'

interface Props {
  imageName: string
  tag: TagInfo
  onClose: () => void
}

function Block({ title, action, children }: { title: React.ReactNode; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <div className="flex min-h-6 items-center justify-between gap-2">
        <h3 className="text-xs font-medium text-muted-foreground">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-sm font-medium">{value}</dd>
    </div>
  )
}

function ScanBlock({ imageName, tag }: { imageName: string; tag: TagInfo }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [showReport, setShowReport] = useState(false)
  const params = { name: imageName, digest: tag.digest, limit: 1 }
  const scans = useScans(params, { pollWhileRunning: true })
  const scan: ScanResult | undefined = scans.data?.scans[0]
  const inFlight = scan?.status === 'queued' || scan?.status === 'running'

  const trigger = useMutation({
    mutationFn: () => triggerScan(imageName, tag.digest),
    onSuccess: result => {
      if (result.cached) toast.info(t('imageDetails.reusingScan'))
      void queryClient.invalidateQueries({ queryKey: ['scans'] })
    },
    onError: err => toast.error(errorMessage(err, t('imageDetails.scanStartFailed'))),
  })

  // 404: scanning is not enabled on this server — say nothing, like the rest
  // of the panel does for features that are off.
  if (scans.isError && isUnavailable(scans.error)) return null
  if (scans.isPending) return null

  return (
    <Block
      title={
        <span className="flex items-center gap-1.5">
          <ShieldAlert className="size-3.5" />
          {t('imageDetails.vulnScan')}
        </span>
      }
      action={
        scan && !inFlight ? (
          <Button variant="ghost" size="xs" onClick={() => trigger.mutate()} disabled={trigger.isPending} className="text-muted-foreground">
            {trigger.isPending ? <Spinner className="size-3" /> : null}
            {t('imageDetails.rescan')}
          </Button>
        ) : undefined
      }
    >
      {!scan ? (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-dashed px-3 py-2.5">
          <p className="text-xs text-muted-foreground">{t('imageDetails.neverScanned')}</p>
          <Button variant="outline" size="sm" onClick={() => trigger.mutate()} disabled={trigger.isPending}>
            {trigger.isPending ? <Spinner /> : <ShieldAlert />}
            {t('imageDetails.scanNow')}
          </Button>
        </div>
      ) : (
        <div className="space-y-2 rounded-lg border bg-muted/40 px-3 py-2.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            {scan.status === 'succeeded' ? <SeverityCounts scan={scan} /> : <ScanStatusBadge status={scan.status} />}
            {inFlight && <Spinner className="text-muted-foreground" />}
            {scan.status === 'succeeded' && (
              <Button variant="outline" size="sm" onClick={() => setShowReport(true)}>
                <FileSearch />
                {t('imageDetails.viewReport')}
              </Button>
            )}
          </div>
          {scan.status === 'failed' && scan.error && (
            <p className="break-words font-mono text-xs text-destructive">{scan.error}</p>
          )}
          {scan.finished_at && (
            <p className="text-xs text-muted-foreground">{t('imageDetails.scannedAt', { date: formatDateTime(scan.finished_at) })}</p>
          )}
        </div>
      )}
      {showReport && scan && (
        <VulnerabilityReport scan={scan} image={`${imageName}:${tag.tag}`} onClose={() => setShowReport(false)} />
      )}
    </Block>
  )
}

export default function ImageDetailsPanel({ imageName, tag, onClose }: Props) {
  const { t } = useTranslation()
  const session = useSession()
  const manifest = useManifest(imageName, tag.digest)
  const [browsingLayer, setBrowsingLayer] = useState<string | null>(null)
  const details = manifest.data
  const reference = `${registryHost()}/${imageName}`

  return (
    <>
      <Sheet open onOpenChange={open => !open && onClose()}>
        <SheetContent side="right" className="gap-0 overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
          <SheetHeader className="border-b pr-12">
            <SheetTitle className="truncate font-mono text-sm">{imageName}</SheetTitle>
            <SheetDescription asChild>
              <div className="flex flex-wrap items-center gap-1.5">
                <TagBadge>{tag.tag}</TagBadge>
                {details?.signed !== undefined && (
                  <StatusBadge tone={details.signed ? 'success' : 'neutral'}>
                    {details.signed ? t('imageDetails.signed') : t('imageDetails.unsigned')}
                  </StatusBadge>
                )}
              </div>
            </SheetDescription>
          </SheetHeader>

          <div className="space-y-6 p-4">
            <Block title={t('imageDetails.pull')}>
              <div className="space-y-1.5">
                <Command>{`docker pull ${reference}:${tag.tag}`}</Command>
                <Command>{`docker pull ${reference}@${tag.digest}`}</Command>
              </div>
              <p className="text-xs text-muted-foreground text-pretty">{t('imageDetails.pullByDigestHint')}</p>
            </Block>

            {manifest.isPending ? (
              <TableSkeleton rows={4} />
            ) : manifest.isError ? (
              <ErrorState error={manifest.error} onRetry={() => manifest.refetch()} />
            ) : details ? (
              <>
                <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
                  <Stat label={t('imageDetails.totalSize')} value={details.total_size_human} />
                  <Stat
                    label={details.platforms ? t('imageDetails.uniqueLayers') : t('imageDetails.layers')}
                    value={details.layers.length}
                  />
                  {details.os && details.architecture && (
                    <Stat
                      label={t('imageDetails.platform')}
                      value={[details.os, details.architecture, details.variant].filter(Boolean).join('/')}
                    />
                  )}
                  {details.created && <Stat label={t('imageDetails.built')} value={formatDateTime(details.created)} />}
                </dl>

                <Block title={t('imageDetails.digest')}>
                  <div className="flex items-start gap-1 rounded-lg border bg-muted/40 py-1.5 pr-1 pl-3">
                    <code className="min-w-0 flex-1 py-0.5 text-xs break-all text-muted-foreground">{details.digest}</code>
                    <CopyButton value={details.digest} label={t('kit.copyDigest')} />
                  </div>
                </Block>

                {session.isAdmin && <ScanBlock imageName={imageName} tag={tag} />}

                {details.platforms && details.platforms.length > 0 && (
                  <Block title={t('imageDetails.platforms', { count: details.platforms.length })}>
                    <ul className="divide-y rounded-lg border">
                      {details.platforms.map(platform => (
                        <li key={platform.digest} className="flex items-center gap-3 px-3 py-2">
                          <code className="shrink-0 text-xs font-medium">
                            {[platform.os, platform.architecture, platform.variant].filter(Boolean).join('/')}
                          </code>
                          <span className="min-w-0 flex-1">
                            <Digest value={platform.digest} />
                          </span>
                          <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{platform.size_human}</span>
                        </li>
                      ))}
                    </ul>
                  </Block>
                )}

                {historyIsAttributed(details.history) ? (
                  <Block title={t('imageDetails.history')}>
                    <p className="-mt-1 text-xs text-muted-foreground text-pretty">{t('imageDetails.historyBody')}</p>
                    <BuildSteps history={details.history!} onBrowse={setBrowsingLayer} />
                  </Block>
                ) : (
                <Block title={t('imageDetails.layersCount', { count: details.layers.length })}>
                  {details.history?.length && details.layers.length > 0 ? (
                    <p className="-mt-1 text-xs text-muted-foreground text-pretty">{t('imageDetails.historyUnmatched')}</p>
                  ) : null}
                  {details.layers.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t('imageDetails.noLayers')}</p>
                  ) : (
                    <ol className="divide-y rounded-lg border">
                      {details.layers.map((layer, index) => (
                        <li key={layer.digest + index} className="flex items-center gap-3 py-1.5 pr-1.5 pl-3">
                          <span className="w-5 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{index + 1}</span>
                          <span className="min-w-0 flex-1">
                            <Digest value={layer.digest} />
                          </span>
                          <span className="shrink-0 text-xs tabular-nums">{layer.size_human}</span>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            aria-label={t('imageDetails.browseLayer')}
                            title={t('imageDetails.browseLayer')}
                            onClick={() => setBrowsingLayer(layer.digest)}
                            className="text-muted-foreground hover:text-foreground"
                          >
                            <FolderOpen />
                          </Button>
                        </li>
                      ))}
                    </ol>
                  )}
                </Block>
                )}
              </>
            ) : null}
          </div>
        </SheetContent>
      </Sheet>

      {browsingLayer && <LayerBrowser imageName={imageName} digest={browsingLayer} onClose={() => setBrowsingLayer(null)} />}
    </>
  )
}
