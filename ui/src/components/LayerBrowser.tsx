import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, File, Folder, Link as LinkIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLayerEntries, type LayerEntry } from '../api'
import { ErrorState, TableSkeleton } from './kit'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'

interface Props {
  imageName: string
  digest: string
  onClose: () => void
}

export default function LayerBrowser({ imageName, digest, onClose }: Props) {
  const { t } = useTranslation()
  const [filter, setFilter] = useState('')
  const layer = useQuery({
    queryKey: ['layer', digest],
    queryFn: () => getLayerEntries(imageName, digest),
    staleTime: Infinity, // a layer is immutable
  })
  const entries = layer.data?.entries

  const filtered = useMemo(() => {
    if (!entries) return []
    const q = filter.toLowerCase()
    return q ? entries.filter(e => e.path.toLowerCase().includes(q)) : entries
  }, [entries, filter])

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-sm">{t('layerBrowser.title')}</DialogTitle>
          <DialogDescription className="font-mono text-xs truncate">{digest}</DialogDescription>
        </DialogHeader>

        {layer.isPending ? (
          <TableSkeleton rows={5} />
        ) : layer.isError ? (
          <ErrorState error={layer.error} onRetry={() => layer.refetch()} />
        ) : entries && entries.length === 0 ? (
          <p className="text-sm text-muted-foreground py-10 text-center">
            {t('layerBrowser.empty')}
          </p>
        ) : (
          <>
            <div className="relative shrink-0">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground/60 pointer-events-none" />
              <Input
                autoFocus
                placeholder={t('layerBrowser.filterPlaceholder', { count: entries?.length ?? 0 })}
                aria-label={t('layerBrowser.filterPlaceholder', { count: entries?.length ?? 0 })}
                value={filter}
                onChange={e => setFilter(e.target.value)}
                className="pl-8 h-8 text-xs"
              />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto -mx-4 px-4 border-t">
              {filtered.length === 0 ? (
                <p className="text-xs text-muted-foreground py-6 text-center">{t('layerBrowser.noMatch', { filter })}</p>
              ) : (
                <div className="divide-y">
                  {filtered.map(e => (
                    <div key={e.path} className="flex items-center gap-2 py-1.5 text-xs">
                      <EntryIcon type={e.type} />
                      <span className="font-mono truncate flex-1">
                        {e.path}
                        {e.type === 'symlink' && e.link_name && (
                          <span className="text-muted-foreground/60"> → {e.link_name}</span>
                        )}
                      </span>
                      {e.size_human && (
                        <span className="shrink-0 tabular-nums text-muted-foreground">{e.size_human}</span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
            <p className="text-xs text-muted-foreground/60 shrink-0">
              {t('layerBrowser.entryCount', {
                count: filtered.length,
                total: filtered.length !== entries?.length ? ` / ${entries?.length}` : '',
              })}
            </p>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function EntryIcon({ type }: { type: LayerEntry['type'] }) {
  const cls = 'size-3.5 shrink-0 text-muted-foreground/60'
  if (type === 'dir') return <Folder className={cls} />
  if (type === 'symlink' || type === 'hardlink') return <LinkIcon className={cls} />
  return <File className={cls} />
}
