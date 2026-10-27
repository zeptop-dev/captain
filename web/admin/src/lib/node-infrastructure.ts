import { renewalPeriodMonths, type NodeAutoRenewal } from './node-renewal.ts'

export interface SupplierOption { id: number; name: string; url: string }
export interface InfraSupplier extends SupplierOption { contact: string; notes: string }
export interface InfraAsset { id: number; supplier_id: number; supplier_name: string; node_id: number | null; primary_node_id?: number | null; auto_renewal_enabled?: boolean; name: string; external_ref: string; currency: string; amount_minor: number; period_months: number; anchor_day: number; next_due: string; remind_days: number; active: boolean; notes: string; revision: number }
export interface NodeCost { amount: string; currency: string; cycle: string; years?: string }
export interface NodeInfrastructure { revision: string; supplier: SupplierOption | null; asset: InfraAsset | null; billing: NodeCost | null; price: string; expires_at: string; auto_renewal: NodeAutoRenewal | null }
export interface FinanceInfo { billing?: NodeCost | null; provider?: string; provider_url?: string; price?: string; expires_at?: string; auto_renewal?: NodeAutoRenewal | null }
export interface FinanceDraft { InfrastructureRevision: string; SupplierID: number; provider: string; provider_url: string; price: string; costMode: string; amount: string | number; currency: string; cycle: string; years: string | number; AutoRenewal: boolean; expires_at: string }
export const financeKeys: (keyof FinanceDraft)[] = ['InfrastructureRevision', 'SupplierID', 'provider', 'provider_url', 'price', 'costMode', 'amount', 'currency', 'cycle', 'years', 'AutoRenewal', 'expires_at']
export function financeDraft(info: FinanceInfo = {}, revision = '', supplierID = 0): FinanceDraft {
  return { InfrastructureRevision: revision, SupplierID: supplierID, provider: info.provider ?? '', provider_url: info.provider_url ?? '', price: info.price ?? '', costMode: !info.billing && info.price ? 'legacy' : 'structured', amount: info.billing?.amount ?? '', currency: info.billing?.currency ?? 'USD', cycle: info.billing?.cycle ?? 'annual', years: info.billing?.years ?? '1', AutoRenewal: info.auto_renewal?.enabled ?? false, expires_at: info.expires_at ?? '' }
}
export function sameInfrastructureSnapshot(probeRevision: string | undefined, infra: NodeInfrastructure | undefined): boolean {
  return !!probeRevision && !!infra && probeRevision === infra.revision
}
export function applyDraftSave<T extends object>(current: T, submitted: T, saved: T): T {
  return Object.fromEntries(Object.entries(saved).map(([key, value]) => [key, JSON.stringify(current[key as keyof T]) === JSON.stringify(submitted[key as keyof T]) ? value : current[key as keyof T]])) as T
}
export function reloadFinance<T extends FinanceDraft>(current: T, saved: FinanceDraft): T { return { ...current, ...saved } }
export function supportedAssetPeriod(months: number): boolean { return Number.isInteger(months) && months > 0 && months <= 120 && (months === 1 || months % 3 === 0) }
export function eligibleBillingAsset(asset: InfraAsset, nodeID: number): boolean {
  return asset.active && !asset.primary_node_id && (!asset.node_id || asset.node_id === nodeID) && supportedAssetPeriod(asset.period_months)
}
export function createBillingReason(infra: NodeInfrastructure): string | null {
  if (!infra.supplier) return 'supplierRequired'
  if (!infra.billing || !infra.billing.amount.trim()) return 'structuredRequired'
  const months = renewalPeriodMonths(infra.billing.cycle, infra.billing.years ?? '')
  if (months === null || !supportedAssetPeriod(months)) return 'periodRequired'
  if (!/^\d{4}-\d{2}-\d{2}$/.test(infra.expires_at) || infra.expires_at < '1970-01-01' || infra.expires_at > '9900-12-31') return 'expiryRequired'
  return null
}
export function supplierURL(value: string): string | undefined {
  try { const url = new URL(value); return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : undefined } catch { return undefined }
}
export function infraMoney(amount: number, currency: string, locale?: string): string {
  try { const f = new Intl.NumberFormat(locale, { style: 'currency', currency }); return f.format(amount / 10 ** (f.resolvedOptions().maximumFractionDigits ?? 2)) } catch { return `${currency} ${amount}` }
}

export const nodeInfrastructureKey = (nodeID: number) => ['node-infrastructure', nodeID]
export const nodeInfrastructureURL = (nodeID: number) => `/api/admin/infrastructure/nodes/${nodeID}`
export const suppliersKey = ['infrastructure-supplier-options']
