const WIDTH = 100
const HEIGHT = 28

interface Props {
  values: number[]
  className?: string
}

// Decorative trend line for a stat tile — no axes, no hover. The detailed,
// interactive version of the same series lives in GrowthChart below it.
export default function Sparkline({ values, className }: Props) {
  if (values.length < 2) return null

  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = max - min || 1

  const points = values.map((v, i) => {
    const x = (i / (values.length - 1)) * WIDTH
    const y = HEIGHT - ((v - min) / span) * HEIGHT
    return `${x},${y}`
  })

  return (
    <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} preserveAspectRatio="none" className={className} aria-hidden="true">
      <polyline
        points={points.join(' ')}
        fill="none"
        className="text-primary"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
