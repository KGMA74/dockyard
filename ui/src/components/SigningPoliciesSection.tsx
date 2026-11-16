import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { createSigningPolicy, deleteSigningPolicy, type SigningPolicy } from '@/api'
import { keys, useInvalidate, useSigningPolicies, useSigningStatus } from '@/lib/queries'
import {
  ConfirmDialog,
  Fact,
  Facts,
  Field,
  FormDialog,
  NativeSelect,
  Panel,
  QueryPanel,
  RowAction,
  Section,
  StatusBadge,
} from './kit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

function OverrideDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const invalidate = useInvalidate()
  const [pattern, setPattern] = useState('')
  const [required, setRequired] = useState('true')

  async function submit() {
    await createSigningPolicy(pattern.trim(), required === 'true')
    toast.success(t('signing.created'))
    await invalidate(keys.signingPolicies)
    onClose()
  }

  return (
    <FormDialog
      title={t('signing.newOverride')}
      description={t('signing.overrideIntro')}
      submitLabel={t('signing.create')}
      onSubmit={submit}
      onClose={onClose}
      canSubmit={pattern.trim() !== ''}
    >
      <Field label={t('fields.repoPattern')} hint={t('fields.repoPatternHint')}>
        {ids => <Input {...ids} value={pattern} onChange={e => setPattern(e.target.value)} placeholder="team-a/*" className="font-mono" required />}
      </Field>
      <Field label={t('signing.rule')}>
        {ids => (
          <NativeSelect {...ids} value={required} onChange={e => setRequired(e.target.value)}>
            <option value="true">{t('signing.require')}</option>
            <option value="false">{t('signing.doNotRequire')}</option>
          </NativeSelect>
        )}
      </Field>
    </FormDialog>
  )
}

/** Signed-push enforcement: which repositories refuse a tag that carries no
 *  valid cosign signature. Verification only — keys never leave the client. */
export default function SigningPoliciesSection() {
  const { t } = useTranslation()
  const status = useSigningStatus()
  const policies = useSigningPolicies()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<SigningPolicy | null>(null)

  return (
    <Section
      id="signing"
      title={t('signing.title')}
      description={t('signing.description')}
      actions={
        policies.isSuccess && (
          <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {t('signing.newOverride')}
          </Button>
        )
      }
    >
      <QueryPanel query={status} unavailable={{ title: t('signing.unavailableTitle') }} skeletonRows={2}>
        {data => (
          <Panel>
            <Facts>
              <Fact label={t('signing.globalDefault')}>
                <StatusBadge tone={data.enabled ? 'success' : 'neutral'}>
                  {data.enabled ? t('signing.required') : t('signing.notRequired')}
                </StatusBadge>
              </Fact>
              <Fact label={t('signing.publicKeys')}>
                {data.keys_loaded > 0 ? (
                  <span className="tabular-nums">{t('signing.keysLoaded', { count: data.keys_loaded })}</span>
                ) : (
                  <StatusBadge tone={data.enabled ? 'danger' : 'neutral'}>{t('signing.noKeys')}</StatusBadge>
                )}
              </Fact>
            </Facts>
            {data.enabled && data.keys_loaded === 0 && (
              <p className="border-t px-4 py-2.5 text-xs text-destructive text-pretty">{t('signing.noKeysWarning')}</p>
            )}
            {policies.data && policies.data.length > 0 && (
              <ul className="divide-y border-t">
                {policies.data.map(policy => (
                  <li key={policy.id} className="flex items-center gap-3 px-4 py-2">
                    <code className="min-w-0 flex-1 truncate text-xs font-medium">{policy.repo_pattern}</code>
                    <StatusBadge tone={policy.required ? 'success' : 'neutral'}>
                      {policy.required ? t('signing.required') : t('signing.notRequired')}
                    </StatusBadge>
                    <RowAction label={t('signing.deleteOverride')} onClick={() => setToDelete(policy)} destructive>
                      <Trash2 />
                    </RowAction>
                  </li>
                ))}
              </ul>
            )}
            {policies.data?.length === 0 && (
              <p className="border-t px-4 py-2.5 text-xs text-muted-foreground">{t('signing.noOverride')}</p>
            )}
          </Panel>
        )}
      </QueryPanel>

      {creating && <OverrideDialog onClose={() => setCreating(false)} />}
      {toDelete && (
        <ConfirmDialog
          title={t('signing.deleteOverride')}
          description={t('signing.deleteBody', { pattern: toDelete.repo_pattern })}
          confirmLabel={t('common.delete')}
          destructive
          onConfirm={async () => {
            await deleteSigningPolicy(toDelete.id)
            setToDelete(null)
            await invalidate(keys.signingPolicies)
          }}
          onCancel={() => setToDelete(null)}
        />
      )}
    </Section>
  )
}
