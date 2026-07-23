import { useState, useEffect, useCallback, useRef } from 'react'
import { toast } from 'sonner'
import { LayoutGrid, List, Search, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { logout, getRepositories, getStorageStats, subscribeToEvents, formatEventMessage, RegistryEvent, StorageStats, RepoSummary, TagInfo } from '../api'
import DenseRepoView from '../components/DenseRepoView'
import ImageDetailsPanel from '../components/ImageDetailsPanel'
import { NotificationItem } from '../components/NotificationBell'
import RepoList from '../components/RepoList'
import Sidebar, { Tab } from '../components/Sidebar'
import StorageTab from '../components/StorageTab'
import SettingsTab from '../components/SettingsTab'
import UsersTab from '../components/UsersTab'
import ChangePasswordModal from '../components/ChangePasswordModal'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

interface Props {
  onLogout: () => void
}

type SortKey = 'name' | 'tags' | 'pushed'

export default function Dashboard({ onLogout }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('images')
  const [repos, setRepos] = useState<RepoSummary[]>([])
  const [stats, setStats] = useState<StorageStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState<SortKey>('name')
  const [dense, setDense] = useState(false)
  const [details, setDetails] = useState<{ name: string; tag: TagInfo } | null>(null)
  const [showPasswordModal, setShowPasswordModal] = useState(false)
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [unreadCount, setUnreadCount] = useState(0)
  const [notifOpen, setNotifOpen] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)
  const notifId = useRef(0)

  const loadData = useCallback(async () => {
    setError('')
    try {
      const [reposRes, statsRes] = await Promise.allSettled([
        getRepositories(),
        getStorageStats(),
      ])
      if (reposRes.status === 'fulfilled') setRepos(reposRes.value.repositories)
      else setError(t('dashboard.loadFailed'))
      if (statsRes.status === 'fulfilled') setStats(statsRes.value)
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => { loadData() }, [loadData])

  // Live-refresh the list and surface a toast + notification-bell entry when
  // something happens elsewhere (docker push, CI, scheduled GC, a scan
  // completing, …). Events that change what the repo list shows trigger a
  // reload; scan doesn't (it doesn't add/remove tags), so it's notify-only.
  const REFRESH_ON: RegistryEvent['type'][] = ['push', 'delete', 'retention', 'gc', 'import']
  useEffect(() => {
    return subscribeToEvents(event => {
      const message = formatEventMessage(event)
      if (event.type === 'push') toast.success(message)
      else toast.info(message)

      setNotifications(items => [
        { id: notifId.current++, event, at: new Date().toISOString() },
        ...items,
      ].slice(0, 20))
      setUnreadCount(n => n + 1)

      if (REFRESH_ON.includes(event.type)) loadData()
    })
  }, [loadData])

  // "/" focuses the search field, Escape clears it
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement
      const typing = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA'
      if (e.key === '/' && !typing && tab === 'images') {
        e.preventDefault()
        searchRef.current?.focus()
      } else if (e.key === 'Escape' && target === searchRef.current) {
        setSearch('')
        searchRef.current?.blur()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [tab])

  async function handleLogout() {
    await logout()
    onLogout()
  }

  function handleNotifOpenChange(open: boolean) {
    setNotifOpen(open)
    if (open) setUnreadCount(0)
  }

  const filtered = repos
    .filter(r => r.name.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => {
      if (sort === 'tags') return b.total - a.total || a.name.localeCompare(b.name)
      if (sort === 'pushed') {
        const bt = b.last_pushed ? Date.parse(b.last_pushed) : -Infinity
        const at = a.last_pushed ? Date.parse(a.last_pushed) : -Infinity
        return bt - at || a.name.localeCompare(b.name)
      }
      return a.name.localeCompare(b.name)
    })

  return (
    <div className="min-h-screen bg-muted/40 dark:bg-background flex">
      <Sidebar
        tab={tab}
        onTabChange={setTab}
        onChangePassword={() => setShowPasswordModal(true)}
        onLogout={handleLogout}
        notifications={notifications}
        unreadCount={unreadCount}
        notifOpen={notifOpen}
        onNotifOpenChange={handleNotifOpenChange}
      />

      <main className="flex-1 min-w-0 px-6 py-6">
        {tab === 'images' && (
          <div>
            <div className="flex items-center gap-3 mb-3">
              <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest shrink-0">
                {t('sidebar.images')}
                {!loading && !dense && repos.length > 0 && (
                  <span className="ml-2 text-muted-foreground/60 normal-case tracking-normal font-normal">
                    ({filtered.length}{filtered.length !== repos.length && `/${repos.length}`})
                  </span>
                )}
              </h2>

              <div className="flex-1 relative">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground/60 pointer-events-none" />
                <Input
                  ref={searchRef}
                  type="text"
                  placeholder={dense ? t('dashboard.searchPlaceholder') : t('dashboard.filterPlaceholder')}
                  value={search}
                  onChange={e => setSearch(e.target.value)}
                  className="pl-8 h-8 text-xs bg-card"
                />
              </div>

              {!dense && (
                <Select value={sort} onValueChange={v => setSort(v as SortKey)}>
                  <SelectTrigger size="sm" className="shrink-0 text-xs bg-card" title={t('dashboard.sortImages')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="name">{t('dashboard.sortNameAZ')}</SelectItem>
                    <SelectItem value="tags">{t('dashboard.sortMostTags')}</SelectItem>
                    <SelectItem value="pushed">{t('dashboard.sortRecentlyPushed')}</SelectItem>
                  </SelectContent>
                </Select>
              )}

              <Button
                variant="outline"
                size="sm"
                onClick={() => setDense(v => !v)}
                className="shrink-0 text-xs"
                title={dense ? t('dashboard.switchToCards') : t('dashboard.switchToDense')}
              >
                {dense ? <LayoutGrid /> : <List />}
                {dense ? t('dashboard.cards') : t('dashboard.dense')}
              </Button>

              <Button
                variant="ghost"
                size="sm"
                onClick={loadData}
                className="shrink-0 text-muted-foreground"
              >
                <RefreshCw />
                {t('dashboard.refresh')}
              </Button>
            </div>

            {dense ? (
              <DenseRepoView query={search} onShowDetails={(name, tag) => setDetails({ name, tag })} />
            ) : loading ? (
              <div className="space-y-2">
                <Skeleton className="h-12 rounded-xl" />
                <Skeleton className="h-12 rounded-xl" />
                <Skeleton className="h-12 rounded-xl" />
              </div>
            ) : error ? (
              <div className="text-center py-20 text-destructive text-sm">{error}</div>
            ) : repos.length === 0 ? (
              <div className="text-center py-20">
                <p className="text-muted-foreground text-sm">{t('dashboard.noImages')}</p>
              </div>
            ) : filtered.length === 0 ? (
              <div className="text-center py-20 text-muted-foreground text-sm">
                {t('dashboard.noMatch', { search })}
              </div>
            ) : (
              <RepoList repos={filtered} onRefresh={loadData} />
            )}

            {details && (
              <ImageDetailsPanel
                imageName={details.name}
                tag={details.tag}
                onClose={() => setDetails(null)}
              />
            )}
          </div>
        )}

        {tab === 'storage' && (
          <StorageTab stats={stats} repos={repos} onRefresh={loadData} />
        )}

        {tab === 'users' && <UsersTab />}

        {tab === 'settings' && (
          <SettingsTab onChangePassword={() => setShowPasswordModal(true)} />
        )}
      </main>

      {showPasswordModal && (
        <ChangePasswordModal onClose={() => setShowPasswordModal(false)} />
      )}
    </div>
  )
}
