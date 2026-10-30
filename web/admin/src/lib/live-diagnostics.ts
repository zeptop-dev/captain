export interface LiveSample {
  epoch: string; sequence: number; sampled_at: string; elapsed_ms: number
  cpu_percent: number | null; memory_used: number | null; memory_total: number | null
  net_up_bytes_per_second: number | null; net_down_bytes_per_second: number | null
  interfaces: { name: string; rx_bytes_per_second: number | null; tx_bytes_per_second: number | null }[] | null; truncated: boolean
}
export interface LiveDiagnostics {
  status: 'waiting' | 'live' | 'stale' | 'unsupported' | 'expired'
  interval_ms: number; fresh_for_ms: number; node_id: number; epoch: string; latest_sequence: number; samples: LiveSample[]
}
export interface LiveLease { lease_id: string; ttl_ms: number; renew_after_ms: number; interval_ms: number }
// Cursor and ring belong to one viewing lease. A late GET from a closed lease
// must not alter a reopened view, even when its node epoch is unchanged.
export class LiveSampleWindow {
  private leaseID?: string
  private epoch = ''
  private samples: LiveSample[] = []
  after = 0
  open(id: string) { this.leaseID = id; this.epoch = ''; this.samples = []; this.after = 0 }
  close(id: string) {
    if (this.leaseID === id) { this.leaseID = undefined; this.epoch = ''; this.samples = []; this.after = 0 }
  }
  accept(id: string, view: LiveDiagnostics): LiveDiagnostics {
    if (this.leaseID !== id) throw new Error('closed diagnostics session')
    const changedEpoch = this.epoch !== view.epoch
    this.samples = mergeLiveSamples(changedEpoch ? [] : this.samples, view)
    // A restarted process may have a lower sequence than the old cursor.
    this.after = changedEpoch && !view.samples.length ? 0 : view.latest_sequence
    this.epoch = view.epoch
    return { ...view, samples: this.samples }
  }
}
export function mergeLiveSamples(previous: LiveSample[], view: LiveDiagnostics): LiveSample[] {
  const all = [...previous.filter(s => s.epoch === view.epoch), ...view.samples.filter(s => s.epoch === view.epoch)]
  const bySequence = new Map<number, LiveSample>()
  for (const sample of all) {
    if (Number.isSafeInteger(sample.sequence) && sample.sequence > 0 && Number.isFinite(Date.parse(sample.sampled_at))) bySequence.set(sample.sequence, sample)
  }
  const ordered = [...bySequence.values()].sort((a, b) => a.sequence - b.sequence).slice(-120)
  const latestAt = Date.parse(ordered.at(-1)?.sampled_at ?? '')
  // Also bound time, so disconnected gaps cannot make 120 points span hours.
  // A node clock rollback discards points that would now lie in its future.
  return ordered.filter(s => { const age = latestAt - Date.parse(s.sampled_at); return age >= 0 && age < 120000 })
}
// Break graphs at missing observations. A line between distant samples would
// invent continuity during browser/network gaps or a paused sampling loop.
export function liveGraphPoints(samples: LiveSample[]) {
  const out: { index: number; up: number | null; down: number | null }[] = []
  for (let i = 0; i < samples.length; i++) {
    const sample = samples[i], prior = samples[i - 1]
    const elapsed = prior ? Date.parse(sample.sampled_at) - Date.parse(prior.sampled_at) : 0
    if (prior && (sample.epoch !== prior.epoch || sample.sequence !== prior.sequence + 1 || elapsed <= 0 || elapsed > 3000 || sample.elapsed_ms > 3000)) out.push({ index: out.length, up: null, down: null })
    out.push({ index: out.length, up: finiteMetric(sample.net_up_bytes_per_second), down: finiteMetric(sample.net_down_bytes_per_second) })
  }
  return out
}
export function finiteMetric(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}
