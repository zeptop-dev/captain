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
export interface ClaudeRule { type: 'domain' | 'full'; value: string }
export interface ClaudeRuleSnapshot { source: 'builtin' | 'v2fly'; revision: string; rules: ClaudeRule[] }
export interface ClaudeRuleLibrary {
  snapshot: ClaudeRuleSnapshot
  synced_at: number
  count: number
  url: string
  license_url: string
}
export interface ClaudeRulesData {
  library: ClaudeRuleLibrary
  builtin: ClaudeRuleSnapshot
  applied: { status: 'unknown' | 'applied' | 'disabled'; snapshot?: ClaudeRuleSnapshot; applied_at?: number }
}
export interface ClaudeRulesSyncResult {
  library: ClaudeRuleLibrary
  added: ClaudeRule[]
  removed: ClaudeRule[]
  previous_revision: string
}
export const claudeRulesEndpoint = '/api/admin/settings/sub-design/claude-rules'
export const claudeRulesQueryKey = ['sub-design-claude-rules'] as const
export interface DesignEnhancements {
  regions?: boolean
  dns_protection?: boolean
  claude?: boolean
  webrtc?: boolean
  quic?: boolean
  claude_policy?: string
  claude_rules?: ClaudeRuleSnapshot
  dns_policy?: string
  [field: string]: unknown
}

// Missing snapshots in existing drafts retain the original built-in rules. Merely
// fetching a newer library must never change what that draft will publish.
export function draftClaudeRules(design: SubDesign, builtin: ClaudeRuleSnapshot): ClaudeRuleSnapshot {
  return design.enhancements?.claude_rules ?? builtin
}

export function diffClaudeRules(before: ClaudeRuleSnapshot, after: ClaudeRuleSnapshot): { added: ClaudeRule[]; removed: ClaudeRule[] } {
  const key = (rule: ClaudeRule) => `${rule.type}:${rule.value.toLowerCase()}`
  const oldRules = new Map(before.rules.map(rule => [key(rule), rule]))
  const newRules = new Map(after.rules.map(rule => [key(rule), rule]))
  return {
    added: [...newRules].filter(([id]) => !oldRules.has(id)).map(([, rule]) => rule),
    removed: [...oldRules].filter(([id]) => !newRules.has(id)).map(([, rule]) => rule),
  }
}

export function setClaudeEnabled(design: SubDesign, enabled: boolean): SubDesign {
  const next = structuredClone(design)
  next.enhancements = { ...next.enhancements, claude: enabled }
  // Even when the library GET is pending, toggling cannot reinterpret a legacy
  // draft. Only the explicit review/adopt action selects another rule version.
  return next
}

// A review applies to one draft. If a preset or another edit replaced it while
// the dialog was open, require a fresh comparison instead of overwriting it.
export function adoptClaudeRules(design: SubDesign, expectedDraft: string, snapshot: ClaudeRuleSnapshot): SubDesign | null {
  if (!design.enhancements?.claude || JSON.stringify(design) !== expectedDraft) return null
  return { ...structuredClone(design), enhancements: { ...structuredClone(design.enhancements), claude_rules: structuredClone(snapshot) } }
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
  url?: string
  official_notes?: string
  supported: boolean
  reason?: string
}
export interface DesignFormatSupport { format: string; supported: boolean; reasons: string[]; reason_codes?: string[] }
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

export function designSupportReasons(support: DesignFormatSupport[], labels: Record<string, string>): string[] {
  return [...new Set(support.filter(item => !item.supported).flatMap(item => {
    if (!item.reason_codes?.length) return item.reasons
    return [...item.reason_codes.map((code, index) => labels[code] ?? item.reasons[index] ?? code), ...item.reasons.slice(item.reason_codes.length)]
  }))]
}

// Protection policies must not offer a direct route, including through nested
// groups. A malformed cycle is left to the compiler but cannot loop the editor.
export function designPolicyCanDirect(design: SubDesign, policy: string, seen = new Set<string>()): boolean {
  if (policy === 'DIRECT') return true
  if (seen.has(policy)) return false
  seen.add(policy)
  return design.groups.find(group => group.name === policy)?.members.some(member => designPolicyCanDirect(design, member, seen)) ?? false
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
