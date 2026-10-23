import type { ResourceHost } from './resources'
export interface MonitorNode { id: number; name: string; group: string; paired: boolean; online: boolean; stale: boolean; last_seen: string | null; sampled_at: number; info: { region?: string; provider?: string }; host: ResourceHost | null }
export interface HistorySeries { key: string; kind: string; device: string; metric: string }
export interface ResourceHistory { step: number; points: { ts: number; average: number | null; peak: number | null; samples: number }[]; series: HistorySeries[]; truncated: boolean }
export const hostMetrics = ['cpu', 'memory', 'swap', 'disk', 'up', 'down', 'load', 'tcp', 'udp', 'processes']
export const historyRanges = ['1h', '24h', '7d', '14d', '30d', '90d']
export function metricUnit(metric: string) { return ['cpu', 'memory', 'swap', 'disk', 'inodes', 'utilization', 'vramPct'].includes(metric) ? '%' : ['up', 'down', 'read', 'write'].includes(metric) ? 'B/s' : ['rss', 'vram', 'used', 'total'].includes(metric) ? 'B' : metric === 'temperature' ? '°C' : metric === 'power' ? 'W' : '' }
