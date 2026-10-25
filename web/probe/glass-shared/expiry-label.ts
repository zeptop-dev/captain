import type { ExpireStatus } from './utils/tagHelper'

// Both list and detail labels use the active dictionary, rather than treating
// every language except simplified Chinese as English.
export function expiryLabel(
  status: ExpireStatus,
  days: number,
  translate: (key: string) => string,
  remaining = false,
): string {
  if (status === 'unknown') return '-'
  if (status === 'expired') return translate('已过期')
  if (status === 'long_term') return translate('长期')
  if (!remaining && days === 1) return translate('1 天')
  const key = remaining
    ? days === 1 ? 'expiry.remainingDay' : 'expiry.remainingDays'
    : 'expiry.days'
  return translate(key).replace('{days}', String(days))
}
