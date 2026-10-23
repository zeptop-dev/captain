export interface DesignGroup {
  name: string
  type: string
  members: string[]
  url?: string
  interval?: number
  timeout?: number
  tolerance?: number
  region?: string
  match_case_sensitive?: boolean
  [field: string]: unknown
}

export interface DesignRule { set: string; policy: string; [field: string]: unknown }
export interface DesignRuleSet { key: string; name: string; url?: string; inline?: string; [field: string]: unknown }
export interface DesignSource { key: string; name: string; revision?: string; url?: string; [field: string]: unknown }
export interface DesignEnhancements {
  regions?: boolean
  dns_protection?: boolean
  claude?: boolean
  webrtc?: boolean
  quic?: boolean
  claude_policy?: string
  dns_policy?: string
  [field: string]: unknown
}
export interface SubDesign {
  groups: DesignGroup[]
  rules: DesignRule[]
  final: string
  source?: DesignSource
  enhancements?: DesignEnhancements
  rule_sets?: DesignRuleSet[]
  [field: string]: unknown
}
export interface DesignPreset {
  key: string
  name: string
  source: 'builtin' | 'acl4ssr' | 'custom'
  revision?: string
  supported: boolean
  reason?: string
}
export interface DesignFormatSupport { format: string; supported: boolean; reasons: string[] }
export interface DesignInspection { effective_design: SubDesign; formats: DesignFormatSupport[] }
export interface DesignLibrary { revision: string; synced_at: string | number; count: number }
export interface SubDesignData {
  design: SubDesign
  catalogue: DesignRuleSet[]
  presets: DesignPreset[]
  library?: DesignLibrary
  regions: { name: string; match: string }[]
  tags: string[]
  formats: string[]
}

export const designFormatLabels: Record<string, string> = {
  clash: 'mihomo / Clash Meta', stash: 'Stash', surge: 'Surge', surfboard: 'Surfboard',
  loon: 'Loon', qx: 'Quantumult X', egern: 'Egern',
}

// Renaming a policy keeps references, provenance and fields understood by newer
// compilers. The editable base is never replaced by the derived enhancement output.
export function renameDesignGroup(design: SubDesign, index: number, name: string): SubDesign {
  const next = structuredClone(design)
  const old = next.groups[index].name
  next.groups[index].name = name
  if (!old) return next
  next.groups.forEach(group => { group.members = group.members.map(member => member === old ? name : member) })
  next.rules.forEach(rule => { if (rule.policy === old) rule.policy = name })
  if (next.final === old) next.final = name
  if (next.enhancements?.claude_policy === old) next.enhancements.claude_policy = name
  if (next.enhancements?.dns_policy === old) next.enhancements.dns_policy = name
  return next
}

export function selectedDesignFormats(support: DesignFormatSupport[], selected: string[] | null): string[] {
  return support.filter(item => item.supported && (selected === null || selected.includes(item.format))).map(item => item.format)
}

export function designCatalogue(catalogue: DesignRuleSet[], design: SubDesign): DesignRuleSet[] {
  const items = new Map(catalogue.map(item => [item.key, item]))
  for (const item of design.rule_sets ?? []) items.set(item.key, item)
  // An unknown key must remain visible so it can be corrected, not disappear
  // from the select while the draft silently keeps an invalid value.
  for (const rule of design.rules) if (!items.has(rule.set)) items.set(rule.set, { key: rule.set, name: rule.set })
  return [...items.values()]
}

export function moveDesignItem<T>(items: T[], index: number, direction: -1 | 1): T[] {
  const target = index + direction
  if (target < 0 || target >= items.length) return items
  const result = [...items]
  ;[result[index], result[target]] = [result[target], result[index]]
  return result
}
