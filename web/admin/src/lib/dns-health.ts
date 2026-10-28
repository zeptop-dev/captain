export type DNSRecordType = 'A' | 'AAAA'
export const dnsRecordTypes: DNSRecordType[] = ['A', 'AAAA']
export interface DNSAnswer { source: string; addresses: string[]; error?: string }
export interface DNSFamily { type: DNSRecordType; expected: string[]; status: string; answers: DNSAnswer[]; propagation_pending?: boolean }
export interface DNSTarget { propagation_pending?: boolean; key: string; host: string; label: string; mode: string; expected: string[]; status: string; answers: DNSAnswer[]; families?: DNSFamily[] }
export interface DNSHealth { node_id: number; node: string; checked_at: number; stale: boolean; targets: DNSTarget[]; failure_count: number }
export interface DNSRecord { id?: string; type: string; name: string; content: string; ttl: number; proxied: boolean }
export interface DNSSyncRecord { type: DNSRecordType; expected: string; action: string; before?: DNSRecord | null; reason?: string }
export interface DNSSyncPlan { node_id: number; host: string; revision: string; enabled: boolean; reason?: string; records: DNSSyncRecord[] }
export interface DNSSyncResult { name: string; ip: string; action: string; outcome?: string; error?: string; reason?: string }

export function addressType(address: string): DNSRecordType | undefined {
  const value = address.trim()
  if (!value) return undefined
  if (value.includes(':')) {
    try { return new URL(`http://[${value}]/`).hostname.startsWith('[::ffff:') ? 'A' : 'AAAA' } catch { return undefined }
  }
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(value) && value.split('.').every(part => Number(part) <= 255)) return 'A'
  return undefined
}

// PublicAddr historically also accepted an IPv6 address. Group by the address,
// not the field name, without losing a second distinct IPv6 address.
export function registeredAddresses(node: { public_addr: string; v6_addr: string }): Record<DNSRecordType, string[]> {
  const result: Record<DNSRecordType, string[]> = { A: [], AAAA: [] }
  for (const address of [node.public_addr, node.v6_addr]) {
    const value = address?.trim(), type = addressType(value ?? '')
    if (type && !result[type].some(existing => normalizedIP(existing) === normalizedIP(value))) result[type].push(value)
  }
  return result
}

export function dnsSnapshotStale(health: DNSHealth, now = Date.now() / 1000) {
  return health.stale || !health.checked_at || health.checked_at > now || now - health.checked_at > 15 * 60
}

export function dnsFamily(target: DNSTarget, type: DNSRecordType): DNSFamily {
  const value = target.families?.find(family => family.type === type)
  if (value) return value
  // Legacy snapshots have no independent family evidence. Even a combined
  // green result must not become two green badges after upgrading the UI.
  return { type, expected: (target.expected ?? []).filter(ip => addressType(ip) === type), status: 'unknown', answers: [] }
}

export function dnsFamilyStatus(family: DNSFamily, stale: boolean, mode: string) {
  if (stale) return 'stale'
  if (family.status === 'unknown' && family.answers?.some(answer => answer.error && answer.error !== 'not_found')) return 'query_failed'
  if (family.propagation_pending && family.status !== 'ok') return 'propagating'
  if (family.status === 'ok' && mode !== 'direct') return 'resolved'
  if (family.status === 'unconfigured' && mode !== 'direct') return 'no_record'
  return family.status
}

export function dnsStatusColor(status: string) {
  if (['missing', 'mismatch', 'conflict', 'invalid', 'problem', 'failed'].includes(status)) return 'red'
  if (['ok', 'resolved', 'success'].includes(status)) return 'teal'
  if (['inconsistent', 'unexpected', 'propagating', 'query_failed'].includes(status)) return 'orange'
  return 'gray'
}

export function dnsCheckWait(checkedAt: number, now = Date.now() / 1000) {
  return checkedAt > now ? 0 : Math.max(0, Math.ceil(checkedAt + 30 - now))
}

const normalizedHost = (host: string) => {
  try { return new URL(`http://${host.trim().replace(/\.$/, '')}`).hostname.toLowerCase() } catch { return '' }
}
const normalizedIP = (ip: string) => {
  try {
    const host = new URL(ip.includes(':') ? `http://[${ip}]/` : `http://${ip}/`).hostname
    if (host.startsWith('[::ffff:')) {
      const parts = host.slice(8, -1).split(':').map(part => Number.parseInt(part, 16))
      return [parts[0] >>> 8, parts[0] & 255, parts[1] >>> 8, parts[1] & 255].join('.')
    }
    return host
  } catch { return ip }
}

export function nodeDNSTarget(health: DNSHealth | undefined, node: { id: number; domain: string; public_addr: string; v6_addr: string; domain_shared?: boolean; domain_conflict?: boolean }) {
  if (!health || health.node_id !== node.id || !node.domain) return undefined
  const target = health.targets.find(value => value.key === 'node' && normalizedHost(value.host) === normalizedHost(node.domain))
  if (!target) return undefined
  const registered = registeredAddresses(node)
  // The API's conflict flag counts duplicate names even when they are shared.
  // DNS checks give shared mode priority, including another owner's setting.
  // Reject old ownership modes in both directions while fresh health loads.
  const modes = node.domain_shared ? ['shared'] : node.domain_conflict ? ['shared', 'conflict'] : [registered.A.length || registered.AAAA.length ? 'direct' : 'resolve_only']
  if (!modes.includes(target.mode)) return undefined
  if (target.mode === 'direct') {
    const expected = [...registered.A, ...registered.AAAA].map(normalizedIP).sort()
    if (JSON.stringify(expected) !== JSON.stringify(target.expected.map(normalizedIP).sort())) return undefined
  }
  return target
}

export const dnsFamilyLabel = (type: DNSRecordType) => type === 'A' ? 'IPv4 · A' : 'IPv6 · AAAA'
export const dnsFamilyAnchor = (id: number, key: string, type: DNSRecordType) => `dns-${id}-${encodeURIComponent(key)}-${type.toLowerCase()}`
export const nodeDNSLink = (id: number, type: DNSRecordType) => `/nodes/${id}/diagnostics?view=health#${dnsFamilyAnchor(id, 'node', type)}`
export const syncHasChanges = (plan: DNSSyncPlan) => plan.enabled && !!plan.revision && plan.records.some(record => ['created', 'updated'].includes(record.action))
export function syncResultStatus(result: DNSSyncResult) {
  if (result.outcome === 'unknown') return 'unknown'
  if (result.outcome === 'failed' || result.error) return 'failed'
  if (result.reason || result.action === 'skipped') return 'skipped'
  if (result.outcome === 'success' || ['created', 'updated', 'unchanged'].includes(result.action)) return 'success'
  return 'unknown'
}
