import { useId, useState } from 'react'
import { TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { errorMessage, Spinner } from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface Props {
  title: string
  /** Exact text the user must type to arm the button. */
  resourceName: string
  description: React.ReactNode
  /** A consequence that is easy to miss (e.g. other tags removed as well). */
  warning?: React.ReactNode
  detail?: string
  confirmLabel?: string
  onConfirm: () => Promise<void>
  onCancel: () => void
}

/** Type-to-confirm dialog for deletions that cannot be undone. */
export default function DeleteModal({
  title,
  resourceName,
  description,
  warning,
  detail,
  confirmLabel,
  onConfirm,
  onCancel,
}: Props) {
  const { t } = useTranslation()
  const inputId = useId()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [confirmText, setConfirmText] = useState('')

  const matches = confirmText.trim() === resourceName

  async function handleConfirm(event: React.FormEvent) {
    event.preventDefault()
    if (!matches || loading) return
    setLoading(true)
    setError('')
    try {
      await onConfirm()
    } catch (err) {
      setError(errorMessage(err, t('deleteModal.failed')))
      setLoading(false)
    }
  }

  return (
    <Dialog open onOpenChange={open => !open && !loading && onCancel()}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleConfirm} className="grid gap-4">
          <DialogHeader>
            <div className="flex items-start gap-3">
              <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border border-destructive/20 bg-destructive/10">
                <TriangleAlert className="size-4 text-destructive" />
              </div>
              <div className="min-w-0 space-y-1.5">
                <DialogTitle className="text-sm">{title}</DialogTitle>
                <DialogDescription className="text-pretty">{description}</DialogDescription>
              </div>
            </div>
          </DialogHeader>

          {warning && (
            <p className="rounded-lg border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-pretty">{warning}</p>
          )}

          {detail && (
            <p className="truncate px-1 font-mono text-xs text-muted-foreground" title={detail}>
              {detail}
            </p>
          )}

          <div className="space-y-1.5">
            <label htmlFor={inputId} className="block text-xs text-muted-foreground">
              {t('deleteModal.typePrefix')} <span className="font-mono font-medium text-foreground select-all">{resourceName}</span>{' '}
              {t('deleteModal.typeSuffix')}
            </label>
            <Input
              id={inputId}
              type="text"
              value={confirmText}
              onChange={e => setConfirmText(e.target.value)}
              autoFocus
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              className="font-mono"
            />
          </div>

          {error && (
            <p role="alert" className="text-xs text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={loading || !matches} className="bg-destructive text-white hover:bg-destructive/90">
              {loading && <Spinner />}
              {confirmLabel ?? t('common.delete')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
