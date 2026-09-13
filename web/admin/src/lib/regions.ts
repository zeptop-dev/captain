// Region codes offered in the entry form; the server detects the rest from names.
export const REGIONS: { code: string; name: string }[] = [
  { code: 'HK', name: 'Hong Kong' }, { code: 'TW', name: 'Taiwan' }, { code: 'JP', name: 'Japan' }, { code: 'SG', name: 'Singapore' },
  { code: 'US', name: 'United States' }, { code: 'KR', name: 'Korea' }, { code: 'GB', name: 'United Kingdom' }, { code: 'DE', name: 'Germany' },
  { code: 'FR', name: 'France' }, { code: 'NL', name: 'Netherlands' }, { code: 'RU', name: 'Russia' }, { code: 'CA', name: 'Canada' },
  { code: 'AU', name: 'Australia' }, { code: 'IN', name: 'India' }, { code: 'BR', name: 'Brazil' }, { code: 'TR', name: 'Turkey' },
  { code: 'AR', name: 'Argentina' }, { code: 'MY', name: 'Malaysia' }, { code: 'TH', name: 'Thailand' }, { code: 'VN', name: 'Vietnam' },
  { code: 'PH', name: 'Philippines' }, { code: 'ID', name: 'Indonesia' }, { code: 'AE', name: 'UAE' }, { code: 'CH', name: 'Switzerland' },
  { code: 'SE', name: 'Sweden' }, { code: 'FI', name: 'Finland' }, { code: 'IT', name: 'Italy' }, { code: 'ES', name: 'Spain' },
  { code: 'PL', name: 'Poland' }, { code: 'UA', name: 'Ukraine' }, { code: 'CN', name: 'China' }, { code: 'MO', name: 'Macau' },
  { code: 'IE', name: 'Ireland' }, { code: 'IL', name: 'Israel' }, { code: 'ZA', name: 'South Africa' }, { code: 'MX', name: 'Mexico' },
  { code: 'CL', name: 'Chile' }, { code: 'NZ', name: 'New Zealand' }, { code: 'PT', name: 'Portugal' }, { code: 'AT', name: 'Austria' },
  { code: 'BE', name: 'Belgium' }, { code: 'CZ', name: 'Czechia' }, { code: 'HU', name: 'Hungary' }, { code: 'RO', name: 'Romania' },
  { code: 'NO', name: 'Norway' }, { code: 'DK', name: 'Denmark' }, { code: 'KZ', name: 'Kazakhstan' }, { code: 'EG', name: 'Egypt' },
  { code: 'SA', name: 'Saudi Arabia' }, { code: 'PK', name: 'Pakistan' }, { code: 'NG', name: 'Nigeria' }, { code: 'GR', name: 'Greece' },
  { code: 'BG', name: 'Bulgaria' }, { code: 'LV', name: 'Latvia' }, { code: 'LT', name: 'Lithuania' }, { code: 'EE', name: 'Estonia' },
  { code: 'LU', name: 'Luxembourg' }, { code: 'IS', name: 'Iceland' }, { code: 'CO', name: 'Colombia' }, { code: 'PE', name: 'Peru' },
  { code: 'KH', name: 'Cambodia' }, { code: 'BD', name: 'Bangladesh' }, { code: 'NP', name: 'Nepal' }, { code: 'MN', name: 'Mongolia' },
]

export function flag(code: string): string {
  const c = code.toUpperCase()
  if (!/^[A-Z]{2}$/.test(c)) return ''
  return String.fromCodePoint(0x1f1e6 + c.charCodeAt(0) - 65, 0x1f1e6 + c.charCodeAt(1) - 65)
}
