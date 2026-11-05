/** Choose a display name without changing the stable group/category identity. */
export function localizedName(language: string, zh?: string, en?: string, legacy?: string): string {
  const chinese = /^zh(?:[-_]|$)/i.test(language.trim())
  const preferred = chinese ? zh : en
  const alternate = chinese ? en : zh
  return preferred?.trim() || alternate?.trim() || legacy || ''
}

interface MonitorGroup {
  group?: string
  group_zh?: string
  group_en?: string
}

export function monitorGroupOptions(nodes: readonly MonitorGroup[], language: string): { value: string; label: string }[] {
  const groups = new Map<string, MonitorGroup>()
  for (const node of nodes) {
    if (node.group && !groups.has(node.group)) groups.set(node.group, node)
  }
  return [...groups].map(([value, node]) => ({ value, label: localizedName(language, node.group_zh, node.group_en, value) }))
}
