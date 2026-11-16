// Shared building blocks for every page of the console. Before this file each
// screen re-typed its own section header, status colours, copy button and
// "Loading…" paragraph — which is why no two of them quite matched.

import { useId, useState } from 'react'
import { AlertCircle, Check, Copy, Loader2, RefreshCw, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ApiError } from '@/api'
import { formatDateTime, relativeTime, shortDigest } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

// ── Layout ───────────────────────────────────────────────────────────────────

/** Constrains and pads a page's content. Every page renders exactly one. */
export function Page({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('mx-auto w-full max-w-6xl space-y-8 px-4 py-6 sm:px-6', className)}>{children}</div>
}

export function PageHeader({
  title,
  description,
  actions,
  eyebrow,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  eyebrow?: React.ReactNode
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="min-w-0 space-y-1">
        {eyebrow && <div className="text-xs text-muted-foreground">{eyebrow}</div>}
        <h1 className="text-xl font-semibold tracking-tight text-balance">{title}</h1>
        {description && <p className="max-w-2xl text-sm text-muted-foreground text-pretty">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}

/** A titled block within a page. `id` makes it linkable (#retention…). */
export function Section({
  title,
  description,
  actions,
  children,
  id,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  children: React.ReactNode
  id?: string
}) {
  return (
    <section id={id} className="scroll-mt-16 space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
        <div className="min-w-0 space-y-1">
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="max-w-3xl text-sm text-muted-foreground text-pretty">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  )
}

/** The bordered surface tables and lists sit on. */
export function Panel({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('overflow-hidden rounded-xl border bg-card text-card-foreground', className)}>{children}</div>
}

// ── Status ───────────────────────────────────────────────────────────────────

export type Tone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'

const TONE_CLASS: Record<Tone, string> = {
  neutral: 'border-border bg-muted/60 text-muted-foreground',
  info: 'border-primary/25 bg-primary/10 text-primary',
  success: 'border-success/30 bg-success/10 text-success',
  warning: 'border-warning/35 bg-warning/10 text-warning',
  danger: 'border-destructive/30 bg-destructive/10 text-destructive',
}

/** The one badge used for every state in the app, so "green" always means the
 *  same green. Colour is never the only signal: the label carries the state. */
export function StatusBadge({
  tone = 'neutral',
  children,
  className,
  mono,
}: {
  tone?: Tone
  children: React.ReactNode
  className?: string
  mono?: boolean
}) {
  return (
    <Badge variant="outline" className={cn(TONE_CLASS[tone], mono && 'font-mono', className)}>
      {children}
    </Badge>
  )
}

export function TagBadge({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <StatusBadge tone="info" mono className={cn('max-w-full', className)}>
      <span className="truncate">{children}</span>
    </StatusBadge>
  )
}

const ROLE_TONE: Record<string, Tone> = { admin: 'info', pusher: 'warning', reader: 'neutral' }

export function RoleBadge({ role }: { role: string }) {
  const { t } = useTranslation()
  return <StatusBadge tone={ROLE_TONE[role] ?? 'neutral'}>{t(`roles.${role}`, role)}</StatusBadge>
}

// ── Copy ─────────────────────────────────────────────────────────────────────

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // Clipboard API needs a secure context; a registry reached over plain
    // HTTP by IP has none. Fall back to the legacy path.
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    area.remove()
    return ok
  }
}

