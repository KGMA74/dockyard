import { useState, useEffect, useCallback } from 'react'
import { logout, getRepositories, getStorageStats, StorageStats, RepoSummary } from '../api'
import StatsBar from '../components/StatsBar'
import RepoList from '../components/RepoList'
import ChangePasswordModal from '../components/ChangePasswordModal'

interface Props {
  onLogout: () => void
}

export default function Dashboard({ onLogout }: Props) {
  const [repos, setRepos] = useState<RepoSummary[]>([])
  const [stats, setStats] = useState<StorageStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [showPasswordModal, setShowPasswordModal] = useState(false)

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

  const filtered = repos.filter(r =>
    r.name.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100">
      <header className="sticky top-0 z-10 border-b border-zinc-800/80 bg-zinc-950/80 backdrop-blur">
        <div className="max-w-5xl mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <svg className="w-5 h-5 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
            </svg>
            <span className="font-semibold text-zinc-100 text-sm tracking-tight">Dockyard</span>
            <span className="text-xs bg-zinc-900 text-zinc-500 px-2 py-0.5 rounded-full border border-zinc-800">
              Registry
            </span>
          </div>

          <div className="flex items-center gap-1">
            <button
              onClick={() => setShowPasswordModal(true)}
              title="Change password"
              className="text-zinc-500 hover:text-zinc-200 transition-colors p-2 rounded-lg hover:bg-zinc-900"
            >
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
              </svg>
            </button>
            <button
              onClick={handleLogout}
              title="Sign out"
              className="text-zinc-500 hover:text-zinc-200 transition-colors p-2 rounded-lg hover:bg-zinc-900"
            >
              <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75" />
              </svg>
            </button>
          </div>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        {stats && <StatsBar stats={stats} onRefresh={loadData} />}

        <div>
          <div className="flex items-center gap-3 mb-3">
            <h2 className="text-xs font-medium text-zinc-500 uppercase tracking-widest shrink-0">
              Images
              {!loading && repos.length > 0 && (
                <span className="ml-2 text-zinc-700 normal-case tracking-normal font-normal">
                  ({filtered.length}{filtered.length !== repos.length && `/${repos.length}`})
                </span>
              )}
            </h2>

            <div className="flex-1 relative">
              <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-zinc-600 pointer-events-none"
                fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                  d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <input
                type="text"
                placeholder="Filter images…"
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full bg-zinc-900 border border-zinc-800 rounded-lg pl-8 pr-3 py-1.5 text-xs text-zinc-300 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-zinc-700 transition-colors"
              />
            </div>

            <button
              onClick={loadData}
              className="text-xs text-zinc-600 hover:text-zinc-300 transition-colors shrink-0"
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
          ) : filtered.length === 0 ? (
            <div className="text-center py-20 text-zinc-600 text-sm">
              No images match "{search}"
            </div>
          ) : (
            <RepoList repos={filtered} onRefresh={loadData} />
          )}
        </div>
      </main>

      {showPasswordModal && (
        <ChangePasswordModal onClose={() => setShowPasswordModal(false)} />
      )}
    </div>
  )
}
