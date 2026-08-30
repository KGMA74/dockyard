import { Box, HardDrive, KeyRound, LogOut, Settings, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getRole } from '../api'
import { ThemeSwitcher } from '../theme'
import { LanguageSwitcher } from '../i18nSwitcher'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'

export type Tab = 'images' | 'storage' | 'users' | 'settings'

interface Props {
  tab: Tab
  onTabChange: (tab: Tab) => void
  onChangePassword: () => void
  onLogout: () => void
}

const navItems: { tab: Tab; labelKey: string; icon: typeof Box; adminOnly?: boolean }[] = [
  { tab: 'images', labelKey: 'sidebar.images', icon: Box },
  { tab: 'storage', labelKey: 'sidebar.storage', icon: HardDrive },
  { tab: 'users', labelKey: 'sidebar.users', icon: Users, adminOnly: true },
  { tab: 'settings', labelKey: 'sidebar.settings', icon: Settings },
]

export default function AppSidebar({ tab, onTabChange, onChangePassword, onLogout }: Props) {
  const { t } = useTranslation()
  const isAdmin = getRole() === 'admin'

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex h-8 items-center gap-2.5 px-1.5 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
          <Box className="size-5 shrink-0 text-blue-500 dark:text-blue-400" strokeWidth={1.5} />
          <span className="font-semibold text-sm tracking-tight group-data-[collapsible=icon]:hidden">
            Dockyard
          </span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {navItems
                .filter(item => !item.adminOnly || isAdmin)
                .map(item => (
                  <SidebarMenuItem key={item.tab}>
                    <SidebarMenuButton
                      isActive={tab === item.tab}
                      tooltip={t(item.labelKey)}
                      onClick={() => onTabChange(item.tab)}
                    >
                      <item.icon strokeWidth={1.5} />
                      <span>{t(item.labelKey)}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <div className="space-y-1.5 px-1 pb-1 group-data-[collapsible=icon]:hidden">
          <ThemeSwitcher />
          <LanguageSwitcher />
        </div>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={onChangePassword} tooltip={t('sidebar.changePassword')}>
              <KeyRound strokeWidth={1.5} />
              <span>{t('sidebar.changePassword')}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={onLogout} tooltip={t('sidebar.signOut')}>
              <LogOut strokeWidth={1.5} />
              <span>{t('sidebar.signOut')}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}
