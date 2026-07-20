import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { GitFork, Plug, Plus, Trash2, X } from 'lucide-react'
import {
  createReplicationTarget, deleteReplicationTarget, listReplicationTargets, ReplicationTarget,
  testReplicationTarget,
} from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

// ReplicationSection lives in the Settings tab (admin only, embedded/mirror
// mode — hidden by a 404/403 in proxy mode since the routes aren't
// registered there). Replication is push-based: every tag push is copied to
// each enabled target whose repo_pattern matches.
export default function ReplicationSection() {
  const [targets, setTargets] = useState<ReplicationTarget[] | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(() => {
    listReplicationTargets()
      .then(r => setTargets(r.targets))
      .catch(() => setTargets(null))
  }, [])

  useEffect(load, [load])

  if (targets === null) return null

  async function handleDelete(id: number) {
    try {
      await deleteReplicationTarget(id)
      toast.success('Target deleted')
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  async function handleTest(id: number) {
    try {
      await testReplicationTarget(id)
      toast.success('Target reachable')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Target unreachable')
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          Replication
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? 'Cancel' : 'New target'}
        </Button>
      </div>

      {showCreate && (
        <CreateTargetForm
          onCreated={() => {
            setShowCreate(false)
            load()
          }}
        />
      )}

      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <GitFork className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            Every tag push is copied to each enabled target below whose repository pattern
            matches, including referenced blobs and (for multi-arch images) every platform
            manifest. Delivery retries with backoff if a target is briefly unreachable.
          </p>
        </div>

        {targets.length === 0 ? (
          <p className="text-xs text-muted-foreground">No replication target configured.</p>
        ) : (
          <div className="space-y-2">
            {targets.map(t => (
              <div key={t.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <Badge variant="outline" className={t.enabled ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10 shrink-0' : 'text-muted-foreground shrink-0'}>
                  {t.enabled ? 'enabled' : 'disabled'}
                </Badge>
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium truncate">{t.name}</div>
                  <div className="font-mono text-xs text-muted-foreground truncate">{t.base_url} · {t.repo_pattern}</div>
                </div>
                <Button variant="ghost" size="icon-sm" onClick={() => handleTest(t.id)} title="Test connection">
                  <Plug className="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(t.id)} title="Delete target">
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

function CreateTargetForm({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [repoPattern, setRepoPattern] = useState('*')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !baseUrl.trim()) {
      toast.error('Name and base URL are required')
      return
    }
    setBusy(true)
    try {
      await createReplicationTarget(name.trim(), baseUrl.trim(), username, password, repoPattern || '*')
      toast.success('Target created')
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
        <input className={inputClass} placeholder="Target name (dr-site)" value={name} onChange={e => setName(e.target.value)} />
        <input className={inputClass} placeholder="Base URL (https://dr.example.com)" value={baseUrl} onChange={e => setBaseUrl(e.target.value)} />
        <input className={inputClass} placeholder="Username (optional)" value={username} onChange={e => setUsername(e.target.value)} />
        <input className={inputClass} type="password" placeholder="Password (optional)" value={password} onChange={e => setPassword(e.target.value)} />
        <input className={inputClass} placeholder="Repository pattern (team-a/*, * = all)" value={repoPattern} onChange={e => setRepoPattern(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy} className="justify-self-start self-center">
          <Plus />
          Create target
        </Button>
      </form>
    </Card>
  )
}
