import { describe, expect, it } from 'vitest'
import en from './en.json'
import fr from './fr.json'

type Tree = { [key: string]: string | Tree }

function flatten(tree: Tree, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([key, value]) =>
    typeof value === 'string' ? [prefix + key] : flatten(value, `${prefix}${key}.`),
  )
}

const enKeys = flatten(en as Tree)
const frKeys = flatten(fr as Tree)

// Every source file, as text — a missing translation should fail in CI, not
// show up as a raw "some.key" in front of a user.
const sources = import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true }) as Record<
  string,
  string
>

const PLURAL_SUFFIXES = ['_one', '_other']

function exists(key: string, keys: Set<string>): boolean {
  return keys.has(key) || PLURAL_SUFFIXES.every(suffix => keys.has(key + suffix))
}

describe('locales', () => {
  it('English and French define exactly the same keys', () => {
    expect(frKeys.filter(key => !enKeys.includes(key))).toEqual([])
    expect(enKeys.filter(key => !frKeys.includes(key))).toEqual([])
  })

  it('keeps interpolation placeholders identical across languages', () => {
    const placeholders = (text: string) => [...text.matchAll(/\{\{(\w+)\}\}/g)].map(match => match[1]).sort()
    const lookup = (tree: Tree, key: string) =>
      key.split('.').reduce<string | Tree>((node, part) => (node as Tree)[part], tree) as string
    const mismatched = enKeys.filter(
      key => placeholders(lookup(en as Tree, key)).join() !== placeholders(lookup(fr as Tree, key)).join(),
    )
    // Singular forms may legitimately drop {{count}} ("the latest tag").
    expect(mismatched.filter(key => !key.endsWith('_one'))).toEqual([])
  })

  it('defines every key the source code asks for', () => {
    const known = new Set(enKeys)
    const missing = new Set<string>()
    for (const [file, code] of Object.entries(sources)) {
      if (file.includes('.test.')) continue
      // t('a.b') and i18n.t('a.b'), across line breaks.
      for (const match of code.matchAll(/\bt\(\s*'([A-Za-z0-9_.-]+)'/g)) {
        if (!exists(match[1], known)) missing.add(`${match[1]}  (${file})`)
      }
      // labelKey: 'a.b' — keys passed around as data.
      for (const match of code.matchAll(/\b(?:labelKey|titleKey):\s*'([A-Za-z0-9_.-]+)'/g)) {
        if (!exists(match[1], known)) missing.add(`${match[1]}  (${file})`)
      }
    }
    expect([...missing]).toEqual([])
  })

  it('defines the key families that are built dynamically', () => {
    const known = new Set(enKeys)
    const families: Record<string, string[]> = {
      nav: ['overview', 'images', 'activity', 'storage', 'security', 'access', 'integrations', 'settings'],
      roles: ['admin', 'pusher', 'reader'],
      modes: ['embedded', 'proxy', 'mirror'],
      'scan.status': ['queued', 'running', 'succeeded', 'failed'],
      'scan.severity': ['CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'UNKNOWN'],
      eventTypes: ['push', 'delete', 'retention', 'gc', 'scan', 'import', 'quota_warning'],
      'audit.actions': ['login', 'logout', 'change-password', 'push', 'delete-manifest', 'scan', 'pull-blocked'],
      // Keys the server sends in GET /api/admin/config (internal/server/config_summary.go).
      'settings.configGroups': ['registry', 'access', 'security', 'maintenance', 'observability'],
      'settings.configKeys': [
        'mode', 'upstream', 'mirror_tag_ttl', 'storage_backend', 's3_bucket', 's3_endpoint', 'data_path',
        'v2_auth', 'anonymous_pull', 'sso', 'tls', 'login_rate_limit', 'global_rate_limit', 'trusted_proxies',
        'cors_origins', 'scanning', 'scanner', 'scan_offline', 'require_signed_push', 'cosign_keys',
        'maintenance_schedule', 'gc_grace_period', 'metrics', 'tracing',
      ],
      'settings.storageBackends': ['local', 's3'],
      'login.sso': ['provider_denied', 'expired', 'provider_unreachable', 'exchange_failed', 'invalid_token', 'not_allowed', 'username_invalid', 'username_taken', 'session_failed'],
      'vulnPolicy.threshold': ['off', 'CRITICAL', 'HIGH', 'MEDIUM', 'LOW'],
      sso: [
        'admin_groups', 'pusher_groups', 'reader_groups',
        'admin_groupsPlaceholder', 'pusher_groupsPlaceholder', 'reader_groupsPlaceholder',
      ],
    }
    const missing = Object.entries(families).flatMap(([family, members]) =>
      members.map(member => `${family}.${member}`).filter(key => !known.has(key)),
    )
    expect(missing).toEqual([])
  })
})
