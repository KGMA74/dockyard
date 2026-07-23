import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Plus, ShieldCheck, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  createSigningPolicy, deleteSigningPolicy, getSigningStatus, listSigningPolicies,
  SigningPolicy,
} from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

// SigningPoliciesSection lives in the Settings tab; hidden for non-admins
// (403). Signing itself always happens client-side (cosign CLI) — Dockyard
// only verifies against configured public keys.
export default function SigningPoliciesSection() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<{ enabled: boolean; keys_loaded: number } | null>(null)
  const [policies, setPolicies] = useState<SigningPolicy[] | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(() => {
    getSigningStatus().then(setStatus).catch(() => setStatus(null))
    listSigningPolicies()
      .then(r => setPolicies(r.policies))
      .catch(() => setPolicies(null))
  }, [])

  useEffect(load, [load])

  if (policies === null) return null

  async function handleDelete(id: number) {
    try {
      await deleteSigningPolicy(id)
      toast.success(t('signingPolicies.deleted'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('signingPolicies.deleteFailed'))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
          {t('signingPolicies.title')}
        </h2>
        <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
          {showCreate ? <X /> : <Plus />}
          {showCreate ? t('common.cancel') : t('signingPolicies.newOverride')}
        </Button>
      </div>

      {showCreate && (
        <CreateOverrideForm
          onCreated={() => {
            setShowCreate(false)
            load()
          }}
        />
      )}

      <Card className="p-4 rounded-xl gap-3">
        <div className="flex items-center gap-3">
          <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
            <ShieldCheck className="size-4 text-muted-foreground" strokeWidth={1.5} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('signingPolicies.description')}
          </p>
        </div>

        {status && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">{t('signingPolicies.globalDefault')}</span>
            <Badge variant="outline" className={status.enabled ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10' : 'text-muted-foreground'}>
              {status.enabled ? t('signingPolicies.required') : t('signingPolicies.notRequired')}
            </Badge>
            <span className="text-xs text-muted-foreground">
              · {t('signingPolicies.keysConfigured', { count: status.keys_loaded })}
            </span>
          </div>
        )}

        {policies.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('signingPolicies.noOverride')}</p>
        ) : (
          <div className="space-y-2">
            {policies.map(p => (
              <div key={p.id} className="flex items-center gap-3 bg-muted/50 border rounded-lg px-3 py-2">
                <span className="font-mono text-xs flex-1 truncate">{p.repo_pattern}</span>
                <Badge variant="outline" className={p.required ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10' : 'text-muted-foreground'}>
                  {p.required ? t('signingPolicies.required') : t('signingPolicies.notRequired')}
                </Badge>
                <Button variant="ghost" size="icon-sm" onClick={() => handleDelete(p.id)} title={t('signingPolicies.deleteOverride')}>
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

function CreateOverrideForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [pattern, setPattern] = useState('*')
  const [required, setRequired] = useState(true)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await createSigningPolicy(pattern || '*', required)
      toast.success(t('signingPolicies.created'))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('signingPolicies.createFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} placeholder={t('signingPolicies.patternPlaceholder')} value={pattern} onChange={e => setPattern(e.target.value)} />
        <select className={inputClass} value={required ? 'required' : 'not-required'} onChange={e => setRequired(e.target.value === 'required')}>
          <option value="required">{t('signingPolicies.requireSignature')}</option>
          <option value="not-required">{t('signingPolicies.doNotRequire')}</option>
        </select>
        <Button type="submit" size="sm" disabled={busy} className="justify-self-start self-center">
          <Plus />
          {t('signingPolicies.createOverride')}
        </Button>
      </form>
    </Card>
  )
}
