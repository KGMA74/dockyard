import { useTranslation } from 'react-i18next'
import { Page, PageHeader } from '@/components/kit'
import ScansSection from '@/components/ScansSection'
import SigningPoliciesSection from '@/components/SigningPoliciesSection'

export default function SecurityPage() {
  const { t } = useTranslation()
  return (
    <Page>
      <PageHeader title={t('security.title')} description={t('security.description')} />
      <ScansSection />
      <SigningPoliciesSection />
    </Page>
  )
}
