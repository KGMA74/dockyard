import { useEffect, useState } from 'react'
import { User, KeyRound, Server, CircleCheck, CircleAlert, GitFork, BookOpen, Bug, ScrollText } from 'lucide-react'
import { getAudit, getHealth, getUsername, AuditEntry, HealthInfo } from '../api'
import QuotasSection from './QuotasSection'
import ReplicationSection from './ReplicationSection'
import SigningPoliciesSection from './SigningPoliciesSection'
import WebhooksSection from './WebhooksSection'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const REPO_URL = 'https://github.com/KGMA74/dockyard'

interface Props {
  onChangePassword: () => void
}

export default function SettingsTab({ onChangePassword }: Props) {
  const [health, setHealth] = useState<HealthInfo | null>(null)
  const [audit, setAudit] = useState<AuditEntry[] | null>(null)
  const username = getUsername()

  useEffect(() => {
    getHealth().then(setHealth).catch(() => setHealth(null))
    // 403 for non-admin users — the section simply stays hidden.
    getAudit(30).then(r => setAudit(r.entries)).catch(() => setAudit(null))
  }, [])

  const proxyUnreachable = health?.mode === 'proxy' && health.registry?.startsWith('unreachable')

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          Account
        </h2>
        <Card className="p-4 rounded-xl gap-3">
          <div className="flex items-center gap-3">
            <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
              <User className="size-4 text-muted-foreground" strokeWidth={1.5} />
            </div>
            <div>
              <p className="text-sm font-medium">{username ?? 'Unknown user'}</p>
              <p className="text-xs text-muted-foreground">Registry administrator</p>
            </div>
          </div>
          <Button variant="outline" size="sm" onClick={onChangePassword} className="self-start">
            <KeyRound />
            Change password
          </Button>
        </Card>
      </div>

      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          Registry
        </h2>
        <Card className="p-4 rounded-xl gap-3">
          <div className="flex items-center gap-3">
            <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
              <Server className="size-4 text-muted-foreground" strokeWidth={1.5} />
            </div>
            <div>
              <p className="text-sm font-medium capitalize">{health?.mode ?? '—'} mode</p>
              <p className="text-xs text-muted-foreground">
                {health?.mode === 'proxy'
                  ? 'Forwarding requests to an upstream registry'
                  : 'Storing blobs and manifests locally'}
              </p>
            </div>
          </div>

          {health?.mode === 'proxy' && (
            <div className="flex items-center justify-between gap-3 bg-muted/50 border rounded-lg px-3 py-2">
              <span className="font-mono text-xs text-muted-foreground truncate">
                {health.registry?.replace(/^unreachable: /, '')}
              </span>
              <Badge
                variant="outline"
                className={proxyUnreachable
                  ? 'text-destructive border-destructive/30 bg-destructive/10 gap-1'
                  : 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10 gap-1'}
              >
                {proxyUnreachable
                  ? <><CircleAlert className="size-3" /> Unreachable</>
                  : <><CircleCheck className="size-3" /> Reachable</>}
              </Badge>
            </div>
          )}
        </Card>
      </div>

      <SigningPoliciesSection />

      <QuotasSection />

      <ReplicationSection />

      <WebhooksSection />

      {audit !== null && (
        <div>
          <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
            Audit log
          </h2>
          <Card className="p-4 rounded-xl gap-3">
            <div className="flex items-center gap-3">
              <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
                <ScrollText className="size-4 text-muted-foreground" strokeWidth={1.5} />
              </div>
              <div>
                <p className="text-sm font-medium">Recent sensitive actions</p>
                <p className="text-xs text-muted-foreground">Logins, pushes, deletions, GC — last 30 events</p>
              </div>
            </div>
            {audit.length === 0 ? (
              <p className="text-xs text-muted-foreground">No events recorded yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-left text-muted-foreground border-b">
                      <th className="py-1.5 pr-3 font-medium">Date</th>
                      <th className="py-1.5 pr-3 font-medium">Actor</th>
                      <th className="py-1.5 pr-3 font-medium">Action</th>
                      <th className="py-1.5 pr-3 font-medium">Repository</th>
                      <th className="py-1.5 font-medium">Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {audit.map(e => (
                      <tr key={e.id} className="border-b last:border-0">
                        <td className="py-1.5 pr-3 whitespace-nowrap text-muted-foreground">
                          {new Date(e.at + (e.at.endsWith('Z') ? '' : 'Z')).toLocaleString()}
                        </td>
                        <td className="py-1.5 pr-3 font-medium">{e.actor || '—'}</td>
                        <td className="py-1.5 pr-3 font-mono">{e.action}</td>
                        <td className="py-1.5 pr-3 font-mono">
                          {e.repo ? `${e.repo}${e.tag ? ':' + e.tag : ''}` : '—'}
                        </td>
                        <td className="py-1.5">
                          <Badge
                            variant="outline"
                            className={/^(2\d\d|success)$/.test(e.result)
                              ? 'text-emerald-600 dark:text-emerald-400 border-emerald-500/30 bg-emerald-500/10'
                              : 'text-destructive border-destructive/30 bg-destructive/10'}
                          >
                            {e.result}
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      )}

      <div>
        <h2 className="text-xs font-medium text-muted-foreground uppercase tracking-widest mb-3">
          About
        </h2>
        <Card className="p-4 rounded-xl gap-3">
          <div className="flex items-center gap-3">
            <div className="size-9 rounded-full bg-muted flex items-center justify-center shrink-0">
              <GitFork className="size-4 text-muted-foreground" strokeWidth={1.5} />
            </div>
            <div>
              <p className="text-sm font-medium">
                Dockyard {health?.version ? <span className="font-mono">{health.version}</span> : null}
              </p>
              <p className="text-xs text-muted-foreground">Self-hosted Docker Registry V2</p>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button variant="outline" size="sm" asChild>
              <a href={REPO_URL} target="_blank" rel="noopener noreferrer">
                <GitFork />
                View source
              </a>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href={`${REPO_URL}#contributing`} target="_blank" rel="noopener noreferrer">
                <BookOpen />
                Contribute
              </a>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href={`${REPO_URL}/issues`} target="_blank" rel="noopener noreferrer">
                <Bug />
                Report an issue
              </a>
            </Button>
          </div>
        </Card>
      </div>
    </div>
  )
}
