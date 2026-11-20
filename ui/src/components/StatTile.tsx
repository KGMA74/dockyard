import type { LucideIcon } from 'lucide-react'
import Sparkline from './Sparkline'
import { Skeleton } from '@/components/ui/skeleton'

interface Props {
  icon: LucideIcon
  label: string
  /** Undefined while loading. */
  value: string | undefined
  hint?: React.ReactNode
  /** Recent history, drawn as a small trend line. */
  trend?: number[]
}

export default function StatTile({ icon: Icon, label, value, hint, trend }: Props) {
  return (
    <div className="relative overflow-hidden rounded-xl border bg-card p-4">
      <div className="flex items-center gap-1.5 text-muted-foreground">
        <Icon className="size-3.5" strokeWidth={1.5} />
        <p className="text-xs font-medium">{label}</p>
      </div>
      {value === undefined ? (
        <Skeleton className="mt-2 h-7 w-20" />
      ) : (
        <p className="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{value}</p>
      )}
      {hint && <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>}
      {trend && trend.length >= 2 && (
        <Sparkline values={trend} className="absolute right-3 bottom-3 h-6 w-16 text-primary opacity-70" />
      )}
    </div>
  )
}
