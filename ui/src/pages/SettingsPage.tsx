import { useState } from 'react'
import { BookOpen, Bug, GitFork, KeyRound, LogOut, Monitor, MonitorSmartphone, RefreshCw, Terminal, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { createCLISecret, deleteCLISecret, registryHost, revokeMySession, type ConfigGroupId, type ConfigItem, type SessionInfo } from '@/api'
import { LanguageSwitcher } from '@/i18nSwitcher'
import { clientKind, describeClient } from '@/lib/client'
import { relativeTime } from '@/lib/format'
import { keys, useHealth, useInvalidate, useMe, useMySessions, useServerConfig } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { ThemeSwitcher } from '@/theme'
import ChangePasswordModal from '@/components/ChangePasswordModal'
import SSOSettingsSection from '@/components/SSOSettingsSection'
import {
  Command,
  CopyButton,
  errorMessage,
  ConfirmDialog,
  EmptyState,
  Fact,
  Facts,
  Page,
  PageHeader,
  Panel,
  QueryPanel,
  RoleBadge,
  RowAction,
  StatusBadge,
  type Tone,
} from '@/components/kit'
import { Button } from '@/components/ui/button'

const REPO_URL = 'https://github.com/KGMA74/dockyard'

/** SQLite timestamps come back without a zone; they are UTC. */
const asUtc = (value: string) => (/[zZ]|[+-]\d\d:?\d\d$/.test(value) ? value : value + 'Z')

/** Settings use the full page width like every other page, with the heading
 *  beside the content on wide screens rather than one narrow centred column:
 *  forms stay readable without the page looking shrunk. */
function SettingsSection({
  title,
  description,
  actions,
  children,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="grid gap-x-10 gap-y-3 border-t pt-6 first-of-type:border-t-0 first-of-type:pt-0 lg:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && <p className="text-sm text-muted-foreground text-pretty">{description}</p>}
        {actions && <div className="flex flex-wrap gap-2 pt-2">{actions}</div>}
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  )
}

// ── Account ──────────────────────────────────────────────────────────────────

function AccountCard({ onChangePassword }: { onChangePassword: () => void }) {
  const { t } = useTranslation()
  const session = useSession()
  const username = session.username ?? t('nav.unknownUser')

  return (
    <Panel>
      <div className="flex flex-wrap items-center gap-4 p-4">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted text-lg font-semibold uppercase">
          {username.slice(0, 1)}
        </span>
        <div className="min-w-0 flex-1 space-y-1">
          <p className="flex items-center gap-2">
            <span className="truncate text-base font-semibold">{username}</span>
            <RoleBadge role={session.role} />
            {session.sso && <StatusBadge>{t('settings.ssoAccount')}</StatusBadge>}
          </p>
          <p className="text-xs text-muted-foreground text-pretty">
            {session.sso ? t('settings.ssoHint') : t(`access.roleHint_${session.role}`, '')}
          </p>
        </div>
        {!session.sso && (
          <Button variant="outline" size="sm" onClick={onChangePassword}>
            <KeyRound />
            {t('changePassword.title')}
          </Button>
        )}
      </div>
      <Facts className="border-t">
        <Fact label={t('access.repositories')}>
          {session.repoPatterns.length === 0 ? (
            <span className="text-muted-foreground">{t('access.allRepositories')}</span>
          ) : (
            <span className="flex flex-wrap justify-end gap-1">
              {session.repoPatterns.map(pattern => (
                <StatusBadge key={pattern} mono>
                  {pattern}
                </StatusBadge>
              ))}
            </span>
          )}
        </Fact>
      </Facts>
    </Panel>
  )
}

// ── CLI secret ───────────────────────────────────────────────────────────────

/** A generated docker login password: the only one an SSO account has, and a
 *  revocable stand-in for a real password anywhere else. */
function CLISecretSection() {
  const { t } = useTranslation()
  const session = useSession()
  const me = useMe()
  const invalidate = useInvalidate()
  const [revealed, setRevealed] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<'replace' | 'revoke' | null>(null)
  const host = registryHost()
  const username = session.username ?? '<username>'
  const has = me.data?.has_cli_secret ?? false

  async function issue() {
    const { secret } = await createCLISecret()
    setConfirming(null)
    setRevealed(secret)
    await invalidate(keys.me)
  }

  return (
    <SettingsSection
      title={t('settings.cliSecret')}
      description={session.sso ? t('settings.cliSecretBodySso') : t('settings.cliSecretBody')}
      actions={
        has ? (
          <>
            <Button variant="outline" size="sm" onClick={() => setConfirming('replace')}>
              <RefreshCw />
              {t('settings.cliSecretReplace')}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirming('revoke')}>
              {t('settings.cliSecretRevoke')}
            </Button>
          </>
        ) : (
          <Button size="sm" onClick={() => void issue().catch(err => toast.error(errorMessage(err, t('kit.unknownError'))))}>
            <KeyRound />
            {t('settings.cliSecretCreate')}
          </Button>
        )
      }
    >
      <Panel className="space-y-3 p-4">
        {revealed ? (
          <>
            <p className="flex items-start gap-2 text-xs text-muted-foreground">
              <TriangleAlert className="mt-px size-3.5 shrink-0 text-warning" />
              {t('settings.cliSecretOnce')}
            </p>
            <div className="flex items-center gap-1 rounded-lg border bg-muted/50 py-1 pr-1 pl-3">
              <code className="min-w-0 flex-1 truncate text-xs">{revealed}</code>
              <CopyButton value={revealed} label={t('robots.copySecret')} />
            </div>
            <Command>{`docker login ${host} -u ${username}`}</Command>
          </>
        ) : (
          <p className="flex items-center gap-2 text-sm">
            <StatusBadge tone={has ? 'success' : 'neutral'}>
              {has ? t('settings.cliSecretActive') : t('settings.cliSecretNone')}
            </StatusBadge>
            <span className="text-xs text-muted-foreground">
              {has ? t('settings.cliSecretActiveHint') : t('settings.cliSecretNoneHint')}
            </span>
          </p>
        )}
      </Panel>

      {confirming && (
        <ConfirmDialog
          title={confirming === 'replace' ? t('settings.cliSecretReplaceTitle') : t('settings.cliSecretRevokeTitle')}
          description={t('settings.cliSecretInvalidates')}
          confirmLabel={confirming === 'replace' ? t('settings.cliSecretReplace') : t('settings.cliSecretRevoke')}
          destructive={confirming === 'revoke'}
          onConfirm={async () => {
            if (confirming === 'replace') return issue()
            await deleteCLISecret()
            setConfirming(null)
            setRevealed(null)
            await invalidate(keys.me)
          }}
          onCancel={() => setConfirming(null)}
        />
      )}
    </SettingsSection>
  )
}

