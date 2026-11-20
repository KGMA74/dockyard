import { Languages, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useTheme } from '../theme'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

// Two one-click toggles, not a menu: light/dark flips directly (the
// three-way "system" option lives in Settings), and language alternates.
export function ThemeToggle() {
  const { t } = useTranslation()
  const { resolvedDark, setTheme } = useTheme()
  const label = t('theme.switchTo', { theme: t(resolvedDark ? 'theme.light' : 'theme.dark') })

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={() => setTheme(resolvedDark ? 'light' : 'dark')}
          aria-label={label}
        >
          {resolvedDark ? <Sun strokeWidth={1.5} /> : <Moon strokeWidth={1.5} />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

const LANGUAGE_NAMES: Record<string, string> = { en: 'English', fr: 'Français' }

export function LanguageToggle() {
  const { i18n } = useTranslation()
  const current = i18n.resolvedLanguage === 'fr' ? 'fr' : 'en'
  const next = current === 'en' ? 'fr' : 'en'
  // Named in the target language: that is the one its reader understands.
  const label = LANGUAGE_NAMES[next]

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={() => i18n.changeLanguage(next)}
          aria-label={label}
          lang={next}
        >
          <Languages strokeWidth={1.5} />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
