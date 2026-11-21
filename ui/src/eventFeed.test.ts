import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { subscribeToEvents, type RegistryEvent } from './api'

// Two console tabs, simulated in one test environment: they share the Web
// Lock manager and the BroadcastChannel bus, as tabs of one browser do.

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(readonly url: string) {
    FakeEventSource.instances.push(this)
  }
  close() {
    this.closed = true
  }
  emit(event: RegistryEvent) {
    this.onmessage?.({ data: JSON.stringify(event) })
  }
  static open() {
    return FakeEventSource.instances.filter(s => !s.closed)
  }
}

class FakeChannel {
  static all: FakeChannel[] = []
  onmessage: ((e: { data: unknown }) => void) | null = null
  closed = false
  constructor(readonly name: string) {
    FakeChannel.all.push(this)
  }
  postMessage(data: unknown) {
    if (this.closed) throw new Error('closed')
    for (const other of FakeChannel.all) {
      if (other !== this && !other.closed && other.name === this.name) {
        queueMicrotask(() => other.onmessage?.({ data: structuredClone(data) }))
      }
    }
  }
  close() {
    this.closed = true
  }
}

/** An exclusive Web Lock: callbacks run one at a time, in request order. */
function fakeLocks() {
  const queue: { cb: () => Promise<void>; signal?: AbortSignal; resolve: () => void; reject: (e: unknown) => void }[] = []
  let held = false
  const next = () => {
    if (held) return
    const waiter = queue.shift()
    if (!waiter) return
    held = true
    waiter
      .cb()
      .then(waiter.resolve, waiter.reject)
      .finally(() => {
        held = false
        next()
      })
  }
  return {
    request(_name: string, opts: { signal?: AbortSignal }, cb: () => Promise<void>) {
      return new Promise<void>((resolve, reject) => {
        const waiter = { cb, signal: opts.signal, resolve, reject }
        opts.signal?.addEventListener('abort', () => {
          const i = queue.indexOf(waiter)
          if (i >= 0) {
            queue.splice(i, 1)
            reject(new DOMException('aborted', 'AbortError'))
          }
        })
        queue.push(waiter)
        next()
      })
    },
  }
}

const flush = () => new Promise(r => setTimeout(r, 0))
const push = (tag: string): RegistryEvent => ({ type: 'push', name: 'team/app', tag }) as RegistryEvent

describe('subscribeToEvents shares one stream across tabs', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    FakeChannel.all = []
    localStorage.setItem('dockyard_token', 'header.payload.sig')
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.stubGlobal('BroadcastChannel', FakeChannel)
    Object.defineProperty(navigator, 'locks', { value: fakeLocks(), configurable: true })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    Object.defineProperty(navigator, 'locks', { value: undefined, configurable: true })
    localStorage.clear()
  })

  it('opens one connection for two tabs and relays events and status to the other', async () => {
    const tabA = { events: [] as string[], status: [] as boolean[] }
    const tabB = { events: [] as string[], status: [] as boolean[] }
    const stopA = subscribeToEvents(e => tabA.events.push(e.tag!), s => tabA.status.push(s))
    const stopB = subscribeToEvents(e => tabB.events.push(e.tag!), s => tabB.status.push(s))
    await flush()

    expect(FakeEventSource.open()).toHaveLength(1)
    const stream = FakeEventSource.open()[0]
    stream.onopen?.()
    stream.emit(push('1.0'))
    await flush()

    expect(tabA.events).toEqual(['1.0'])
    expect(tabB.events).toEqual(['1.0'])
    expect(tabA.status).toEqual([true])
    expect(tabB.status).toContain(true)

    stopA()
    stopB()
  })

  it('hands the connection over when the leading tab closes', async () => {
    const tabB = [] as string[]
    const stopA = subscribeToEvents(() => {})
    const stopB = subscribeToEvents(e => tabB.push(e.tag!))
    await flush()
    const first = FakeEventSource.open()[0]

    stopA() // the leading tab goes away
    await flush()

    expect(first.closed).toBe(true)
    const open = FakeEventSource.open()
    expect(open).toHaveLength(1)
    expect(open[0]).not.toBe(first)
    open[0].emit(push('2.0'))
    await flush()
    expect(tabB).toEqual(['2.0'])

    stopB()
    await flush()
    expect(FakeEventSource.open()).toHaveLength(0)
  })

  it('a tab that leaves before getting the lock never opens a connection', async () => {
    const stopA = subscribeToEvents(() => {})
    const stopB = subscribeToEvents(() => {})
    await flush()
    stopB()
    stopA()
    await flush()
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('falls back to one stream per tab without Web Locks', async () => {
    Object.defineProperty(navigator, 'locks', { value: undefined, configurable: true })
    const stopA = subscribeToEvents(() => {})
    const stopB = subscribeToEvents(() => {})
    expect(FakeEventSource.open()).toHaveLength(2)
    stopA()
    stopB()
  })
})
