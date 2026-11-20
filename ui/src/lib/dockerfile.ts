import type { HistoryStep } from '@/api'

/** Whether the server could pair steps with layers (it refuses to guess). */
export function historyIsAttributed(history?: HistoryStep[]): boolean {
  return !!history?.some(step => step.layer_digest)
}

/** A build step as a person would have written it in the Dockerfile. */
export interface Instruction {
  /** Upper-case Dockerfile keyword (RUN, COPY, ENV…), or '' when unknown. */
  keyword: string
  /** Everything after the keyword. */
  args: string
}

const KEYWORDS = new Set([
  'ADD', 'ARG', 'CMD', 'COPY', 'ENTRYPOINT', 'ENV', 'EXPOSE', 'HEALTHCHECK', 'LABEL',
  'MAINTAINER', 'ONBUILD', 'RUN', 'SHELL', 'STOPSIGNAL', 'USER', 'VOLUME', 'WORKDIR',
])

/**
 * Turns an image config's `created_by` back into a Dockerfile line.
 *
 * Builders record steps differently:
 * - BuildKit:  `RUN /bin/sh -c apk add curl # buildkit`, `COPY app /app # buildkit`
 * - legacy:    `/bin/sh -c apk add curl` for RUN, `/bin/sh -c #(nop)  CMD ["/app"]` for the rest
 * - build args are prefixed: `|2 VERSION=1.2 TARGET=x /bin/sh -c make`
 */
export function parseInstruction(createdBy: string): Instruction {
  let text = createdBy.trim().replace(/\s+# buildkit$/, '')
  text = text.replace(/^\|\d+\s+(?:\S+=\S*\s+)*/, '')

  const nop = text.match(/^\/bin\/sh -c #\(nop\)\s+(.*)$/s)
  if (nop) text = nop[1].trim()
  else if (text.startsWith('/bin/sh -c ')) text = `RUN ${text.slice('/bin/sh -c '.length)}`

  // BuildKit keeps the shell form inside RUN: "RUN /bin/sh -c apk add curl".
  text = text.replace(/^RUN \/bin\/sh -c /, 'RUN ')

  const [first, ...rest] = text.split(' ')
  if (KEYWORDS.has(first)) return { keyword: first, args: rest.join(' ').trim() }
  return { keyword: '', args: text }
}
