import { TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { registryHost, registryIsInsecure } from '@/api'
import { useSession } from '@/lib/session'
import { Command } from './kit'

/** Namespace to suggest in examples: the account's own prefix when it is
 *  restricted to one ("team-a/*" → "team-a"), a neutral placeholder otherwise. */
function exampleNamespace(patterns: string[]): string {
  const prefix = patterns.find(p => /^[A-Za-z0-9._/-]+\/\*$/.test(p))
  return prefix ? prefix.slice(0, -2) : 'my-team'
}

/** The three commands that get an image into this registry, with this
 *  registry's real address filled in. Shown on the overview and wherever a
 *  list is empty — the first thing a newcomer needs is how to push. */
export default function PushGuide({ repo, className }: { repo?: string; className?: string }) {
  const { t } = useTranslation()
  const session = useSession()
  const host = registryHost()
  const image = `${host}/${repo ?? `${exampleNamespace(session.repoPatterns)}/my-image`}`

  return (
    <div className={className}>
      <ol className="space-y-3">
        <li className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t('pushGuide.login')}</p>
          <Command>{`docker login ${host}`}</Command>
        </li>
        {session.canPush && (
          <>
            <li className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">{t('pushGuide.tag')}</p>
              <Command>{`docker tag my-image:latest ${image}:1.0.0`}</Command>
            </li>
            <li className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">{t('pushGuide.push')}</p>
              <Command>{`docker push ${image}:1.0.0`}</Command>
            </li>
          </>
        )}
        {!session.canPush && (
          <li className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">{t('pushGuide.pull')}</p>
            <Command>{`docker pull ${image}:1.0.0`}</Command>
          </li>
        )}
      </ol>

      {registryIsInsecure() && (
        <div className="mt-4 flex gap-2.5 rounded-lg border border-warning/35 bg-warning/10 px-3 py-2.5 text-xs">
          <TriangleAlert className="mt-px size-3.5 shrink-0 text-warning" />
          <p className="text-pretty">
            {t('pushGuide.insecurePrefix')} <code className="font-medium">{`"insecure-registries": ["${host}"]`}</code>{' '}
            {t('pushGuide.insecureSuffix')}
          </p>
        </div>
      )}
    </div>
  )
}
