import { useState } from 'react'
import { ArrowRight, CircleCheck, Circle, Rocket, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { SetupStepId } from '@/api'
import { useSetup } from '@/lib/queries'
import { Link } from '@/lib/router'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'
import { Panel } from './kit'
import { Button } from '@/components/ui/button'

const DISMISS_KEY = 'dockyard_setup_dismissed'
const DOCS = 'https://github.com/KGMA74/dockyard/blob/main/CONFIGURATION.md'

/** Where each step is done: a console page, the push guide just below, or
 *  the documentation (HTTPS is a deployment matter). */
const ACTION: Record<SetupStepId, { to?: string; anchor?: string; href?: string }> = {
  admin_password: { to: '/settings' },
  first_image: { anchor: 'push-guide' },
  robot_account: { to: '/access' },
  scan_on_push: { to: '/security' },
  team: { to: '/access' },
  https: { href: DOCS },
}

function readDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === '1'
  } catch {
    return false
  }
}

/** Getting started, for admins of a new install: what is left to make the
 *  registry safe and useful, each step ticked from the server's actual state
 *  and linked to where it is done. Gone once everything is done, or when
 *  dismissed. */
export default function SetupChecklist() {
  const { t } = useTranslation()
  const session = useSession()
  const steps = useSetup(session.isAdmin)
  const [dismissed, setDismissed] = useState(readDismissed)

  if (!session.isAdmin || dismissed || !steps.data || steps.data.length === 0) return null
  const done = steps.data.filter(step => step.done).length
  if (done === steps.data.length) return null

  function dismiss() {
    try {
      localStorage.setItem(DISMISS_KEY, '1')
    } catch {
      // private mode: hidden for this visit only
    }
    setDismissed(true)
  }

  return (
    <Panel className="overflow-hidden">
      <div className="flex items-start gap-3 border-b px-4 py-3">
        <Rocket className="mt-0.5 size-4 shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">{t('setup.title')}</h2>
          <p className="text-xs text-muted-foreground">{t('setup.body')}</p>
          <div className="mt-2 flex items-center gap-2">
            <div className="h-1.5 w-40 overflow-hidden rounded-full bg-muted" aria-hidden="true">
              <div
                className="h-full rounded-full bg-primary transition-all"
                style={{ width: `${(done / steps.data.length) * 100}%` }}
              />
            </div>
            <span className="text-xs text-muted-foreground tabular-nums">
              {t('setup.progress', { done, total: steps.data.length })}
            </span>
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={dismiss}
          aria-label={t('setup.dismiss')}
          title={t('setup.dismiss')}
        >
          <X />
        </Button>
      </div>
      <ol className="divide-y">
        {steps.data.map(step => {
          const action = ACTION[step.id]
          const label = t(`setup.steps.${step.id}.action`)
          return (
            <li key={step.id} className="flex items-center gap-3 px-4 py-2.5">
              {step.done ? (
                <CircleCheck
                  className="size-4 shrink-0 text-success"
                  aria-label={t('setup.done')}
                />
              ) : (
                <Circle
                  className="size-4 shrink-0 text-muted-foreground/60"
                  aria-label={t('setup.todo')}
                />
              )}
              <div className="min-w-0 flex-1">
                <p
                  className={cn(
                    'text-sm',
                    step.done &&
                      'text-muted-foreground line-through decoration-muted-foreground/40',
                  )}
                >
                  {t(`setup.steps.${step.id}.title`)}
                </p>
                {!step.done && (
                  <p className="text-xs text-muted-foreground">
                    {t(`setup.steps.${step.id}.body`)}
                  </p>
                )}
              </div>
              {!step.done && (
                <Button asChild variant="outline" size="xs" className="shrink-0">
                  {action.to ? (
                    <Link to={action.to}>
                      {label}
                      <ArrowRight />
                    </Link>
                  ) : action.anchor ? (
                    <a
                      href={`#${action.anchor}`}
                      onClick={event => {
                        event.preventDefault()
                        document
                          .getElementById(action.anchor!)
                          ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
                      }}
                    >
                      {label}
                      <ArrowRight />
                    </a>
                  ) : (
                    <a href={action.href} target="_blank" rel="noopener noreferrer">
                      {label}
                      <ArrowRight />
                    </a>
                  )}
                </Button>
              )}
            </li>
          )
        })}
      </ol>
    </Panel>
  )
}
