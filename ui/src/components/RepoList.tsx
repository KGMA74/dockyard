import { useState, useCallback } from 'react'
import { getTags, deleteManifest, TagInfo, RepoSummary } from '../api'
import DeleteModal from './DeleteModal'
import { useToast } from './Toast'

interface Props {
  repos: RepoSummary[]
  onRefresh: () => void
}

export default function RepoList({ repos, onRefresh }: Props) {
  return (
    <div className="space-y-2">
      {repos.map(repo => (
        <RepoCard key={repo.name} repo={repo} onRefresh={onRefresh} />
      ))}
    </div>
  )
}

function RepoCard({ repo, onRefresh }: { repo: RepoSummary; onRefresh: () => void }) {
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [tags, setTags] = useState<TagInfo[]>([])
  const [loadingTags, setLoadingTags] = useState(false)
  const [toDelete, setToDelete] = useState<TagInfo | null>(null)

  async function handleExpand() {
    if (!open && tags.length === 0) {
      setLoadingTags(true)
      try {
        const data = await getTags(repo.name)
        setTags(data.tags ?? [])
      } finally {
        setLoadingTags(false)
      }
    }
    setOpen(o => !o)
  }

  async function handleDelete(tag: TagInfo) {
    await deleteManifest(repo.name, tag.digest)
    setTags(ts => ts.filter(t => t.digest !== tag.digest))
    setToDelete(null)
    toast(`Deleted ${repo.name}:${tag.tag}`)
    onRefresh()
  }

  return (
    <>
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
        <button
          onClick={handleExpand}
          className="w-full flex items-center justify-between px-4 py-3.5 hover:bg-zinc-800/40 active:bg-zinc-800/60 transition-colors text-left"
        >
          <div className="flex items-center gap-3 min-w-0">
            <svg className="w-4 h-4 text-zinc-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
            </svg>
            <span className="font-mono text-sm text-zinc-100 truncate">{repo.name}</span>
            <span className="shrink-0 text-xs bg-zinc-800 text-zinc-500 px-2 py-0.5 rounded-full border border-zinc-700/50">
              {repo.total} {repo.total === 1 ? 'tag' : 'tags'}
            </span>
          </div>
          <svg
            className={`w-4 h-4 text-zinc-600 shrink-0 transition-transform duration-200 ${open ? 'rotate-180' : ''}`}
            fill="none" viewBox="0 0 24 24" stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </button>

        {open && (
          <div className="border-t border-zinc-800">
            {loadingTags ? (
              <p className="px-4 py-3 text-sm text-zinc-600">Loading…</p>
            ) : tags.length === 0 ? (
              <p className="px-4 py-3 text-sm text-zinc-600">No tags found</p>
            ) : (
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-zinc-800">
                    <th className="text-left px-4 py-2 text-xs text-zinc-600 font-medium">Tag</th>
                    <th className="text-left px-4 py-2 text-xs text-zinc-600 font-medium hidden sm:table-cell">Digest</th>
                    <th className="text-left px-4 py-2 text-xs text-zinc-600 font-medium hidden md:table-cell">Pull</th>
                    <th className="px-4 py-2 w-16" />
                  </tr>
                </thead>
                <tbody>
                  {tags.map(tag => (
                    <TagRow
                      key={tag.digest}
                      imageName={repo.name}
                      tag={tag}
                      onDelete={() => setToDelete(tag)}
                    />
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      {toDelete && (
        <DeleteModal
          imageName={repo.name}
          tag={toDelete}
          onConfirm={() => handleDelete(toDelete)}
          onCancel={() => setToDelete(null)}
        />
      )}
    </>
  )
}

function TagRow({ imageName, tag, onDelete }: { imageName: string; tag: TagInfo; onDelete: () => void }) {
  const { toast } = useToast()
  const [copiedDigest, setCopiedDigest] = useState(false)
  const [copiedPull, setCopiedPull] = useState(false)

  const pullCmd = `docker pull ${window.location.host}/${imageName}:${tag.tag}`

  const copyDigest = useCallback(() => {
    navigator.clipboard.writeText(tag.digest)
    setCopiedDigest(true)
    setTimeout(() => setCopiedDigest(false), 1500)
  }, [tag.digest])

  const copyPull = useCallback(() => {
    navigator.clipboard.writeText(pullCmd)
    setCopiedPull(true)
    toast('Pull command copied', 'info')
    setTimeout(() => setCopiedPull(false), 1500)
  }, [pullCmd, toast])

  return (
    <tr className="border-b border-zinc-800/50 last:border-0 hover:bg-zinc-800/20 group/row">
      <td className="px-4 py-3">
        <span className="font-mono text-blue-400 text-xs bg-blue-950/30 px-2 py-0.5 rounded-md border border-blue-900/30">
          {tag.tag}
        </span>
      </td>

      <td className="px-4 py-3 hidden sm:table-cell">
        <button
          onClick={copyDigest}
          title={tag.digest}
          className="group/digest flex items-center gap-1.5 font-mono text-xs text-zinc-600 hover:text-zinc-300 transition-colors"
        >
          <span>{copiedDigest ? 'Copied!' : `${tag.digest.slice(0, 7)}…${tag.digest.slice(-6)}`}</span>
          {!copiedDigest && (
            <svg className="w-3 h-3 opacity-0 group-hover/digest:opacity-100 transition-opacity" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
          )}
        </button>
      </td>

      <td className="px-4 py-3 hidden md:table-cell">
        <button
          onClick={copyPull}
          title={pullCmd}
          className="group/pull flex items-center gap-1.5 font-mono text-xs text-zinc-700 hover:text-zinc-300 transition-colors max-w-[220px] truncate"
        >
          <span className="truncate">{copiedPull ? 'Copied!' : pullCmd}</span>
          {!copiedPull && (
            <svg className="w-3 h-3 shrink-0 opacity-0 group-hover/pull:opacity-100 transition-opacity" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
          )}
        </button>
      </td>

      <td className="px-4 py-3 text-right">
        <button
          onClick={onDelete}
          className="text-xs text-zinc-700 hover:text-red-400 transition-colors opacity-0 group-hover/row:opacity-100"
        >
          Delete
        </button>
      </td>
    </tr>
  )
}