export function useCopy(): { copied: boolean; copy: (text: string) => void } {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  function copy(text: string) {
    void copyText(text).then(ok => {
      if (!ok) {
        toast.error(t('kit.copyFailed'))
        return
      }
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  return { copied, copy }
}

export function CopyButton({
  value,
  label,
  className,
}: {
  value: string
  /** What is being copied, for the tooltip and screen readers. */
  label?: string
  className?: string
}) {
  const { t } = useTranslation()
  const { copied, copy } = useCopy()
  const text = copied ? t('kit.copied') : (label ?? t('kit.copy'))
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={text}
          onClick={event => {
            event.stopPropagation()
            copy(value)
          }}
          className={cn('text-muted-foreground hover:text-foreground', className)}
        >
          {copied ? <Check className="text-success" /> : <Copy />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  )
}

/** A one-line shell command with a copy button — the thing people actually
 *  come to a registry UI for. */
export function Command({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn('flex items-center gap-2 rounded-lg border bg-muted/50 py-1 pr-1 pl-3', className)}>
      <span className="select-none font-mono text-xs text-muted-foreground/70" aria-hidden="true">
        $
      </span>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap py-1 text-xs [scrollbar-width:none]">
        {children}
      </code>
      <CopyButton value={children} />
    </div>
  )
}

/** A digest shortened for display, full value on hover, one click to copy. */
export function Digest({ value, className }: { value: string; className?: string }) {
  const { t } = useTranslation()
  if (!value) return <span className="text-muted-foreground">—</span>
  return (
    <span className={cn('inline-flex items-center gap-0.5', className)}>
      <Tooltip>
        <TooltipTrigger asChild>
          <code className="text-xs text-muted-foreground">{shortDigest(value)}</code>
        </TooltipTrigger>
        <TooltipContent className="font-mono">{value}</TooltipContent>
      </Tooltip>
      <CopyButton value={value} label={t('kit.copyDigest')} />
    </span>
  )
}

/** Relative time with the absolute timestamp on hover. */
export function Timestamp({ value, className }: { value?: string | null; className?: string }) {
  if (!value) return <span className={cn('text-muted-foreground', className)}>—</span>
  // SQLite timestamps come back without a zone; they are UTC.
  const iso = /[zZ]|[+-]\d\d:?\d\d$/.test(value) ? value : value + 'Z'
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={iso} className={cn('whitespace-nowrap', className)}>
          {relativeTime(iso)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatDateTime(iso)}</TooltipContent>
    </Tooltip>
  )
}

// ── Empty / loading / error ──────────────────────────────────────────────────

export function EmptyState({
  icon: Icon,
  title,
  description,
  children,
  className,
}: {
  icon?: React.ComponentType<{ className?: string; strokeWidth?: number }>
  title: React.ReactNode
  description?: React.ReactNode
  children?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-col items-center gap-3 px-6 py-12 text-center', className)}>
      {Icon && (
        <div className="flex size-10 items-center justify-center rounded-xl border bg-muted/50">
          <Icon className="size-5 text-muted-foreground" strokeWidth={1.5} />
        </div>
      )}
      <div className="space-y-1">
        <p className="text-sm font-medium">{title}</p>
        {description && <p className="mx-auto max-w-md text-sm text-muted-foreground text-pretty">{description}</p>}
      </div>
      {children}
    </div>
  )
}

export function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

/** True for "this feature is off / not yours" responses, as opposed to a
 *  real failure: the section explains itself instead of showing an error. */
export function isUnavailable(error: unknown): boolean {
  return error instanceof ApiError && [403, 404, 501].includes(error.status)
}

export function ErrorState({
  error,
  onRetry,
  className,
}: {
  error: unknown
  onRetry?: () => void
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <div role="alert" className={cn('flex flex-col items-center gap-3 px-6 py-10 text-center', className)}>
      <AlertCircle className="size-6 text-destructive" strokeWidth={1.5} />
      <div className="space-y-1">
        <p className="text-sm font-medium">{t('kit.loadFailed')}</p>
        <p className="mx-auto max-w-md text-sm text-muted-foreground">{errorMessage(error, t('kit.unknownError'))}</p>
      </div>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCw />
          {t('kit.retry')}
        </Button>
      )}
    </div>
  )
}

/** Explains why a whole feature is absent (disabled by config, proxy mode…). */
export function Unavailable({ title, children }: { title: React.ReactNode; children?: React.ReactNode }) {
  return (
    <Panel className="border-dashed bg-transparent">
      <div className="px-4 py-5 text-sm">
        <p className="font-medium">{title}</p>
        {children && <p className="mt-1 text-muted-foreground text-pretty">{children}</p>}
      </div>
    </Panel>
  )
}

export function TableSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="divide-y" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-4 px-4 py-3">
          <Skeleton className="h-4 w-1/3" />
          <Skeleton className="h-4 w-16" />
          <Skeleton className="ml-auto h-4 w-24" />
        </div>
      ))}
    </div>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn('size-4 animate-spin', className)} aria-hidden="true" />
}

// ── Forms ────────────────────────────────────────────────────────────────────

/** A labelled form control with optional help text. Placeholder-as-label
 *  forms are unusable once you have typed something — every field gets a
 *  real, persistent label. */
export function Field({
  label,
  hint,
  children,
  className,
  optional,
}: {
  label: string
  hint?: React.ReactNode
  /** Render prop receiving the ids to wire onto the control. */
  children: (ids: { id: string; 'aria-describedby'?: string }) => React.ReactNode
  className?: string
  optional?: boolean
}) {
  const { t } = useTranslation()
  const id = useId()
  const hintId = hint ? `${id}-hint` : undefined
  return (
    <div className={cn('space-y-1.5', className)}>
      <Label htmlFor={id} className="text-xs">
        {label}
        {optional && <span className="font-normal text-muted-foreground">{t('kit.optional')}</span>}
      </Label>
      {children({ id, 'aria-describedby': hintId })}
      {hint && (
        <p id={hintId} className="text-xs text-muted-foreground text-pretty">
          {hint}
        </p>
      )}
    </div>
  )
}

/** Native <select> styled like the other inputs. Native on purpose: it is
 *  the most accessible option list there is, and these lists are tiny. */
export function NativeSelect({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      className={cn(
        'h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50 dark:bg-input/30',
        className,
      )}
      {...props}
    />
  )
}

