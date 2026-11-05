// Translated labels never replace the stable key used by selections or grouping.
export function localizedName(language: string, zh?: string, en?: string, legacy = ''): string {
  const chinese = /^zh(?:[-_]|$)/i.test(language)
  return (chinese ? zh?.trim() || en?.trim() : en?.trim() || zh?.trim()) || legacy
}

export function localizedNameDraft(zh?: string, en?: string, legacy = ''): { zh: string; en: string } {
  return { zh: zh || (!en ? legacy : ''), en: en || '' }
}
