import { describe, expect, it } from 'vitest'
import { formatBytes, shortDigest } from './format'

describe('formatBytes', () => {
  it('formats zero and negatives', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
  })

  it('scales through units', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1.00 KB')
    expect(formatBytes(1536)).toBe('1.50 KB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.00 MB')
    expect(formatBytes(20 * 1024 * 1024 * 1024)).toBe('20.0 GB')
  })
})

describe('shortDigest', () => {
  it('trims a sha256 digest', () => {
    expect(shortDigest('sha256:1a2b3c4d5e6f7a8b9c0d')).toBe('1a2b3c4…8b9c0d')
  })

  it('leaves short values alone', () => {
    expect(shortDigest('sha256:abcd')).toBe('abcd')
  })
})
