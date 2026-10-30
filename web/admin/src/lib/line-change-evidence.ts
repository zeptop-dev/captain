import type { LineChange, LineChangeTarget } from './api'

export interface EvidenceClock { monotonic: number; wall: number }
export interface EvidenceLease { monotonicDeadline: number; wallDeadline: number }
export interface ObservedLineTarget extends LineChangeTarget { lease: EvidenceLease }
export interface ObservedLineChange extends Omit<LineChange, 'targets'> { targets: ObservedLineTarget[] }
export interface LineHistorySelection { lineID?: number; lineName?: string; changeID?: string }

// Measure from request start, so response latency cannot extend server evidence.
export function evidenceLease(start: EvidenceClock, freshForMs: number): EvidenceLease {
  const remaining = Number.isFinite(freshForMs) ? Math.max(0, freshForMs) : 0
  return { monotonicDeadline: start.monotonic + remaining, wallDeadline: start.wall + remaining }
}

export function evidenceIsFresh(lease: EvidenceLease, now: EvidenceClock, connected: boolean): boolean {
  // Wall time covers sleeping platforms whose monotonic clock stops; monotonic
  // time covers wall-clock rollback. Failed refreshes revoke cached success.
  return connected && now.monotonic < lease.monotonicDeadline && now.wall < lease.wallDeadline
}

export function observeLineChange(change: LineChange, start: EvidenceClock): ObservedLineChange {
  return { ...change, targets: change.targets.map(target => ({ ...target, lease: evidenceLease(start, target.fresh_for_ms) })) }
}

export function lineTargetStatus(target: ObservedLineTarget, now: EvidenceClock, connected: boolean): LineChangeTarget['status'] | 'unknown' {
  if (!connected) return 'unknown'
  return target.status === 'applied' && !evidenceIsFresh(target.lease, now, connected) ? 'stale' : target.status
}

export function lineChangeStatus(change: ObservedLineChange, now: EvidenceClock, connected: boolean): LineChange['status'] | 'unknown' {
  if (!connected) return 'unknown'
  if (change.status === 'superseded' || change.superseded_at) return 'superseded'
  if (!change.targets.length) return 'no_runtime_change'
  if (change.targets.every(target => target.status === 'superseded')) return 'superseded'
  const applied = change.targets.filter(target => lineTargetStatus(target, now, connected) === 'applied').length
  if (change.status === 'applied' && applied === change.targets.length) return 'applied'
  return applied > 0 ? 'partial' : 'pending'
}

export function lineChangesURL(lineID?: number, before?: number): string {
  const query = new URLSearchParams({ limit: '20' })
  if (lineID) query.set('line_id', String(lineID))
  if (before) query.set('before', String(before))
  return `/api/admin/line-changes?${query}`
}
