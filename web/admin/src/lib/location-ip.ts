export type LocationIPMode = 'auto' | 'manual' | 'entry'
export type LocationIPSource = 'manual' | 'entry_override' | 'primary' | 'fallback' | 'entry_fallback'
export interface LocationIPSettings {
  mode: LocationIPMode
  manual_ip?: string
  allow_entry_fallback?: boolean
}

export type LocationIPDraft = Required<LocationIPSettings>

export function locationIPDraft(value?: LocationIPSettings | null) {
  return { mode: value?.mode ?? 'auto', manual_ip: value?.manual_ip ?? '', allow_entry_fallback: value?.allow_entry_fallback ?? false }
}

export function locationIPSettings(value: LocationIPSettings): LocationIPSettings {
  const manual = value.manual_ip?.trim() || ''
  // Keep valid manual choices for switching back, but never send an unfinished
  // hidden draft to the API when another source is selected.
  return { mode: value.mode, manual_ip: value.mode === 'manual' || validLocationIP(manual) ? manual : '', allow_entry_fallback: value.allow_entry_fallback ?? false }
}

export function locationIPChanged(value: LocationIPSettings, saved?: LocationIPSettings | null) {
  return JSON.stringify(locationIPSettings(value)) !== JSON.stringify(locationIPSettings(locationIPDraft(saved)))
}

// Literal-address UX validation mirrors the agent's public-address rules. The
// API remains authoritative; a URL parser alone would also accept ports/hosts.
export function validLocationIP(value: string) {
  const ip = value.trim()
  if (/^(?:0|[1-9]\d{0,2})(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(ip)) {
    const [a, b, c, d] = ip.split('.').map(Number)
    return [a, b, c, d].every(part => part <= 255) &&
      !(a === 0 && b === 0 && c === 0 && d === 0) &&
      !(a === 255 && b === 255 && c === 255 && d === 255) &&
      a !== 10 && a !== 127 && !(a === 169 && b === 254) &&
      !(a === 172 && b >= 16 && b <= 31) && !(a === 192 && b === 168) &&
      !(a === 100 && b >= 64 && b <= 127) && !(a >= 224 && a <= 239)
  }
  if (!ip.includes(':') || !/^[0-9a-f:.]+$/i.test(ip)) return false
  try {
    const normalized = new URL(`http://[${ip}]/`).hostname.slice(1, -1)
    const halves = normalized.split('::')
    const left = halves[0] ? halves[0].split(':') : []
    const right = halves[1] ? halves[1].split(':') : []
    const words = [...left, ...Array(8 - left.length - right.length).fill('0'), ...right].map(part => Number.parseInt(part, 16))
    const [first] = words
    return !words.every(word => word === 0) &&
      !(words.slice(0, 7).every(word => word === 0) && words[7] === 1) &&
      !(words.slice(0, 5).every(word => word === 0) && words[5] === 0xffff) &&
      (first & 0xff00) !== 0xff00 && (first & 0xffc0) !== 0xfe80 && (first & 0xfe00) !== 0xfc00
  } catch { return false }
}

export function locationIPError(value: LocationIPSettings) {
  return value.mode === 'manual' && !validLocationIP(value.manual_ip ?? '') ? 'invalid' : null
}

export function locationIPSourceKey(source?: string) {
  return ['manual', 'entry_override', 'primary', 'fallback', 'entry_fallback'].includes(source ?? '') ? `locationIP.sources.${source}` : 'locationIP.sources.unknown'
}
