import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { CalendarClock, Eye, Play, Plus, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  createRetentionPolicy, deleteRetentionPolicy, listRetentionPolicies, runRetention,
  RetentionPlan, RetentionPolicy,
} from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

function ruleSummary(p: RetentionPolicy, t: TFunction): string {
  const rules: string[] = []
  if (p.keep_n > 0) rules.push(t('retention.ruleKeepLast', { n: p.keep_n }))
  if (p.unpulled_days > 0) rules.push(t('retention.ruleDropAfter', { days: p.unpulled_days }))
  if (p.keep_patterns.length > 0) rules.push(t('retention.ruleAlwaysKeep', { patterns: p.keep_patterns.join(', ') }))
  if (p.protected_tags.length > 0) rules.push(t('retention.ruleProtect', { tags: p.protected_tags.join(', ') }))
  return rules.join(' · ')
}

// RetentionSection lives inside the Storage tab, next to the GC it feeds.
// Hidden for non-admins (the list call 403s) and in proxy mode (501/404).
export default function RetentionSection() {
  const { t } = useTranslation()
  const [policies, setPolicies] = useState<RetentionPolicy[] | null>(null)
  const [plan, setPlan] = useState<RetentionPlan | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    listRetentionPolicies()
      .then(r => setPolicies(r.policies))
      .catch(() => setPolicies(null))
  }, [])

  useEffect(load, [load])

  if (policies === null) return null

  async function handleRun(dryRun: boolean) {
    setBusy(true)
    try {
      const result = await runRetention(dryRun)
      setPlan(result.plan)
      if (dryRun) {
        toast.info(
          result.plan.delete.length === 0
            ? t('retention.nothingToClean')
            : t('retention.previewCount', { count: result.plan.delete.length }),
        )
      } else {
        toast.success(t('retention.applied', { count: result.deleted }))
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('retention.runFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function handleDeletePolicy(id: number) {
    try {
      await deleteRetentionPolicy(id)
      toast.success(t('retention.deleted'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('retention.deleteFailed'))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          {t('retention.title')}
        </h3>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? t('common.cancel') : t('retention.newPolicy')}
        </Button>
      </div>

      {showCreate && (
        <CreatePolicyForm
          onCreated={() => {
            setShowCreate(false)
            load()
          }}
        />
      )}

      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <CalendarClock className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('retention.description')}
          </p>
        </div>

        {policies.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('retention.noPolicy')}</p>
        ) : (
          <div className="space-y-2">
            {policies.map(p => (
              <div key={p.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <span className="font-mono text-xs shrink-0">{p.repo_pattern}</span>
                <span className="text-xs text-muted-foreground flex-1 truncate">{ruleSummary(p, t)}</span>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDeletePolicy(p.id)} title={t('retention.deletePolicy')}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}
          </div>
        )}

        {policies.length > 0 && (
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => handleRun(true)} disabled={busy}>
              <Eye />
              {t('retention.previewPlan')}
            </Button>
            <Button variant="secondary" size="sm" onClick={() => handleRun(false)} disabled={busy}>
              <Play />
              {busy ? t('storageTab.running') : t('retention.applyNow')}
            </Button>
          </div>
        )}

        {plan && (plan.delete.length > 0 || plan.skipped.length > 0) && (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-muted-foreground border-b">
                  <th className="py-1.5 pr-3 font-medium">{t('denseRepoView.repository')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('denseRepoView.tag')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('retention.outcome')}</th>
                  <th className="py-1.5 font-medium">{t('retention.reason')}</th>
                </tr>
              </thead>
              <tbody>
                {plan.delete.map(c => (
                  <tr key={`d-${c.repo}-${c.tag}`} className="border-b last:border-0">
                    <td className="py-1.5 pr-3 font-mono">{c.repo}</td>
                    <td className="py-1.5 pr-3 font-mono">{c.tag}</td>
                    <td className="py-1.5 pr-3">
                      <Badge variant="outline" className="text-destructive border-destructive/30 bg-destructive/10">
                        {t('common.delete').toLowerCase()}
                      </Badge>
                    </td>
                    <td className="py-1.5 text-muted-foreground">{c.reason}</td>
                  </tr>
                ))}
                {plan.skipped.map(s => (
                  <tr key={`s-${s.repo}-${s.tag}`} className="border-b last:border-0">
                    <td className="py-1.5 pr-3 font-mono">{s.repo}</td>
                    <td className="py-1.5 pr-3 font-mono">{s.tag}</td>
                    <td className="py-1.5 pr-3">
                      <Badge variant="outline" className="text-muted-foreground">{t('retention.skipped')}</Badge>
                    </td>
                    <td className="py-1.5 text-muted-foreground">{s.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}

function CreatePolicyForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [pattern, setPattern] = useState('*')
  const [keepN, setKeepN] = useState('')
  const [unpulledDays, setUnpulledDays] = useState('')
  const [keepPatterns, setKeepPatterns] = useState('')
  const [protectedTags, setProtectedTags] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const csv = (raw: string) => raw.split(',').map(s => s.trim()).filter(Boolean)
      await createRetentionPolicy({
        repo_pattern: pattern || '*',
        keep_n: parseInt(keepN, 10) || 0,
        unpulled_days: parseInt(unpulledDays, 10) || 0,
        keep_patterns: csv(keepPatterns),
        protected_tags: csv(protectedTags),
      })
      toast.success(t('retention.created'))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('retention.createFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} placeholder={t('retention.patternPlaceholder')} value={pattern} onChange={e => setPattern(e.target.value)} />
        <input className={inputClass} type="number" min="0" placeholder={t('retention.keepNPlaceholder')} value={keepN} onChange={e => setKeepN(e.target.value)} />
        <input className={inputClass} type="number" min="0" placeholder={t('retention.unpulledDaysPlaceholder')} value={unpulledDays} onChange={e => setUnpulledDays(e.target.value)} />
        <input className={inputClass} placeholder={t('retention.keepPatternsPlaceholder')} value={keepPatterns} onChange={e => setKeepPatterns(e.target.value)} />
        <input className={inputClass} placeholder={t('retention.protectedTagsPlaceholder')} value={protectedTags} onChange={e => setProtectedTags(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy} className="justify-self-start self-center">
          <Plus />
          {t('retention.createPolicy')}
        </Button>
      </form>
    </Card>
  )
}
