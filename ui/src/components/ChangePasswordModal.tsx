import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { changePassword } from '@/api'
import { Field, FormDialog } from './kit'
import { Input } from '@/components/ui/input'

const MIN_LENGTH = 8

export default function ChangePasswordModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')

  const mismatch = confirm !== '' && next !== confirm

  async function submit() {
    await changePassword(current, next)
    toast.success(t('changePassword.success'))
    onClose()
  }

  return (
    <FormDialog
      title={t('changePassword.title')}
      description={t('changePassword.intro')}
      submitLabel={t('changePassword.update')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={current !== '' && next.length >= MIN_LENGTH && next === confirm}
    >
      <Field label={t('changePassword.current')}>
        {ids => (
          <Input {...ids} type="password" value={current} onChange={e => setCurrent(e.target.value)} autoComplete="current-password" autoFocus required />
        )}
      </Field>
      <Field label={t('changePassword.new')} hint={t('changePassword.newHint', { min: MIN_LENGTH })}>
        {ids => (
          <Input {...ids} type="password" value={next} onChange={e => setNext(e.target.value)} autoComplete="new-password" minLength={MIN_LENGTH} required />
        )}
      </Field>
      <Field label={t('changePassword.confirm')} hint={mismatch ? <span className="text-destructive">{t('changePassword.mismatch')}</span> : undefined}>
        {ids => (
          <Input {...ids} type="password" value={confirm} onChange={e => setConfirm(e.target.value)} autoComplete="new-password" aria-invalid={mismatch} required />
        )}
      </Field>
    </FormDialog>
  )
}
