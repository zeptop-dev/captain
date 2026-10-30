import type { EvidenceClock } from './line-change-evidence'

export interface PathHealthPolicy {
  enabled: boolean
  auto_failover: boolean
  server_name?: string
  interval_seconds: number
  timeout_seconds: number
  failure_threshold: number
  recovery_threshold: number
}
export interface PathHealthCandidate {
  id: string; tag: string; target: string; config_digest: string; candidate_digest: string
  status: string; result: string; eligible: boolean
  observed_at?: string; received_at?: string; fresh_for_ms: number
  epoch: string; sequence: number; rtt_ms: number; error?: string
}
export interface PathHealthView {
  route_id: number; version: number; enabled: boolean; auto_failover: boolean
  entry_node_id: number; capable: boolean; protocol: string; server_name: string
  status: string; candidates: PathHealthCandidate[]
}

export function pathHealthInput(value?: PathHealthPolicy | null): PathHealthPolicy {
  return {
    enabled: value?.enabled ?? false, auto_failover: value?.auto_failover ?? false,
    server_name: value?.server_name?.trim().toLowerCase() || '',
    interval_seconds: value?.interval_seconds ?? 15, timeout_seconds: value?.timeout_seconds ?? 3,
    failure_threshold: value?.failure_threshold ?? 3, recovery_threshold: value?.recovery_threshold ?? 2,
  }
}
export function validPathHealthPolicy(value: PathHealthPolicy, strategy: string): boolean {
  const integer = (n: number, low: number, high: number) => Number.isInteger(n) && n >= low && n <= high
  const name = value.server_name?.trim() || ''
  const validName = !name || (name.length <= 253 && !/^\d+(\.\d+){3}$/.test(name) && name.split('.').every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label)))
  return validName && integer(value.interval_seconds, 10, 300) && integer(value.timeout_seconds, 1, 10)
    && value.timeout_seconds <= value.interval_seconds && integer(value.failure_threshold, 1, 10)
    && integer(value.recovery_threshold, 1, 10) && (!value.enabled || !value.auto_failover || strategy === 'failover')
}

// Expiry is counted from request dispatch, not response arrival. A slow GET or
// copied response cannot give an old probe a new freshness window.
export function pathHealthElapsed(start: EvidenceClock, now: EvidenceClock, received = now): number {
  // Wall time covers sleeping platforms; monotonic time covers clock rollback.
  return Math.max(0, now.monotonic - start.monotonic, now.wall - start.wall,
    received.monotonic - start.monotonic, received.wall - start.wall)
}
export function pathCandidateEvidence(candidate: PathHealthCandidate, elapsedMS: number, available: boolean) {
  const fresh = available && Number.isFinite(candidate.fresh_for_ms) && candidate.fresh_for_ms > Math.max(0, elapsedMS)
    && Number.isFinite(Date.parse(candidate.observed_at || '')) && candidate.sequence > 0 && !!candidate.epoch
  const status = fresh && ['up', 'down', 'degraded', 'unsupported'].includes(candidate.status) ? candidate.status : 'unknown'
  return { status, fresh, eligible: fresh && candidate.eligible && (status === 'up' || status === 'degraded') }
}
export function pathHealthStatus(view: PathHealthView, elapsedMS: number, available: boolean): string {
  if (!available) return 'unknown'
  if (!view.enabled) return 'disabled'
  if (['paused', 'unsupported', 'unknown'].includes(view.status)) return view.status
  if (!view.capable || !Array.isArray(view.candidates) || !view.candidates.length) return 'unknown'
  const states = view.candidates.map(candidate => pathCandidateEvidence(candidate, elapsedMS, available).status)
  if (states.every(state => state === 'up')) return 'healthy'
  if (states.some(state => state === 'up' || state === 'degraded')) return 'degraded'
  if (states.every(state => state === 'down')) return 'down'
  if (states.every(state => state === 'unsupported')) return 'unsupported'
  return 'unknown'
}
