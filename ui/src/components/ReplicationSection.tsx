import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Copy, PlugZap, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { createReplicationTarget, deleteReplicationTarget, testReplicationTarget, type ReplicationTarget } from '@/api'
import { keys, useInvalidate, useReplicationTargets } from '@/lib/queries'
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
  StatusBadge,
} from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

function TargetDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [pattern, setPattern] = useState('*')

  async function submit() {
    await createReplicationTarget(name.trim(), baseUrl.trim(), username.trim(), password, pattern.trim() || '*')
    toast.success(t('replication.created'))
    await invalidate(keys.replication)
    onClose()
  }

  return (
    <FormDialog
      title={t('replication.newTarget')}
      description={t('replication.formIntro')}
      submitLabel={t('replication.create')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={name.trim() !== '' && baseUrl.trim() !== ''}
      className="sm:max-w-lg"
    >
      <div className="grid gap-3 sm:grid-cols-[10rem_1fr]">
        <Field label={t('replication.name')}>
          {ids => <Input {...ids} value={name} onChange={e => setName(e.target.value)} placeholder="dr-site" required />}
        </Field>
        <Field label={t('replication.baseUrl')}>
          {ids => (
            <Input {...ids} type="url" value={baseUrl} onChange={e => setBaseUrl(e.target.value)} placeholder="https://registry.dr.example.com" className="font-mono" required />
          )}
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('fields.username')} optional>
          {ids => <Input {...ids} value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" />}
        </Field>
        <Field label={t('fields.password')} optional>
          {ids => <Input {...ids} type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" />}
        </Field>
      </div>
      <Field label={t('fields.repoPattern')} hint={t('replication.patternHint')}>
        {ids => <Input {...ids} value={pattern} onChange={e => setPattern(e.target.value)} className="font-mono" />}
      </Field>
    </FormDialog>
  )
}

/** Push-based replication: every tag pushed here is copied to each matching
 *  target, blobs and platform manifests included. */
export default function ReplicationSection() {
  const { t } = useTranslation()
  const targets = useReplicationTargets()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<ReplicationTarget | null>(null)

  const test = useMutation({
    mutationFn: (id: number) => testReplicationTarget(id),
    onSuccess: () => toast.success(t('replication.reachable')),
    onError: err => toast.error(errorMessage(err, t('replication.unreachable'))),
  })

  return (
    <Section
      id="replication"
      title={t('replication.title')}
      description={t('replication.description')}
      actions={
        targets.isSuccess && (
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('replication.newTarget')}
          </Button>
        )
      }
    >
      <QueryPanel
        query={targets}
        unavailable={{ title: t('replication.unavailableTitle'), body: t('kit.proxyUnavailable') }}
        isEmpty={data => data.length === 0}
        empty={<EmptyState icon={Copy} title={t('replication.emptyTitle')} description={t('replication.emptyBody')} />}
      >
        {data => (
          <Panel>
            <ul className="divide-y">
              {data.map(target => (
                <li key={target.id} className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2.5">
                  <span className="shrink-0 text-sm font-medium">{target.name}</span>
                  <code className="min-w-0 flex-1 basis-56 truncate text-xs text-muted-foreground" title={target.base_url}>
                    {target.base_url}
                  </code>
                  <span className="flex items-center gap-1.5">
                    <StatusBadge mono>{target.repo_pattern}</StatusBadge>
                    <StatusBadge tone={target.enabled ? 'success' : 'neutral'}>
                      {target.enabled ? t('replication.enabled') : t('replication.disabled')}
                    </StatusBadge>
                  </span>
                  <span className="flex">
                    <RowAction
                      label={t('replication.testConnection')}
                      onClick={() => test.mutate(target.id)}
                      disabled={test.isPending && test.variables === target.id}
                    >
                      {test.isPending && test.variables === target.id ? <Spinner /> : <PlugZap />}
                    </RowAction>
                    <RowAction label={t('replication.deleteTarget')} onClick={() => setToDelete(target)} destructive>
                      <Trash2 />
                    </RowAction>
                  </span>
                </li>
              ))}
            </ul>
          </Panel>
        )}
      </QueryPanel>

      {creating && <TargetDialog onClose={() => setCreating(false)} />}
      {toDelete && (
        <ConfirmDialog
          title={t('replication.deleteTarget')}
          description={t('replication.deleteBody', { name: toDelete.name })}
          confirmLabel={t('common.delete')}
          destructive
          onConfirm={async () => {
            await deleteReplicationTarget(toDelete.id)
            setToDelete(null)
            await invalidate(keys.replication)
          }}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Section>
  )
}
