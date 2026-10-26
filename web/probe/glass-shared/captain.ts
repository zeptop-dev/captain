import { nodeBandwidth } from '../src/lib/node-bandwidth.ts'
import { nodePrice } from '../src/lib/node-price.ts'
import { cpuSpec } from '../src/lib/cpu-spec.ts'
import { matchesPing } from './ping-selection.ts'
import { networkIdentity, networkLabel } from '../src/lib/network.ts'
import { storageItems } from '../src/lib/storage.ts'
/** Read-only Captain transport for the MIT Glassmorphism public frontend.
 * Only the public probe API is used: no Komari RPC, agent tokens or admin data.
 */
import { box } from './box.ts'
import type { Snapshot, Node, Host, StatPoint, Sample, Validity } from '../src/lib/api'
import type { NetworkHistory, NetworkPoint } from '../src/lib/network'
// Re-exported so both component trees take these from one module.
export type { Snapshot, Node, Host, StatPoint, Sample, Validity, NetworkHistory, NetworkPoint }
export type { PublicPingTask } from '../src/lib/api'
import type { Client, NodeStatus, StatusRecord, MetricQueryParams, MetricQueryResponse, MetricSeries, PingMetricStatsResponse } from './utils/rpc'

export const snapshot = box<Snapshot | null>(null)
export const shows = (section: string) => snapshot.value?.public_sections == null || snapshot.value.public_sections.includes(section)
export function captainStorage(uuid: string, translate: (key: string) => string) {
  return shows('disk') ? storageItems(snapshot.value?.nodes.find(n => String(n.id) === uuid), translate) : []
}
export function captainCPUSpec(uuid: string, locale = 'en'): string {
  if (!shows('system')) return ''
  return cpuSpec(snapshot.value?.nodes.find(n => String(n.id) === uuid)?.host?.info, locale)
}
// These totals survive node/agent restarts. Never fall back to OS lifetime
// counters on older snapshots: that would silently change the period again.
export function periodTraffic(n: Node) {
  const count = (v: unknown) => shows('traffic') && typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : Number.NaN
  return { up: count(n.traffic.used_up), down: count(n.traffic.used_down), used: count(n.traffic.used) }
}
const iso = (seconds: number) => new Date(seconds * 1000).toISOString()
const metric = (host: Host | null, section: string, validity: keyof Validity, key: keyof Host): number =>
  !shows(section) || !host || host.valid?.[validity] === false ? Number.NaN : Number(host[key] ?? 0)

export async function readJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000) })
  if (!response.ok) throw new Error(String(response.status))
  return response.json() as Promise<T>
}

export function clientFor(n: Node, weight: number): Client {
  const h = n.host, info = h?.info
  return {
    uuid: String(n.id), name: n.name, cpu_name: info?.cpu_model ?? '', cpu_cores: info?.cpu_cores ?? 0,
    virtualization: info?.virt ?? '', arch: info?.arch ?? '', os: info?.os ?? '', kernel_version: info?.kernel ?? '',
    region: n.info.region ?? '', location: shows('info') ? n.info.location : null, public_remark: n.info.note ?? '',
    ...(n.addr ? { [n.addr.includes(':') ? 'ipv6' : 'ipv4']: n.addr } : {}),
    mem_total: metric(h, 'memory', 'memory', 'mem_total'), swap_total: metric(h, 'memory', 'swap', 'swap_total'),
    disk_total: metric(h, 'disk', 'disk', 'disk_total'), version: n.version, weight,
    // Keep the legacy day-based finance model disabled. Cost cards read Captain's
    // structured calendar billing from the public snapshot; never infer days or FX.
    price: Number.NaN, billing_cycle: 0, auto_renewal: false, currency: '', expired_at: n.info.expires_at ?? '',
    group: n.group ?? '', tags: '', hidden: false, traffic_limit: shows('traffic') ? n.traffic.limit : Number.NaN,
    traffic_limit_type: n.traffic.mode || 'sum', created_at: '', updated_at: n.last_seen ?? '',
  }
}

