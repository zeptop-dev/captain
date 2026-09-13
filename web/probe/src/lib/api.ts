export interface Sample { t: number; cpu: number; mem: number; up: number; down: number }
export interface Ping { task_id: number; name: string; latency_ms: number; loss?: number; at?: number }
export interface Host {
  cpu_percent?: number; mem_total?: number; mem_used?: number; swap_total?: number; swap_used?: number; disk_total?: number; disk_used?: number
  load1?: number; load5?: number; load15?: number; net_up?: number; net_down?: number; net_total_up?: number; net_total_down?: number
  tcp?: number; udp?: number; processes?: number; uptime?: number; ipv4?: boolean; ipv6?: boolean
  info?: { cpu_model?: string; cpu_cores?: number; os?: string; kernel?: string; arch?: string; virt?: string }
  pings?: Ping[]
}
export interface Node {
  id: number; name: string; online: boolean; addr?: string; version?: string; last_seen: string | null
  info: { region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }
  host: Host | null
  traffic: { used: number; limit: number; prev: number; mode: string; period_start: number; reset_day: number }
  recent: Sample[]
}
export interface Snapshot { title: string; logo: string; show_globe: boolean; beat_seconds: number; visibility: string; carrier_ping: boolean; now: number; nodes: Node[]; staff: boolean }
export interface StatPoint { ts: number; n: number; cpu: number; mem_used: number; mem_total: number; swap_used: number; disk_used: number; disk_total: number; net_up: number; net_down: number; load1: number; tcp: number; udp: number; procs: number }
export interface PingPoint { task_id: number; name: string; ts: number; n: number; lost: number; avg_ms: number }

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
