import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Gauge, Plus, RotateCcw, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  deleteQuota, listQuotas, Quota, QuotaScopeType, QuotaUsage, resetQuotaUsage, setQuota,
} from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

// QuotasSection lives in the Settings tab (admin only). Quota enforcement
// happens at blob-upload commit in embedded/mirror mode — proxy mode
// doesn't own the storage writes, so quotas configured there are
// informational only until pushes land through an embedded/mirror instance.
export default function QuotasSection() {
  const { t } = useTranslation()
  const [quotas, setQuotas] = useState<Quota[] | null>(null)
  const [usage, setUsage] = useState<QuotaUsage[]>([])
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(() => {
    listQuotas()
      .then(r => {
        setQuotas(r.quotas)
        setUsage(r.usage)
      })
      .catch(() => setQuotas(null))
  }, [])

  useEffect(load, [load])

  if (quotas === null) return null

  function usageFor(scopeType: QuotaScopeType, scopeValue: string): number {
    return usage.find(u => u.scope_type === scopeType && u.scope_value === scopeValue)?.bytes_used ?? 0
  }

  async function handleDelete(id: number) {
    try {
      await deleteQuota(id)
      toast.success(t('quotas.deleted'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('quotas.deleteFailed'))
    }
  }

  async function handleReset(scopeType: QuotaScopeType, scopeValue: string) {
    try {
      await resetQuotaUsage(scopeType, scopeValue)
      toast.success(t('quotas.usageReset'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('quotas.resetFailed'))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          {t('quotas.title')}
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? t('common.cancel') : t('quotas.newQuota')}
        </Button>
      </div>

      {showCreate && (
        <CreateQuotaForm
          onCreated={() => {
            setShowCreate(false)
            load()
          }}
        />
      )}

      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <Gauge className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('quotas.description')}
          </p>
        </div>

        {quotas.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('quotas.noQuota')}</p>
        ) : (
          <div className="space-y-2">
            {quotas.map(q => {
              const used = usageFor(q.scope_type, q.scope_value)
              const pct = q.max_bytes > 0 ? Math.min(100, Math.round((used / q.max_bytes) * 100)) : 0
              const over = pct >= q.warn_percent
              return (
                <div key={q.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                  <Badge variant="outline" className="shrink-0">{q.scope_type}</Badge>
                  <span className="font-mono text-xs flex-1 truncate">{q.scope_value}</span>
                  <span className="text-xs text-muted-foreground shrink-0">
                    {formatBytes(used)} / {formatBytes(q.max_bytes)}
                  </span>
                  <Badge
                    variant="outline"
                    className={over ? 'text-amber-600 dark:text-amber-400 border-amber-500/30 bg-amber-500/10 shrink-0' : 'text-muted-foreground shrink-0'}
                  >
                    {pct}%
                  </Badge>
                  <Button variant="ghost" size="icon-sm" onClick={() => handleReset(q.scope_type, q.scope_value)} title={t('quotas.resetUsage')}>
                    <RotateCcw className="size-4" />
                  </Button>
                  <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(q.id)} title={t('quotas.deleteQuota')}>
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              )
            })}
          </div>
        )}
      </Card>
    </div>
  )
}

function CreateQuotaForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [scopeType, setScopeType] = useState<QuotaScopeType>('repo')
  const [scopeValue, setScopeValue] = useState('')
  const [maxGB, setMaxGB] = useState('10')
  const [warnPercent, setWarnPercent] = useState('90')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!scopeValue.trim()) {
      toast.error(t('quotas.scopeRequired'))
      return
    }
    const maxBytes = Math.round(parseFloat(maxGB) * 1024 * 1024 * 1024)
    if (!(maxBytes > 0)) {
      toast.error(t('quotas.invalidLimit'))
      return
    }
    setBusy(true)
    try {
      await setQuota(scopeType, scopeValue.trim(), maxBytes, parseInt(warnPercent, 10) || 90)
      toast.success(t('quotas.saved'))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('quotas.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-4">
        <select className={inputClass} value={scopeType} onChange={e => setScopeType(e.target.value as QuotaScopeType)}>
          <option value="repo">{t('quotas.repository')}</option>
          <option value="user">{t('quotas.user')}</option>
        </select>
        <input
          className={inputClass}
          placeholder={scopeType === 'repo' ? 'team/api' : 'alice'}
          value={scopeValue}
          onChange={e => setScopeValue(e.target.value)}
        />
        <input className={inputClass} type="number" min="0.01" step="0.01" placeholder={t('quotas.limitGB')} value={maxGB} onChange={e => setMaxGB(e.target.value)} />
        <input className={inputClass} type="number" min="1" max="100" placeholder={t('quotas.warnPercent')} value={warnPercent} onChange={e => setWarnPercent(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy} className="justify-self-start self-center sm:col-span-4">
          <Plus />
          {t('quotas.saveQuota')}
        </Button>
      </form>
    </Card>
  )
}
