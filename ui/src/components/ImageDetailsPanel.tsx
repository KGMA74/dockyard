import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Layers as LayersIcon, FolderOpen, ShieldAlert } from 'lucide-react'
import { getManifestDetails, listScans, triggerScan, ManifestDetails, ScanResult, TagInfo } from '../api'
import LayerBrowser from './LayerBrowser'
import { ScanStatusBadge, SeverityBadge } from './ScanBadges'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'

interface Props {
  imageName: string
  tag: TagInfo
  onClose: () => void
}

export default function ImageDetailsPanel({ imageName, tag, onClose }: Props) {
  const [details, setDetails] = useState<ManifestDetails | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [browsingLayer, setBrowsingLayer] = useState<string | null>(null)
  // undefined = still loading / feature unavailable (403 in non-admin roles,
  // or SCAN_ENABLED=false) — the whole section stays hidden in that case.
  const [scan, setScan] = useState<ScanResult | null | undefined>(undefined)
  const [triggering, setTriggering] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    getManifestDetails(imageName, tag.digest)
      .then(d => { if (!cancelled) setDetails(d) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load details') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [imageName, tag.digest])

  useEffect(() => {
    let cancelled = false
    listScans({ name: imageName, digest: tag.digest, limit: 1 })
      .then(r => { if (!cancelled) setScan(r.scans[0] ?? null) })
      .catch(() => { if (!cancelled) setScan(undefined) })
    return () => { cancelled = true }
  }, [imageName, tag.digest])

  // Poll while a scan is in flight — the backend docs recommend polling
  // GET /api/admin/scans/:id rather than wiring SSE for this.
  useEffect(() => {
    if (!scan || (scan.status !== 'queued' && scan.status !== 'running')) return
    const id = setInterval(() => {
      listScans({ name: imageName, digest: tag.digest, limit: 1 })
        .then(r => setScan(r.scans[0] ?? null))
        .catch(() => {})
    }, 2000)
    return () => clearInterval(id)
  }, [scan, imageName, tag.digest])

  async function handleScan() {
    setTriggering(true)
    try {
      const result = await triggerScan(imageName, tag.digest)
      setScan(result.scan)
      if (result.cached) toast.info('Reusing a recent scan for this image')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to start scan')
    } finally {
      setTriggering(false)
    }
  }

  return (
    <>
      <Sheet open onOpenChange={open => { if (!open) onClose() }}>
        <SheetContent side="right" className="overflow-y-auto gap-0">
          <SheetHeader className="border-b">
            <SheetTitle className="font-mono text-sm truncate pr-8">{imageName}</SheetTitle>
            <SheetDescription asChild>
              <span>
                <span className="inline-block mt-1 font-mono text-xs text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/30 px-2 py-0.5 rounded-md border border-blue-200 dark:border-blue-900/30">
                  {tag.tag}
                </span>
              </span>
            </SheetDescription>
          </SheetHeader>

          <div className="p-4 space-y-5">
            {loading ? (
              <p className="text-sm text-muted-foreground py-10 text-center">Loading…</p>
            ) : error ? (
              <p className="text-sm text-destructive py-10 text-center">{error}</p>
            ) : details ? (
              <>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Total size" value={details.total_size_human} />
                  <Field
                    label={details.platforms ? 'Unique layers' : 'Layers'}
                    value={String(details.layers.length)}
                  />
                  {details.os && <Field label="OS" value={details.os} />}
                  {details.architecture && <Field label="Architecture" value={details.architecture} />}
                  {details.created && (
                    <Field label="Created" value={new Date(details.created).toLocaleString()} wide />
                  )}
                </div>

                <div>
                  <p className="text-xs text-muted-foreground font-medium mb-2 flex items-center gap-1.5">
                    Digest
                    {details.signed !== undefined && (
                      <Badge
                        variant="outline"
                        className={
                          details.signed
                            ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10'
                            : 'text-muted-foreground'
                        }
                      >
                        {details.signed ? 'Signed' : 'Unsigned'}
                      </Badge>
                    )}
                  </p>
                  <p className="font-mono text-xs text-muted-foreground bg-muted/50 border rounded-lg px-3 py-2 break-all">
                    {details.digest}
                  </p>
                </div>

                {scan !== undefined && (
                  <div>
                    <p className="text-xs text-muted-foreground font-medium mb-2 flex items-center gap-1.5">
                      <ShieldAlert className="size-3.5" />
                      Vulnerability scan
                    </p>
                    {scan === null ? (
                      <Button variant="outline" size="sm" onClick={handleScan} disabled={triggering}>
                        <ShieldAlert />
                        {triggering ? 'Starting…' : 'Scan for vulnerabilities'}
                      </Button>
                    ) : (
                      <div className="bg-muted/50 border rounded-lg px-3 py-2 space-y-2">
                        <div className="flex items-center justify-between gap-2">
                          <ScanStatusBadge status={scan.status} />
                          {(scan.status === 'succeeded' || scan.status === 'failed') && (
                            <Button variant="outline" size="sm" onClick={handleScan} disabled={triggering}>
                              {triggering ? 'Starting…' : 'Re-scan'}
                            </Button>
                          )}
                        </div>
                        {scan.status === 'succeeded' && (
                          <div className="flex items-center gap-1.5 flex-wrap">
                            <SeverityBadge label="Critical" count={scan.critical_count} tone="critical" />
                            <SeverityBadge label="High" count={scan.high_count} tone="high" />
                            <SeverityBadge label="Medium" count={scan.medium_count} tone="medium" />
                            <SeverityBadge label="Low" count={scan.low_count} tone="low" />
                          </div>
                        )}
                        {scan.status === 'failed' && scan.error && (
                          <p className="text-xs text-destructive">{scan.error}</p>
                        )}
                      </div>
                    )}
                  </div>
                )}

                {details.platforms && details.platforms.length > 0 && (
                  <div>
                    <p className="text-xs text-muted-foreground font-medium mb-2">
                      Platforms ({details.platforms.length})
                    </p>
                    <div className="space-y-1.5">
                      {details.platforms.map(p => (
                        <div
                          key={p.digest}
                          className="flex items-center justify-between gap-3 bg-muted/50 border rounded-lg px-3 py-2"
                        >
                          <Badge variant="secondary" className="font-mono">
                            {p.os}/{p.architecture}
                          </Badge>
                          <span className="text-xs shrink-0 tabular-nums text-muted-foreground">{p.size_human}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div>
                  <p className="text-xs text-muted-foreground font-medium mb-2 flex items-center gap-1.5">
                    <LayersIcon className="size-3.5" />
                    Layers ({details.layers.length})
                  </p>
                  {details.layers.length === 0 ? (
                    <p className="text-xs text-muted-foreground/70">No layers to show for this manifest.</p>
                  ) : (
                    <div className="space-y-1.5">
                      {details.layers.map((layer, i) => (
                        <div
                          key={layer.digest + i}
                          className="flex items-center justify-between gap-3 bg-muted/50 border rounded-lg px-3 py-2"
                        >
                          <span className="font-mono text-xs text-muted-foreground truncate">
                            {layer.digest.slice(0, 19)}…
                          </span>
                          <div className="flex items-center gap-2 shrink-0">
                            <span className="text-xs tabular-nums">{layer.size_human}</span>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              title="Browse layer contents"
                              onClick={() => setBrowsingLayer(layer.digest)}
                              className="text-muted-foreground/60 hover:text-blue-500 dark:hover:text-blue-400"
                            >
                              <FolderOpen strokeWidth={1.5} />
                            </Button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </>
            ) : null}
          </div>
        </SheetContent>
      </Sheet>

      {browsingLayer && (
        <LayerBrowser
          imageName={imageName}
          digest={browsingLayer}
          onClose={() => setBrowsingLayer(null)}
        />
      )}
    </>
  )
}

function Field({ label, value, wide }: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={wide ? 'col-span-2' : undefined}>
      <p className="text-xs text-muted-foreground font-medium">{label}</p>
      <p className="text-sm mt-0.5">{value}</p>
    </div>
  )
}
