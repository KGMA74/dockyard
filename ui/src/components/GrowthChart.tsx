import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { StatsSample } from '../api'
import { formatBytes, formatDate } from '@/lib/format'

const WIDTH = 600
const HEIGHT = 140
const PAD_Y = 12

interface Props {
  samples: StatsSample[]
}

// Single-series area chart for storage growth. Index-spaced on x (samples
// land roughly every 6h so this reads the same as a time scale), one hue,
// hover crosshair — the minimum a "trend" claim needs to actually show one.
export default function GrowthChart({ samples }: Props) {
  const { t } = useTranslation()
  const gradientId = useId()
  const [hover, setHover] = useState<number | null>(null)

  const values = samples.map(s => s.total_size)
  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = max - min || 1

  const points = samples.map((s, i) => {
    const x = samples.length > 1 ? (i / (samples.length - 1)) * WIDTH : WIDTH / 2
    const y = PAD_Y + (1 - (s.total_size - min) / span) * (HEIGHT - PAD_Y * 2)
    return { x, y, sample: s }
  })

  const linePath = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x},${p.y}`).join(' ')
  const areaPath = `${linePath} L${WIDTH},${HEIGHT} L0,${HEIGHT} Z`

  const first = samples[0]
  const last = samples[samples.length - 1]
  const delta = last.total_size - first.total_size
  const trendUp = delta >= 0

  const active = hover !== null ? points[hover] : null
  // The detail row always shows a sample — the hovered one, else the latest —
  // so the card keeps the same height whether or not the pointer is over it.
  const shown = active ?? points[points.length - 1]

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = (e.clientX - rect.left) / rect.width
    const idx = Math.round(ratio * (points.length - 1))
    setHover(Math.max(0, Math.min(points.length - 1, idx)))
  }

  return (
    <div>
      <div className="flex items-baseline justify-between mb-1">
        <p className="text-lg font-semibold tabular-nums">{formatBytes(last.total_size)}</p>
        <p className="text-xs text-muted-foreground tabular-nums">
          {trendUp ? '+' : '−'}{formatBytes(Math.abs(delta))} {t('insights.sinceDate', { date: formatDate(first.at) })}
        </p>
      </div>

      <div className="relative">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        preserveAspectRatio="none"
        className="block w-full h-28 overflow-visible"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" className="text-primary" stopColor="currentColor" stopOpacity="0.25" />
            <stop offset="100%" className="text-primary" stopColor="currentColor" stopOpacity="0" />
          </linearGradient>
        </defs>

        <path d={areaPath} fill={`url(#${gradientId})`} />
        <path
          d={linePath}
          className="text-primary"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
        />

        {active && (
          <line
            x1={active.x} x2={active.x} y1={PAD_Y} y2={HEIGHT}
            className="text-border" stroke="currentColor" strokeWidth="1" vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      {/* The SVG is stretched (preserveAspectRatio="none"), which would squash
          an SVG circle into an ellipse; an HTML dot stays round. */}
      {active && (
        <span
          aria-hidden
          className="pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary ring-2 ring-card"
          style={{ left: `${(active.x / WIDTH) * 100}%`, top: `${(active.y / HEIGHT) * 100}%` }}
        />
      )}
      </div>

      <div
        className={`mt-1 flex items-center justify-between text-xs tabular-nums ${active ? 'text-foreground' : 'text-muted-foreground'}`}
      >
        <span>{formatDate(shown.sample.at)}</span>
        <span>{formatBytes(shown.sample.total_size)} · {t('insights.repoCount', { count: shown.sample.repo_count })}</span>
      </div>
    </div>
  )
}
