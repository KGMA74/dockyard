import type { TFunction } from 'i18next'
import type { AuditEntry } from '@/api'
import type { Tone } from '@/components/kit'

// Audit actions are stored as short verbs ("push", "login") for protocol and
// auth events, and as "METHOD /api/admin/path" for admin API mutations. The
// second form is precise but not something anyone wants to read in a table.

const KNOWN_ACTIONS = ['login', 'logout', 'change-password', 'push', 'delete-manifest', 'scan']

const ADMIN_ROUTES: [RegExp, string][] = [
  [/^DELETE \/api\/admin\/repositories\/manifests$/, 'deleteTag'],
  [/^DELETE \/api\/admin\/repositories$/, 'deleteRepository'],
  [/^POST \/api\/admin\/repositories\/import$/, 'import'],
  [/^POST \/api\/admin\/gc$/, 'gc'],
  [/^POST \/api\/admin\/users$/, 'createUser'],
  [/^PUT \/api\/admin\/users\/[^/]+$/, 'updateUser'],
  [/^DELETE \/api\/admin\/users\/[^/]+$/, 'deleteUser'],
  [/^DELETE \/api\/admin\/sessions\/(?:mine\/)?[^/]+$/, 'revokeSession'],
  [/^POST \/api\/admin\/retention$/, 'createRetention'],
  [/^DELETE \/api\/admin\/retention\/[^/]+$/, 'deleteRetention'],
  [/^POST \/api\/admin\/retention\/run$/, 'runRetention'],
  [/^POST \/api\/admin\/webhooks$/, 'createWebhook'],
  [/^DELETE \/api\/admin\/webhooks\/[^/]+$/, 'deleteWebhook'],
  [/^POST \/api\/admin\/webhooks\/[^/]+\/test$/, 'testWebhook'],
  [/^POST \/api\/admin\/replication\/targets$/, 'createReplication'],
  [/^DELETE \/api\/admin\/replication\/targets\/[^/]+$/, 'deleteReplication'],
  [/^POST \/api\/admin\/replication\/targets\/[^/]+\/test$/, 'testReplication'],
  [/^PUT \/api\/admin\/quotas$/, 'setQuota'],
  [/^DELETE \/api\/admin\/quotas\/[^/]+$/, 'deleteQuota'],
  [/^POST \/api\/admin\/quotas\/usage\/reset$/, 'resetQuota'],
  [/^POST \/api\/admin\/signing\/policies$/, 'createSigningPolicy'],
  [/^DELETE \/api\/admin\/signing\/policies\/[^/]+$/, 'deleteSigningPolicy'],
  [/^POST \/api\/admin\/scans$/, 'scan'],
]

/** Human label for an audit action, plus the raw action when they differ. */
export function describeAction(action: string, t: TFunction): { label: string; raw?: string } {
  if (KNOWN_ACTIONS.includes(action)) return { label: t(`audit.actions.${action}`) }
  for (const [pattern, key] of ADMIN_ROUTES) {
    if (pattern.test(action)) return { label: t(`audit.actions.${key}`), raw: action }
  }
  return { label: action }
}

/** What the action was applied to: a repository[:tag], or the last path
 *  segment of an admin route (a username, an id). */
export function auditTarget(entry: AuditEntry): string {
  if (entry.repo) return entry.tag ? `${entry.repo}:${entry.tag}` : entry.repo
  const match = entry.action.match(/^(?:PUT|DELETE) \/api\/admin\/users\/([^/]+)$/)
  return match ? decodeURIComponent(match[1]) : ''
}

export function resultTone(result: string): Tone {
  if (result === 'success' || /^2\d\d$/.test(result)) return 'success'
  if (/^4(01|03|29)$/.test(result) || result === 'failure') return 'warning'
  if (/^[45]\d\d$/.test(result)) return 'danger'
  return 'neutral'
}

export function resultLabel(result: string, t: TFunction): string {
  if (result === 'success' || /^2\d\d$/.test(result)) return t('audit.results.ok')
  if (result === 'failure') return t('audit.results.failed')
  if (result === '401') return t('audit.results.unauthorized')
  if (result === '403') return t('audit.results.denied')
  if (result === '404') return t('audit.results.notFound')
  if (result === '429') return t('audit.results.rateLimited')
  return result
}