export function statusFor(n: Node): NodeStatus {
  const h = n.host
  const traffic = periodTraffic(n)
  return {
    client: String(n.id), time: n.last_seen ?? '', online: n.online,
    cpu: metric(h, 'cpu', 'cpu', 'cpu_percent'), gpu: Number.NaN, temp: Number.NaN,
    ram: metric(h, 'memory', 'memory', 'mem_used'), ram_total: metric(h, 'memory', 'memory', 'mem_total'),
    swap: metric(h, 'memory', 'swap', 'swap_used'), swap_total: metric(h, 'memory', 'swap', 'swap_total'),
    disk: metric(h, 'disk', 'disk', 'disk_used'), disk_total: metric(h, 'disk', 'disk', 'disk_total'),
    load: metric(h, 'system', 'load', 'load1'), load5: metric(h, 'system', 'load', 'load5'), load15: metric(h, 'system', 'load', 'load15'),
    net_in: metric(h, 'network', 'network', 'net_down'), net_out: metric(h, 'network', 'network', 'net_up'),
    // The original UI's total fields now consistently mean persisted period
    // traffic; the Captain API's host.net_total_* fields retain their OS meaning.
    net_total_up: traffic.up, net_total_down: traffic.down,
    traffic_up: traffic.up, traffic_down: traffic.down,
    process: metric(h, 'system', 'processes', 'processes'), connections: metric(h, 'system', 'connections', 'tcp'),
    connections_udp: metric(h, 'system', 'connections', 'udp'), uptime: shows('system') && h ? h.uptime ?? 0 : Number.NaN,
  }
}

// Cache successful history reads briefly and coalesce concurrent latency/stats calls.
// Nothing is persisted in the visitor's browser; a new snapshot invalidates it.
const histories = new Map<string, { at: number, promise: Promise<unknown> }>()
export function clearHistoryCache() { histories.clear() }
function rangeFor(hours: number, ping: boolean) {
  if (hours <= 1) return ping ? '1h' : '24h' // persisted minute data has all resource metrics
  if (hours <= 24) return '24h'
  return hours <= 168 ? '7d' : '30d'
}
interface History { from: number; to: number; step: number; points: (StatPoint | Sample)[] }
export async function historyFor<T>(id: string, kind: 'pings' | 'history', hours: number, signal?: AbortSignal): Promise<T> {
  if (!/^\d+$/.test(id) || !snapshot.value?.nodes.some(n => String(n.id) === id)) throw new Error('404')
  if (!shows('history') || (kind === 'pings' && !shows('latency'))) throw new Error('404')
  const url = `/api/probe/nodes/${id}/${kind}?range=${rangeFor(hours, kind === 'pings')}`
  const cached = histories.get(url)
  if (cached && Date.now() - cached.at < 10_000) return cached.promise as Promise<T>
  // Shared requests have their own timeout; a chart cancelling must not cancel other subscribers.
  const promise = readJSON<T>(url).catch(error => { histories.delete(url); throw error })
  if (histories.size >= 256) histories.delete(histories.keys().next().value!)
  histories.set(url, { at: Date.now(), promise })
  const result = await promise
  signal?.throwIfAborted()
  return result
}
function requestedHours(params: Record<string, unknown>) {
  const start = params.start ?? params.start_time
  const hours = start ? (Date.now() - new Date(start as string).getTime()) / 3600000 : Number(params.hours) || 1
  return Math.min(720, Math.max(1, Math.ceil(hours)))
}
const taskKey = networkIdentity
// Retain legacy task IDs while adding the opaque configuration identity for new probes.
function taskID(point: Pick<NetworkPoint, 'task_id' | 'name' | 'series'>): string { return point.series ? networkIdentity(point) : `${point.task_id}:${point.name}` }
export async function pingMetrics(id: string, hours: number, bounds?: Pick<MetricQueryParams, 'start' | 'end'>): Promise<{ metrics: MetricQueryResponse; stats: PingMetricStatsResponse }> {
  const history = await historyFor<NetworkHistory>(id, 'pings', hours)
  const start = bounds?.start ? new Date(bounds.start).getTime() / 1000 : history.to - hours * 3600
  const end = bounds?.end ? Math.min(new Date(bounds.end).getTime() / 1000, history.to) : history.to
  const groups = new Map<string, NetworkPoint[]>()
  const configured = snapshot.value?.nodes.find(n => String(n.id) === id)?.ping_tasks
  for (const p of (history.points ?? []).filter(p => p.ts >= start && p.ts <= end && p.n > 0 && (configured === undefined || configured.some(task => task.state !== 'unsupported' && matchesPing(p, task))))) {
    const key = taskKey(p)
    groups.set(key, [...(groups.get(key) ?? []), p])
  }
  const series: MetricSeries[] = [], stats: PingMetricStatsResponse['stats'] = []
  for (const points of groups.values()) {
    points.sort((a, b) => a.ts - b.ts)
    const p = points[0]!, tags = { task_id: taskID(p), task_name: networkLabel(p), task_type: p.quality?.type ?? 'tcp' }
    const total = points.reduce((n, p) => n + p.n, 0), lost = points.reduce((n, p) => n + p.lost, 0), valid = total - lost
    const weighted = points.reduce((n, p) => n + Math.max(0, p.n - p.lost) * p.avg_ms, 0)
    for (const key of ['ping.latency_ms', 'ping.loss']) {
      const byTime = new Map(points.map(p => [p.ts, p]))
      const grid: MetricSeries['points'] = []
      for (let t = Math.ceil(start / history.step) * history.step; t <= end; t += history.step) {
        const p = byTime.get(t)
        grid.push({ time: iso(t), value: !p ? null : key === 'ping.loss' ? p.lost / p.n : p.n > p.lost ? p.avg_ms : null,
          count: p ? key === 'ping.loss' ? p.n : p.n - p.lost : 0 })
      }
      series.push({ metric_key: key, entity_id: id, tags, count: grid.length, points: grid, downsampled: true, interval_seconds: history.step })
    }
    stats.push({ entity_id: id, task_id: taskID(p), name: networkLabel(p), type: p.quality?.type ?? 'tcp', tags,
      total, valid, loss: total ? lost / total * 100 : 0, loss_approximate: false,
      ...(valid > 0 ? { avg: weighted / valid } : {}),
      // Quantiles cannot be recovered by averaging bucket quantiles. Omit them.
    })
  }
  const range = { start: iso(start), end: iso(end) }
  return { metrics: { ...range, series, count: series.length }, stats: { ...range, stats, count: stats.length, interval_seconds: history.step } }
}

