import { carrierProbeDraft, carrierProbePayload, type CarrierProbe } from './carrier-probe.ts'

export type ProbePlanMode = 'inherit' | 'custom' | 'disabled'
export interface NodeProbePlan {
  mode: ProbePlanMode
  revision: string
  custom: CarrierProbe | null
  monitoring_enabled: boolean
  global: { enabled: boolean; config: CarrierProbe }
  effective: { enabled: boolean; source: ProbePlanMode; config: CarrierProbe; changed_at: number }
}
export function nodeProbePlanDraft(plan: NodeProbePlan) {
  return { mode: plan.mode, revision: plan.revision, initialized: plan.custom !== null, ...carrierProbeDraft(plan.custom ?? plan.global.config) }
}
export function nodeProbePlanPayload(draft: ReturnType<typeof nodeProbePlanDraft>) {
  return { mode: draft.mode, revision: draft.revision, ...(draft.initialized || draft.mode === 'custom' ? { custom: carrierProbePayload(draft.probe, draft.texts) } : {}) }
}