// ── Sessions ─────────────────────────────────────────────────────────────────

function SessionRow({ item, current, onSignOut }: { item: SessionInfo; current: boolean; onSignOut: () => void }) {
  const { t } = useTranslation()
  const Icon = clientKind(item.user_agent) === 'cli' ? Terminal : Monitor
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md border text-muted-foreground">
        <Icon className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-2 text-sm font-medium">
          <span className="truncate" title={item.user_agent}>
            {describeClient(item.user_agent)}
          </span>
          {current && <StatusBadge tone="success">{t('access.thisSession')}</StatusBadge>}
        </p>
        <p className="truncate text-xs text-muted-foreground">
          {t('settings.sessionMeta', { ip: item.ip || '—', signedIn: relativeTime(asUtc(item.created_at)) })}
        </p>
      </div>
      <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">
        {t('settings.lastActive', { when: relativeTime(asUtc(item.last_seen_at)) })}
      </span>
      <span className="w-8 shrink-0">
        {!current && (
          <RowAction label={t('settings.signOut')} onClick={onSignOut} destructive>
            <LogOut />
          </RowAction>
        )}
      </span>
    </li>
  )
}

function SessionsSection() {
  const { t } = useTranslation()
  const sessions = useMySessions()
  const invalidate = useInvalidate()
  const [signingOut, setSigningOut] = useState<SessionInfo | null>(null)
  const [signingOutOthers, setSigningOutOthers] = useState(false)

  const data = sessions.data
  // Current session first, then most recently active.
  const ordered = data
    ? [...data.sessions].sort((a, b) =>
        a.id === data.current_id ? -1 : b.id === data.current_id ? 1 : b.last_seen_at.localeCompare(a.last_seen_at),
      )
    : []
  const others = ordered.filter(item => item.id !== data?.current_id)

  return (
    <SettingsSection
      title={t('settings.sessions')}
      description={t('settings.sessionsBody')}
      actions={
        others.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => setSigningOutOthers(true)}>
            <LogOut />
            {t('settings.signOutOthers')}
          </Button>
        )
      }
    >
      <QueryPanel
        query={sessions}
        isEmpty={value => value.sessions.length === 0}
        empty={<EmptyState icon={MonitorSmartphone} title={t('access.noSession')} />}
      >
        {() => (
          <Panel>
            <ul className="divide-y">
              {ordered.map(item => (
                <SessionRow key={item.id} item={item} current={item.id === data?.current_id} onSignOut={() => setSigningOut(item)} />
              ))}
            </ul>
          </Panel>
        )}
      </QueryPanel>

      {signingOut && (
        <ConfirmDialog
          title={t('settings.signOutSessionTitle')}
          description={t('settings.signOutSessionBody', { client: describeClient(signingOut.user_agent) })}
          confirmLabel={t('settings.signOut')}
          destructive
          onConfirm={async () => {
            await revokeMySession(signingOut.id)
            setSigningOut(null)
            await invalidate(keys.mySessions, keys.sessions)
          }}
          onCancel={() => setSigningOut(null)}
        />
      )}

      {signingOutOthers && (
        <ConfirmDialog
          title={t('settings.signOutOthersTitle')}
          description={t('settings.signOutOthersBody', { count: others.length })}
          confirmLabel={t('settings.signOutOthers')}
          destructive
          onConfirm={async () => {
            await Promise.all(others.map(item => revokeMySession(item.id)))
            setSigningOutOthers(false)
            toast.success(t('settings.signedOutOthers', { count: others.length }))
            await invalidate(keys.mySessions, keys.sessions)
          }}
          onCancel={() => setSigningOutOthers(false)}
        />
      )}
    </SettingsSection>
  )
}