const loadFields: Record<string, [string, keyof Validity, keyof StatPoint]> = {
  'cpu.usage': ['cpu', 'cpu', 'cpu'], 'memory.used': ['memory', 'memory', 'mem_used'], 'memory.total': ['memory', 'memory', 'mem_total'],
  'swap.used': ['memory', 'swap', 'swap_used'], 'disk.used': ['disk', 'disk', 'disk_used'], 'disk.total': ['disk', 'disk', 'disk_total'],
  'net.in.rate': ['network', 'network', 'net_down'], 'net.out.rate': ['network', 'network', 'net_up'],
  'load.average': ['system', 'load', 'load1'], 'process.count': ['system', 'processes', 'procs'],
  'connections.tcp': ['system', 'connections', 'tcp'], 'connections.udp': ['system', 'connections', 'udp'],
}
async function loadMetrics(id: string, hours: number, keys: string[]): Promise<MetricQueryResponse> {
  const history = await historyFor<History>(id, 'history', hours)
  const byTime = new Map((history.points ?? []).filter((p): p is StatPoint => 'ts' in p).map(p => [p.ts, p]))
  const start = history.to - hours * 3600
  const series = keys.flatMap(key => {
    const field = loadFields[key]
    if (!field || !shows(field[0])) return []
    const points: MetricSeries['points'] = []
    for (let ts = Math.ceil(start / history.step) * history.step; ts <= history.to; ts += history.step) {
      const p = byTime.get(ts)
      points.push({ time: iso(ts), value: p && p.n > 0 && p.valid?.[field[1]] !== false ? Number(p[field[2]] ?? 0) : null })
    }
    return [{ metric_key: key, entity_id: id, count: points.length, points, downsampled: true, interval_seconds: history.step }]
  })
  return { start: iso(start), end: iso(history.to), series, count: series.length }
}
async function loadRecords(id: string, hours: number) {
  const metrics = await loadMetrics(id, hours, Object.keys(loadFields))
  const fields: Record<string, string> = { 'cpu.usage': 'cpu', 'memory.used': 'ram', 'memory.total': 'ram_total', 'swap.used': 'swap',
    'disk.used': 'disk', 'disk.total': 'disk_total', 'net.in.rate': 'net_in', 'net.out.rate': 'net_out', 'load.average': 'load',
    'process.count': 'process', 'connections.tcp': 'connections', 'connections.udp': 'connections_udp' }
  const records = new Map<string, Record<string, unknown>>()
  for (const s of metrics.series) for (const p of s.points) {
    const row = records.get(p.time) ?? { client: id, time: p.time }
    row[fields[s.metric_key]!] = p.value
    records.set(p.time, row)
  }
  return { count: records.size, records: [...records.values()] as unknown as StatusRecord[] }
}

