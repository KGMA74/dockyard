import { useState, useEffect, useCallback } from 'react'
import { logout, getRepositories, getStorageStats, StorageStats, RepoSummary } from '../api'
import StatsBar from '../components/StatsBar'
import RepoList from '../components/RepoList'

interface Props {
  onLogout: () => void
}

export default function Dashboard({ onLogout }: Props) {
  const [repos, setRepos] = useState<RepoSummary[]>([])
  const [stats, setStats] = useState<StorageStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const loadData = useCallback(async () => {
    setError('')
    try {
      const [reposRes, statsRes] = await Promise.allSettled([
        getRepositories(),
        getStorageStats(),
      ])
      if (reposRes.status === 'fulfilled') setRepos(reposRes.value.repositories)
      else setError('Failed to load repositories')
      if (statsRes.status === 'fulfilled') setStats(statsRes.value)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  async function handleLogout() {
    await logout()
    onLogout()
  }

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100">
      <header className="sticky top-0 z-10 border-b border-zinc-800/80 bg-zinc-950/80 backdrop-blur">
        <div className="max-w-5xl mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <svg className="w-5 h-5 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
            </svg>
            <span className="font-semibold text-zinc-100 text-sm tracking-tight">Maestro</span>
            <span className="text-xs bg-zinc-900 text-zinc-500 px-2 py-0.5 rounded-full border border-zinc-800">
              Registry
            </span>
          </div>
          <button
            onClick={handleLogout}
            className="text-xs text-zinc-500 hover:text-zinc-200 transition-colors px-3 py-1.5 rounded-lg hover:bg-zinc-900"
          >
            Sign out
          </button>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        {stats && <StatsBar stats={stats} onRefresh={loadData} />}

        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-xs font-medium text-zinc-500 uppercase tracking-widest">
              Images
              {!loading && repos.length > 0 && (
                <span className="ml-2 text-zinc-700 normal-case tracking-normal font-normal">
                  ({repos.length})
                </span>
              )}
            </h2>
            <button
              onClick={loadData}
              className="text-xs text-zinc-600 hover:text-zinc-300 transition-colors"
            >
              Refresh
            </button>
          </div>

          {loading ? (
            <div className="flex items-center justify-center py-20 text-zinc-700 text-sm">
              Loading…
            </div>
          ) : error ? (
            <div className="text-center py-20 text-red-400 text-sm">{error}</div>
          ) : repos.length === 0 ? (
            <div className="text-center py-20">
              <p className="text-zinc-600 text-sm">No images pushed yet</p>
              <p className="text-zinc-700 text-xs mt-1">
                docker push host.docker.internal:8080/my-image:tag
              </p>
            </div>
          ) : (
            <RepoList repos={repos} onRefresh={loadData} />
          )}
        </div>
      </main>
    </div>
  )
}
