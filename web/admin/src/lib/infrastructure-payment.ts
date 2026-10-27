import type { InfraAsset } from './node-infrastructure.ts'

export type PaymentDateAction = '' | 'record_only' | 'set_due_date'
export interface InfrastructurePayment { id: number; asset_id: number; supplier_name: string; asset_name: string; currency: string; amount_minor: number; paid_date: string; period_start: string; next_due: string; reference: string; notes: string; date_action?: 'record_only' | 'set_due_date' | 'advance_period' }
export interface PaymentDraft { asset: InfraAsset; request_key: string; amount_minor: number; paid_date: string; reference: string; notes: string; confirm: boolean; date_action: PaymentDateAction; next_due: string }

export function validBillingDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || value < '1970-01-01' || value > '9900-12-31') return false
  const parsed = new Date(`${value}T00:00:00Z`)
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value
}
export function nextBillingDate(asset: InfraAsset): string {
  if (!validBillingDate(asset.next_due) || asset.period_months <= 0) return ''
  const [year, month] = asset.next_due.split('-').map(Number)
  const next = new Date(Date.UTC(year, month - 1 + asset.period_months, 1))
  const last = new Date(Date.UTC(next.getUTCFullYear(), next.getUTCMonth() + 1, 0)).getUTCDate()
  next.setUTCDate(Math.min(asset.anchor_day, last))
  const result = next.toISOString().slice(0, 10)
  return validBillingDate(result) ? result : ''
}
export function paymentDateDefaults(asset: InfraAsset): Pick<PaymentDraft, 'date_action' | 'next_due'> {
  if (!asset.primary_node_id && asset.period_months === 0) return { date_action: '', next_due: '' }
  return { date_action: asset.auto_renewal_enabled ? 'record_only' : 'set_due_date', next_due: nextBillingDate(asset) }
}
export function newPaymentDraft(asset: InfraAsset, date: string, requestKey: string): PaymentDraft {
  return { asset, request_key: requestKey, amount_minor: asset.amount_minor, paid_date: date, reference: '', notes: '', confirm: false, ...paymentDateDefaults(asset) }
}
// A conflict must be reviewed again against a fresh bill. Never replay its old date target.
export function reloadPaymentDraft(draft: PaymentDraft, fresh: InfraAsset, requestKey: string): PaymentDraft {
  return { ...draft, asset: fresh, request_key: requestKey, amount_minor: fresh.currency === draft.asset.currency ? draft.amount_minor : fresh.amount_minor, confirm: false, ...paymentDateDefaults(fresh) }
}
export function paymentDateError(draft: PaymentDraft): 'invalidDate' | 'earlierDate' | null {
  if (draft.date_action !== 'set_due_date') return null
  if (!validBillingDate(draft.next_due)) return 'invalidDate'
  return draft.next_due < draft.asset.next_due ? 'earlierDate' : null
}
export function paymentPayload(draft: PaymentDraft) {
  return { request_key: draft.request_key, revision: draft.asset.revision, amount_minor: draft.amount_minor, paid_date: draft.paid_date, reference: draft.reference, notes: draft.notes, ...(draft.date_action ? { date_action: draft.date_action } : {}), ...(draft.date_action === 'set_due_date' ? { next_due: draft.next_due } : {}) }
}
export function paymentHistoryAction(payment: InfrastructurePayment): 'record_only' | 'set_due_date' | 'advance_period' { return payment.date_action || 'advance_period' }
