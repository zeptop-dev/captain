export type PriceInput = number | string
export interface PeriodPriceDraft { days: PriceInput; yuan: PriceInput }

// Parse decimal digits directly: binary multiplication can change cent values.
export function yuanToCents(value: PriceInput): number | null {
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(String(value).trim())
  if (!match) return null
  const cents = BigInt(match[1]) * 100n + BigInt((match[2] ?? '').padEnd(2, '0'))
  return cents <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(cents) : null
}

export function centsToYuan(cents: number): string {
  if (!Number.isSafeInteger(cents) || cents < 0) return ''
  return `${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, '0')}`
}

export function periodDays(value: PriceInput): number | null {
  if (!/^\d+$/.test(String(value).trim())) return null
  const days = Number(value)
  return Number.isSafeInteger(days) && days > 0 ? days : null
}

export function periodPricesPayload(prices: PeriodPriceDraft[]) {
  return prices.map(price => {
    const days = periodDays(price.days), cents = yuanToCents(price.yuan)
    if (days === null || cents === null) throw new Error('Invalid period price')
    return { period_days: days, price_cents: cents }
  })
}
