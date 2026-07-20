import { useState, useCallback } from 'react'
import { toast } from 'sonner'
import { Box, ChevronDown, Copy, Check, GitCompare, Info, Trash2 } from 'lucide-react'
import { getTags, deleteManifest, deleteRepository, TagInfo, RepoSummary } from '../api'
import DeleteModal from './DeleteModal'
import ImageDetailsPanel from './ImageDetailsPanel'
import TagDiff from './TagDiff'
import { relativeTime } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

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

function TagBadge({ children }: { children: React.ReactNode }) {
  return (
    <Badge
      variant="outline"
      className="font-mono text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/30 border-blue-200 dark:border-blue-900/30"
    >
      {children}
    </Badge>
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
  const [open, setOpen] = useState(false)
  const [tags, setTags] = useState<TagInfo[]>([])
  const [loadingTags, setLoadingTags] = useState(false)
  const [toDelete, setToDelete] = useState<TagInfo | null>(null)
  const [deleteRepo, setDeleteRepo] = useState(false)
  const [selected, setSelected] = useState<TagInfo[]>([])
  const [diffing, setDiffing] = useState(false)

  function toggleSelect(tag: TagInfo) {
    setSelected(cur => {
      if (cur.some(t => t.digest === tag.digest && t.tag === tag.tag)) {
        return cur.filter(t => !(t.digest === tag.digest && t.tag === tag.tag))
      }
      if (cur.length >= 2) return [cur[1], tag] // keep the two most recently picked
      return [...cur, tag]
    })
  }

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
    toast.success(`Deleted ${repo.name}:${tag.tag}`)
    onRefresh()
  }

  async function handleDeleteRepo() {
    await deleteRepository(repo.name)
    setDeleteRepo(false)
    toast.success(`Deleted repository ${repo.name}`)
    onRefresh()
  }

  return (
    <>
      <div className="bg-card text-card-foreground border rounded-xl overflow-hidden group/card">
        <div className="w-full flex items-center hover:bg-muted/50 transition-colors">
          <button
            onClick={handleExpand}
            className="flex-1 flex items-center justify-between px-4 py-3.5 min-w-0 text-left"
          >
            <div className="flex items-center gap-3 min-w-0">
              <Box className="size-4 text-muted-foreground/60 shrink-0" strokeWidth={1.5} />
              <span className="font-mono text-sm truncate">{repo.name}</span>
              <Badge variant="secondary" className="shrink-0 rounded-full text-muted-foreground">
                {repo.total} {repo.total === 1 ? 'tag' : 'tags'}
              </Badge>
              {repo.last_pushed && (
                <span className="shrink-0 text-xs text-muted-foreground/60">
                  pushed {relativeTime(repo.last_pushed)}
                </span>
              )}
            </div>
            <ChevronDown
              className={`size-4 text-muted-foreground/60 shrink-0 transition-transform duration-200 ${open ? 'rotate-180' : ''}`}
            />
          </button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setDeleteRepo(true)}
            title="Delete repository"
            className="shrink-0 mr-3 text-muted-foreground/40 hover:text-destructive hover:bg-destructive/10 transition-colors"
          >
            <Trash2 className="size-3.5" strokeWidth={1.5} />
          </Button>
        </div>

        {open && (
          <div className="border-t">
            {loadingTags ? (
              <p className="px-4 py-3 text-sm text-muted-foreground">Loading…</p>
            ) : tags.length === 0 ? (
              <p className="px-4 py-3 text-sm text-muted-foreground">No tags found</p>
            ) : (
              <>
                {selected.length > 0 && (
                  <div className="flex items-center justify-between gap-3 px-4 py-2 bg-muted/50 border-b text-xs">
                    <span className="text-muted-foreground">
                      {selected.length === 1
                        ? `${selected[0].tag} selected — pick one more tag to compare`
                        : `Comparing ${selected[0].tag} and ${selected[1].tag}`}
                    </span>
                    <div className="flex items-center gap-2 shrink-0">
                      {selected.length === 2 && (
                        <Button variant="outline" size="sm" onClick={() => setDiffing(true)}>
                          <GitCompare />
                          Compare
                        </Button>
                      )}
                      <Button variant="ghost" size="sm" onClick={() => setSelected([])}>
                        Clear
                      </Button>
                    </div>
                  </div>
                )}
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="px-4 w-10" />
                      <TableHead className="px-4 text-xs">Tag</TableHead>
                      <TableHead className="px-4 text-xs hidden sm:table-cell">Digest</TableHead>
                      <TableHead className="px-4 text-xs hidden md:table-cell">Pull</TableHead>
                      <TableHead className="px-4 text-xs hidden lg:table-cell">Pushed</TableHead>
                      <TableHead className="px-4 w-16" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {tags.map(tag => (
                      <TagRow
                        key={tag.digest}
                        imageName={repo.name}
                        tag={tag}
                        selected={selected.some(t => t.digest === tag.digest && t.tag === tag.tag)}
                        onToggleSelect={() => toggleSelect(tag)}
                        onDelete={() => setToDelete(tag)}
                        onShowDetails={() => onShowDetails(tag)}
                      />
                    ))}
                  </TableBody>
                </Table>
              </>
            )}
          </div>
        )}
      </div>

      {diffing && selected.length === 2 && (
        <TagDiff
          imageName={repo.name}
          tagA={selected[0].tag}
          tagB={selected[1].tag}
          onClose={() => setDiffing(false)}
        />
      )}

      {toDelete && (
        <DeleteModal
          title="Delete tag"
          resourceName={`${repo.name}:${toDelete.tag}`}
          description={
            <>
              This removes the manifest for{' '}
              <span className="font-mono text-foreground text-xs">
                {repo.name}:{toDelete.tag}
              </span>
              . Unreferenced blobs are freed on the next GC run.
            </>
          }
          detail={toDelete.digest}
          onConfirm={() => handleDelete(toDelete)}
          onCancel={() => setToDelete(null)}
        />
      )}

      {deleteRepo && (
        <DeleteModal
          title="Delete repository"
          resourceName={repo.name}
          confirmLabel="Delete repository"
          description={
            <>
              This removes{' '}
              <span className="font-mono text-foreground text-xs">{repo.name}</span>{' '}
              and all its {repo.total} {repo.total === 1 ? 'tag' : 'tags'}. Unreferenced blobs are
              freed on the next GC run. This cannot be undone.
            </>
          }
          onConfirm={handleDeleteRepo}
          onCancel={() => setDeleteRepo(false)}
        />
      )}
    </>
  )
}

