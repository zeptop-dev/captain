// Shared service-status semantics; the site additionally consumes public map metadata.
import type { GeoLocation } from '../../../shared/geolocation'
export const statusStates = ['healthy', 'degraded', 'down', 'maintenance', 'unknown'] as const
export type StatusState = typeof statusStates[number]
export const statusReasons = ['healthy', 'no_data', 'stale', 'config_pending', 'component_failure', 'no_route', 'partial_failure', 'partial_unknown', 'unsupported', 'external_path', 'maintenance'] as const
export const statusEvidence = ['inbound', 'forward_tcp', 'wireguard', 'vless_reverse', 'client_chain'] as const
export interface StatusCategory { id: number; name: string; description: string; sort: number }
export interface StatusEntry {
  id: number; name: string; rate: number; region: string; category: StatusCategory | null
  location?: GeoLocation | null
  state: StatusState; reason: string; evidence: string[]; observed_at: number | null
  end_to_end: false; maintenance: { starts_at: number; ends_at: number; note: string } | null
}
export interface ServiceStatus {
  generated_at: number; refresh_seconds: number
  summary: Record<StatusState, number> & { total: number }; entries: StatusEntry[]
}
export class StatusRequestError extends Error {
  status: number
  constructor(status: number) { super('Service status unavailable'); this.status = status }
}
export async function fetchServiceStatus(url: string, signal: AbortSignal): Promise<ServiceStatus> {
  // A stalled response must become an error rather than keeping old green data.
  const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.any([signal, AbortSignal.timeout(12_000)]) })
  if (!response.ok) throw new StatusRequestError(response.status)
  const data = await response.json() as ServiceStatus
  if (!data || !Number.isFinite(data.generated_at) || !Array.isArray(data.entries) || data.entries.some(entry => !entry || !Number.isSafeInteger(entry.id) || typeof entry.name !== 'string' || !Array.isArray(entry.evidence))) throw new StatusRequestError(0)
  return data
}
export function snapshotExpired(data: ServiceStatus, now: number): boolean {
  return data.generated_at <= 0 || now - data.generated_at >= 180 || data.generated_at - now > 60
}
export function currentEntries(data: ServiceStatus, now: number): StatusEntry[] {
  const expired = snapshotExpired(data, now)
  return data.entries.map(entry => {
    const unknown = (reason: string): StatusEntry => ({ ...entry, state: 'unknown', reason, maintenance: null })
    if (expired) return unknown('stale')
    if (!statusStates.includes(entry.state)) return unknown('unsupported')
    if (entry.state === 'maintenance') {
      const maintenance = entry.maintenance
      return maintenance && Number.isFinite(maintenance.starts_at) && Number.isFinite(maintenance.ends_at) && maintenance.starts_at <= now && maintenance.ends_at > now ? entry : unknown('stale')
    }
    if (entry.observed_at !== null && (!Number.isFinite(entry.observed_at) || now - entry.observed_at >= 180 || entry.observed_at - now > 60)) return unknown('stale')
    if (!entry.observed_at && entry.state !== 'unknown') return unknown('no_data')
    if (!statusReasons.includes(entry.reason as typeof statusReasons[number])) return unknown('unsupported')
    return entry
  })
}
export function summarize(entries: StatusEntry[]) {
  const result = { healthy: 0, degraded: 0, down: 0, maintenance: 0, unknown: 0, total: entries.length }
  for (const entry of entries) result[entry.state]++
  return result
}
export function statusCategories(entries: StatusEntry[]) {
  return Array.from(new Map(entries.flatMap(entry => entry.category ? [[entry.category.id, entry.category] as const] : [])).values()).sort((a, b) => a.sort - b.sort || a.id - b.id)
}
export const statusColors: Record<StatusState, string> = { healthy: 'teal', degraded: 'orange', down: 'red', maintenance: 'yellow', unknown: 'gray' }
export function headlineKey(summary: ReturnType<typeof summarize>): string {
  if (!summary.total) return 'emptyTitle'
  if (summary.down === summary.total) return 'allDown'
  if (summary.down || summary.degraded) return 'issues'
  if (summary.maintenance) return 'maintaining'
  if (summary.unknown) return 'awaiting'
  return 'allHealthy'
}
