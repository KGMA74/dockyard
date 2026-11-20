import { useEffect, useState, SubmitEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Eye, EyeOff, KeyRound } from 'lucide-react'
import { DockyardMark } from '@/components/Logo'
import { useTranslation } from 'react-i18next'
import { ApiError, completeSSO, getSSOInfo, login, ssoLoginUrl } from '../api'
import { ThemeSwitcher } from '../theme'
import { LanguageSwitcher } from '../i18nSwitcher'
import { Spinner } from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface Props {
  onLogin: () => void
}

// A stack of three layers converging into one image — the same shape as a
// Docker image manifest — set inside a hex "package" outline. Ringed by the
// same dashed-orbit / grid motif a distributed system diagram would use,
// since a registry's job is serving that stack out to many pullers at once.
function RegistryGlyph({ className }: { className?: string }) {
  const { t } = useTranslation()
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 680 420"
      fill="none"
      role="img"
      aria-labelledby="dockyardGlyphTitle dockyardGlyphDesc"
      className={className}
    >
      <title id="dockyardGlyphTitle">{t('login.glyphTitle')}</title>
      <desc id="dockyardGlyphDesc">{t('login.glyphDescription')}</desc>

      <g stroke="currentColor" strokeWidth="1" opacity="0.28" className="animate-orbit-slow">
        <ellipse cx="340" cy="210" rx="286" ry="164" strokeDasharray="5 7" />
        <ellipse cx="340" cy="210" rx="240" ry="132" strokeDasharray="2 8" />
        <path
          d="M152 176 L70 118 M152 244 L70 302 M528 176 L610 118 M528 244 L610 302 M340 116 L340 52 M340 304 L340 368"
          strokeDasharray="4 4"
        />
      </g>

      {/* isometric cube: top / right / left faces, each a separate image layer */}
      <g className="animate-cube-breathe">
        <path d="M340 80 L452.6 145 L340 210 L227.4 145 Z" className="fill-primary" opacity="0.6" />
        <path d="M452.6 145 L452.6 275 L340 340 L340 210 Z" className="fill-primary" opacity="0.35" />
        <path d="M227.4 145 L227.4 275 L340 340 L340 210 Z" className="fill-primary" opacity="0.2" />
        <path
          d="M340 80 L452.6 145 L452.6 275 L340 340 L227.4 275 L227.4 145 Z"
          className="stroke-primary"
          strokeWidth="3"
          strokeLinejoin="round"
        />
        <path d="M340 210 L340 80 M340 210 L227.4 275 M340 210 L452.6 275" className="stroke-primary" strokeWidth="1.5" opacity="0.6" />
      </g>

      <g className="fill-primary">
        <circle cx="70" cy="118" r="7" />
        <circle cx="70" cy="302" r="7" />
        <circle cx="610" cy="118" r="7" />
        <circle cx="610" cy="302" r="7" />
      </g>
      <g fill="currentColor" opacity="0.7">
        <circle cx="340" cy="52" r="7" />
        <circle cx="340" cy="368" r="7" />
      </g>
      <g fill="currentColor" opacity="0.45">
        <circle cx="126" cy="210" r="4" />
        <circle cx="554" cy="210" r="4" />
      </g>
    </svg>
  )
}

const SSO_ERRORS = [
  'provider_denied',
  'expired',
  'provider_unreachable',
  'exchange_failed',
  'invalid_token',
  'not_allowed',
  'username_invalid',
  'username_taken',
  'session_failed',
] as const

/** What the SSO callback left in the URL fragment, read once and erased so a
 *  reload or a shared link never replays it. */
function takeSSOResult(): { code?: string; returnTo: string; error?: string } {
  const params = new URLSearchParams(window.location.hash.slice(1))
  const code = params.get('oidc_code') ?? undefined
  const error = params.get('oidc_error') ?? undefined
  // Signing in from a deep link comes back to it.
  const ret = params.get('return') ?? window.location.pathname + window.location.search
  const returnTo = ret.startsWith('/') && !ret.startsWith('//') ? ret : '/'
  if (code || error) window.history.replaceState(null, '', code ? returnTo : '/')
  return { code, returnTo, error }
}