function TagRow({
  imageName,
  tag,
  selected,
  onToggleSelect,
  onDelete,
  onShowDetails,
}: {
  imageName: string
  tag: TagInfo
  selected: boolean
  onToggleSelect: () => void
  onDelete: () => void
  onShowDetails: () => void
}) {
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
    toast.info('Pull command copied')
    setTimeout(() => setCopiedPull(false), 1500)
  }, [pullCmd])

  return (
    <TableRow className="group/row">
      <TableCell className="px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={onToggleSelect}
          title="Select for diff"
          className="size-3.5 cursor-pointer align-middle"
        />
      </TableCell>
      <TableCell className="px-4 py-3">
        <TagBadge>{tag.tag}</TagBadge>
      </TableCell>

      <TableCell className="px-4 py-3 hidden sm:table-cell">
        <button
          onClick={copyDigest}
          title={tag.digest}
          className="group/digest flex items-center gap-1.5 font-mono text-xs text-muted-foreground/70 hover:text-foreground transition-colors"
        >
          <span>{copiedDigest ? 'Copied!' : `${tag.digest.slice(0, 7)}…${tag.digest.slice(-6)}`}</span>
          {copiedDigest
            ? <Check className="size-3 text-emerald-500" />
            : <Copy className="size-3 opacity-0 group-hover/digest:opacity-100 transition-opacity" />}
        </button>
      </TableCell>

      <TableCell className="px-4 py-3 hidden md:table-cell">
        <button
          onClick={copyPull}
          title={pullCmd}
          className="group/pull flex items-center gap-1.5 font-mono text-xs text-muted-foreground/70 hover:text-foreground transition-colors max-w-55"
        >
          <span className="truncate">{copiedPull ? 'Copied!' : pullCmd}</span>
          {copiedPull
            ? <Check className="size-3 shrink-0 text-emerald-500" />
            : <Copy className="size-3 shrink-0 opacity-0 group-hover/pull:opacity-100 transition-opacity" />}
        </button>
      </TableCell>

      <TableCell className="px-4 py-3 hidden lg:table-cell">
        <span className="text-xs text-muted-foreground/70" title={tag.pushed_at}>
          {tag.pushed_at ? relativeTime(tag.pushed_at) : '—'}
        </span>
      </TableCell>

      <TableCell className="px-4 py-3 text-right">
        <div className="flex items-center justify-end gap-1">
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={onShowDetails}
            title="View details"
            className="text-muted-foreground/60 hover:text-blue-500 dark:hover:text-blue-400"
          >
            <Info strokeWidth={1.5} />
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={onDelete}
            title="Delete tag"
            className="text-muted-foreground/60 hover:text-destructive"
          >
            <Trash2 strokeWidth={1.5} />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}
