import type { StorageVolume } from './storage'

export interface StorageHistoryPoint { ts: number; used: number | null; total: number | null }
export interface StorageHistoryVolume extends Pick<StorageVolume, 'id' | 'name' | 'kind'> { points: StorageHistoryPoint[] }
export interface StorageHistory { res: 'm' | 'h' | 'd'; from: number; to: number; step: number; truncated?: boolean; volumes: StorageHistoryVolume[] }
export interface StorageHistoryRange { range?: '1h' | '4h' | '24h' | '7d' | '30d'; from?: number; to?: number }

export function storageHistoryURL(uuid: string, range: StorageHistoryRange): string {
  if (!/^\d+$/.test(uuid)) throw new Error('404')
  const query = new URLSearchParams()
  if (range.from !== undefined && range.to !== undefined) {
    query.set('from', String(Math.floor(range.from)))
    query.set('to', String(Math.floor(range.to)))
  } else query.set('range', range.range ?? '1h')
  return `/api/probe/nodes/${uuid}/storage-history?${query}`
}

/** History averages are fractional counts; round only the display, never the series. */
export function formatMetricCount(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(Number(value.toFixed(1))) : '—'
}

/** Use one actual paired sample from this bucket or the immediately preceding bucket. */
export function latestStoragePoint(points: StorageHistoryPoint[], to: number, step: number): StorageHistoryPoint | undefined {
  if (!Number.isFinite(to) || !Number.isFinite(step) || step <= 0) return undefined
  const oldest = Math.floor(to / step) * step - step
  return points.findLast(point => point.ts >= oldest && point.ts <= to && point.used !== null && point.total !== null && Number.isFinite(point.used) && Number.isFinite(point.total))
}
