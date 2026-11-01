import { observationFresh, type ObservationClock, type ObservedResponse } from './observed-request.ts'
export interface RenderedArtifact {
  core: string; digest: string; digest_scope: 'process'; scope: 'activation_files'; format: 'json' | 'yaml' | 'ini'
  files: number; processes: number; activated_at: string; runtime_updates: boolean
}
export interface RenderedConfigReport { version: 1; status: 'observed' | 'unavailable'; artifacts: RenderedArtifact[] | null }
export interface RenderedConfigRecord {
  id: string; session: string; config_revision: string; state_revision: string; issued_at: string
  confirmed_at?: string; last_receipt_at?: string; observed_at?: string
  receipt_sequence: number; current: boolean; applied: boolean; fresh: boolean; fresh_for_ms: number
  metadata_status: 'valid' | 'invalid' | 'unavailable'; evidence: RenderedConfigReport | null
}
export interface RenderedConfigView {
  status: 'unknown' | 'unavailable' | 'pending' | 'stale' | 'observed'
  config_revision: string; state_revision: string; current: RenderedConfigRecord | null; history: RenderedConfigRecord[]
}
export function renderedConfigStatus(response: ObservedResponse<RenderedConfigView> | undefined, now: ObservationClock, available: boolean): RenderedConfigView['status'] {
  if (!response || !available) return 'unknown'
  const view = response.value, row = view.current
  if (view.status !== 'observed') return view.status
  if (!row || !row.current || !row.applied || !row.fresh || row.metadata_status !== 'valid' || row.evidence?.version !== 1
      || row.evidence.status !== 'observed' || row.config_revision !== view.config_revision || row.state_revision !== view.state_revision) return 'unknown'
  return observationFresh(row.fresh_for_ms, response.started, response.received, now, available) ? 'observed' : 'stale'
}
export function renderedConfigURL(nodeID: number) { return `/api/admin/nodes/${nodeID}/rendered-config` }
