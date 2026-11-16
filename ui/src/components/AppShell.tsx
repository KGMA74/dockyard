import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Lock, Search, SearchX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatEventMessage, logout, subscribeToEvents, type RegistryEvent } from '@/api'
import { invalidateForEvent } from '@/lib/queries'
import { Link, useLocation, type PageId, type Route } from '@/lib/router'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import CommandPalette from './CommandPalette'
import { LanguageToggle, ThemeToggle } from './HeaderToggles'
import { EmptyState, Page, Spinner } from './kit'
import NotificationBell, { type NotificationItem } from './NotificationBell'
import AppSidebar from './Sidebar'
import { Button } from '@/components/ui/button'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import ImagesPage from '@/pages/ImagesPage'
import OverviewPage from '@/pages/OverviewPage'
import RepositoryPage from '@/pages/RepositoryPage'

// Admin screens are split out of the initial bundle: readers and pushers —
// most accounts — never download them.
const ActivityPage = lazy(() => import('@/pages/ActivityPage'))
const StoragePage = lazy(() => import('@/pages/StoragePage'))
const SecurityPage = lazy(() => import('@/pages/SecurityPage'))
const AccessPage = lazy(() => import('@/pages/AccessPage'))
const IntegrationsPage = lazy(() => import('@/pages/IntegrationsPage'))
const SettingsPage = lazy(() => import('@/pages/SettingsPage'))

const ADMIN_PAGES: PageId[] = ['activity', 'storage', 'security', 'access', 'integrations']

// The shadcn Sidebar persists its state in a cookie; this is a SPA (no SSR),
// so read it back here to seed `defaultOpen` across reloads.
function sidebarInitiallyOpen(): boolean {
  const match = document.cookie.match(/(?:^|;\s*)sidebar_state=([^;]+)/)
  return match ? match[1] === 'true' : true
}

function PageContent({ route }: { route: Route }) {
  const { t } = useTranslation()
  const session = useSession()

  if (ADMIN_PAGES.includes(route.page) && !session.isAdmin) {
    return (
      <Page>
        <EmptyState icon={Lock} title={t('shell.adminOnlyTitle')} description={t('shell.adminOnlyBody')}>
          <Button asChild variant="outline" size="sm">
            <Link to="/images">{t('shell.backToImages')}</Link>
          </Button>
        </EmptyState>
      </Page>
    )
  }

  switch (route.page) {
    case 'overview':
      return <OverviewPage />
    case 'images':
      return <ImagesPage />
    case 'repository':
      return <RepositoryPage key={route.repo} repo={route.repo} />
    case 'activity':
      return <ActivityPage />
    case 'storage':
      return <StoragePage />
    case 'security':
      return <SecurityPage />
    case 'access':
      return <AccessPage />
    case 'integrations':
      return <IntegrationsPage />
    case 'settings':
      return <SettingsPage />
    default:
      return (
        <Page>
          <EmptyState icon={SearchX} title={t('shell.notFoundTitle')} description={t('shell.notFoundBody')}>
            <Button asChild variant="outline" size="sm">
              <Link to="/">{t('shell.backToOverview')}</Link>
            </Button>
          </EmptyState>
        </Page>
      )
  }
}

function Breadcrumb({ route }: { route: Route }) {
  const { t } = useTranslation()
  if (route.page === 'repository') {
    return (
      <nav aria-label={t('shell.breadcrumb')} className="flex min-w-0 items-center gap-1 text-sm">
        <Link to="/images" className="shrink-0 text-muted-foreground hover:text-foreground">
          {t('nav.images')}
        </Link>
        <ChevronRight className="size-3.5 shrink-0 text-muted-foreground/60" />
        <span className="truncate font-mono text-[13px] font-medium">{route.repo}</span>
      </nav>
    )
  }
  const label = route.page === 'notFound' ? t('shell.notFoundTitle') : t(`nav.${route.page}`)
  return <span className="truncate text-sm font-medium">{label}</span>
}

