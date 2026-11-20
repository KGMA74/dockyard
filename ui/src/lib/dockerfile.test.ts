import { describe, expect, it } from 'vitest'
import { parseInstruction } from './dockerfile'

describe('parseInstruction', () => {
  it.each([
    // BuildKit
    ['RUN /bin/sh -c apk add --no-cache curl # buildkit', 'RUN', 'apk add --no-cache curl'],
    ['COPY dist /app # buildkit', 'COPY', 'dist /app'],
    ['ENV PATH=/usr/local/bin:/usr/bin', 'ENV', 'PATH=/usr/local/bin:/usr/bin'],
    ['WORKDIR /app', 'WORKDIR', '/app'],
    // legacy builder
    ['/bin/sh -c apt-get update && apt-get install -y git', 'RUN', 'apt-get update && apt-get install -y git'],
    ['/bin/sh -c #(nop)  CMD ["/app"]', 'CMD', '["/app"]'],
    ['/bin/sh -c #(nop) ADD file:1a2b3c in / ', 'ADD', 'file:1a2b3c in /'],
    // build args prefix
    ['|2 VERSION=1.2 TARGET=linux /bin/sh -c make build', 'RUN', 'make build'],
  ])('%s', (input, keyword, args) => {
    expect(parseInstruction(input)).toEqual({ keyword, args })
  })

  it('keeps text it does not recognise as-is', () => {
    expect(parseInstruction('crane append')).toEqual({ keyword: '', args: 'crane append' })
  })
})
