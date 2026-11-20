import {
  Activity,
  Box,
  HardDrive,
  LayoutDashboard,
  LogOut,
  Plug,
  ShieldCheck,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DockyardMark } from '@/components/Logo'
import { Link, type PageId } from '@/lib/router'
import { useSession } from '@/lib/session'
import { RoleBadge } from './kit'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@/components/ui/sidebar'

export interface NavItem {
  page: PageId
  href: string
  icon: LucideIcon
  adminOnly?: boolean
}

export interface NavGroup {
  /** i18n key of the group label; omitted for the first, unlabeled group. */
  labelKey?: string
  items: NavItem[]
}

// The single description of what exists in the console and who sees it. The
// sidebar and the command palette both render from it.
export const NAVIGATION: NavGroup[] = [
  {
    items: [
      { page: 'overview', href: '/', icon: LayoutDashboard },
      { page: 'images', href: '/images', icon: Box },
    ],
  },
  {
    labelKey: 'nav.groupOperate',
    items: [
      { page: 'activity', href: '/activity', icon: Activity, adminOnly: true },
      { page: 'storage', href: '/storage', icon: HardDrive, adminOnly: true },
      { page: 'security', href: '/security', icon: ShieldCheck, adminOnly: true },
    ],
  },
  {
    labelKey: 'nav.groupAdmin',
    items: [
      { page: 'access', href: '/access', icon: Users, adminOnly: true },
      { page: 'integrations', href: '/integrations', icon: Plug, adminOnly: true },
    ],
  },
]

export function visibleNavigation(isAdmin: boolean): NavGroup[] {
  return NAVIGATION.map(group => ({
    ...group,
    items: group.items.filter(item => isAdmin || !item.adminOnly),
  })).filter(group => group.items.length > 0)
}

interface Props {
  /** Page currently shown; a repository page highlights "Images". */
  current: PageId
  onLogout: () => void
}

export default function AppSidebar({ current, onLogout }: Props) {
  const { t } = useTranslation()
  const session = useSession()
  const active = current === 'repository' ? 'images' : current
  const username = session.username ?? t('nav.unknownUser')

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <Link
          to="/"
          className="flex h-8 items-center gap-2.5 rounded-md px-1.5 outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
        >
          <DockyardMark className="size-5 text-sidebar-foreground" />
          <span className="text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">Dockyard</span>
        </Link>
      </SidebarHeader>

      <SidebarContent>
        {visibleNavigation(session.isAdmin).map((group, index) => (
          <SidebarGroup key={group.labelKey ?? index}>
            {group.labelKey && <SidebarGroupLabel>{t(group.labelKey)}</SidebarGroupLabel>}
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map(item => (
                  <SidebarMenuItem key={item.page}>
                    <SidebarMenuButton asChild isActive={active === item.page} tooltip={t(`nav.${item.page}`)}>
                      <Link to={item.href} aria-current={active === item.page ? 'page' : undefined}>
                        <item.icon strokeWidth={1.5} />
                        <span>{t(`nav.${item.page}`)}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              size="lg"
              isActive={active === 'settings'}
              tooltip={`${username} · ${t('nav.settings')}`}
            >
              <Link to="/settings" aria-current={active === 'settings' ? 'page' : undefined}>
                <span className="flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted text-xs font-semibold uppercase text-foreground">
                  {username.slice(0, 1)}
                </span>
                <span className="flex min-w-0 flex-1 flex-col gap-0.5 leading-none">
                  <span className="truncate font-medium">{username}</span>
                  <span className="truncate text-xs text-muted-foreground">{t('nav.settings')}</span>
                </span>
                <span className="group-data-[collapsible=icon]:hidden">
                  <RoleBadge role={session.role} />
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton onClick={onLogout} tooltip={t('nav.signOut')}>
              <LogOut strokeWidth={1.5} />
              <span>{t('nav.signOut')}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
