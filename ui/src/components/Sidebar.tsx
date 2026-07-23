import { Box, HardDrive, KeyRound, LogOut, Settings, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getRole } from '../api'
import { ThemeSwitcher } from '../theme'
import { LanguageSwitcher } from '../i18nSwitcher'
import NotificationBell, { NotificationItem } from './NotificationBell'
import { Button } from '@/components/ui/button'

export type Tab = 'images' | 'storage' | 'users' | 'settings'

interface Props {
  tab: Tab
  onTabChange: (tab: Tab) => void
  onChangePassword: () => void
  onLogout: () => void
  notifications: NotificationItem[]
  unreadCount: number
  notifOpen: boolean
  onNotifOpenChange: (open: boolean) => void
}

const navItems: { tab: Tab; labelKey: string; icon: typeof Box; adminOnly?: boolean }[] = [
  { tab: 'images', labelKey: 'sidebar.images', icon: Box },
  { tab: 'storage', labelKey: 'sidebar.storage', icon: HardDrive },
  { tab: 'users', labelKey: 'sidebar.users', icon: Users, adminOnly: true },
  { tab: 'settings', labelKey: 'sidebar.settings', icon: Settings },
]

export default function Sidebar({
  tab,
  onTabChange,
  onChangePassword,
  onLogout,
  notifications,
  unreadCount,
  notifOpen,
  onNotifOpenChange,
}: Props) {
  const { t } = useTranslation()
  const isAdmin = getRole() === 'admin'
  return (
    <aside className="w-56 shrink-0 h-screen sticky top-0 border-r bg-card flex flex-col">
      <div className="h-14 flex items-center gap-2.5 px-4 border-b">
        <Box className="size-5 text-blue-500 dark:text-blue-400" strokeWidth={1.5} />
        <span className="font-semibold text-sm tracking-tight flex-1">Dockyard</span>
        <NotificationBell
          items={notifications}
          unreadCount={unreadCount}
          open={notifOpen}
          onOpenChange={onNotifOpenChange}
        />
      </div>

      <nav className="flex-1 px-3 py-4 space-y-1">
        {navItems.filter(item => !item.adminOnly || isAdmin).map(item => (
          <button
            key={item.tab}
            onClick={() => onTabChange(item.tab)}
            className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors ${
              tab === item.tab
                ? 'bg-muted font-medium'
                : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
            }`}
          >
            <item.icon className="size-4" strokeWidth={1.5} />
            {t(item.labelKey)}
          </button>
        ))}
      </nav>

      <div className="px-3 py-4 border-t space-y-1">
        <div className="px-3 pb-2 space-y-1.5">
          <ThemeSwitcher />
          <LanguageSwitcher />
        </div>
        <Button
          variant="ghost"
          onClick={onChangePassword}
          className="w-full justify-start gap-2.5 px-3 text-muted-foreground font-normal"
        >
          <KeyRound strokeWidth={1.5} />
          {t('sidebar.changePassword')}
        </Button>
        <Button
          variant="ghost"
          onClick={onLogout}
          className="w-full justify-start gap-2.5 px-3 text-muted-foreground font-normal"
        >
          <LogOut strokeWidth={1.5} />
          {t('sidebar.signOut')}
        </Button>
      </div>
    </aside>
  )
}
