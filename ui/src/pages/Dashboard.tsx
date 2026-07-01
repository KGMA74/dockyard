import { useState, useEffect, useCallback } from 'react'
import { logout, getRepositories, getStorageStats, StorageStats, RepoSummary } from '../api'
import StatsBar from '../components/StatsBar'
import RepoList from '../components/RepoList'
import Sidebar from '../components/Sidebar'
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
    <div className="min-h-screen bg-zinc-950 text-zinc-100 flex">
      <Sidebar onChangePassword={() => setShowPasswordModal(true)} onLogout={handleLogout} />

      <main className="flex-1 min-w-0 px-6 py-6 space-y-6">
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
