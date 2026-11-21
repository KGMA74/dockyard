import { useState } from 'react'
import { ScanSearch, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { registryHost, triggerScan, type TagInfo } from '@/api'
import { keys } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { CopyButton, errorMessage, Spinner } from './kit'
import { CompactScan } from './ScanBadges'
import VulnerabilityReport from './VulnerabilityReport'
import { Button } from '@/components/ui/button'

/** Starts scans and refreshes what shows their results. */
function useStartScans(repo: string) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  return async (digests: string[]) => {
    let queued = 0
    let reused = 0
    for (const digest of digests) {
      const result = await triggerScan(repo, digest)
      if (result.cached) reused++
      else queued++
    }
    void queryClient.invalidateQueries({ queryKey: ['scans'] })
    void queryClient.invalidateQueries({ queryKey: keys.tags(repo) })
    if (queued > 0) toast.success(t('repository.scansQueued', { count: queued }))
    if (reused > 0) toast.info(t('repository.scansReused', { count: reused }))
  }
}

/** A tag's vulnerability cell: the counts open the full report in one click;
 *  a tag never scanned gets a Scan button (admins, when scanning is on). */
export function TagScanCell({
  repo,
  tag,
  canScan,
}: {
  repo: string
  tag: TagInfo
  canScan: boolean
}) {
  const { t } = useTranslation()
  const startScans = useStartScans(repo)
  const [starting, setStarting] = useState(false)
  const [showReport, setShowReport] = useState(false)
  const scan = tag.scan

  if (!scan && canScan) {
    return (
      <Button
        variant="ghost"
        size="xs"
        disabled={starting}
        onClick={async () => {
          setStarting(true)
          try {
            await startScans([tag.digest])
          } catch (err) {
            toast.error(errorMessage(err, t('imageDetails.scanStartFailed')))
          } finally {
            setStarting(false)
          }
        }}
        className="-ml-2 text-muted-foreground"
        aria-label={t('repository.scanTag', { tag: tag.tag })}
      >
        {starting ? <Spinner className="size-3" /> : <ScanSearch />}
        {t('repository.scan')}
      </Button>
    )
  }
  if (scan?.status === 'succeeded' && scan.id) {
    return (
      <>
        <button
          type="button"
          onClick={() => setShowReport(true)}
          title={t('repository.openReport')}
          aria-label={t('repository.openReportOf', { tag: tag.tag })}
          className="-m-1 rounded-md p-1 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
        >
          <CompactScan scan={scan} blocked={tag.pull_blocked} />
        </button>
        {showReport && (
          <VulnerabilityReport
            scan={{ id: scan.id, trivy_version: scan.trivy_version ?? '' }}
            image={`${repo}:${tag.tag}`}
            onClose={() => setShowReport(false)}
          />
        )}
      </>
    )
  }
  return <CompactScan scan={scan} blocked={tag.pull_blocked} />
}

/** Scans every tag never scanned yet, one request per distinct image. */
export function ScanUnscannedButton({ repo, tags }: { repo: string; tags: TagInfo[] }) {
  const { t } = useTranslation()
  const startScans = useStartScans(repo)
  const [running, setRunning] = useState(false)
  const digests = [...new Set(tags.filter(tag => !tag.scan).map(tag => tag.digest))]
  if (digests.length === 0) return null
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={running}
      onClick={async () => {
        setRunning(true)
        try {
          await startScans(digests)
        } catch (err) {
          toast.error(errorMessage(err, t('imageDetails.scanStartFailed')))
        } finally {
          setRunning(false)
        }
      }}
    >
      {running ? <Spinner /> : <ShieldAlert />}
      {t('repository.scanUnscanned', { count: digests.length })}
    </Button>
  )
}

type PullTool = 'docker' | 'podman' | 'kubernetes'
const PULL_TOOLS: PullTool[] = ['docker', 'podman', 'kubernetes']

/** How to pull this image, for the tool at hand. Kubernetes needs the
 *  registry credentials as an image pull secret: the snippet shows both. */
export function PullCommands({ repo, tag }: { repo: string; tag: string }) {
  const { t } = useTranslation()
  const [tool, setTool] = useState<PullTool>('docker')
  const host = registryHost()
  const image = `${host}/${repo}:${tag}`
  const snippet =
    tool === 'kubernetes'
      ? [
          `kubectl create secret docker-registry dockyard-pull \\`,
          `  --docker-server=${host} \\`,
          `  --docker-username=robot+<name> --docker-password=<secret>`,
          ``,
          `# in the pod spec`,
          `imagePullSecrets:`,
          `  - name: dockyard-pull`,
          `containers:`,
          `  - name: app`,
          `    image: ${image}`,
        ].join('\n')
      : `${tool} pull ${image}`

  return (
    <div className="max-w-2xl space-y-1.5">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs font-medium text-muted-foreground">{t('repository.pull')}</p>
        <div
          role="tablist"
          aria-label={t('repository.pullTool')}
          className="flex gap-0.5 rounded-md border p-0.5"
        >
          {PULL_TOOLS.map(name => (
            <button
              key={name}
              type="button"
              role="tab"
              aria-selected={tool === name}
              onClick={() => setTool(name)}
              className={cn(
                'rounded px-2 py-0.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring',
                tool === name
                  ? 'bg-foreground text-background'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {t(`repository.tool_${name}`)}
            </button>
          ))}
        </div>
      </div>
      <div className="flex items-start gap-2 rounded-lg border bg-muted/50 py-1 pr-1 pl-3">
        <pre className="min-w-0 flex-1 overflow-x-auto py-1 text-xs [scrollbar-width:none]">
          {tool !== 'kubernetes' && (
            <span className="select-none text-muted-foreground/70" aria-hidden="true">
              ${' '}
            </span>
          )}
          {snippet}
        </pre>
        <CopyButton value={snippet} />
      </div>
      {tool === 'kubernetes' && (
        <p className="text-xs text-muted-foreground">{t('repository.kubernetesHint')}</p>
      )}
    </div>
  )
}
