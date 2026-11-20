import { cn } from '@/lib/utils'

/**
 * The Dockyard mark: a "D" whose stem is a stack of three layers — image
 * layers, or containers stacked on a quay — closed by a single stroke. The
 * newest layer sits on top, lightest. The stack takes the brand colour; the
 * stroke follows the text colour, so the mark works on any background.
 */
export function DockyardMark({ className, title }: { className?: string; title?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={cn('shrink-0', className)}
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : true}
    >
      {title && <title>{title}</title>}
      <rect x="4" y="4.5" width="7.5" height="6.5" rx="1.75" className="fill-primary" opacity="0.5" />
      <rect x="4" y="12.75" width="7.5" height="6.5" rx="1.75" className="fill-primary" opacity="0.75" />
      <rect x="4" y="21" width="7.5" height="6.5" rx="1.75" className="fill-primary" />
      <path d="M14.75 6.75h1.75a9.25 9.25 0 0 1 0 18.5h-1.75" stroke="currentColor" strokeWidth="4" strokeLinecap="round" />
    </svg>
  )
}

/** Mark and name, for headers. */
export function DockyardLogo({ className, markClassName }: { className?: string; markClassName?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2 font-semibold tracking-tight', className)}>
      <DockyardMark className={cn('size-5', markClassName)} />
      Dockyard
    </span>
  )
}
