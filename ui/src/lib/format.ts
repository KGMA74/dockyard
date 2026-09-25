import i18n from '../i18n'

// Formatting helpers that follow the active UI language. Previously each
// component hard-coded `Intl.*Format("en")` or `toLocaleString()` (browser
// locale), so dates showed up in a mix of languages.

function locale(): string {
  return i18n.resolvedLanguage ?? 'en'
}

const RELATIVE_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 31536000],
  ['month', 2592000],
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
]

export function relativeTime(iso: string): string {
  const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' })
  const seconds = (Date.parse(iso) - Date.now()) / 1000
  for (const [unit, secondsInUnit] of RELATIVE_UNITS) {
    if (Math.abs(seconds) >= secondsInUnit) {
      return rtf.format(Math.round(seconds / secondsInUnit), unit)
    }
  }
  return rtf.format(Math.round(seconds), 'second')
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString(locale())
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(locale())
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  let value = n
  let i = 0
  while (value >= 1024 && i < BYTE_UNITS.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(i === 0 ? 0 : value >= 10 ? 1 : 2)} ${BYTE_UNITS[i]}`
}

// shortDigest turns "sha256:1a2b3c…" into "1a2b3c…9f8e7d" for compact display.
export function shortDigest(digest: string, head = 7, tail = 6): string {
  const hex = digest.startsWith('sha256:') ? digest.slice(7) : digest
  if (hex.length <= head + tail) return hex
  return `${hex.slice(0, head)}…${hex.slice(-tail)}`
}
