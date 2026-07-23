import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { MonitorSmartphone, Plus, ShieldCheck, Trash2, UserRound, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  createUser, deleteUser, getUsername, listSessions, listUsers, revokeSession, updateUser,
  SessionInfo, UserInfo,
} from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const ROLES = ['admin', 'pusher', 'reader'] as const

const roleBadgeClass: Record<string, string> = {
  admin: 'text-blue-600 dark:text-blue-400 border-blue-500/30 bg-blue-500/10',
  pusher: 'text-amber-600 dark:text-amber-400 border-amber-500/30 bg-amber-500/10',
  reader: 'text-muted-foreground',
}

export default function UsersTab() {
  const { t } = useTranslation()
  const [users, setUsers] = useState<UserInfo[]>([])
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [currentSessionId, setCurrentSessionId] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const me = getUsername()

  const load = useCallback(() => {
    listUsers().then(r => setUsers(r.users)).catch(err => toast.error(String(err)))
    listSessions()
      .then(r => {
        setSessions(r.sessions)
        setCurrentSessionId(r.current_id)
      })
      .catch(() => setSessions([]))
  }, [])

  useEffect(load, [load])

  async function handleDelete(username: string) {
    if (!confirm(t('usersTab.confirmDelete', { username }))) return
    try {
      await deleteUser(username)
      toast.success(t('usersTab.userDeleted', { username }))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('usersTab.deleteFailed'))
    }
  }

  async function handleRoleChange(username: string, role: string, patterns: string[]) {
    try {
      await updateUser(username, { role, repo_patterns: patterns })
      toast.success(t('usersTab.roleChanged', { username, role }))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('usersTab.updateFailed'))
    }
  }

  async function handleRevokeSession(id: number) {
    try {
      await revokeSession(id)
      toast.success(t('usersTab.sessionRevoked'))
      load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('usersTab.revokeFailed'))
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
            {t('sidebar.users')}
          </h2>
          <Button variant="outline" size="sm" onClick={() => setShowCreate(v => !v)}>
            {showCreate ? <X /> : <Plus />}
            {showCreate ? t('common.cancel') : t('usersTab.newUser')}
          </Button>
        </div>

        {showCreate && (
          <CreateUserForm
            onCreated={() => {
              setShowCreate(false)
              load()
            }}
          />
        )}

        <div className="space-y-2">
          {users.map(u => (
            <Card key={u.id} className="p-4 rounded-xl gap-2">
              <div className="flex items-center gap-3">
                <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
                  {u.role === 'admin'
                    ? <ShieldCheck className="size-4 text-muted-foreground" strokeWidth={1.5} />
                    : <UserRound className="size-4 text-muted-foreground" strokeWidth={1.5} />}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">
                    {u.username}
                    {u.username === me && <span className="text-muted-foreground font-normal"> {t('usersTab.you')}</span>}
                  </p>
                  <p className="text-xs text-muted-foreground font-mono truncate">
                    {u.repo_patterns.length > 0 ? u.repo_patterns.join(', ') : t('usersTab.allRepositories')}
                  </p>
                </div>
                <select
                  className="text-xs bg-transparent border rounded-md px-2 py-1"
                  value={u.role}
                  onChange={e => handleRoleChange(u.username, e.target.value, u.repo_patterns)}
                >
                  {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
                </select>
                <Badge variant="outline" className={roleBadgeClass[u.role]}>{u.role}</Badge>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => handleDelete(u.username)}
                  disabled={u.username === me}
                  title={u.username === me ? t('usersTab.cannotDeleteSelf') : t('usersTab.deleteUser')}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      </div>

      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          {t('usersTab.activeSessions')}
        </h2>
        <Card className="p-4 rounded-xl gap-3">
          <div className="flex items-center gap-3">
            <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
              <MonitorSmartphone className="size-4 text-muted-foreground" strokeWidth={1.5} />
            </div>
            <p className="text-xs text-muted-foreground">
              {t('usersTab.revokeDescription')}
            </p>
          </div>
          {sessions.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('usersTab.noSession')}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-left text-muted-foreground border-b">
                    <th className="py-1.5 pr-3 font-medium">{t('usersTab.user')}</th>
                    <th className="py-1.5 pr-3 font-medium">IP</th>
                    <th className="py-1.5 pr-3 font-medium">{t('usersTab.client')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('usersTab.lastSeen')}</th>
                    <th className="py-1.5 font-medium"></th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map(s => (
                    <tr key={s.id} className="border-b last:border-0">
                      <td className="py-1.5 pr-3 font-medium">
                        {s.username}
                        {s.id === currentSessionId && (
                          <span className="text-muted-foreground font-normal"> {t('usersTab.thisSession')}</span>
                        )}
                      </td>
                      <td className="py-1.5 pr-3 font-mono">{s.ip || '—'}</td>
                      <td className="py-1.5 pr-3 text-muted-foreground max-w-48 truncate" title={s.user_agent}>
                        {s.user_agent || '—'}
                      </td>
                      <td className="py-1.5 pr-3 whitespace-nowrap text-muted-foreground">
                        {new Date(s.last_seen_at).toLocaleString()}
                      </td>
                      <td className="py-1.5 text-right">
                        <Button variant="ghost" size="icon-sm" onClick={() => handleRevokeSession(s.id)} title={t('usersTab.revokeSession')}>
                          <X className="size-4" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>
    </div>
  )
}

function CreateUserForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<string>('reader')
  const [patterns, setPatterns] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const repoPatterns = patterns.split(',').map(p => p.trim()).filter(Boolean)
      await createUser(username, password, role, repoPatterns)
      toast.success(t('usersTab.userCreated', { username }))
      onCreated()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('usersTab.createFailed'))
    } finally {
      setBusy(false)
    }
  }

  const inputClass = 'w-full text-sm bg-transparent border rounded-md px-3 py-1.5'
  return (
    <Card className="p-4 rounded-xl mb-3">
      <form onSubmit={submit} className="grid gap-2 sm:grid-cols-2">
        <input className={inputClass} placeholder={t('loginPage.username')} value={username} onChange={e => setUsername(e.target.value)} required />
        <input className={inputClass} type="password" placeholder={t('usersTab.passwordPlaceholder')} value={password} onChange={e => setPassword(e.target.value)} required minLength={8} />
        <select className={inputClass} value={role} onChange={e => setRole(e.target.value)}>
          {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
        </select>
        <input className={inputClass} placeholder={t('usersTab.patternsPlaceholder')} value={patterns} onChange={e => setPatterns(e.target.value)} />
        <Button type="submit" size="sm" disabled={busy} className="sm:col-span-2 justify-self-start">
          <Plus />
          {t('usersTab.createUser')}
        </Button>
      </form>
    </Card>
  )
}
