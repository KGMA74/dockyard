import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { CalendarClock, Eye, Play, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { toast } from 'sonner'
import {
  createRetentionPolicy,
  deleteRetentionPolicy,
  runRetention,
  type RetentionPlan,
  type RetentionPolicy,
} from '@/api'
import { keys, useInvalidate, useRetentionPolicies } from '@/lib/queries'
import {
  ConfirmDialog,
  EmptyState,
  errorMessage,
  Field,
  FormDialog,
  Panel,
  QueryPanel,
  RowAction,
  Section,
  Spinner,
  splitList,
  StatusBadge,
} from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

function rules(policy: RetentionPolicy, t: TFunction): string[] {
  const out: string[] = []
  if (policy.keep_n > 0) out.push(t('retention.ruleKeepLast', { count: policy.keep_n }))
  if (policy.unpulled_days > 0) out.push(t('retention.ruleUnpulled', { count: policy.unpulled_days }))
  if (policy.keep_patterns.length > 0) out.push(t('retention.ruleAlwaysKeep', { patterns: policy.keep_patterns.join(', ') }))
  if (policy.protected_tags.length > 0) out.push(t('retention.ruleProtect', { tags: policy.protected_tags.join(', ') }))
  return out
}

function PlanTable({ plan, dryRun }: { plan: RetentionPlan; dryRun: boolean }) {
  const { t } = useTranslation()
  if (plan.delete.length === 0 && plan.skipped.length === 0) {
    return <p className="border-t px-4 py-3 text-sm text-muted-foreground">{t('retention.nothing')}</p>
  }
  return (
    <div className="border-t">
      <p className="px-4 pt-3 text-xs font-medium text-muted-foreground">
        {dryRun ? t('retention.planPreview', { count: plan.delete.length }) : t('retention.planApplied', { count: plan.delete.length })}
      </p>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="px-4">{t('columns.repository')}</TableHead>
            <TableHead>{t('columns.tag')}</TableHead>
            <TableHead>{t('retention.outcome')}</TableHead>
            <TableHead className="px-4">{t('retention.reason')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {plan.delete.map(item => (
            <TableRow key={`d-${item.repo}-${item.tag}`}>
              <TableCell className="px-4 font-mono text-xs">{item.repo}</TableCell>
              <TableCell className="font-mono text-xs">{item.tag}</TableCell>
              <TableCell>
                <StatusBadge tone="danger">{dryRun ? t('retention.wouldDelete') : t('retention.deleted')}</StatusBadge>
              </TableCell>
              <TableCell className="px-4 text-xs whitespace-normal text-muted-foreground">{item.reason}</TableCell>
            </TableRow>
          ))}
          {plan.skipped.map(item => (
            <TableRow key={`s-${item.repo}-${item.tag}`}>
              <TableCell className="px-4 font-mono text-xs">{item.repo}</TableCell>
              <TableCell className="font-mono text-xs">{item.tag}</TableCell>
              <TableCell>
                <StatusBadge>{t('retention.kept')}</StatusBadge>
              </TableCell>
              <TableCell className="px-4 text-xs whitespace-normal text-muted-foreground">{item.reason}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function CreatePolicyDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [pattern, setPattern] = useState('*')
  const [keepN, setKeepN] = useState('')
  const [unpulledDays, setUnpulledDays] = useState('')
  const [keepPatterns, setKeepPatterns] = useState('')
  const [protectedTags, setProtectedTags] = useState('latest')

  const keep = parseInt(keepN, 10) || 0
  const days = parseInt(unpulledDays, 10) || 0

  async function submit() {
    await createRetentionPolicy({
      repo_pattern: pattern.trim() || '*',
      keep_n: keep,
      unpulled_days: days,
      keep_patterns: splitList(keepPatterns),
      protected_tags: splitList(protectedTags),
    })
    toast.success(t('retention.created'))
    await invalidate(keys.retention)
    onClose()
  }

  return (
    <FormDialog
      title={t('retention.newPolicy')}
      description={t('retention.formIntro')}
      submitLabel={t('retention.create')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={keep > 0 || days > 0}
      className="sm:max-w-lg"
    >
      <Field label={t('fields.repoPattern')} hint={t('fields.repoPatternHint')}>
        {ids => <Input {...ids} value={pattern} onChange={e => setPattern(e.target.value)} className="font-mono" required />}
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('retention.keepN')} hint={t('retention.keepNHint')}>
          {ids => <Input {...ids} type="number" min="0" inputMode="numeric" value={keepN} onChange={e => setKeepN(e.target.value)} placeholder="0" />}
        </Field>
        <Field label={t('retention.unpulledDays')} hint={t('retention.unpulledDaysHint')}>
          {ids => (
            <Input {...ids} type="number" min="0" inputMode="numeric" value={unpulledDays} onChange={e => setUnpulledDays(e.target.value)} placeholder="0" />
          )}
        </Field>
      </div>
      <Field label={t('retention.keepPatterns')} hint={t('retention.keepPatternsHint')} optional>
        {ids => <Input {...ids} value={keepPatterns} onChange={e => setKeepPatterns(e.target.value)} placeholder="v*, release-*" className="font-mono" />}
      </Field>
      <Field label={t('retention.protectedTags')} hint={t('retention.protectedTagsHint')} optional>
        {ids => <Input {...ids} value={protectedTags} onChange={e => setProtectedTags(e.target.value)} placeholder="latest, stable" className="font-mono" />}
      </Field>
      {keep === 0 && days === 0 && <p className="text-xs text-muted-foreground">{t('retention.needOneRule')}</p>}
    </FormDialog>
  )
}

export default function RetentionSection() {
  const { t } = useTranslation()
  const policies = useRetentionPolicies()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<RetentionPolicy | null>(null)
  const [applying, setApplying] = useState(false)
  const [outcome, setOutcome] = useState<{ plan: RetentionPlan; dryRun: boolean } | null>(null)

  const preview = useMutation({
    mutationFn: () => runRetention(true),
    onSuccess: result => setOutcome({ plan: result.plan, dryRun: true }),
    onError: err => toast.error(errorMessage(err, t('retention.runFailed'))),
  })

  async function apply() {
    const result = await runRetention(false)
    setOutcome({ plan: result.plan, dryRun: false })
    setApplying(false)
    toast.success(t('retention.applied', { count: result.deleted }))
    await invalidate(keys.repositories, ['tags'])
  }

  const hasPolicies = (policies.data?.length ?? 0) > 0

  return (
    <Section
      id="retention"
      title={t('retention.title')}
      description={t('retention.description')}
      actions={
        policies.isSuccess && (
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('retention.newPolicy')}
          </Button>
        )
      }
    >
      <QueryPanel
        query={policies}
        unavailable={{ title: t('retention.unavailableTitle'), body: t('kit.proxyUnavailable') }}
        isEmpty={data => data.length === 0}
        empty={<EmptyState icon={CalendarClock} title={t('retention.emptyTitle')} description={t('retention.emptyBody')} />}
      >
        {data => (
          <Panel>
            <ul className="divide-y">
              {data.map(policy => (
                <li key={policy.id} className="flex items-center gap-3 px-4 py-2.5">
                  <code className="shrink-0 text-xs font-medium">{policy.repo_pattern}</code>
                  <span className="flex min-w-0 flex-1 flex-wrap gap-1.5">
                    {rules(policy, t).map(rule => (
                      <StatusBadge key={rule}>{rule}</StatusBadge>
                    ))}
                  </span>
                  <RowAction label={t('retention.deletePolicy')} onClick={() => setToDelete(policy)} destructive>
                    <Trash2 />
                  </RowAction>
                </li>
              ))}
            </ul>
            {hasPolicies && (
              <div className="flex flex-wrap gap-2 border-t px-4 py-3">
                <Button variant="outline" size="sm" onClick={() => preview.mutate()} disabled={preview.isPending}>
                  {preview.isPending ? <Spinner /> : <Eye />}
                  {t('retention.preview')}
                </Button>
                <Button variant="secondary" size="sm" onClick={() => setApplying(true)}>
                  <Play />
                  {t('retention.applyNow')}
                </Button>
              </div>
            )}
            {outcome && <PlanTable plan={outcome.plan} dryRun={outcome.dryRun} />}
          </Panel>
        )}
      </QueryPanel>

      {creating && <CreatePolicyDialog onClose={() => setCreating(false)} />}

      {applying && (
        <ConfirmDialog
          title={t('retention.applyTitle')}
          description={t('retention.applyBody')}
          confirmLabel={t('retention.applyNow')}
          destructive
          onConfirm={apply}
          onCancel={() => setApplying(false)}
        />
      )}

      {toDelete && (
        <ConfirmDialog
          title={t('retention.deletePolicy')}
          description={t('retention.deletePolicyBody', { pattern: toDelete.repo_pattern })}
          confirmLabel={t('common.delete')}
          destructive
          onConfirm={async () => {
            await deleteRetentionPolicy(toDelete.id)
            setToDelete(null)
            await invalidate(keys.retention)
          }}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Section>
  )
}
