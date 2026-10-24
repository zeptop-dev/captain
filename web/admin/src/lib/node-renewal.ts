export interface NodeAutoRenewal { enabled: boolean; anchor_date?: string }

export function renewalPeriodMonths(cycle: string, years: string | number): number | null {
  switch (cycle) {
    case 'monthly': return 1
    case 'quarterly': return 3
    case 'semiannual': return 6
    case 'annual': return 12
  }
  if (cycle !== 'custom' || !/^(0|[1-9][0-9]{0,2})(\.[0-9]{1,4})?$/.test(String(years))) return null
  // Keep the four decimal places as integers so fractional months are never rounded.
  const [whole, fraction = ''] = String(years).split('.')
  const scaled = Number(whole) * 10_000 + Number(fraction.padEnd(4, '0'))
  return scaled > 0 && (scaled * 12) % 10_000 === 0 ? scaled * 12 / 10_000 : null
}

export function renewalUnavailable(date: string, costMode: string, cycle: string, years: string | number): string | null {
  if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(date) || date.startsWith('0000-')) return 'nodeCost.renewalDateRequired'
  const time = new Date(`${date}T00:00:00Z`)
  if (!Number.isFinite(time.getTime()) || time.toISOString().slice(0, 10) !== date) return 'nodeCost.renewalDateRequired'
  if (costMode !== 'structured') return 'nodeCost.renewalCycleRequired'
  if (renewalPeriodMonths(cycle, years) === null) return 'nodeCost.renewalWholeMonths'
  return null
}
