import { useState } from 'react'
import { MonitorSmartphone, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { createUser, deleteUser, revokeSession, updateUser, type Role, type SessionInfo, type UserInfo } from '@/api'
import { keys, useInvalidate, useSessions, useUsers } from '@/lib/queries'
import { useSession } from '@/lib/session'
import {
  ConfirmDialog,
  EmptyState,
  Field,
  FormDialog,
  NativeSelect,
  Page,
  PageHeader,
  Panel,
  QueryPanel,
  RoleBadge,
  RowAction,
  Section,
  splitList,
  StatusBadge,
  Timestamp,
} from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const ROLES: Role[] = ['admin', 'pusher', 'reader']
const MIN_PASSWORD = 8

function RoleField({ value, onChange }: { value: Role; onChange: (role: Role) => void }) {
  const { t } = useTranslation()
  return (
    <Field label={t('access.role')} hint={t(`access.roleHint_${value}`)}>
      {ids => (
        <NativeSelect {...ids} value={value} onChange={e => onChange(e.target.value as Role)}>
          {ROLES.map(role => (
            <option key={role} value={role}>
              {t(`roles.${role}`)}
            </option>
          ))}
        </NativeSelect>
      )}
    </Field>
  )
}

function PatternsField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { t } = useTranslation()
  return (
    <Field label={t('access.repositories')} hint={t('access.repositoriesHint')} optional>
      {ids => <Input {...ids} value={value} onChange={e => onChange(e.target.value)} placeholder="team-a/*, shared/base" className="font-mono" />}
    </Field>
  )
}

function CreateUserDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('reader')
  const [patterns, setPatterns] = useState('')

  async function submit() {
    await createUser(username.trim(), password, role, splitList(patterns))
    toast.success(t('access.userCreated', { username: username.trim() }))
    await invalidate(keys.users)
    onClose()
  }

  return (
    <FormDialog
      title={t('access.newUser')}
      description={t('access.newUserIntro')}
      submitLabel={t('access.createUser')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={username.trim() !== '' && password.length >= MIN_PASSWORD}
    >
      <Field label={t('fields.username')}>
        {ids => <Input {...ids} value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" autoCapitalize="off" spellCheck={false} required />}
      </Field>
      <Field label={t('fields.password')} hint={t('access.passwordHint', { min: MIN_PASSWORD })}>
        {ids => (
          <Input {...ids} type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" minLength={MIN_PASSWORD} required />
        )}
      </Field>
      <RoleField value={role} onChange={setRole} />
      <PatternsField value={patterns} onChange={setPatterns} />
    </FormDialog>
  )
}

function EditUserDialog({ user, isSelf, onClose }: { user: UserInfo; isSelf: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [role, setRole] = useState<Role>(user.role)
  const [patterns, setPatterns] = useState(user.repo_patterns.join(', '))
  const [password, setPassword] = useState('')

  const passwordOk = password === '' || password.length >= MIN_PASSWORD

  async function submit() {
    await updateUser(user.username, {
      role,
      repo_patterns: splitList(patterns),
      ...(password ? { password } : {}),
    })
    toast.success(t('access.userUpdated', { username: user.username }))
    await invalidate(keys.users, keys.sessions)
    onClose()
  }

  return (
    <FormDialog
      title={t('access.editUser', { username: user.username })}
      description={t('access.editIntro')}
      submitLabel={t('common.save')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={passwordOk}
    >
      <RoleField value={role} onChange={setRole} />
      <PatternsField value={patterns} onChange={setPatterns} />
      <Field
        label={t('access.newPassword')}
        hint={isSelf ? t('access.newPasswordSelfHint') : t('access.newPasswordHint', { min: MIN_PASSWORD })}
        optional
      >
        {ids => (
          <Input {...ids} type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" aria-invalid={!passwordOk} />
        )}
      </Field>
    </FormDialog>
  )
}

/** "Mozilla/5.0 (…) Chrome/126…" → something a human can scan in a table. */
function describeClient(userAgent: string): string {
  if (!userAgent) return '—'
  if (/dockyard-cli/i.test(userAgent)) return 'dockyard-cli'
  if (/^docker\//i.test(userAgent)) return 'Docker'
  if (/curl\//i.test(userAgent)) return 'curl'
  const browser = /Edg\//.test(userAgent)
    ? 'Edge'
    : /Firefox\//.test(userAgent)
      ? 'Firefox'
      : /Chrome\//.test(userAgent)
        ? 'Chrome'
        : /Safari\//.test(userAgent)
          ? 'Safari'
          : ''
  const os = /Windows/.test(userAgent)
    ? 'Windows'
    : /Mac OS X|Macintosh/.test(userAgent)
      ? 'macOS'
      : /Android/.test(userAgent)
        ? 'Android'
        : /iPhone|iPad/.test(userAgent)
          ? 'iOS'
          : /Linux/.test(userAgent)
            ? 'Linux'
            : ''
  return [browser, os].filter(Boolean).join(' · ') || userAgent.slice(0, 40)
}

export default function AccessPage() {
  const { t } = useTranslation()
  const session = useSession()
  const users = useUsers()
  const sessions = useSessions()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<UserInfo | null>(null)
  const [deleting, setDeleting] = useState<UserInfo | null>(null)
  const [revoking, setRevoking] = useState<SessionInfo | null>(null)

  const sessionCount = (username: string) => sessions.data?.sessions.filter(s => s.username === username).length ?? 0

  return (
    <Page>
      <PageHeader title={t('access.title')} description={t('access.description')} />

      <Section
        title={t('access.users')}
        actions={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('access.newUser')}
          </Button>
        }
      >
        <QueryPanel query={users} skeletonRows={3}>
          {data => (
            <Panel>
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="px-4">{t('fields.username')}</TableHead>
                    <TableHead>{t('access.role')}</TableHead>
                    <TableHead className="hidden md:table-cell">{t('access.repositories')}</TableHead>
                    <TableHead className="hidden text-right lg:table-cell">{t('access.sessionsColumn')}</TableHead>
                    <TableHead className="hidden text-right sm:table-cell">{t('access.created')}</TableHead>
                    <TableHead className="w-20 px-4" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.map(user => {
                    const isSelf = user.username === session.username
                    return (
                      <TableRow key={user.id}>
                        <TableCell className="px-4 font-medium">
                          {user.username}
                          {isSelf && <span className="ml-1.5 font-normal text-muted-foreground">{t('access.you')}</span>}
                        </TableCell>
                        <TableCell>
                          <RoleBadge role={user.role} />
                        </TableCell>
                        <TableCell className="hidden max-w-72 md:table-cell">
                          {user.repo_patterns.length === 0 ? (
                            <span className="text-muted-foreground">{t('access.allRepositories')}</span>
                          ) : (
                            <span className="flex flex-wrap gap-1">
                              {user.repo_patterns.map(pattern => (
                                <StatusBadge key={pattern} mono>
                                  {pattern}
                                </StatusBadge>
                              ))}
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="hidden text-right text-muted-foreground lg:table-cell">{sessionCount(user.username)}</TableCell>
                        <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                          <Timestamp value={user.created_at} />
                        </TableCell>
                        <TableCell className="px-4">
                          <span className="flex justify-end">
                            <RowAction label={t('access.editNamed', { username: user.username })} onClick={() => setEditing(user)}>
                              <Pencil />
                            </RowAction>
                            <RowAction
                              label={isSelf ? t('access.cannotDeleteSelf') : t('access.deleteNamed', { username: user.username })}
                              onClick={() => setDeleting(user)}
                              disabled={isSelf}
                              destructive
                            >
                              <Trash2 />
                            </RowAction>
                          </span>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </Panel>
          )}
        </QueryPanel>
      </Section>

      <Section title={t('access.sessions')} description={t('access.sessionsBody')}>
        <QueryPanel
          query={sessions}
          isEmpty={data => data.sessions.length === 0}
          empty={<EmptyState icon={MonitorSmartphone} title={t('access.noSession')} />}
        >
          {data => (
            <Panel>
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="px-4">{t('fields.username')}</TableHead>
                    <TableHead>{t('access.client')}</TableHead>
                    <TableHead className="hidden md:table-cell">{t('access.ipAddress')}</TableHead>
                    <TableHead className="hidden text-right sm:table-cell">{t('access.signedIn')}</TableHead>
                    <TableHead className="text-right">{t('access.lastSeen')}</TableHead>
                    <TableHead className="w-12 px-4" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.sessions.map(item => {
                    const current = item.id === data.current_id
                    return (
                      <TableRow key={item.id}>
                        <TableCell className="px-4 font-medium">
                          {item.username}
                          {current && <StatusBadge tone="info" className="ml-2">{t('access.thisSession')}</StatusBadge>}
                        </TableCell>
                        <TableCell className="max-w-48 truncate text-muted-foreground" title={item.user_agent}>
                          {describeClient(item.user_agent)}
                        </TableCell>
                        <TableCell className="hidden font-mono text-xs text-muted-foreground md:table-cell">{item.ip || '—'}</TableCell>
                        <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                          <Timestamp value={item.created_at} />
                        </TableCell>
                        <TableCell className="text-right text-xs text-muted-foreground">
                          <Timestamp value={item.last_seen_at} />
                        </TableCell>
                        <TableCell className="px-4 text-right">
                          <RowAction label={t('access.revoke')} onClick={() => setRevoking(item)} destructive>
                            <X />
                          </RowAction>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </Panel>
          )}
        </QueryPanel>
      </Section>

      {creating && <CreateUserDialog onClose={() => setCreating(false)} />}
      {editing && <EditUserDialog user={editing} isSelf={editing.username === session.username} onClose={() => setEditing(null)} />}

      {deleting && (
        <ConfirmDialog
          title={t('access.deleteTitle', { username: deleting.username })}
          description={t('access.deleteBody')}
          confirmLabel={t('access.deleteUser')}
          destructive
          onConfirm={async () => {
            await deleteUser(deleting.username)
            toast.success(t('access.userDeleted', { username: deleting.username }))
            setDeleting(null)
            await invalidate(keys.users, keys.sessions)
          }}
          onCancel={() => setDeleting(null)}
        />
      )}

      {revoking && (
        <ConfirmDialog
          title={t('access.revokeTitle')}
          description={
            revoking.id === sessions.data?.current_id
              ? t('access.revokeSelfBody')
              : t('access.revokeBody', { username: revoking.username })
          }
          confirmLabel={t('access.revoke')}
          destructive
          onConfirm={async () => {
            await revokeSession(revoking.id)
            setRevoking(null)
            await invalidate(keys.sessions)
          }}
          onCancel={() => setRevoking(null)}
        />
      )}
    </Page>
  )
}
