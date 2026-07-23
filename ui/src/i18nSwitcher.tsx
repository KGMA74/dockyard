import { useTranslation } from 'react-i18next'

const options: { value: string; label: string }[] = [
  { value: 'en', label: 'EN' },
  { value: 'fr', label: 'FR' },
]

export function LanguageSwitcher() {
  const { i18n } = useTranslation()
  const current = i18n.resolvedLanguage ?? 'en'

  return (
    <div className="flex items-center gap-0.5 bg-muted border rounded-lg p-0.5">
      {options.map(opt => (
        <button
          key={opt.value}
          onClick={() => i18n.changeLanguage(opt.value)}
          title={opt.label}
          aria-label={opt.label}
          className={`flex-1 py-1.5 rounded-md text-xs font-medium transition-colors ${
            current === opt.value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground/60 hover:text-muted-foreground'
          }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}
