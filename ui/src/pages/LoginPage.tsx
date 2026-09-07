import { useState, SubmitEvent } from 'react'
import { Box, Eye, EyeOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { login } from '../api'
import { ThemeSwitcher } from '../theme'
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
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 680 420"
      fill="none"
      role="img"
      aria-labelledby="dockyardGlyphTitle dockyardGlyphDesc"
      className={className}
    >
      <title id="dockyardGlyphTitle">Registre d'images conteneurs</title>
      <desc id="dockyardGlyphDesc">
        Un cube isométrique représentant des couches d'image empilées, au centre d'un réseau de nœuds.
      </desc>

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

export default function LoginPage({ onLogin }: Props) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await login(username, password)
      onLogin()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('loginPage.loginFailed'))
    } finally {
      setLoading(false)
    }
  }

  const facts = [t('loginPage.fact1'), t('loginPage.fact2'), t('loginPage.fact3')]

  return (
    <main className="grid min-h-screen lg:grid-cols-[minmax(0,1fr)_minmax(0,1.05fr)] bg-muted/40 dark:bg-background">
      <aside className="hidden flex-col items-center justify-between gap-8 overflow-hidden border-r bg-sidebar p-12 lg:flex">
        <div className="flex items-center gap-3 animate-in fade-in slide-in-from-top-2 duration-500">
          <Box className="size-6 text-blue-500 dark:text-blue-400" strokeWidth={1.5} />
          <span className="font-semibold text-sidebar-foreground tracking-tight">Dockyard</span>
        </div>

        <div className="max-w-md animate-in fade-in slide-in-from-left-4 duration-700 delay-150 fill-mode-both">
          <RegistryGlyph className="mx-auto mb-8 h-auto w-full max-w-[300px] text-muted-foreground" />
          <p className="text-xl font-semibold text-sidebar-foreground">{t('loginPage.heroTitle')}</p>
          <p className="mt-3 text-sm text-muted-foreground">{t('loginPage.heroDescription')}</p>

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
          {t('loginPage.footerCaption')}
        </p>
      </aside>

      <div className="relative flex items-center justify-center p-4">
        <div className="fixed top-4 right-4 w-28">
          <ThemeSwitcher />
        </div>

        <div className="w-full max-w-sm animate-in fade-in slide-in-from-bottom-3 duration-500 delay-150 fill-mode-both">
          <div className="text-center mb-8 lg:hidden">
            <div className="inline-flex items-center justify-center w-12 h-12 rounded-2xl bg-card border mb-4">
              <Box className="size-6 text-blue-500 dark:text-blue-400" strokeWidth={1.5} />
            </div>
            <h1 className="text-xl font-semibold tracking-tight">Dockyard</h1>
            <p className="text-muted-foreground text-sm mt-1">{t('loginPage.subtitle')}</p>
          </div>

          <div className="mb-6 hidden lg:block">
            <h1 className="text-2xl font-semibold tracking-tight">{t('loginPage.formTitle')}</h1>
            <p className="text-muted-foreground text-sm mt-1.5">{t('loginPage.formSubtitle')}</p>
          </div>

          <Card className="[--card-spacing:--spacing(7)] border-t-2 border-t-primary">
            <CardContent>
              <form onSubmit={handleSubmit} className="space-y-5">
                <div className="space-y-1.5">
                  <Label htmlFor="username">{t('loginPage.username')}</Label>
                  <Input
                    id="username"
                    type="text"
                    value={username}
                    onChange={e => setUsername(e.target.value)}
                    placeholder="admin"
                    autoComplete="username"
                    autoFocus
                    required
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="password">{t('loginPage.password')}</Label>
                  <div className="relative">
                    <Input
                      id="password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={e => setPassword(e.target.value)}
                      placeholder="••••••••"
                      autoComplete="current-password"
                      required
                      className="pr-9"
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword(v => !v)}
                      title={showPassword ? t('loginPage.hidePassword') : t('loginPage.showPassword')}
                      aria-label={showPassword ? t('loginPage.hidePassword') : t('loginPage.showPassword')}
                      className="absolute right-0 top-1/2 -translate-y-1/2 flex items-center justify-center size-8 text-muted-foreground/60 hover:text-foreground transition-colors"
                    >
                      {showPassword ? <EyeOff className="size-3.5" strokeWidth={1.5} /> : <Eye className="size-3.5" strokeWidth={1.5} />}
                    </button>
                  </div>
                </div>

                {error && (
                  <p className="text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-lg px-3 py-2">
                    {error}
                  </p>
                )}

                <Button type="submit" size="lg" disabled={loading} className="w-full">
                  {loading ? t('loginPage.signingIn') : t('loginPage.signIn')}
                </Button>
              </form>
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}
