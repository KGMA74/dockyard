import { Bell } from 'lucide-react'
import { formatEventMessage, RegistryEvent } from '../api'
import { relativeTime } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
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

// NotificationBell is a session-only feed (nothing persisted server-side) of
// recent registry events — the same SSE stream that drives push toasts,
// kept here as a short history so a completion isn't missed if the toast
// wasn't seen.
export default function NotificationBell({ items, unreadCount, open, onOpenChange }: Props) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="relative text-muted-foreground" title="Recent activity">
          <Bell strokeWidth={1.5} />
          {unreadCount > 0 && (
            <Badge
              variant="destructive"
              className="absolute -top-1 -right-1 h-4 min-w-4 px-1 justify-center rounded-full text-[10px]"
            >
              {unreadCount > 9 ? '9+' : unreadCount}
            </Badge>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="max-h-96 overflow-y-auto">
        <div className="px-3 py-2.5 border-b">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-widest">Activity</p>
        </div>
        {items.length === 0 ? (
          <p className="px-3 py-6 text-xs text-muted-foreground text-center">Nothing yet this session.</p>
        ) : (
          <div className="divide-y">
            {items.map(item => (
              <div key={item.id} className="px-3 py-2.5">
                <p className="text-xs">{formatEventMessage(item.event)}</p>
                <p className="text-[11px] text-muted-foreground/70 mt-0.5">
                  {relativeTime(item.at)}
                  {item.event.actor && item.event.actor !== 'scheduler' && ` · ${item.event.actor}`}
                </p>
              </div>
            ))}
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
