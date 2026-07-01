import { useState, useCallback } from 'react'
import { getTags, deleteManifest, TagInfo, RepoSummary } from '../api'
import DeleteModal from './DeleteModal'
import ImageDetailsPanel from './ImageDetailsPanel'
import { useToast } from './Toast'

interface Props {
  repos: RepoSummary[]
  onRefresh: () => void
}

export default function RepoList({ repos, onRefresh }: Props) {
  const [details, setDetails] = useState<{ name: string; tag: TagInfo } | null>(null)

  return (
    <div className="space-y-2">
      {repos.map(repo => (
        <RepoCard
          key={repo.name}
          repo={repo}
          onRefresh={onRefresh}
          onShowDetails={tag => setDetails({ name: repo.name, tag })}
        />
      ))}

      {details && (
        <ImageDetailsPanel
          imageName={details.name}
          tag={details.tag}
          onClose={() => setDetails(null)}
        />
      )}
    </div>
  )
}

function RepoCard({
  repo,
  onRefresh,
  onShowDetails,
}: {
  repo: RepoSummary
  onRefresh: () => void
  onShowDetails: (tag: TagInfo) => void
}) {
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
                      onShowDetails={() => onShowDetails(tag)}
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

function TagRow({
  imageName,
  tag,
  onDelete,
  onShowDetails,
}: {
  imageName: string
  tag: TagInfo
  onDelete: () => void
  onShowDetails: () => void
}) {
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
        <div className="flex items-center justify-end gap-1">
          <button
            onClick={onShowDetails}
            title="View details"
            className="text-zinc-600 hover:text-blue-400 transition-colors p-1 rounded"
          >
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" />
            </svg>
          </button>
          <button
            onClick={onDelete}
            title="Delete tag"
            className="text-zinc-600 hover:text-red-400 transition-colors p-1 rounded"
          >
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
            </svg>
          </button>
        </div>
      </td>
    </tr>
  )
}