export default function LoginPage({ onLogin }: Props) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [sso] = useState(takeSSOResult)
  const [error, setError] = useState(() =>
    sso.error ? t(`login.sso.${(SSO_ERRORS as readonly string[]).includes(sso.error) ? sso.error : 'session_failed'}`) : '',
  )
  const [loading, setLoading] = useState(false)
  const [completing, setCompleting] = useState(Boolean(sso.code))
  const ssoInfo = useQuery({ queryKey: ['sso-info'], queryFn: getSSOInfo, staleTime: Infinity, retry: false })

  // Back from the identity provider: finish signing in.
  useEffect(() => {
    if (!sso.code) return
    completeSSO(sso.code)
      .then(onLogin)
      .catch(() => {
        window.history.replaceState(null, '', '/')
        setError(t('login.sso.expired'))
        setCompleting(false)
      })
  }, [sso.code, onLogin, t])

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await login(username, password)
      onLogin()
    } catch (err) {
      // Say what actually happened: a wrong password, a lockout and an
      // unreachable server call for three different reactions.
      if (err instanceof ApiError && err.status === 401) setError(t('login.invalidCredentials'))
      else if (err instanceof ApiError && err.status === 429) setError(t('login.rateLimited'))
      else if (err instanceof ApiError) setError(t('login.serverError', { status: err.status }))
      else setError(t('login.unreachable'))
    } finally {
      setLoading(false)
    }
  }

  const facts = [t('login.fact1'), t('login.fact2'), t('login.fact3')]

  return (
    <main className="grid min-h-screen lg:grid-cols-[minmax(0,1fr)_minmax(0,1.05fr)] bg-muted/40 dark:bg-background">
      <aside className="hidden flex-col items-center justify-between gap-8 overflow-hidden border-r bg-sidebar p-12 lg:flex">
        <div className="flex items-center gap-3 animate-in fade-in slide-in-from-top-2 duration-500">
          <DockyardMark className="size-6 text-sidebar-foreground" />
          <span className="font-semibold text-sidebar-foreground tracking-tight">Dockyard</span>
        </div>

        <div className="max-w-md animate-in fade-in slide-in-from-left-4 duration-700 delay-150 fill-mode-both">
          <RegistryGlyph className="mx-auto mb-8 h-auto w-full max-w-[300px] text-muted-foreground" />
          <p className="text-xl font-semibold text-sidebar-foreground">{t('login.heroTitle')}</p>
          <p className="mt-3 text-sm text-muted-foreground">{t('login.heroDescription')}</p>

          <ul className="mt-6 flex flex-col gap-2">
            {facts.map(fact => (
              <li key={fact} className="flex items-baseline gap-2.5 font-mono text-xs text-muted-foreground">
                <span className="text-primary" aria-hidden="true">·</span>
                {fact}
              </li>
            ))}
          </ul>
        </div>

        <p className="font-mono text-[11px] text-muted-foreground animate-in fade-in duration-700 delay-300 fill-mode-both">
          {t('login.footerCaption')}
        </p>
      </aside>

      <div className="relative flex items-center justify-center p-4">
        <div className="fixed top-4 right-4 flex gap-2">
          <div className="w-20">
            <LanguageSwitcher />
          </div>
          <div className="w-28">
            <ThemeSwitcher />
          </div>
        </div>

        <div className="w-full max-w-sm animate-in fade-in slide-in-from-bottom-3 duration-500 delay-150 fill-mode-both">
          <div className="text-center mb-8 lg:hidden">
            <div className="inline-flex items-center justify-center w-12 h-12 rounded-2xl bg-card border mb-4">
              <DockyardMark className="size-6 text-foreground" />
            </div>
            <h1 className="text-xl font-semibold tracking-tight">Dockyard</h1>
            <p className="text-muted-foreground text-sm mt-1">{t('login.subtitle')}</p>
          </div>

          <div className="mb-6 hidden lg:block">
            <h1 className="text-2xl font-semibold tracking-tight">{t('login.formTitle')}</h1>
            <p className="text-muted-foreground text-sm mt-1.5">{t('login.formSubtitle')}</p>
          </div>

          <Card className="[--card-spacing:--spacing(7)] border-t-2 border-t-primary">
            <CardContent>
              {completing ? (
                <p className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground" role="status">
                  <Spinner />
                  {t('login.sso.completing')}
                </p>
              ) : (
              <>
              {ssoInfo.data?.enabled && (
                <div className="mb-5 space-y-5">
                  <Button asChild variant="outline" size="lg" className="w-full">
                    <a href={ssoLoginUrl(sso.returnTo)}>
                      <KeyRound />
                      {t('login.sso.signInWith', { name: ssoInfo.data.name ?? 'SSO' })}
                    </a>
                  </Button>
                  <div className="flex items-center gap-3 text-xs text-muted-foreground" aria-hidden="true">
                    <span className="h-px flex-1 bg-border" />
                    {t('login.sso.or')}
                    <span className="h-px flex-1 bg-border" />
                  </div>
                </div>
              )}
              <form onSubmit={handleSubmit} className="space-y-5">
                <div className="space-y-1.5">
                  <Label htmlFor="username">{t('login.username')}</Label>
                  <Input
                    id="username"
                    type="text"
                    value={username}
                    onChange={e => setUsername(e.target.value)}
                                        autoComplete="username"
                    autoFocus
                    required
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="password">{t('login.password')}</Label>
                  <div className="relative">
                    <Input
                      id="password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={e => setPassword(e.target.value)}
                      autoComplete="current-password"
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword(v => !v)}
                      title={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                      aria-label={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                      className="absolute right-0 top-1/2 -translate-y-1/2 flex items-center justify-center size-8 text-muted-foreground/60 hover:text-foreground transition-colors"
                    >
                      {showPassword ? <EyeOff className="size-3.5" strokeWidth={1.5} /> : <Eye className="size-3.5" strokeWidth={1.5} />}
                    </button>
                  </div>
                </div>

                {error && (
                  <p role="alert" className="text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-lg px-3 py-2">
                    {error}
                  </p>
                )}

                <Button type="submit" size="lg" disabled={loading} className="w-full">
                  {loading && <Spinner />}
                  {loading ? t('login.signingIn') : t('login.signIn')}
                </Button>
              </form>
              </>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}
