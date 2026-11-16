import { useState } from 'react'
import { BookOpen, Bug, GitFork, KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { registryHost } from '@/api'
import { LanguageSwitcher } from '@/i18nSwitcher'
import { useHealth } from '@/lib/queries'
import { useSession } from '@/lib/session'
import { ThemeSwitcher } from '@/theme'
import ChangePasswordModal from '@/components/ChangePasswordModal'
import { Command, Fact, Facts, Page, PageHeader, Panel, RoleBadge, Section, StatusBadge } from '@/components/kit'
import { Button } from '@/components/ui/button'

const REPO_URL = 'https://github.com/KGMA74/dockyard'

export default function SettingsPage() {
  const { t } = useTranslation()
  const session = useSession()
  const health = useHealth()
  const [changingPassword, setChangingPassword] = useState(false)
  const host = registryHost()

  return (
    <Page className="max-w-3xl">
      <PageHeader title={t('settings.title')} description={t('settings.description')} />

      <Section title={t('settings.account')}>
        <Panel>
          <Facts>
            <Fact label={t('fields.username')}>
              <span className="font-medium">{session.username ?? t('nav.unknownUser')}</span>
            </Fact>
            <Fact label={t('access.role')}>
              <span className="inline-flex items-center gap-2">
                <span className="hidden text-xs text-muted-foreground sm:inline">{t(`access.roleHint_${session.role}`, '')}</span>
                <RoleBadge role={session.role} />
              </span>
            </Fact>
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
          <div className="border-t px-4 py-3">
            <Button variant="outline" size="sm" onClick={() => setChangingPassword(true)}>
              <KeyRound />
              {t('changePassword.title')}
            </Button>
          </div>
        </Panel>
      </Section>

      <Section title={t('settings.preferences')}>
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
      </Section>

      <Section title={t('settings.cli')} description={t('settings.cliBody')}>
        <Panel className="space-y-1.5 p-4">
          <Command>{`docker login ${host} -u ${session.username ?? '<username>'}`}</Command>
          <Command>{`dockyard-cli login ${window.location.origin} -u ${session.username ?? '<username>'}`}</Command>
        </Panel>
      </Section>

      <Section title={t('settings.registry')}>
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
      </Section>

      {changingPassword && <ChangePasswordModal onClose={() => setChangingPassword(false)} />}
    </Page>
  )
}
