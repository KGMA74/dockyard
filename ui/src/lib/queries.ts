// Server state for the whole console, through react-query. Every screen used
// to fetch in its own useEffect and keep its own copy, so a push seen on one
// tab left the others stale and nothing was shared or deduplicated. Queries
// are keyed here once; live registry events invalidate the keys they affect.

import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import {
  getAudit,
  getHealth,
  getInsights,
  getManifestDetails,
  getMe,
  getRepositories,
  getScanVulnerabilities,
  getServerConfig,
  getSigningStatus,
  getStorageStats,
  getTags,
  listQuotas,
  listReplicationTargets,
  listRetentionPolicies,
  listMySessions,
  listScans,
  listSessions,
  listSigningPolicies,
  listRobots,
  listUsers,
  listWebhooks,
  getVulnerabilityPolicy,
  type RegistryEvent,
} from '@/api'

export const keys = {
  repositories: ['repositories'] as const,
  tags: (repo: string) => ['tags', repo] as const,
  manifest: (repo: string, reference: string) => ['manifest', repo, reference] as const,
  stats: ['storage-stats'] as const,
  health: ['health'] as const,
  insights: ['insights'] as const,
  audit: (params: object) => ['audit', params] as const,
  scans: (params: object) => ['scans', params] as const,
  scanReport: (id: number) => ['scan-report', id] as const,
  users: ['users'] as const,
  me: ['me'] as const,
  robots: ['robots'] as const,
  sessions: ['sessions'] as const,
  mySessions: ['sessions', 'mine'] as const,
  serverConfig: ['server-config'] as const,
  retention: ['retention'] as const,
  quotas: ['quotas'] as const,
  webhooks: ['webhooks'] as const,
  replication: ['replication'] as const,
  signingStatus: ['signing-status'] as const,
  signingPolicies: ['signing-policies'] as const,
  vulnerabilityPolicy: ['vulnerability-policy'] as const,
}

export const useRepositories = () =>
  useQuery({ queryKey: keys.repositories, queryFn: getRepositories, select: data => data.repositories })

export const useTags = (repo: string) =>
  useQuery({ queryKey: keys.tags(repo), queryFn: () => getTags(repo), select: data => data.tags ?? [] })

export const useManifest = (repo: string, reference: string) =>
  useQuery({
    queryKey: keys.manifest(repo, reference),
    queryFn: () => getManifestDetails(repo, reference),
    // A digest never changes what it points at.
    staleTime: reference.startsWith('sha256:') ? Infinity : 15_000,
  })

export const useStorageStats = () => useQuery({ queryKey: keys.stats, queryFn: getStorageStats })

export const useHealth = () => useQuery({ queryKey: keys.health, queryFn: getHealth, staleTime: 30_000 })

export const useInsights = (enabled = true) => useQuery({ queryKey: keys.insights, queryFn: getInsights, enabled })

export const useAudit = (
  params: { limit?: number; offset?: number; actor?: string; repo?: string },
  enabled = true,
) =>
  useQuery({
    queryKey: keys.audit(params),
    queryFn: () => getAudit(params),
    enabled,
    placeholderData: previous => previous, // keep the table while paging
  })

export const useScans = (
  params: { name?: string; digest?: string; limit?: number; offset?: number },
  options: { enabled?: boolean; pollWhileRunning?: boolean } = {},
) =>
  useQuery({
    queryKey: keys.scans(params),
    queryFn: () => listScans(params),
    enabled: options.enabled ?? true,
    // Scans finish on their own schedule: poll only while one is in flight.
    refetchInterval: query =>
      options.pollWhileRunning &&
      query.state.data?.scans.some(scan => scan.status === 'queued' || scan.status === 'running')
        ? 2000
        : false,
  })

export const useScanReport = (id: number | null) =>
  useQuery({
    queryKey: keys.scanReport(id ?? 0),
    queryFn: () => getScanVulnerabilities(id!),
    enabled: id !== null,
    staleTime: Infinity, // a finished scan's report is immutable
  })

export const useUsers = () => useQuery({ queryKey: keys.users, queryFn: listUsers, select: data => data.users })
export const useRobots = () => useQuery({ queryKey: keys.robots, queryFn: listRobots, select: data => data.robots })
export const useMe = () => useQuery({ queryKey: keys.me, queryFn: getMe })
export const useSessions = () => useQuery({ queryKey: keys.sessions, queryFn: listSessions })
export const useMySessions = () => useQuery({ queryKey: keys.mySessions, queryFn: listMySessions })
export const useServerConfig = (enabled: boolean) =>
  useQuery({
    queryKey: keys.serverConfig,
    queryFn: getServerConfig,
    enabled,
    staleTime: Infinity, // read from env at startup: only a restart changes it
    select: data => data.groups,
  })
export const useRetentionPolicies = () =>
  useQuery({ queryKey: keys.retention, queryFn: listRetentionPolicies, select: data => data.policies })
export const useQuotas = () => useQuery({ queryKey: keys.quotas, queryFn: listQuotas })
export const useWebhooks = () =>
  useQuery({ queryKey: keys.webhooks, queryFn: listWebhooks, select: data => data.webhooks })
export const useReplicationTargets = () =>
  useQuery({ queryKey: keys.replication, queryFn: listReplicationTargets, select: data => data.targets })
export const useSigningStatus = () => useQuery({ queryKey: keys.signingStatus, queryFn: getSigningStatus })
export const useSigningPolicies = () =>
  useQuery({ queryKey: keys.signingPolicies, queryFn: listSigningPolicies, select: data => data.policies })

export const useVulnerabilityPolicy = () => useQuery({ queryKey: keys.vulnerabilityPolicy, queryFn: getVulnerabilityPolicy })

/** Returns a function that marks the given query keys stale and refetches the
 *  ones currently on screen. */
export function useInvalidate() {
  const client = useQueryClient()
  return (...queryKeys: (readonly unknown[])[]) =>
    Promise.all(queryKeys.map(queryKey => client.invalidateQueries({ queryKey })))
}

/** What a live registry event makes stale. `scan` only touches scan data;
 *  everything that adds or removes content also moves stats and the audit log. */
export function invalidateForEvent(client: QueryClient, event: RegistryEvent): void {
  const stale: (readonly unknown[])[] = [['audit']]
  if (event.type === 'scan') {
    stale.push(['scans'])
  } else if (event.type === 'quota_warning') {
    stale.push(keys.quotas)
  } else {
    stale.push(keys.repositories, ['tags'], keys.stats, keys.insights, keys.quotas)
  }
  for (const queryKey of stale) void client.invalidateQueries({ queryKey })
}