export default function AppShell({ onLogout }: { onLogout: () => void }) {
  const { t } = useTranslation()
  const { route } = useLocation()
  const session = useSession()
  const queryClient = useQueryClient()
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [unread, setUnread] = useState(0)
  const [bellOpen, setBellOpen] = useState(false)
  const [live, setLive] = useState(true)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const nextId = useRef(0)
  const me = session.username

  // One SSE subscription for the whole app: it feeds the notification bell,
  // raises a toast, and marks the affected queries stale so whatever page is
  // open refreshes itself.
  useEffect(() => {
    function onEvent(event: RegistryEvent) {
      invalidateForEvent(queryClient, event)
      setNotifications(items => [{ id: nextId.current++, event, at: new Date().toISOString() }, ...items].slice(0, 30))
      setUnread(count => count + 1)
      // Your own delete / GC already confirmed itself where you clicked.
      const ownAction = event.actor === me && (event.type === 'delete' || event.type === 'gc')
      if (ownAction) return
      const message = formatEventMessage(event)
      if (event.type === 'push') toast.success(message)
      else if (event.type === 'quota_warning') toast.warning(message)
      else toast.info(message)
    }
    return subscribeToEvents(onEvent, setLive)
  }, [queryClient, me])

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setPaletteOpen(open => !open)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const pageTitle =
    route.page === 'repository' ? route.repo : route.page === 'notFound' ? t('shell.notFoundTitle') : t(`nav.${route.page}`)
  useEffect(() => {
    document.title = `${pageTitle} · Dockyard`
  }, [pageTitle])

  async function handleLogout() {
    await logout()
    queryClient.clear()
    onLogout()
  }

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform)

  return (
    <SidebarProvider className="h-svh overflow-hidden" defaultOpen={sidebarInitiallyOpen()}>
      <AppSidebar current={route.page} onLogout={handleLogout} />

      <SidebarInset className="min-h-0 overflow-hidden">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b bg-background px-3 sm:px-4">
          <SidebarTrigger className="text-muted-foreground" aria-label={t('shell.toggleSidebar')} />
          <div className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden="true" />
          <div className="min-w-0 flex-1">
            <Breadcrumb route={route} />
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => setPaletteOpen(true)}
            className="hidden w-56 justify-start gap-2 text-muted-foreground sm:flex"
          >
            <Search />
            <span className="flex-1 text-left">{t('shell.search')}</span>
            <kbd className="rounded border bg-muted px-1 text-[10px] font-medium">{isMac ? '⌘K' : 'Ctrl K'}</kbd>
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setPaletteOpen(true)}
            className="text-muted-foreground sm:hidden"
            aria-label={t('shell.search')}
          >
            <Search />
          </Button>

          <Tooltip>
            <TooltipTrigger asChild>
              <span
                role="status"
                className="hidden items-center gap-1.5 px-1 text-xs text-muted-foreground md:flex"
                aria-label={live ? t('shell.liveOn') : t('shell.liveOff')}
              >
                <span
                  className={cn('size-1.5 rounded-full', live ? 'bg-success' : 'animate-pulse bg-warning')}
                  aria-hidden="true"
                />
                {live ? t('shell.live') : t('shell.reconnecting')}
              </span>
            </TooltipTrigger>
            <TooltipContent>{live ? t('shell.liveOn') : t('shell.liveOff')}</TooltipContent>
          </Tooltip>

          <div className="flex items-center gap-0.5">
            <ThemeToggle />
            <LanguageToggle />
            <NotificationBell
              items={notifications}
              unreadCount={unread}
              open={bellOpen}
              onOpenChange={open => {
                setBellOpen(open)
                if (open) setUnread(0)
              }}
            />
          </div>
        </header>

        <main className="min-h-0 min-w-0 flex-1 overflow-y-auto bg-muted/40 dark:bg-background">
          <Suspense
            fallback={
              <div className="flex justify-center py-16 text-muted-foreground">
                <Spinner className="size-5" />
              </div>
            }
          >
            <PageContent route={route} />
          </Suspense>
        </main>
      </SidebarInset>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </SidebarProvider>
  )
}