// ── Server configuration ─────────────────────────────────────────────────────

// Settings whose "on" (or "off") weakens the registry are flagged, not just shown.
const RISKY_WHEN: Partial<Record<string, boolean>> = { v2_auth: false, anonymous_pull: true }

function ConfigValue({ item }: { item: ConfigItem }) {
  const { t } = useTranslation()
  const { key, value } = item

  if (typeof value === 'boolean') {
    const risky = RISKY_WHEN[key] === value
    const tone: Tone = risky ? 'warning' : value ? 'success' : 'neutral'
    return <StatusBadge tone={tone}>{value ? t('settings.on') : t('settings.off')}</StatusBadge>
  }
  if (Array.isArray(value)) {
    return value.length === 0 ? (
      <span className="text-muted-foreground">{t('settings.none')}</span>
    ) : (
      <code className="text-xs">{value.join(', ')}</code>
    )
  }
  if (key === 'mode') return <>{t(`modes.${value}`, String(value))}</>
  if (key === 'storage_backend') return <>{t(`settings.storageBackends.${value}`, String(value))}</>
  if (key === 'login_rate_limit') return <>{t('settings.perMinute', { count: Number(value) })}</>
  if (key === 'global_rate_limit') return <>{t('settings.perSecond', { count: Number(value) })}</>
  if (key === 'cosign_keys') {
    const count = Number(value)
    return count === 0 ? (
      <span className="text-muted-foreground">{t('settings.none')}</span>
    ) : (
      <>{t('settings.keyCount', { count })}</>
    )
  }
  if ((key === 'tls' || key === 'sso') && value === 'off') return <StatusBadge>{t('settings.off')}</StatusBadge>
  // Go durations ("1h0m0s", "5m0s") without their zero tail: "1h", "5m".
  if (key === 'gc_grace_period' || key === 'mirror_tag_ttl') {
    return <code className="text-xs">{String(value).replace(/(\D)0s$/, '$1').replace(/(\D)0m$/, '$1')}</code>
  }
  return <code className="text-xs">{String(value)}</code>
}

