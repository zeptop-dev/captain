import type { NetworkSample } from './network'
import type { ProbeAppearance } from '../appearance'
export interface Validity { cpu: boolean; memory: boolean; swap: boolean; disk: boolean; network: boolean; load: boolean; connections: boolean; processes: boolean }
export interface Sample { valid?: Validity; t: number; cpu: number; mem: number; up: number; down: number }
export type Ping = NetworkSample
export interface PublicPingTask { id: number; name: string; series?: string; family?: 'ipv4' | 'ipv6'; type?: string; state?: string; reason?: string }
export interface Host {
  carrier_probe_version?: number
  valid?: Validity
  cpu_percent?: number; mem_total?: number; mem_used?: number; swap_total?: number; swap_used?: number; disk_total?: number; disk_used?: number
  load1?: number; load5?: number; load15?: number; net_up?: number; net_down?: number; net_total_up?: number; net_total_down?: number
  tcp?: number; udp?: number; processes?: number; uptime?: number; ipv4?: boolean; ipv6?: boolean
  info?: import('./cpu-spec').CPUInfo & { cpu_model?: string; os?: string; kernel?: string; arch?: string; virt?: string }
  pings?: Ping[]
}
export interface Node {
  storage?: import('./storage').StorageVolume[]
  ping_tasks?: PublicPingTask[]
  id: number; name: string; group?: string; online: boolean; addr?: string; version?: string; last_seen: string | null
  info: { bandwidth?: import('./node-bandwidth').NodeBandwidth; billing?: import('./node-price').NodeCost; auto_renewal?: import('./node-cost').NodeAutoRenewal; region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }
  host: Host | null
  traffic: { used: number; used_up?: number; used_down?: number; limit: number; prev: number; mode: string; period_start: number; reset_day: number }
  recent: Sample[]
}
export interface Snapshot { finance_currency?: string; exchange_rates?: import('./node-cost').ExchangeRates; appearance_scheme?: string; appearance?: ProbeAppearance | null; public_sections?: string[] | null; layout?: string; title: string; logo: string; show_globe: boolean; beat_seconds: number; visibility: string; carrier_ping: boolean; now: number; nodes: Node[]; staff: boolean }
export interface StatPoint { valid?: Validity; ts: number; n: number; cpu: number; mem_used: number; mem_total: number; swap_used: number; disk_used: number; disk_total: number; net_up: number; net_down: number; load1: number; tcp: number; udp: number; procs: number }
export interface PingPoint { task_id: number; name: string; group?: string; ts: number; n: number; lost: number; avg_ms: number }

export async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin' })
  if (!r.ok) throw new Error(String(r.status))
  return r.json() as Promise<T>
}

export function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0; let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v < 10 ? v.toFixed(2) : v < 100 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}
export function rate(bps: number): string { return bytes(bps * 8 / 8) + '/s' }
export function pct(used?: number, total?: number): number { return used && total ? Math.min(100, Math.round((used / total) * 100)) : 0 }
export function uptime(s?: number): string {
  if (!s) return '—'
  const d = Math.floor(s / 86400); const h = Math.floor((s % 86400) / 3600)
  return d > 0 ? `${d}d ${h}h` : `${h}h ${Math.floor((s % 3600) / 60)}m`
}
export function flag(cc?: string): string {
  if (!cc || cc.length !== 2) return ''
  const up = cc.toUpperCase()
  return String.fromCodePoint(...[...up].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

export function publicShows(s: Snapshot, section: string): boolean { return s.public_sections == null || s.public_sections.includes(section) }
