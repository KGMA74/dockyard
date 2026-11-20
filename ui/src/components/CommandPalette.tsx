import { useEffect, useMemo, useRef, useState } from 'react'
import { Box, CornerDownLeft, Search, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { navigate, repoHref } from '@/lib/router'
import { useRepositories } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import { visibleNavigation } from './Sidebar'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'

interface Entry {
  key: string
  label: string
  hint: string
  href: string
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>
  mono?: boolean
}

const MAX_RESULTS = 12

// Ctrl/Cmd+K: jump to any page or repository by typing a few letters. With a
// few hundred repositories, scrolling a list stops being navigation.
export default function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  // The body only exists while open, so every opening starts from a blank
  // query without any reset logic.
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} className="top-[20%] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-lg">
        {open && <PaletteBody close={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  )
}

function PaletteBody({ close }: { close: () => void }) {
  const { t } = useTranslation()
  const session = useSession()
  const repositories = useRepositories()
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  const entries = useMemo<Entry[]>(() => {
    const pages: Entry[] = visibleNavigation(session.isAdmin)
      .flatMap(group => group.items)
      .map(item => ({
        key: `page:${item.page}`,
        label: t(`nav.${item.page}`),
        hint: t('palette.page'),
        href: item.href,
        icon: item.icon,
      }))
    pages.push({ key: 'page:settings', label: t('nav.settings'), hint: t('palette.page'), href: '/settings', icon: Settings })

    const repos: Entry[] = (repositories.data ?? []).map(repo => ({
      key: `repo:${repo.name}`,
      label: repo.name,
      hint: t('palette.tagCount', { count: repo.total }),
      href: repoHref(repo.name),
      icon: Box,
      mono: true,
    }))

    const needle = query.trim().toLowerCase()
    if (!needle) return [...pages, ...repos].slice(0, MAX_RESULTS)
    const matches = (entry: Entry) => entry.label.toLowerCase().includes(needle)
    // Names that *start* with the query outrank mid-string matches.
    const rank = (entry: Entry) => (entry.label.toLowerCase().startsWith(needle) ? 0 : 1)
    return [...pages.filter(matches), ...repos.filter(matches).sort((a, b) => rank(a) - rank(b))].slice(0, MAX_RESULTS)
  }, [query, repositories.data, session.isAdmin, t])

  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [cursor])

  function go(entry: Entry | undefined) {
    if (!entry) return
    close()
    navigate(entry.href)
  }

  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setCursor(c => Math.min(entries.length - 1, c + 1))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setCursor(c => Math.max(0, c - 1))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      go(entries[cursor])
    }
  }

  return (
    <>
        <DialogTitle className="sr-only">{t('palette.title')}</DialogTitle>
        <DialogDescription className="sr-only">{t('palette.description')}</DialogDescription>
        <div className="flex items-center gap-2 border-b px-3">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={event => {
              setQuery(event.target.value)
              setCursor(0)
            }}
            onKeyDown={onKeyDown}
            placeholder={t('palette.placeholder')}
            role="combobox"
            aria-expanded="true"
            aria-controls="command-palette-list"
            aria-activedescendant={entries[cursor] ? `palette-${cursor}` : undefined}
            className="h-11 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
        <div ref={listRef} id="command-palette-list" role="listbox" className="max-h-80 overflow-y-auto p-1">
          {entries.length === 0 ? (
            <p className="px-3 py-8 text-center text-sm text-muted-foreground">{t('palette.empty', { query })}</p>
          ) : (
            entries.map((entry, index) => (
              <button
                key={entry.key}
                id={`palette-${index}`}
                role="option"
                aria-selected={index === cursor}
                data-active={index === cursor}
                onMouseMove={() => setCursor(index)}
                onClick={() => go(entry)}
                className={cn(
                  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm',
                  index === cursor && 'bg-muted',
                )}
              >
                <entry.icon className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.5} />
                <span className={cn('min-w-0 flex-1 truncate', entry.mono && 'font-mono text-[13px]')}>{entry.label}</span>
                <span className="shrink-0 text-xs text-muted-foreground">{entry.hint}</span>
                {index === cursor && <CornerDownLeft className="size-3 shrink-0 text-muted-foreground" />}
              </button>
            ))
          )}
        </div>
    </>
  )
}
