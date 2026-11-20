import { Bell } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatEventMessage, type RegistryEvent } from '@/api'
import { Link, repoHref } from '@/lib/router'
import { Timestamp } from './kit'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

export interface NotificationItem {
  id: number
  event: RegistryEvent
  at: string // ISO timestamp
}

interface Props {
  items: NotificationItem[]
  unreadCount: number
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Where an event leads, when it names a repository. */
function eventHref(event: RegistryEvent): string | null {
  if (event.type === 'push' || event.type === 'scan') return repoHref(event.name, event.tag)
  if (event.type === 'import') return repoHref(event.name)
  return null
}

// A session-only feed of what happened in the registry while this tab was
// open (same SSE stream as the toasts), so a push is not missed because its
// toast was. The durable record is the audit log.
export default function NotificationBell({ items, unreadCount, open, onOpenChange }: Props) {
  const { t } = useTranslation()
  const label = unreadCount > 0 ? t('bell.titleUnread', { count: unreadCount }) : t('bell.title')

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="relative text-muted-foreground" aria-label={label} title={label}>
          <Bell strokeWidth={1.5} />
          {unreadCount > 0 && (
            <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground tabular-nums">
              {unreadCount > 9 ? '9+' : unreadCount}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="max-h-96 w-80 overflow-y-auto p-0">
        <p className="border-b px-3 py-2.5 text-xs font-medium">{t('bell.heading')}</p>
        {items.length === 0 ? (
          <p className="px-3 py-8 text-center text-xs text-muted-foreground text-pretty">{t('bell.empty')}</p>
        ) : (
          <ul className="divide-y">
            {items.map(item => {
              const href = eventHref(item.event)
              const body = (
                <>
                  <p className="text-xs text-pretty">{formatEventMessage(item.event)}</p>
                  <p className="mt-0.5 text-[11px] text-muted-foreground">
                    <Timestamp value={item.at} />
                    {item.event.actor && item.event.actor !== 'scheduler' && ` · ${item.event.actor}`}
                  </p>
                </>
              )
              return (
                <li key={item.id}>
                  {href ? (
                    <Link to={href} onClick={() => onOpenChange(false)} className="block px-3 py-2.5 outline-none hover:bg-muted/60 focus-visible:bg-muted/60">
                      {body}
                    </Link>
                  ) : (
                    <div className="px-3 py-2.5">{body}</div>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  )
}
