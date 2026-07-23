import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Plus, Send, Trash2, Webhook, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { createWebhook, deleteWebhook, listWebhooks, testWebhook, WebhookInfo } from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const EVENT_TYPES = ['push', 'delete', 'retention', 'gc', 'scan', 'import', 'quota_warning'] as const

// WebhooksSection lives in the Settings tab; hidden for non-admins (403).
export default function WebhooksSection() {
  const { t } = useTranslation()
  const [hooks, setHooks] = useState<WebhookInfo[] | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(() => {
    listWebhooks()
      .then(r => setHooks(r.webhooks))
      .catch(() => setHooks(null))
  }, [])

  useEffect(load, [load])

  if (hooks === null) return null

  async function handleDelete(id: number) {
    try {
      await deleteWebhook(id)
      toast.success(t('webhooks.deleted'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('webhooks.deleteFailed'))
    }
  }

  async function handleTest(id: number) {
    try {
      await testWebhook(id)
      toast.success(t('webhooks.testDelivered'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('webhooks.testFailed'))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          {t('webhooks.title')}
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? t('common.cancel') : t('webhooks.newWebhook')}
        </Button>
      </div>

      {showCreate && (
        <CreateWebhookForm
          onCreated={() => {
            setShowCreate(false)
            load()
          }}
        />
      )}

      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <Webhook className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('webhooks.descriptionPrefix')} <span className="font-mono">X-Dockyard-Signature</span> {t('webhooks.descriptionSuffix')}
          </p>
        </div>

        {hooks.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('webhooks.noWebhook')}</p>
        ) : (
          <div className="space-y-2">
            {hooks.map(h => (
              <div key={h.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <span className="font-mono text-xs flex-1 truncate" title={h.url}>{h.url}</span>
                <span className="text-xs text-muted-foreground shrink-0">{h.events.join(', ')}</span>
                {h.format !== 'generic' && (
                  <Badge variant="outline" className="text-muted-foreground capitalize">{h.format}</Badge>
                )}
                <Button variant="ghost" size="icon-sm" onClick={() => handleTest(h.id)} title={t('webhooks.sendTest')}>
                  <Send className="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(h.id)} title={t('webhooks.deleteWebhook')}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}

function CreateWebhookForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [format, setFormat] = useState('generic')
  const [selected, setSelected] = useState<string[]>(['push'])
  const [busy, setBusy] = useState(false)

  function toggleEvent(ev: string) {
    setSelected(cur => (cur.includes(ev) ? cur.filter(e => e !== ev) : [...cur, ev]))
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await createWebhook({ url, secret, events: selected, format })
      toast.success(t('webhooks.created'))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('webhooks.createFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} type="url" placeholder="https://hooks.example.com/…" value={url} onChange={e => setUrl(e.target.value)} required />
        <input className={inputClass} placeholder={t('webhooks.secretPlaceholder')} value={secret} onChange={e => setSecret(e.target.value)} />
        <select className={inputClass} value={format} onChange={e => setFormat(e.target.value)}>
          <option value="generic">{t('webhooks.formatGeneric')}</option>
          <option value="slack">Slack</option>
          <option value="discord">Discord</option>
        </select>
        <div className="flex items-center gap-3 flex-wrap text-xs">
          {EVENT_TYPES.map(ev => (
            <label key={ev} className="flex items-center gap-1.5 cursor-pointer">
              <input type="checkbox" checked={selected.includes(ev)} onChange={() => toggleEvent(ev)} />
              {ev}
            </label>
          ))}
        </div>
        <Button type="submit" size="sm" disabled={busy || selected.length === 0} className="justify-self-start">
          <Plus />
          {t('webhooks.createWebhook')}
        </Button>
      </form>
    </Card>
  )
}