export function Checkbox({ className, ...props }: Omit<React.ComponentProps<'input'>, 'type'>) {
  return <input type="checkbox" className={cn('size-3.5 cursor-pointer accent-primary', className)} {...props} />
}

// ── Confirmation ─────────────────────────────────────────────────────────────

/** Replaces window.confirm(): states what will happen, names the action on
 *  the button, and keeps the dialog open with the error if the action fails. */
export function ConfirmDialog({
  title,
  description,
  confirmLabel,
  destructive,
  onConfirm,
  onCancel,
}: {
  title: string
  description: React.ReactNode
  confirmLabel: string
  destructive?: boolean
  onConfirm: () => Promise<unknown>
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function confirm() {
    setBusy(true)
    setError('')
    try {
      await onConfirm()
    } catch (err) {
      setError(errorMessage(err, t('kit.unknownError')))
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={open => !open && !busy && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <div className="flex items-start gap-3">
            {destructive && (
              <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border border-destructive/20 bg-destructive/10">
                <TriangleAlert className="size-4 text-destructive" />
              </div>
            )}
            <div className="space-y-1.5">
              <DialogTitle className="text-sm">{title}</DialogTitle>
              <DialogDescription>{description}</DialogDescription>
            </div>
          </div>
        </DialogHeader>
        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            {t('common.cancel')}
          </Button>
          <Button
            onClick={confirm}
            disabled={busy}
            className={destructive ? 'bg-destructive text-white hover:bg-destructive/90' : undefined}
          >
            {busy && <Spinner />}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Dense key/value rows (registry status, image facts). */
export function Facts({ children, className }: { children: React.ReactNode; className?: string }) {
  return <dl className={cn('divide-y text-sm', className)}>{children}</dl>
}

export function Fact({ label, children }: { label: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 px-4 py-2.5">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate text-right">{children}</dd>
    </div>
  )
}

/** A create/edit form in a dialog. Owns the busy and error state so a failed
 *  submit keeps the dialog — and what was typed — on screen, with the reason. */
export function FormDialog({
  title,
  description,
  submitLabel,
  onSubmit,
  onClose,
  children,
  className,
  canSubmit = true,
}: {
  title: string
  description?: React.ReactNode
  submitLabel: string
  onSubmit: () => Promise<unknown>
  onClose: () => void
  children: React.ReactNode
  className?: string
  canSubmit?: boolean
}) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await onSubmit()
    } catch (err) {
      setError(errorMessage(err, t('kit.unknownError')))
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={open => !open && !busy && onClose()}>
      <DialogContent className={cn('sm:max-w-md', className)}>
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle className="text-sm">{title}</DialogTitle>
            {description && <DialogDescription className="text-pretty">{description}</DialogDescription>}
          </DialogHeader>
          <div className="grid gap-3">{children}</div>
          {error && (
            <p role="alert" className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={busy || !canSubmit}>
              {busy && <Spinner />}
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Splits "a, b ,c" into trimmed, non-empty items. */
export function splitList(raw: string): string[] {
  return raw
    .split(/[,\n]/)
    .map(item => item.trim())
    .filter(Boolean)
}

/** Renders the four states every list has — loading, unavailable, failed,
 *  empty — so each section only writes its happy path. */
export function QueryPanel<T>({
  query,
  unavailable,
  isEmpty,
  empty,
  children,
  skeletonRows = 3,
}: {
  query: { isPending: boolean; isError: boolean; error: unknown; data: T | undefined; refetch: () => unknown }
  /** Shown instead of an error when the feature is off or not permitted. */
  unavailable?: { title: React.ReactNode; body?: React.ReactNode }
  isEmpty?: (data: T) => boolean
  empty?: React.ReactNode
  children: (data: T) => React.ReactNode
  skeletonRows?: number
}) {
  if (query.isPending) {
    return (
      <Panel>
        <TableSkeleton rows={skeletonRows} />
      </Panel>
    )
  }
  if (query.isError) {
    if (unavailable && isUnavailable(query.error)) {
      return <Unavailable title={unavailable.title}>{unavailable.body}</Unavailable>
    }
    return (
      <Panel>
        <ErrorState error={query.error} onRetry={() => query.refetch()} />
      </Panel>
    )
  }
  const data = query.data as T
  if (empty && isEmpty?.(data)) return <Panel>{empty}</Panel>
  return <>{children(data)}</>
}

/** Icon-only row action with an accessible name and a tooltip. */
export function RowAction({
  label,
  onClick,
  children,
  destructive,
  disabled,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
  destructive?: boolean
  disabled?: boolean
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={label}
          onClick={onClick}
          disabled={disabled}
          className={cn(
            'text-muted-foreground',
            destructive ? 'hover:bg-destructive/10 hover:text-destructive' : 'hover:text-foreground',
          )}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
