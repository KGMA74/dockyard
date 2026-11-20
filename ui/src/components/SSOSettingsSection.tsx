import { useState } from 'react'
import { CircleCheck, CircleX, Info, Loader2, Lock, PlugZap, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { testSSOIssuer, updateSSOSettings, type SSOSettings, type SSOSettingsInput, type SSOTestResult } from '@/api'
import { keys, useInvalidate, useSSOSettings } from '@/lib/queries'
import { CopyButton, errorMessage, Field, NativeSelect, Panel, QueryPanel, splitList, StatusBadge, Switch, Timestamp } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const DEFAULT_ROLES = ['reader', 'pusher', 'admin', 'none'] as const
type DefaultRole = (typeof DEFAULT_ROLES)[number]

const isHttpUrl = (value: string) => {
  try {
    const url = new URL(value)
    return (url.protocol === 'https:' || url.protocol === 'http:') && url.host !== ''
  } catch {
    return false
  }
}

/** The form as typed: lists stay strings until saved. */
interface Draft {
  enabled: boolean
  issuer_url: string
  client_id: string
  client_secret: string
  redirect_url: string
  display_name: string
  scopes: string
  username_claim: string
  groups_claim: string
  admin_groups: string
  pusher_groups: string
  reader_groups: string
  default_role: DefaultRole
}

function draftFrom(s: SSOSettings): Draft {
  return {
    enabled: s.enabled,
    issuer_url: s.issuer_url,
    client_id: s.client_id,
    client_secret: '',
    redirect_url: s.redirect_url ?? '',
    display_name: s.display_name,
    scopes: s.scopes.join(' '),
    username_claim: s.username_claim,
    groups_claim: s.groups_claim,
    admin_groups: s.admin_groups.join(', '),
    pusher_groups: s.pusher_groups.join(', '),
    reader_groups: s.reader_groups.join(', '),
    default_role: s.default_role,
  }
}

function TestResult({ result }: { result: SSOTestResult }) {
  const { t } = useTranslation()
  if (!result.ok) {
    return (
      <p className="flex items-start gap-2 text-xs text-destructive">
        <CircleX className="mt-px size-3.5 shrink-0" />
        <span className="min-w-0 break-words">{t('sso.testFailed', { error: result.error })}</span>
      </p>
    )
  }
  const groupsAdvertised = result.claims_supported?.includes('groups') || result.scopes_supported?.includes('groups')
  return (
    <div className="space-y-1 text-xs">
      <p className="flex items-center gap-2 text-success">
        <CircleCheck className="size-3.5 shrink-0" />
        {t('sso.testOk')}
      </p>
      <p className="truncate pl-5.5 text-muted-foreground" title={result.authorization_endpoint}>
        {t('sso.authorizationEndpoint')} <code>{result.authorization_endpoint}</code>
      </p>
      {!groupsAdvertised && <p className="pl-5.5 text-muted-foreground text-pretty">{t('sso.noGroupsAdvertised')}</p>}
    </div>
  )
}

function SSOForm({ settings }: { settings: SSOSettings }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [draft, setDraft] = useState(() => draftFrom(settings))
  const [clearSecret, setClearSecret] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [test, setTest] = useState<SSOTestResult | null>(null)

  const locked = settings.managed_by === 'env'
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft(d => ({ ...d, [key]: value }))

  const issuerOk = draft.issuer_url.trim() === '' || isHttpUrl(draft.issuer_url.trim())
  const redirectOk = draft.redirect_url.trim() === '' || isHttpUrl(draft.redirect_url.trim())
  const ready = draft.issuer_url.trim() !== '' && draft.client_id.trim() !== ''
  const canSave = !locked && issuerOk && redirectOk && (!draft.enabled || ready)
  const noGroups = [draft.admin_groups, draft.pusher_groups, draft.reader_groups].every(v => splitList(v).length === 0)
  const everyoneAdmin = draft.default_role === 'admin' && noGroups

  async function runTest() {
    setTesting(true)
    setTest(null)
    try {
      setTest(await testSSOIssuer(draft.issuer_url.trim()))
    } catch (err) {
      toast.error(errorMessage(err, t('kit.unknownError')))
    } finally {
      setTesting(false)
    }
  }

  async function save() {
    setSaving(true)
    try {
      const input: SSOSettingsInput = {
        enabled: draft.enabled,
        issuer_url: draft.issuer_url.trim(),
        client_id: draft.client_id.trim(),
        redirect_url: draft.redirect_url.trim(),
        display_name: draft.display_name.trim(),
        scopes: draft.scopes.split(/[\s,]+/).filter(Boolean),
        username_claim: draft.username_claim.trim(),
        groups_claim: draft.groups_claim.trim(),
        admin_groups: splitList(draft.admin_groups),
        pusher_groups: splitList(draft.pusher_groups),
        reader_groups: splitList(draft.reader_groups),
        default_role: draft.default_role,
      }
      // Absent keeps the stored secret; "" removes it.
      if (!settings.client_secret_from_env) {
        if (draft.client_secret !== '') input.client_secret = draft.client_secret
        else if (clearSecret) input.client_secret = ''
      }
      await updateSSOSettings(input)
      toast.success(draft.enabled ? t('sso.savedOn') : t('sso.savedOff'))
      await invalidate(keys.ssoSettings, keys.serverConfig)
    } catch (err) {
      toast.error(errorMessage(err, t('kit.unknownError')))
    } finally {
      setSaving(false)
    }
  }

  const secretHint = settings.client_secret_from_env
    ? t('sso.secretFromEnv')
    : settings.client_secret_set && !clearSecret
      ? t('sso.secretStored')
      : t('sso.secretHint')

  return (
    <Panel>
      {locked && (
        <p className="flex items-start gap-2 border-b bg-muted/40 px-4 py-2.5 text-xs text-muted-foreground text-pretty">
          <Lock className="mt-px size-3.5 shrink-0" />
          {t('sso.managedByEnv')}
        </p>
      )}
      {settings.error && (
        <p className="flex items-start gap-2 border-b px-4 py-2.5 text-xs text-destructive text-pretty">
          <TriangleAlert className="mt-px size-3.5 shrink-0" />
          {t('sso.notApplied')}
        </p>
      )}

      <form
        onSubmit={e => {
          e.preventDefault()
          if (canSave) void save()
        }}
      >
        <fieldset disabled={locked || saving} className="divide-y">
          <div className="flex items-start justify-between gap-6 px-4 py-3">
            <div className="min-w-0 space-y-0.5">
              <p className="flex items-center gap-2 text-sm font-medium">
                {t('sso.enable')}
                <StatusBadge tone={settings.enabled && !settings.error ? 'success' : 'neutral'}>
                  {settings.enabled && !settings.error ? t('sso.active') : t('sso.inactive')}
                </StatusBadge>
              </p>
              <p className="text-xs text-muted-foreground text-pretty">{t('sso.enableHint')}</p>
            </div>
            <Switch checked={draft.enabled} onCheckedChange={v => set('enabled', v)} disabled={locked} aria-label={t('sso.enable')} />
          </div>

          <div className="grid gap-4 px-4 py-4 sm:grid-cols-2">
            <div className="space-y-2 sm:col-span-2">
              <Field label={t('sso.issuer')} hint={t('sso.issuerHint')}>
                {ids => (
                  <div className="flex gap-2">
                    <Input
                      {...ids}
                      value={draft.issuer_url}
                      onChange={e => set('issuer_url', e.target.value)}
                      placeholder="https://auth.example.com/application/o/dockyard/"
                      className="font-mono"
                      autoComplete="off"
                      spellCheck={false}
                      aria-invalid={!issuerOk}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => void runTest()}
                      disabled={testing || !isHttpUrl(draft.issuer_url.trim())}
                    >
                      {testing ? <Loader2 className="animate-spin" /> : <PlugZap />}
                      {t('sso.test')}
                    </Button>
                  </div>
                )}
              </Field>
              {test && <TestResult result={test} />}
            </div>

            <Field label={t('sso.clientId')}>
              {ids => (
                <Input {...ids} value={draft.client_id} onChange={e => set('client_id', e.target.value)} className="font-mono" autoComplete="off" spellCheck={false} />
              )}
            </Field>

            <Field label={t('sso.clientSecret')} hint={secretHint} optional>
              {ids => (
                <div className="flex gap-2">
                  <Input
                    {...ids}
                    type="password"
                    value={draft.client_secret}
                    onChange={e => {
                      set('client_secret', e.target.value)
                      setClearSecret(false)
                    }}
                    disabled={settings.client_secret_from_env}
                    placeholder={settings.client_secret_set && !clearSecret ? '••••••••••••' : ''}
                    autoComplete="new-password"
                  />
                  {settings.client_secret_set && !settings.client_secret_from_env && !clearSecret && draft.client_secret === '' && (
                    <Button type="button" variant="ghost" onClick={() => setClearSecret(true)}>
                      {t('sso.removeSecret')}
                    </Button>
                  )}
                </div>
              )}
            </Field>

            <div className="space-y-1.5 sm:col-span-2">
              <p className="text-xs font-medium">{t('sso.callback')}</p>
              <div className="flex items-center gap-1 rounded-lg border bg-muted/50 py-1 pr-1 pl-3">
                <code className="min-w-0 flex-1 truncate text-xs">{settings.callback_url}</code>
                <CopyButton value={settings.callback_url} label={t('sso.copyCallback')} />
              </div>
              <p className="text-xs text-muted-foreground text-pretty">{t('sso.callbackHint')}</p>
            </div>

            <Field label={t('sso.displayName')} hint={t('sso.displayNameHint', { name: draft.display_name.trim() || 'SSO' })}>
              {ids => <Input {...ids} value={draft.display_name} onChange={e => set('display_name', e.target.value)} maxLength={40} />}
            </Field>
          </div>

          <div className="space-y-4 px-4 py-4">
            <div className="space-y-0.5">
              <h3 className="text-sm font-medium">{t('sso.roles')}</h3>
              <p className="text-xs text-muted-foreground text-pretty">{noGroups ? t('sso.rolesManual') : t('sso.rolesFromGroups')}</p>
            </div>
            <div className="grid gap-4 sm:grid-cols-3">
              {(['admin_groups', 'pusher_groups', 'reader_groups'] as const).map(key => (
                <Field key={key} label={t(`sso.${key}`)} optional>
                  {ids => (
                    <Input {...ids} value={draft[key]} onChange={e => set(key, e.target.value)} placeholder={t(`sso.${key}Placeholder`)} className="font-mono" spellCheck={false} />
                  )}
                </Field>
              ))}
            </div>
            <Field label={t('sso.defaultRole')} hint={t('sso.defaultRoleHint')}>
              {ids => (
                <NativeSelect {...ids} value={draft.default_role} onChange={e => set('default_role', e.target.value as DefaultRole)} className="sm:w-64">
                  {DEFAULT_ROLES.map(role => (
                    <option key={role} value={role}>
                      {role === 'none' ? t('sso.roleNone') : t(`roles.${role}`)}
                    </option>
                  ))}
                </NativeSelect>
              )}
            </Field>
            {everyoneAdmin && (
              <p className="flex items-start gap-2 text-xs text-warning text-pretty">
                <TriangleAlert className="mt-px size-3.5 shrink-0" />
                {t('sso.everyoneAdmin')}
              </p>
            )}
          </div>

          <details className="group px-4 py-3">
            <summary className="cursor-pointer select-none text-sm font-medium text-muted-foreground hover:text-foreground">
              {t('sso.advanced')}
            </summary>
            <div className="grid gap-4 pt-4 sm:grid-cols-2">
              <Field label={t('sso.scopes')} hint={t('sso.scopesHint')}>
                {ids => <Input {...ids} value={draft.scopes} onChange={e => set('scopes', e.target.value)} className="font-mono" spellCheck={false} />}
              </Field>
              <Field label={t('sso.redirectUrl')} hint={t('sso.redirectUrlHint')} optional>
                {ids => (
                  <Input
                    {...ids}
                    value={draft.redirect_url}
                    onChange={e => set('redirect_url', e.target.value)}
                    className="font-mono"
                    spellCheck={false}
                    aria-invalid={!redirectOk}
                  />
                )}
              </Field>
              <Field label={t('sso.usernameClaim')} hint={t('sso.usernameClaimHint')}>
                {ids => <Input {...ids} value={draft.username_claim} onChange={e => set('username_claim', e.target.value)} className="font-mono" spellCheck={false} />}
              </Field>
              <Field label={t('sso.groupsClaim')}>
                {ids => <Input {...ids} value={draft.groups_claim} onChange={e => set('groups_claim', e.target.value)} className="font-mono" spellCheck={false} />}
              </Field>
            </div>
          </details>
        </fieldset>

        {!locked && (
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {settings.updated_by ? (
                <>
                  {t('vulnPolicy.lastChange', { who: settings.updated_by })} <Timestamp value={settings.updated_at} />
                </>
              ) : (
                <>
                  <Info className="size-3.5" />
                  {t('sso.appliedAtOnce')}
                </>
              )}
            </p>
            <Button type="submit" size="sm" disabled={!canSave || saving}>
              {saving && <Loader2 className="animate-spin" />}
              {t('common.save')}
            </Button>
          </div>
        )}
      </form>
    </Panel>
  )
}

/** Single sign-on, set up from the console — or shown read-only when the
 *  deployment's OIDC_* variables own it. */
export default function SSOSettingsSection() {
  const { t } = useTranslation()
  const query = useSSOSettings()
  return (
    <QueryPanel query={query} unavailable={{ title: t('sso.unavailable') }} skeletonRows={5}>
      {/* Keyed on the last save, so the form restarts from what was stored. */}
      {settings => <SSOForm key={`${settings.updated_at}-${settings.managed_by}`} settings={settings} />}
    </QueryPanel>
  )
}
