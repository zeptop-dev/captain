export function bytes(n: number, locale?: string): string {
  if (!n) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  const digits = v < 10 ? 2 : v < 100 ? 1 : 0
  const value = locale ? v.toLocaleString(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }) : v.toFixed(digits)
  return `${value} ${units[i]}`
}

export function money(cents: number, currency = '¥', locale?: string): string {
  const value = cents / 100
  return `${currency}${locale ? value.toLocaleString(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : value.toFixed(2)}`
}

export function when(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleString()
}

export function ago(iso: string | null | undefined): string {
  if (!iso) return '—'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return `${Math.round(s)}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  if (s < 86400) return `${Math.round(s / 3600)}h`
  return `${Math.round(s / 86400)}d`
}