function ServerConfigSection() {
  const { t } = useTranslation()
  const config = useServerConfig(true)

  return (
    <SettingsSection title={t('settings.serverConfig')} description={t('settings.serverConfigBody')}>
      <QueryPanel query={config} skeletonRows={6}>
        {groups => (
          <div className="space-y-4">
            {groups.map(group => (
              <Panel key={group.id}>
                <h3 className="border-b px-4 py-2.5 text-xs font-medium text-muted-foreground">
                  {t(`settings.configGroups.${group.id satisfies ConfigGroupId}`)}
                </h3>
                <dl className="divide-y text-sm">
                  {group.items.map(item => (
                    <div key={item.key} className="flex items-center justify-between gap-4 px-4 py-2.5">
                      <dt className="min-w-0">
                        <span className="block">{t(`settings.configKeys.${item.key}`, item.key)}</span>
                        {item.env && (
                          <code className="block truncate text-[11px] text-muted-foreground" title={item.env}>
                            {item.env}
                          </code>
                        )}
                      </dt>
                      <dd className="min-w-0 max-w-[55%] truncate text-right">
                        <ConfigValue item={item} />
                      </dd>
                    </div>
                  ))}
                </dl>
              </Panel>
            ))}
          </div>
        )}
      </QueryPanel>
    </SettingsSection>
  )
}

// ── Page ─────────────────────────────────────────────────────────────────────

export default function SettingsPage() {
  const { t } = useTranslation()
  const session = useSession()
  const health = useHealth()
  const [changingPassword, setChangingPassword] = useState(false)
  const host = registryHost()
  const username = session.username ?? '<username>'

  return (
    <Page>
      <PageHeader title={t('settings.title')} description={t('settings.description')} />

      <SettingsSection title={t('settings.account')}>
        <AccountCard onChangePassword={() => setChangingPassword(true)} />
      </SettingsSection>

      <SettingsSection title={t('settings.preferences')}>
        <Panel className="grid gap-4 p-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <p className="text-xs font-medium">{t('settings.appearance')}</p>
            <ThemeSwitcher />
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium">{t('settings.language')}</p>
            <LanguageSwitcher />
          </div>
        </Panel>
      </SettingsSection>

      <CLISecretSection />

      <SessionsSection />

      <SettingsSection title={t('settings.cli')} description={t('settings.cliBody')}>
        <Panel className="space-y-1.5 p-4">
          <Command>{`docker login ${host} -u ${username}`}</Command>
          <Command>{`dockyard-cli login ${window.location.origin} -u ${username}`}</Command>
        </Panel>
      </SettingsSection>

      {session.isAdmin && (
        <SettingsSection title={t('sso.title')} description={t('sso.description')}>
          <SSOSettingsSection />
        </SettingsSection>
      )}

      {session.isAdmin && <ServerConfigSection />}

      <SettingsSection title={t('settings.registry')}>
        <Panel>
          <Facts>
            <Fact label={t('settings.address')}>
              <code className="text-xs">{host}</code>
            </Fact>
            <Fact label={t('overview.mode')}>{health.data ? t(`modes.${health.data.mode}`) : '—'}</Fact>
            <Fact label={t('overview.version')}>
              <code className="text-xs">{health.data?.version ?? '—'}</code>
            </Fact>
          </Facts>
          <div className="flex flex-wrap gap-2 border-t px-4 py-3">
            <Button variant="outline" size="sm" asChild>
              <a href={REPO_URL} target="_blank" rel="noopener noreferrer">
                <GitFork />
                {t('settings.source')}
              </a>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href={`${REPO_URL}#readme`} target="_blank" rel="noopener noreferrer">
                <BookOpen />
                {t('settings.docs')}
              </a>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href={`${REPO_URL}/issues`} target="_blank" rel="noopener noreferrer">
                <Bug />
                {t('settings.reportIssue')}
              </a>
            </Button>
          </div>
        </Panel>
      </SettingsSection>

      {changingPassword && <ChangePasswordModal onClose={() => setChangingPassword(false)} />}
    </Page>
  )
}
