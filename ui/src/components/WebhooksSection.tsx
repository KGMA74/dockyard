import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Plus, Send, Trash2, Webhook } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { createWebhook, deleteWebhook, testWebhook, type RegistryEventType, type WebhookInfo } from '@/api'
import { keys, useInvalidate, useWebhooks } from '@/lib/queries'
import {
  Checkbox,
  ConfirmDialog,
  EmptyState,
  errorMessage,
  Field,
  FormDialog,
  NativeSelect,
  Panel,
  QueryPanel,
  RowAction,
  Section,
  Spinner,
  StatusBadge,
} from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const EVENT_TYPES: RegistryEventType[] = ['push', 'delete', 'retention', 'gc', 'scan', 'import', 'quota_warning']

function WebhookDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [format, setFormat] = useState('generic')
  const [events, setEvents] = useState<string[]>(['push'])

  function toggle(event: string) {
    setEvents(current => (current.includes(event) ? current.filter(e => e !== event) : [...current, event]))
  }

  async function submit() {
    await createWebhook({ url: url.trim(), secret, events, format })
    toast.success(t('webhooks.created'))
    await invalidate(keys.webhooks)
    onClose()
  }

  return (
    <FormDialog
      title={t('webhooks.newWebhook')}
      submitLabel={t('webhooks.create')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={url.trim() !== '' && events.length > 0}
      className="sm:max-w-lg"
    >
      <Field label={t('webhooks.url')}>
        {ids => (
          <Input {...ids} type="url" value={url} onChange={e => setUrl(e.target.value)} placeholder="https://hooks.example.com/dockyard" className="font-mono" required />
        )}
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('webhooks.format')}>
          {ids => (
            <NativeSelect {...ids} value={format} onChange={e => setFormat(e.target.value)}>
              <option value="generic">{t('webhooks.formatGeneric')}</option>
              <option value="slack">Slack</option>
              <option value="discord">Discord</option>
            </NativeSelect>
          )}
        </Field>
        <Field label={t('webhooks.secret')} hint={t('webhooks.secretHint')} optional>
          {ids => <Input {...ids} type="password" autoComplete="new-password" value={secret} onChange={e => setSecret(e.target.value)} />}
        </Field>
      </div>
      <fieldset className="space-y-2">
        <legend className="text-xs font-medium">{t('webhooks.events')}</legend>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-3">
          {EVENT_TYPES.map(event => (
            <label key={event} className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox checked={events.includes(event)} onChange={() => toggle(event)} />
              {t(`eventTypes.${event}`)}
            </label>
          ))}
        </div>
      </fieldset>
    </FormDialog>
  )
}

export default function WebhooksSection() {
  const { t } = useTranslation()
  const hooks = useWebhooks()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<WebhookInfo | null>(null)

  const test = useMutation({
    mutationFn: (id: number) => testWebhook(id),
    onSuccess: () => toast.success(t('webhooks.testDelivered')),
    onError: err => toast.error(errorMessage(err, t('webhooks.testFailed'))),
  })

  return (
    <Section
      id="webhooks"
      title={t('webhooks.title')}
      description={
        <>
          {t('webhooks.descriptionPrefix')} <code className="text-foreground">X-Dockyard-Signature</code> {t('webhooks.descriptionSuffix')}
        </>
      }
      actions={
        hooks.isSuccess && (
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('webhooks.newWebhook')}
          </Button>
        )
      }
    >
      <QueryPanel
        query={hooks}
        unavailable={{ title: t('webhooks.unavailableTitle') }}
        isEmpty={data => data.length === 0}
        empty={<EmptyState icon={Webhook} title={t('webhooks.emptyTitle')} description={t('webhooks.emptyBody')} />}
      >
        {data => (
          <Panel>
            <ul className="divide-y">
              {data.map(hook => (
                <li key={hook.id} className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2.5">
                  <code className="min-w-0 flex-1 basis-64 truncate text-xs font-medium" title={hook.url}>
                    {hook.url}
                  </code>
                  <span className="flex flex-wrap items-center gap-1.5">
                    {hook.format !== 'generic' && <StatusBadge tone="info">{hook.format === 'slack' ? 'Slack' : 'Discord'}</StatusBadge>}
                    {hook.events.map(event => (
                      <StatusBadge key={event}>{t(`eventTypes.${event}`, event)}</StatusBadge>
                    ))}
                  </span>
                  <span className="flex">
                    <RowAction
                      label={t('webhooks.sendTest')}
                      onClick={() => test.mutate(hook.id)}
                      disabled={test.isPending && test.variables === hook.id}
                    >
                      {test.isPending && test.variables === hook.id ? <Spinner /> : <Send />}
                    </RowAction>
                    <RowAction label={t('webhooks.deleteWebhook')} onClick={() => setToDelete(hook)} destructive>
                      <Trash2 />
                    </RowAction>
                  </span>
                </li>
              ))}
            </ul>
          </Panel>
        )}
      </QueryPanel>

      {creating && <WebhookDialog onClose={() => setCreating(false)} />}
      {toDelete && (
        <ConfirmDialog
          title={t('webhooks.deleteWebhook')}
          description={t('webhooks.deleteBody', { url: toDelete.url })}
          confirmLabel={t('common.delete')}
          destructive
          onConfirm={async () => {
            await deleteWebhook(toDelete.id)
            setToDelete(null)
            await invalidate(keys.webhooks)
          }}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Section>
  )
}
