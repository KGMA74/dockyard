import { getRepoPatterns, getRole, getUsername } from '@/api'

export interface Session {
  username: string | null
  role: string
  isAdmin: boolean
  /** May push images (admin or pusher). */
  canPush: boolean
  /** Repository patterns the account is limited to; empty = every repository. */
  repoPatterns: string[]
}

/** The signed-in account, read from the access token. Only used to decide
 *  what the UI *offers* — the server re-checks every request against the
 *  database, so a stale claim can hide or show a button, never grant access. */
export function useSession(): Session {
  const role = getRole()
  return {
    username: getUsername(),
    role,
    isAdmin: role === 'admin',
    canPush: role === 'admin' || role === 'pusher',
    repoPatterns: getRepoPatterns(),
  }
}

/** Cosign stores signatures, attestations and SBOMs as tags next to the image
 *  (sha256-<digest>.sig / .att / .sbom). They are not images to pull. */
export function isArtifactTag(tag: string): boolean {
  return /^sha256-[a-f0-9]{64}\.(sig|att|sbom)$/.test(tag)
}
