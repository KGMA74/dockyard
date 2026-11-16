import { useState } from 'react'
import { Gauge, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { deleteQuota, resetQuotaUsage, setQuota, type Quota, type QuotaScopeType } from '@/api'
import { formatBytes } from '@/lib/format'
import { keys, useInvalidate, useQuotas } from '@/lib/queries'
import { cn } from '@/lib/utils'
import {
  ConfirmDialog,
  EmptyState,
  Field,
  FormDialog,
  NativeSelect,
  Panel,
  QueryPanel,
  RowAction,
  Section,
  StatusBadge,
} from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const GIB = 1024 ** 3

function UsageMeter({ used, max, warnPercent }: { used: number; max: number; warnPercent: number }) {
  const percent = max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0
  const tone = percent >= 100 ? 'bg-destructive' : percent >= warnPercent ? 'bg-warning' : 'bg-primary'
  return (
    <div className="w-full min-w-32 space-y-1 sm:w-56">
      <div className="flex justify-between text-xs tabular-nums">
        <span>
          {formatBytes(used)} <span className="text-muted-foreground">/ {formatBytes(max)}</span>
        </span>
        <span className={cn(percent >= warnPercent ? 'font-medium' : 'text-muted-foreground')}>{percent}%</span>
      </div>
      <div
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="h-1.5 overflow-hidden rounded-full bg-muted"
      >
        <div className={cn('h-full rounded-full', tone)} style={{ width: `${Math.max(percent, used > 0 ? 2 : 0)}%` }} />
      </div>
    </div>
  )
}

function QuotaDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [scopeType, setScopeType] = useState<QuotaScopeType>('repo')
  const [scopeValue, setScopeValue] = useState('')
  const [limitGB, setLimitGB] = useState('')
  const [warnPercent, setWarnPercent] = useState('80')

  const limit = parseFloat(limitGB)
  const warn = parseInt(warnPercent, 10)
  const valid = scopeValue.trim() !== '' && limit > 0 && warn >= 1 && warn <= 100

  async function submit() {
    await setQuota(scopeType, scopeValue.trim(), Math.round(limit * GIB), warn)
    toast.success(t('quotas.saved'))
    await invalidate(keys.quotas)
    onClose()
  }

  return (
    <FormDialog title={t('quotas.newQuota')} submitLabel={t('quotas.save')} onSubmit={submit} onClose={onClose} canSubmit={valid}>
      <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
        <Field label={t('quotas.appliesTo')}>
          {ids => (
            <NativeSelect {...ids} value={scopeType} onChange={e => setScopeType(e.target.value as QuotaScopeType)}>
              <option value="repo">{t('quotas.scope_repo')}</option>
              <option value="user">{t('quotas.scope_user')}</option>
            </NativeSelect>
          )}
        </Field>
        <Field label={scopeType === 'repo' ? t('quotas.repositoryName') : t('quotas.username')}>
          {ids => (
            <Input
              {...ids}
              value={scopeValue}
              onChange={e => setScopeValue(e.target.value)}
              placeholder={scopeType === 'repo' ? 'team/app' : 'alice'}
              className="font-mono"
              required
            />
          )}
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('quotas.limitGB')}>
          {ids => (
            <Input {...ids} type="number" min="0.01" step="0.01" inputMode="decimal" value={limitGB} onChange={e => setLimitGB(e.target.value)} placeholder="50" required />
          )}
        </Field>
        <Field label={t('quotas.warnAt')} hint={t('quotas.warnAtHint')}>
          {ids => (
            <Input {...ids} type="number" min="1" max="100" inputMode="numeric" value={warnPercent} onChange={e => setWarnPercent(e.target.value)} required />
          )}
        </Field>
      </div>
    </FormDialog>
  )
}

// Quotas are enforced when a blob upload completes, in embedded and mirror
// modes. Usage counts bytes pushed — not deduplicated size on disk.
export default function QuotasSection() {
  const { t } = useTranslation()
  const quotas = useQuotas()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<Quota | null>(null)
  const [toReset, setToReset] = useState<Quota | null>(null)

  return (
    <Section
      id="quotas"
      title={t('quotas.title')}
      description={t('quotas.description')}
      actions={
        quotas.isSuccess && (
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('quotas.newQuota')}
          </Button>
        )
      }
    >
      <QueryPanel
        query={quotas}
        unavailable={{ title: t('quotas.unavailableTitle') }}
        isEmpty={data => data.quotas.length === 0}
        empty={<EmptyState icon={Gauge} title={t('quotas.emptyTitle')} description={t('quotas.emptyBody')} />}
      >
        {data => (
          <Panel>
            <ul className="divide-y">
              {data.quotas.map(quota => {
                const used =
                  data.usage.find(u => u.scope_type === quota.scope_type && u.scope_value === quota.scope_value)?.bytes_used ?? 0
                return (
                  <li key={quota.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5">
                    <StatusBadge>{t(`quotas.scope_${quota.scope_type}`)}</StatusBadge>
                    <code className="min-w-0 flex-1 truncate text-xs font-medium">{quota.scope_value}</code>
                    <UsageMeter used={used} max={quota.max_bytes} warnPercent={quota.warn_percent} />
                    <span className="flex">
                      <RowAction label={t('quotas.resetUsage')} onClick={() => setToReset(quota)}>
                        <RotateCcw />
                      </RowAction>
                      <RowAction label={t('quotas.deleteQuota')} onClick={() => setToDelete(quota)} destructive>
                        <Trash2 />
                      </RowAction>
                    </span>
                  </li>
                )
              })}
            </ul>
          </Panel>
        )}
      </QueryPanel>

      {creating && <QuotaDialog onClose={() => setCreating(false)} />}

      {toReset && (
        <ConfirmDialog
          title={t('quotas.resetUsage')}
          description={t('quotas.resetBody', { scope: toReset.scope_value })}
          confirmLabel={t('quotas.resetUsage')}
          onConfirm={async () => {
            await resetQuotaUsage(toReset.scope_type, toReset.scope_value)
            setToReset(null)
            await invalidate(keys.quotas)
          }}
          onCancel={() => setToReset(null)}
        />
      )}
      {toDelete && (
        <ConfirmDialog
          title={t('quotas.deleteQuota')}
          description={t('quotas.deleteBody', { scope: toDelete.scope_value })}
          confirmLabel={t('common.delete')}
          destructive
          onConfirm={async () => {
            await deleteQuota(toDelete.id)
            setToDelete(null)
            await invalidate(keys.quotas)
          }}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Section>
  )
}
