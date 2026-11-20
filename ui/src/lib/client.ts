/** What kind of client a session belongs to — picks its icon. */
export function clientKind(userAgent: string): 'cli' | 'browser' {
  return /dockyard-cli|^docker\/|curl\//i.test(userAgent) ? 'cli' : 'browser'
}

/** "Mozilla/5.0 (…) Chrome/126…" → something a human can scan in a table. */
export function describeClient(userAgent: string): string {
  if (!userAgent) return '—'
  if (/dockyard-cli/i.test(userAgent)) return 'dockyard-cli'
  if (/^docker\//i.test(userAgent)) return 'Docker'
  if (/curl\//i.test(userAgent)) return 'curl'
  const browser = /Edg\//.test(userAgent)
    ? 'Edge'
    : /Firefox\//.test(userAgent)
      ? 'Firefox'
      : /Chrome\//.test(userAgent)
        ? 'Chrome'
        : /Safari\//.test(userAgent)
          ? 'Safari'
          : ''
  const os = /Windows/.test(userAgent)
    ? 'Windows'
    : /Mac OS X|Macintosh/.test(userAgent)
      ? 'macOS'
      : /Android/.test(userAgent)
        ? 'Android'
        : /iPhone|iPad/.test(userAgent)
          ? 'iOS'
          : /Linux/.test(userAgent)
            ? 'Linux'
            : ''
  return [browser, os].filter(Boolean).join(' · ') || userAgent.slice(0, 40)
}
