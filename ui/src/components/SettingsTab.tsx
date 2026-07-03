import { useEffect, useState } from 'react'
import { User, KeyRound, Server, CircleCheck, CircleAlert, GitFork, BookOpen, Bug } from 'lucide-react'
import { getHealth, getUsername, HealthInfo } from '../api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

const REPO_URL = 'https://github.com/KGMA74/dockyard'

interface Props {
  onChangePassword: () => void
}

export default function SettingsTab({ onChangePassword }: Props) {
  const [health, setHealth] = useState<HealthInfo | null>(null)
  const username = getUsername()

  useEffect(() => {
    getHealth().then(setHealth).catch(() => setHealth(null))
  }, [])

  const proxyUnreachable = health?.mode === 'proxy' && health.registry?.startsWith('unreachable')

  return (
    <div className="space-y-6 max-w-3xl">
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
