import { observationFresh, type ObservationClock, type ObservedResponse } from './observed-request.ts'
export const operationSources = ['line_rollout', 'path_rollout', 'node_job', 'topology_changeset', 'node_upgrade'] as const
export type OperationSource = typeof operationSources[number]
export interface OperationStep {
  id: string; node_id: number; node_name: string; phase: string; order: number; attempt: number; status: string
  issued_at?: string; confirmed_at?: string; observed_at?: string; last_receipt_at?: string; fresh_for_ms: number; receipt_applied: boolean
}
export interface Operation {
  id: string; source: OperationSource; source_id: string; kind: string; status: string; evidence: 'historical'
  created_at: string; updated_at?: string; completed_at?: string; node_id?: number; line_id?: number; name?: string; actor?: string; stage?: string; error_code?: string; target_version?: string; action?: string; outcome?: string
  steps?: OperationStep[]; attempts?: { step_id: string; attempt: number; observed_at: string; received_at: string; applied: boolean }[]
  attempts_truncated?: boolean
  related?: { relation: 'compensates' | 'compensated_by' | 'supersedes' | 'superseded_by'; operation_id: string; available: boolean }[]
}
export interface OperationPage { items: Operation[]; next_before?: string }
export interface OperationFilters { source?: string; status?: string; node_id?: string; line_id?: string }
export const positiveID = (value?: string) => !!value && /^[1-9][0-9]*$/.test(value) && Number.isSafeInteger(Number(value))
export function operationsURL(filters: OperationFilters, before?: string) {
  const params = new URLSearchParams({ limit: '20' })
  if (filters.source && operationSources.includes(filters.source as OperationSource)) params.set('source', filters.source)
  if (filters.status) params.set('status', filters.status)
  for (const key of ['node_id', 'line_id'] as const) if (positiveID(filters[key])) params.set(key, filters[key]!)
  if (before) params.set('before', before)
  return `/api/admin/operations?${params}`
}
export function operationDetailURL(id: string) { return `/api/admin/operations/${encodeURIComponent(id)}` }
export function operationPageURL(id: string) { return `/operations/${encodeURIComponent(id)}` }
export const safeOperationSourceID = (value: string | null): string | undefined => value && value !== 'new' && /^[A-Za-z0-9_-]{1,160}$/.test(value) ? value : undefined
export function operationSourceURL(item: Operation): string | null {
  const sourceID = safeOperationSourceID(item.source_id)
  if (!sourceID) return null
  const id = encodeURIComponent(sourceID)
  if (item.source === 'line_rollout' || item.source === 'path_rollout') return `/entries?tab=lines${item.source === 'path_rollout' ? '&mode=paths' : ''}&operation_rollout=${id}&operation_source=${item.source}`
  if (item.source === 'topology_changeset') return `/entries?tab=changesets&changeset=${id}`
  if (item.node_id && Number.isSafeInteger(item.node_id) && item.node_id > 0) return `/nodes/${item.node_id}/diagnostics`
  return null
}
export function operationStepFresh(step: OperationStep, response: ObservedResponse<Operation>, now: ObservationClock, available: boolean): boolean {
  return step.receipt_applied && !!step.observed_at && Number.isFinite(Date.parse(step.observed_at))
    && observationFresh(step.fresh_for_ms, response.started, response.received, now, available)
}
export const operationStatuses: Record<OperationSource, string[]> = {
  line_rollout: ['running', 'paused', 'completed', 'compensated'], path_rollout: ['running', 'paused', 'completed', 'compensated'],
  node_job: ['pending', 'running', 'completed', 'failed', 'expired', 'waiting', 'unknown'], topology_changeset: ['draft', 'committed'], node_upgrade: ['pending', 'observed', 'superseded', 'cancelled', 'identity_changed'],
}
