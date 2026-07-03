import { useEffect, useState } from 'react'
import { Layers as LayersIcon } from 'lucide-react'
import { getManifestDetails, ManifestDetails, TagInfo } from '../api'
import { Badge } from '@/components/ui/badge'
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

  return (
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
                <p className="text-xs text-muted-foreground font-medium mb-2">Digest</p>
                <p className="font-mono text-xs text-muted-foreground bg-muted/50 border rounded-lg px-3 py-2 break-all">
                  {details.digest}
                </p>
              </div>

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
                        <span className="text-xs shrink-0 tabular-nums">{layer.size_human}</span>
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
