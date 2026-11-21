import { useState } from 'react'
import { Download, FileJson, FileSpreadsheet, FileText, LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { downloadScanReport, type ReportFormat } from '@/api'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

const FORMATS: { format: ReportFormat; icon: typeof FileText }[] = [
  { format: 'pdf', icon: FileText },
  { format: 'csv', icon: FileSpreadsheet },
  { format: 'json', icon: FileJson },
]

/** Download a scan's report: PDF to share, CSV for a spreadsheet, or the raw
 *  Trivy JSON for tooling. */
export default function ReportDownloadMenu({ scanId }: { scanId: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState<ReportFormat | null>(null)

  async function download(format: ReportFormat) {
    setBusy(format)
    try {
      await downloadScanReport(scanId, format)
      setOpen(false)
    } catch (err) {
      toast.error(
        t('vulns.downloadFailed', { message: err instanceof Error ? err.message : String(err) }),
      )
    } finally {
      setBusy(null)
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <Download />
          {t('vulns.download')}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 p-1">
        {FORMATS.map(({ format, icon: Icon }) => (
          <button
            key={format}
            onClick={() => download(format)}
            disabled={busy !== null}
            className="flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
          >
            {busy === format ? (
              <LoaderCircle className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
            ) : (
              <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            )}
            <span className="min-w-0">
              <span className="block text-sm font-medium">{format.toUpperCase()}</span>
              <span className="block text-xs text-muted-foreground">
                {t(`vulns.format_${format}`)}
              </span>
            </span>
          </button>
        ))}
      </PopoverContent>
    </Popover>
  )
}
