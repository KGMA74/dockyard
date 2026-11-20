import { useMemo, useSyncExternalStore } from 'react'

// A deliberately small History-API router. The console has a handful of
// pages and one parameterised route (a repository, whose name contains
// slashes) — not enough to justify a routing dependency, but more than enough
// to need real URLs: a link to an image has to be something you can paste to
// a colleague, and the back button has to work.

const listeners = new Set<() => void>()

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange)
  window.addEventListener('popstate', onChange)
  return () => {
    listeners.delete(onChange)
    window.removeEventListener('popstate', onChange)
  }
}

function snapshot(): string {
  return window.location.pathname + window.location.search
}

export function navigate(to: string, options: { replace?: boolean } = {}): void {
  if (to === snapshot()) return
  if (options.replace) window.history.replaceState(null, '', to)
  else window.history.pushState(null, '', to)
  listeners.forEach(notify => notify())
}

export type Route =
  | { page: 'overview' }
  | { page: 'images' }
  | { page: 'repository'; repo: string }
  | { page: 'activity' }
  | { page: 'storage' }
  | { page: 'security' }
  | { page: 'access' }
  | { page: 'integrations' }
  | { page: 'settings' }
  | { page: 'notFound' }

export type PageId = Route['page']

const SIMPLE_PAGES: Record<string, PageId> = {
  '/': 'overview',
  '/images': 'images',
  '/activity': 'activity',
  '/storage': 'storage',
  '/security': 'security',
  '/access': 'access',
  '/integrations': 'integrations',
  '/settings': 'settings',
}

export function parseRoute(pathname: string): Route {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  const simple = SIMPLE_PAGES[path]
  if (simple) return { page: simple } as Route
  if (path.startsWith('/images/')) {
    try {
      const repo = decodeURIComponent(path.slice('/images/'.length))
      if (repo) return { page: 'repository', repo }
    } catch {
      // malformed percent-encoding → not found
    }
  }
  return { page: 'notFound' }
}

/** URL of a repository page, optionally opening one tag's detail panel. */
export function repoHref(repo: string, tag?: string): string {
  // Repository names only contain [A-Za-z0-9._/-]; encode each segment anyway
  // so an unexpected character can never change the route.
  const path = '/images/' + repo.split('/').map(encodeURIComponent).join('/')
  return tag ? `${path}?tag=${encodeURIComponent(tag)}` : path
}

export function useLocation(): { route: Route; path: string; query: URLSearchParams } {
  const href = useSyncExternalStore(subscribe, snapshot)
  return useMemo(() => {
    const url = new URL(href, window.location.origin)
    return { route: parseRoute(url.pathname), path: url.pathname, query: url.searchParams }
  }, [href])
}

/** Updates one query parameter in place (null removes it). */
export function setQueryParam(key: string, value: string | null, options: { replace?: boolean } = {}) {
  const url = new URL(window.location.href)
  if (value === null || value === '') url.searchParams.delete(key)
  else url.searchParams.set(key, value)
  navigate(url.pathname + url.search, options)
}

type LinkProps = Omit<React.ComponentProps<'a'>, 'href'> & { to: string }

/** An <a> that navigates client-side, while keeping every native link
 *  behaviour (open in new tab, copy address, middle click). */
export function Link({ to, onClick, ...props }: LinkProps) {
  return (
    <a
      href={to}
      onClick={event => {
        onClick?.(event)
        if (
          event.defaultPrevented ||
          event.button !== 0 ||
          event.metaKey ||
          event.ctrlKey ||
          event.shiftKey ||
          event.altKey
        ) {
          return
        }
        event.preventDefault()
        navigate(to)
      }}
      {...props}
    />
  )
}
