export type TrafficQuality = 'observed' | 'partial' | 'unknown'
export interface TrafficSource {
  core: string; counter: 'users' | 'inbounds' | 'outbounds'; started_at?: string; ended_at: string
  duration_ms?: number; quality: TrafficQuality; segments: number; reasons: string[]
}
export interface TrafficHistoryItem {
  id: number; epoch: string; sequence: string; received_at: string; accounting_day: string
  up_bytes: number; down_bytes: number; discarded_samples: number; totals_known: boolean; metadata_status: 'valid' | 'legacy' | 'invalid'
  sample?: { version: number; collection_started_at: string; collection_ended_at: string; segments: number; quality: TrafficQuality; reasons: string[]; sources: TrafficSource[] }
}
export interface TrafficDelivery {
  sampled_at: string; received_at: string; fresh_for_ms: number
  pending_bytes: number; buffered_bytes: number; journal_bytes: number; oldest_at?: string
}
export interface TrafficHistoryPage {
  items: TrafficHistoryItem[]; next_before?: number
  retention: { days: number; max_rows: number; global_max_rows: number }; delivery?: TrafficDelivery
}
export function trafficHistoryURL(nodeID: number, before?: number): string {
  const params = new URLSearchParams({ limit: '50' })
  if (before && Number.isSafeInteger(before) && before > 0) params.set('before', String(before))
  return `/api/admin/nodes/${nodeID}/traffic-history?${params}`
}
// Missing boundaries stay unknown. The collection operation is not a substitute
// for a source counter's observed interval, nor a minute-by-minute usage curve.
export function validHistoryTime(value?: string): boolean {
  return !!value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value))
}
export function sourceInterval(source: TrafficSource): [string, string] | null {
  if (!validHistoryTime(source.started_at) || !validHistoryTime(source.ended_at)
    || Date.parse(source.started_at!) > Date.parse(source.ended_at)) return null
  return [source.started_at!, source.ended_at]
}
export function historyQuality(item: TrafficHistoryItem): TrafficQuality | 'legacy' | 'invalid' {
  if (item.metadata_status !== 'valid') return item.metadata_status === 'legacy' ? 'legacy' : 'invalid'
  return item.sample?.version === 1 && ['observed', 'partial', 'unknown'].includes(item.sample.quality) ? item.sample.quality : 'unknown'
}
export function acceptedBatchBytes(item: TrafficHistoryItem): { up: number; down: number } | null {
  if (!item.totals_known || ![item.up_bytes, item.down_bytes].every(n => Number.isFinite(n) && n >= 0)) return null
  return { up: item.up_bytes, down: item.down_bytes }
}
