// A view owns one lease. Responses from a closed view are never adopted, and
// late creates/renewals are released again after they finish on the server.
export interface DiagnosticClock { monotonic: number; wall: number }
export interface DiagnosticLease { id: string; fresh_for_ms: number }
export interface DiagnosticSessionState<T> {
  phase: 'idle' | 'starting' | 'active' | 'error'
  data?: T; requestedAt?: DiagnosticClock; error?: unknown
}
export interface DiagnosticSessionIO<L extends DiagnosticLease, T> {
  newID(): string
  create(id: string, signal: AbortSignal): Promise<L>
  renew(id: string, signal: AbortSignal): Promise<L>
  release(id: string): Promise<unknown>
  read(id: string, signal: AbortSignal): Promise<T>
  clock(): DiagnosticClock
  changed(state: DiagnosticSessionState<T>): void
  schedule(fn: () => void, ms: number): unknown
  cancel(timer: unknown): void
}
export function diagnosticElapsed(start: DiagnosticClock, now: DiagnosticClock): number {
  return Math.max(0, now.monotonic - start.monotonic, now.wall - start.wall)
}
export function diagnosticFresh(freshForMS: number, start: DiagnosticClock | undefined, now: DiagnosticClock, available: boolean): boolean {
  return available && !!start && Number.isFinite(freshForMS) && freshForMS > diagnosticElapsed(start, now)
}
export class DiagnosticSession<L extends DiagnosticLease, T> {
  private io: DiagnosticSessionIO<L, T>
  private generation = 0
  private lease?: L
  private timers = new Set<unknown>()
  private requests = new Set<AbortController>()
  private state: DiagnosticSessionState<T> = { phase: 'idle' }
  constructor(io: DiagnosticSessionIO<L, T>) { this.io = io }
  private later(fn: () => void, ms: number) {
    let handle: unknown
    handle = this.io.schedule(() => { this.timers.delete(handle); fn() }, ms)
    this.timers.add(handle)
    return handle
  }
  private clear(handle: unknown) { this.io.cancel(handle); this.timers.delete(handle) }
  private release(id?: string) { if (id) void this.io.release(id).catch(() => { /* Server TTL is the final cleanup bound. */ }) }
  private publish(state: DiagnosticSessionState<T>) { this.state = state; this.io.changed(state) }
  stop() {
    this.generation++
    for (const timer of this.timers) this.io.cancel(timer)
    this.timers.clear()
    for (const request of this.requests) request.abort()
    this.requests.clear()
    this.release(this.lease?.id); this.lease = undefined
    this.publish({ phase: 'idle' })
  }
  private fail(generation: number, error: unknown) {
    if (generation !== this.generation) return
    this.stop(); this.publish({ phase: 'error', error })
  }
  private async request<V>(generation: number, call: (signal: AbortSignal) => Promise<V>): Promise<V> {
    const controller = new AbortController()
    this.requests.add(controller)
    const timer = this.later(() => this.fail(generation, new Error('request_timeout')), 10000)
    try { return await call(controller.signal) } finally { this.requests.delete(controller); this.clear(timer) }
  }
  async start() {
    if (this.state.phase === 'starting' || this.state.phase === 'active') return
    this.stop()
    const generation = this.generation, started = this.io.clock(), id = this.io.newID()
    this.lease = { id, fresh_for_ms: 0 } as L
    this.publish({ phase: 'starting' })
    try {
      const lease = await this.request(generation, signal => this.io.create(id, signal))
      if (generation !== this.generation) { this.release(lease.id); return }
      if (lease.id !== id) { this.release(lease.id); throw new Error('lease_identity') }
      this.lease = lease
      if (!lease.id || !diagnosticFresh(lease.fresh_for_ms, started, this.io.clock(), true)) throw new Error('lease_expired')
      this.publish({ phase: 'active' })
      this.renewLater(generation, lease, started)
      void this.poll(generation, lease.id)
    } catch (error) { this.fail(generation, error) }
  }
  private renewLater(generation: number, lease: L, started: DiagnosticClock) {
    const remaining = lease.fresh_for_ms - diagnosticElapsed(started, this.io.clock())
    if (remaining <= 0) { this.fail(generation, new Error('lease_expired')); return }
    this.later(() => { void this.renew(generation, lease.id) }, Math.min(10000, Math.max(100, remaining / 3)))
  }
  private async renew(generation: number, id: string) {
    if (generation !== this.generation) return
    const started = this.io.clock()
    try {
      const lease = await this.request(generation, signal => this.io.renew(id, signal))
      if (generation !== this.generation) { this.release(id); if (lease.id !== id) this.release(lease.id); return }
      if (lease.id !== id || !diagnosticFresh(lease.fresh_for_ms, started, this.io.clock(), true)) { this.release(lease.id); throw new Error('lease_expired') }
      this.lease = lease; this.renewLater(generation, lease, started)
    } catch (error) { if (generation !== this.generation) this.release(id); else this.fail(generation, error) }
  }
  private async poll(generation: number, id: string) {
    if (generation !== this.generation) return
    const requestedAt = this.io.clock()
    try {
      const data = await this.request(generation, signal => this.io.read(id, signal))
      if (generation !== this.generation) return
      this.publish({ phase: 'active', data, requestedAt })
      this.later(() => { void this.poll(generation, id) }, 1000)
    } catch (error) { this.fail(generation, error) }
  }
}
