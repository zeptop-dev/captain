import type { NodeCost } from './node-price.ts'

export interface Money { amount: number; currency: string }
/** A server-cached Frankfurter snapshot. Quote dates may differ by currency. */
export interface ExchangeRates {
  base: string
  rates: Record<string, number>
  dates: Record<string, string>
  fetched_at: number
  stale: boolean
  source: 'Frankfurter'
}
export interface NodeAutoRenewal { enabled: boolean; anchor_date?: string; readonly anchor_day?: number }
export interface CostInfo { billing?: NodeCost; price?: string; expires_at?: string; auto_renewal?: NodeAutoRenewal }
export type RemainingCostReason = 'missing-price' | 'legacy-price' | 'missing-expiry' | 'unsupported-cycle' | 'invalid'
export interface CalculatedNodeCost {
  monthly: Money | null
  remaining: Money | null
  remainingReason: RemainingCostReason | null
}

const cycleMonths: Record<string, number> = { monthly: 1, quarterly: 3, semiannual: 6, annual: 12 }
const amountPattern = /^(0|[1-9][0-9]{0,11})(\.[0-9]{1,4})?$/
const yearsPattern = /^(0|[1-9][0-9]{0,2})(\.[0-9]{1,4})?$/

// Use calendar dates throughout: Date.UTC treats years 0–99 as 1900–1999.
function utcDate(year: number, month: number, day: number): Date {
  const date = new Date(0)
  date.setUTCFullYear(year, month, day)
  return date
}

function expiryDate(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const [year, month, day] = value.split('-').map(Number) as [number, number, number]
  const date = utcDate(year, month - 1, day)
  return year >= 1 && date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day ? date : null
}

function previousCycle(expiry: Date, months: number, day = expiry.getUTCDate()): Date {
  const first = utcDate(expiry.getUTCFullYear(), expiry.getUTCMonth() - months, 1)
  const lastDay = utcDate(first.getUTCFullYear(), first.getUTCMonth() + 1, 0).getUTCDate()
  return utcDate(first.getUTCFullYear(), first.getUTCMonth(), Math.min(day, lastDay))
}

function explicitRenewalDay(info: CostInfo): number | null {
  const day = info.auto_renewal?.anchor_day
  return typeof day === 'number' && Number.isInteger(day) && day >= 1 && day <= 31 ? day : null
}

// A persisted renewal anchor defines the original calendar day even after a
// short-month expiry. Ignore inconsistent metadata instead of guessing a cycle.
function renewalDay(info: CostInfo, expiry: Date, months: number): number | null {
  const value = info.auto_renewal?.anchor_date
  if (!value || !Number.isInteger(months)) return null
  const anchor = expiryDate(value)
  if (!anchor || anchor > expiry) return null
  const delta = (expiry.getUTCFullYear() - anchor.getUTCFullYear()) * 12 + expiry.getUTCMonth() - anchor.getUTCMonth()
  const last = utcDate(expiry.getUTCFullYear(), expiry.getUTCMonth() + 1, 0).getUTCDate()
  const day = explicitRenewalDay(info) ?? anchor.getUTCDate()
  return delta % months === 0 && expiry.getUTCDate() === Math.min(day, last) ? day : null
}

/** Display estimates only. Never changes saved prices or interprets legacy text.
 * Remaining value prorates against the actual calendar cycle ending on expiry.
 * Custom years define calendar periods only when saved with a renewal anchor.
 */
export function calculateNodeCost(info: CostInfo, now = new Date()): CalculatedNodeCost {
  const unknown = (remainingReason: RemainingCostReason): CalculatedNodeCost => ({ monthly: null, remaining: null, remainingReason })
  const billing = info.billing
  if (!billing) return unknown(info.price?.trim() ? 'legacy-price' : 'missing-price')
  if (billing.amount === '') return unknown('missing-price')
  if (typeof billing.amount !== 'string' || !amountPattern.test(billing.amount)
    || typeof billing.currency !== 'string' || !/^[A-Z]{3}$/.test(billing.currency)
    || billing.currency === 'XXX' || billing.currency === 'XTS') return unknown('invalid')
  const amount = Number(billing.amount)
  const months = billing.cycle === 'custom'
    ? typeof billing.years === 'string' && yearsPattern.test(billing.years) ? Number(billing.years) * 12 : 0
    : cycleMonths[billing.cycle] ?? 0
  if (!Number.isFinite(months) || months <= 0) return unknown('invalid')
  const money = (value: number): Money => ({ amount: value, currency: billing.currency })
  const monthly = money(amount / months)
  if (amount === 0) return { monthly, remaining: money(0), remainingReason: null }
  if (!info.expires_at) return { monthly, remaining: null, remainingReason: 'missing-expiry' }
  const expiry = expiryDate(info.expires_at)
  if (!expiry || !Number.isFinite(now.getTime())) return { monthly, remaining: null, remainingReason: 'invalid' }
  if (expiry.getTime() <= now.getTime()) return { monthly, remaining: money(0), remainingReason: null }
  const anchorDay = renewalDay(info, expiry, months)
  if ((billing.cycle === 'custom' || explicitRenewalDay(info) !== null) && anchorDay === null) return { monthly, remaining: null, remainingReason: 'unsupported-cycle' }
  const duration = expiry.getTime() - previousCycle(expiry, months, anchorDay ?? expiry.getUTCDate()).getTime()
  const remaining = amount * ((expiry.getTime() - now.getTime()) / duration)
  return { monthly, remaining: money(remaining), remainingReason: null }
}

