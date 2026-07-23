import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { GitFork, Plug, Plus, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
  const { t } = useTranslation()
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
      toast.success(t('replication.deleted'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('replication.deleteFailed'))
    }
  }

  async function handleTest(id: number) {
    try {
      await testReplicationTarget(id)
      toast.success(t('replication.reachable'))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('replication.unreachable'))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          {t('replication.title')}
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? t('common.cancel') : t('replication.newTarget')}
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
            {t('replication.description')}
          </p>
        </div>

        {targets.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('replication.noTarget')}</p>
        ) : (
          <div className="space-y-2">
            {targets.map(t2 => (
              <div key={t2.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <Badge variant="outline" className={t2.enabled ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10 shrink-0' : 'text-muted-foreground shrink-0'}>
                  {t2.enabled ? t('replication.enabled') : t('replication.disabled')}
                </Badge>
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium truncate">{t2.name}</div>
                  <div className="font-mono text-xs text-muted-foreground truncate">{t2.base_url} · {t2.repo_pattern}</div>
                </div>
                <Button variant="ghost" size="icon-sm" onClick={() => handleTest(t2.id)} title={t('replication.testConnection')}>
                  <Plug className="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(t2.id)} title={t('replication.deleteTarget')}>
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
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [repoPattern, setRepoPattern] = useState('*')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !baseUrl.trim()) {
      toast.error(t('replication.nameAndUrlRequired'))
      return
    }
    setBusy(true)
    try {
      await createReplicationTarget(name.trim(), baseUrl.trim(), username, password, repoPattern || '*')
      toast.success(t('replication.created'))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('replication.createFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} placeholder={t('replication.namePlaceholder')} value={name} onChange={e => setName(e.target.value)} />
        <input className={inputClass} placeholder={t('replication.urlPlaceholder')} value={baseUrl} onChange={e => setBaseUrl(e.target.value)} />
        <input className={inputClass} placeholder={t('replication.usernamePlaceholder')} value={username} onChange={e => setUsername(e.target.value)} />
        <input className={inputClass} type="password" placeholder={t('replication.passwordPlaceholder')} value={password} onChange={e => setPassword(e.target.value)} />
        <input className={inputClass} placeholder={t('replication.patternPlaceholder')} value={repoPattern} onChange={e => setRepoPattern(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy} className="justify-self-start self-center">
          <Plus />
          {t('replication.createTarget')}
        </Button>
      </form>
    </Card>
  )
}
