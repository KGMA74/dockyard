import { useTranslation } from 'react-i18next'
import { Page, PageHeader } from '@/components/kit'
import ReplicationSection from '@/components/ReplicationSection'
import WebhooksSection from '@/components/WebhooksSection'

export default function IntegrationsPage() {
  const { t } = useTranslation()
  return (
    <Page>
      <PageHeader title={t('integrations.title')} description={t('integrations.description')} />
      <WebhooksSection />
      <ReplicationSection />
    </Page>
  )
}