export function formatCostAmount(money: Money, locale = 'en'): string {
  if (!Number.isFinite(money.amount) || money.amount < 0) return '—'
  const formatter = new Intl.NumberFormat(locale, { maximumFractionDigits: 2 })
  // A small positive estimate must not be presented as a free server.
  return money.amount > 0 && money.amount < 0.01 ? `<${formatter.format(0.01)}` : formatter.format(money.amount)
}

export function summarizeNodeCosts(infos: CostInfo[], kind: 'monthly' | 'yearly' | 'remaining', now = new Date()): { amounts: Money[]; included: number; total: number } {
  const totals = new Map<string, number>()
  let included = 0
  for (const info of infos) {
    const cost = calculateNodeCost(info, now)
    const value = kind === 'remaining' ? cost.remaining : cost.monthly
    if (!value) continue
    included++
    totals.set(value.currency, (totals.get(value.currency) ?? 0) + value.amount * (kind === 'yearly' ? 12 : 1))
  }
  return {
    amounts: [...totals].sort(([a], [b]) => a.localeCompare(b)).map(([currency, amount]) => ({ amount, currency })),
    included,
    total: infos.length,
  }
}

export interface ConvertedCostSummary {
  money: Money | null
  missingCurrencies: string[]
  rateDate?: string
  stale: boolean
}

/** Convert only display estimates, at full precision until final formatting.
 * Unknown prices remain unknown. A missing quote must never produce a partial
 * total that appears complete; zero amounts and same-currency sums need no FX.
 */
export function summarizeConvertedNodeCosts(
  infos: CostInfo[], kind: 'monthly' | 'yearly' | 'remaining', currency: string,
  exchange: ExchangeRates | null | undefined, now = new Date(),
): ConvertedCostSummary {
  const result: ConvertedCostSummary = { money: null, missingCurrencies: [], stale: false }
  const summary = summarizeNodeCosts(infos, kind, now)
  if (!summary.amounts.length || !/^[A-Z]{3}$/.test(currency) || ['XXX', 'XTS'].includes(currency)) return result
  let total = 0
  const dates: string[] = []
  const missing = new Set<string>()
  function quote(code: string): number | null {
    const rate = exchange?.rates[code]
    const date = exchange?.dates[code]
    const parsed = date ? expiryDate(date) : null
    if (exchange?.base !== 'USD' || exchange.source !== 'Frankfurter'
      || typeof rate !== 'number' || !Number.isFinite(rate) || rate <= 0
      || !parsed || parsed.getTime() > now.getTime() + 2 * 86400_000) {
      missing.add(code)
      return null
    }
    dates.push(date!)
    return rate
  }
  for (const money of summary.amounts) {
    if (money.amount === 0 || money.currency === currency) {
      total += money.amount
      continue
    }
    const source = quote(money.currency), target = quote(currency)
    if (source !== null && target !== null) total += money.amount * (target / source)
  }
  if (missing.size) return { ...result, missingCurrencies: [...missing].sort() }
  if (!Number.isFinite(total) || total < 0) return result
  result.money = { amount: total, currency }
  if (dates.length) {
    result.rateDate = dates.sort()[0]
    result.stale = Boolean(exchange?.stale)
      || !Number.isFinite(exchange?.fetched_at)
      || now.getTime() - (exchange?.fetched_at ?? 0) * 1000 >= 2 * 86400_000
      || now.getTime() - expiryDate(result.rateDate!)!.getTime() >= 7 * 86400_000
  }
  return result
}
