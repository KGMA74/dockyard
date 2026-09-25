import { Languages, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useTheme } from '../theme'
import { Button } from '@/components/ui/button'

// Two one-click toggles, not a menu: light/dark flips directly (no
// three-way "system" stop-off here — that's still available in Settings
// for people who want it), and language just alternates EN/FR.
export function ThemeToggle() {
  const { t } = useTranslation()
  const { resolvedDark, setTheme } = useTheme()

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      className="text-muted-foreground"
      onClick={() => setTheme(resolvedDark ? 'light' : 'dark')}
      title={t('theme.switchTo', { theme: t(resolvedDark ? 'theme.light' : 'theme.dark') })}
    >
      {resolvedDark ? <Sun strokeWidth={1.5} /> : <Moon strokeWidth={1.5} />}
    </Button>
  )
}

export function LanguageToggle() {
  const { i18n } = useTranslation()
  const current = i18n.resolvedLanguage === 'fr' ? 'fr' : 'en'
  const next = current === 'en' ? 'fr' : 'en'

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      className="text-muted-foreground"
      onClick={() => i18n.changeLanguage(next)}
      title={next.toUpperCase()}
    >
      <Languages strokeWidth={1.5} />
    </Button>
  )
}
