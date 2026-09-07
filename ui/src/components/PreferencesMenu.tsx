import { SunMoon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ThemeSwitcher } from '../theme'
import { LanguageSwitcher } from '../i18nSwitcher'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

// Quick access to appearance/language next to the notification bell — the
// same controls also live in Settings for discoverability.
export default function PreferencesMenu() {
  const { t } = useTranslation()

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground" title={t('preferences.title')}>
          <SunMoon strokeWidth={1.5} />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 space-y-3">
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t('preferences.appearance')}</p>
          <ThemeSwitcher />
        </div>
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t('preferences.language')}</p>
          <LanguageSwitcher />
        </div>
      </PopoverContent>
    </Popover>
  )
}
