import { useState } from 'react'
import { TriangleAlert } from 'lucide-react'
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
  resourceName: string
  description: React.ReactNode
  detail?: string
  confirmLabel?: string
  onConfirm: () => Promise<void>
  onCancel: () => void
}

export default function DeleteModal({
  title,
  resourceName,
  description,
  detail,
  confirmLabel = 'Delete',
  onConfirm,
  onCancel,
}: Props) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [confirmText, setConfirmText] = useState('')

  const matches = confirmText === resourceName

  async function handleConfirm() {
    if (!matches) return
    setLoading(true)
    setError('')
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
      setLoading(false)
    }
  }

  return (
    <Dialog open onOpenChange={open => { if (!open) onCancel() }}>
      <DialogContent>
        <DialogHeader>
          <div className="flex items-start gap-3">
            <div className="w-8 h-8 rounded-lg bg-destructive/10 border border-destructive/20 flex items-center justify-center shrink-0 mt-0.5">
              <TriangleAlert className="size-4 text-destructive" />
            </div>
            <div className="space-y-1.5">
              <DialogTitle className="text-sm">{title}</DialogTitle>
              <DialogDescription>{description}</DialogDescription>
            </div>
          </div>
        </DialogHeader>

        {detail && (
          <p className="font-mono text-xs text-muted-foreground/60 truncate px-1" title={detail}>
            {detail}
          </p>
        )}

        <label className="block">
          <span className="text-xs text-muted-foreground">
            Type <span className="font-mono text-foreground">{resourceName}</span> to confirm
          </span>
          <Input
            type="text"
            value={confirmText}
            onChange={e => setConfirmText(e.target.value)}
            autoFocus
            autoComplete="off"
            spellCheck={false}
            className="mt-1.5 font-mono text-sm"
            placeholder={resourceName}
          />
        </label>

        {error && (
          <p className="text-xs text-destructive">{error}</p>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            Cancel
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={loading || !matches}
            className="bg-destructive text-white hover:bg-destructive/90"
          >
            {loading ? 'Deleting…' : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
