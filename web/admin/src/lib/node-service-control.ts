export type ServiceMode = 'normal' | 'limit' | 'pause'
export type ServicePolicyAction = 'alert' | 'limit' | 'pause'
export interface ServicePolicy { action: ServicePolicyAction; threshold_percent: number; limit_mbps: number; auto_resume: boolean }
export interface ServiceManual { mode: ServiceMode; limit_mbps: number }
export interface NodeServiceControl {
  node_id: number
  revision: string
  offline_enabled?: boolean
  policy: ServicePolicy
  manual: ServiceManual
  automatic: { active: boolean; mode: ServiceMode; limit_mbps: number; triggered_at?: string; period_start?: string; exempt_current_period: boolean }
  effective: { mode: ServiceMode; limit_mbps: number; revision: string; reasons: string[] }
  traffic: { monitoring_enabled: boolean; limit_bytes: number; used_bytes: number; period_start?: string; period_end?: string; observed_at?: string; fresh: boolean }
  capability: { supported: boolean; offline_supported?: boolean; reason: string }
  runtime: { status: 'unconfigured' | 'pending' | 'applied' | 'error' | 'unknown' | 'unsupported'; observed_at?: string; mode?: ServiceMode; limit_mbps?: number; error?: string; offline?: { policy_id: string; cycle: number; period_start: number; used_bytes: number; automatic: boolean; state: string; error?: string } }
}
export interface ServiceControlWrite { revision: string; offline_enabled?: boolean; policy?: ServicePolicy; manual?: ServiceManual; exempt_current_period?: boolean }
export interface PolicyDraft { offline_enabled?: boolean; action: ServicePolicyAction; threshold_percent: string | number; limit_mbps: string | number; auto_resume: boolean }
export interface ManualDraft { mode: ServiceMode; limit_mbps: string | number; exempt_current_period: boolean }
export const serviceControlKey = (id: number) => ['node-service-control', id]
export const serviceControlURL = (id: number) => `/api/admin/nodes/${id}/service-control`
export const emptyPolicy: PolicyDraft = { action: 'alert', threshold_percent: 100, limit_mbps: 10, auto_resume: true }
export function policyDraft(policy: ServicePolicy, offline_enabled = false): PolicyDraft { return { ...policy, offline_enabled } }
export function manualDraft(data: NodeServiceControl): ManualDraft { return { ...data.manual, exempt_current_period: data.automatic.exempt_current_period } }
export function validControlRate(value: string | number): boolean { return value !== '' && Number.isSafeInteger(Number(value)) && Number(value) > 0 && Number(value) <= 1_000_000 }
export function policyDraftError(draft: PolicyDraft, data: NodeServiceControl): string | null {
  if (draft.action === 'alert') return null
  if (!data.capability.supported) return 'unavailable'
  if (draft.offline_enabled && !data.capability.offline_supported) return 'offlineUpgradeRequired'
  if (!data.traffic.monitoring_enabled) return 'monitoringRequired'
  if (data.traffic.limit_bytes <= 0) return 'quotaRequired'
  if (!data.traffic.fresh) return 'staleTraffic'
  if (draft.threshold_percent === '' || !Number.isInteger(Number(draft.threshold_percent)) || Number(draft.threshold_percent) < 1 || Number(draft.threshold_percent) > 100) return 'thresholdInvalid'
  if (draft.action === 'limit' && !validControlRate(draft.limit_mbps)) return 'rateInvalid'
  return null
}
export function policyWrite(revision: string, draft: PolicyDraft): ServiceControlWrite {
  return { revision, ...(draft.offline_enabled !== undefined ? { offline_enabled: draft.action !== 'alert' && draft.offline_enabled } : {}), policy: { action: draft.action, threshold_percent: Number(draft.threshold_percent) || 100, limit_mbps: draft.action === 'limit' ? Number(draft.limit_mbps) : 0, auto_resume: draft.auto_resume } }
}
export function manualDraftError(draft: ManualDraft, data: NodeServiceControl): string | null {
  if (draft.mode !== 'normal' && !data.capability.supported) return 'unavailable'
  if (draft.mode === 'limit' && !validControlRate(draft.limit_mbps)) return 'rateInvalid'
  if (draft.mode === 'normal' && (data.automatic.active || data.runtime?.offline?.automatic) && !draft.exempt_current_period) return 'exemptionRequired'
  return null
}
export function manualWrite(revision: string, draft: ManualDraft): ServiceControlWrite {
  return { revision, manual: { mode: draft.mode, limit_mbps: draft.mode === 'limit' ? Number(draft.limit_mbps) : 0 }, ...(draft.mode === 'normal' ? { exempt_current_period: draft.exempt_current_period } : {}) }
}
// Requested settings never prove execution. The backend checks the current
// runtime revision, report freshness and capability before returning "applied".
export function controlStatusColor(status: NodeServiceControl['runtime']['status']): string {
  return status === 'applied' ? 'green' : status === 'error' ? 'red' : status === 'pending' ? 'blue' : 'gray'
}
export const capabilityReasonKeys: Record<string, string> = {
  unpaired: 'unpaired', monitor_only: 'monitorOnly', upgrade_required: 'upgradeRequired', offline: 'offline', report_stale: 'staleCapability', unsupported_platform: 'platformUnsupported', unavailable: 'unavailable',
}
export function controlCapabilityKey(reason: string): string { return capabilityReasonKeys[reason] ?? 'unavailable' }
