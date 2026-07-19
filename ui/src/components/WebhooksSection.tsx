import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Plus, Send, Trash2, Webhook, X } from 'lucide-react'
import { createWebhook, deleteWebhook, listWebhooks, testWebhook, WebhookInfo } from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const EVENT_TYPES = ['push', 'delete', 'retention', 'gc', 'scan'] as const

// WebhooksSection lives in the Settings tab; hidden for non-admins (403).
export default function WebhooksSection() {
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
      toast.success('Webhook deleted')
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  async function handleTest(id: number) {
    try {
      await testWebhook(id)
      toast.success('Test event delivered')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Test delivery failed')
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          Webhooks
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? 'Cancel' : 'New webhook'}
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
            HTTP notifications on registry events, retried with backoff. Payloads are signed
            with <span className="font-mono">X-Dockyard-Signature</span> when a secret is set.
          </p>
        </div>

        {hooks.length === 0 ? (
          <p className="text-xs text-muted-foreground">No webhook configured.</p>
        ) : (
          <div className="space-y-2">
            {hooks.map(h => (
              <div key={h.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <span className="font-mono text-xs flex-1 truncate" title={h.url}>{h.url}</span>
                <span className="text-xs text-muted-foreground shrink-0">{h.events.join(', ')}</span>
                {h.format !== 'generic' && (
                  <Badge variant="outline" className="text-muted-foreground capitalize">{h.format}</Badge>
                )}
                <Button variant="ghost" size="icon-sm" onClick={() => handleTest(h.id)} title="Send a test event">
                  <Send className="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(h.id)} title="Delete webhook">
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
      toast.success('Webhook created')
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Creation failed')
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} type="url" placeholder="https://hooks.example.com/…" value={url} onChange={e => setUrl(e.target.value)} required />
        <input className={inputClass} placeholder="Signing secret (optional)" value={secret} onChange={e => setSecret(e.target.value)} />
        <select className={inputClass} value={format} onChange={e => setFormat(e.target.value)}>
          <option value="generic">Generic JSON</option>
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
          Create webhook
        </Button>
      </form>
    </Card>
  )
}