export async function captainCall<T>(method: string, parameters?: Record<string, unknown> | unknown[], signal?: AbortSignal): Promise<T> {
  signal?.throwIfAborted()
  const p = (parameters ?? {}) as MetricQueryParams, id = String(p.entity_id ?? p.uuid ?? '')
  const bounds = { start: p.start ?? p.start_time, end: p.end ?? p.end_time }
  const hours = requestedHours(p)
  let result: unknown
  switch (method) {
    case 'ping': case 'rpc.ping': result = 'pong'; break
    case 'public:getMe': result = { logged_in: false }; break
    case 'public:getVersion': case 'common:getBackendVersion': result = { version: '', hash: '' }; break
    case 'public:listMetricDefinitions': result = Object.keys(loadFields).filter(k => shows(loadFields[k]![0])).concat(shows('latency') ? ['ping.latency_ms', 'ping.loss'] : []).map(name => ({ name, description: name, type: 'gauge', retention_days: 30 })); break
    case 'public:getPublicPingTasks': result = []; break // names and stable identities come from each node's public history
    case 'public:getPingMetricStats': result = (await pingMetrics(id, hours, bounds)).stats; break
    case 'public:queryMetrics': {
      const keys = p.metric_keys ?? p.metrics ?? (p.metric_key ? [p.metric_key] : [])
      const values = await Promise.all([
        ...(keys.some(k => !k.startsWith('ping.')) ? [loadMetrics(id, hours, keys)] : []),
        ...(shows('latency') && keys.some(k => k.startsWith('ping.')) ? [pingMetrics(id, hours, bounds).then(p => p.metrics)] : []),
      ])
      const start = bounds.start ? new Date(bounds.start).getTime() : Date.now() - hours * 3600000
      const end = bounds.end ? new Date(bounds.end).getTime() : Date.now()
      const series = values.flatMap(v => v.series).map(s => ({...s, points:s.points.filter(v => Date.parse(v.time) >= start && Date.parse(v.time) <= end)}))
      result = {start:new Date(start).toISOString(),end:new Date(end).toISOString(),series,count:series.length}
      break
    }
    case 'public:getClientRecentRecords': result = (await loadRecords(id, hours)).records; break
    case 'common:getRecords': case 'public:getRecordsByUUID': case 'common:getNodeRecentStatus':
      result = p.type === 'ping' ? {records:[],tasks:[]} : await loadRecords(id, hours); break
    default: throw Object.assign(new Error(`Unsupported public operation: ${method}`), { code: -32601 })
  }
  signal?.throwIfAborted()
  return result as T
}

export function captainPrice(id: string, lang: string): string {
 const node = snapshot.value?.nodes.find(n => String(n.id) === id)
 return node ? nodePrice(node.info, lang) : ""
}

export function captainBandwidth(id: string, lang: string): string {
 if (!shows('info')) return ''
 return nodeBandwidth(snapshot.value?.nodes.find(n => String(n.id) === id)?.info.bandwidth, lang)
}
