export interface ProbeTimings { dns_ms: number | null; connect_ms: number | null; tls_ms: number | null; response_ms: number | null }
export interface NetworkSample {
 task_id: number; name: string; series?: string; family?: 'ipv4' | 'ipv6'; type?: string; latency_ms: number; loss?: number; mbps?: number; at?: number
 quality?: { epoch: string; sequence: number; type: string; outcome: string; duration_ms: number; http_status?: number; interval_seconds: number; timings?: ProbeTimings; window: { attempts: number; failed: number; min_ms: number | null; max_ms: number | null; p50_ms: number | null; p95_ms: number | null; jitter_ms: number | null } }
}
export interface NetworkPoint {
 task_id: number; name: string; series?: string; family?: 'ipv4' | 'ipv6'; type?: string; ts: number; n: number; lost: number; avg_ms: number; avg_mbps?: number
 quality?: { samples: number; type: string; min_ms: number | null; max_ms: number | null; p50_ms: number | null; p95_ms: number | null; jitter_ms: number | null; timings: ProbeTimings; outcomes: Record<string, number>; skipped: number }
}
export interface NetworkHistory { current: NetworkSample[]; points: NetworkPoint[]; from: number; to: number; step: number }

// A probe target/protocol change starts a separate history, even when its name is unchanged.
export function networkIdentity(p: Pick<NetworkPoint, 'task_id' | 'name' | 'series'>): string {
 return JSON.stringify([p.task_id, p.name, p.series ?? ''])
}
export function networkLabel(p: { name: string; family?: 'ipv4' | 'ipv6'; type?: string; quality?: { type: string } }): string {
 return [p.name, p.family === 'ipv4' ? 'IPv4' : p.family === 'ipv6' ? 'IPv6' : '', p.family ? (p.type ?? p.quality?.type ?? '').replace('tcp_reachability', 'tcp').toUpperCase() : ''].filter(Boolean).join(' · ')
}
