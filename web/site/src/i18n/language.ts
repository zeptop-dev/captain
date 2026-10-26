export const siteLanguageStorageKey = 'captain-site-language'
export const siteLanguages = ['en', 'zh-CN', 'zh-TW', 'ja', 'ru', 'ko'] as const
export type SiteLanguage = typeof siteLanguages[number]

// Browser locale tags can contain both a script and a region. Prefer the script
// for Chinese (for example, zh-Hant-CN still requests Traditional Chinese).
export function matchSiteLanguage(value: string): SiteLanguage | null {
  const [language, ...parts] = value.trim().toLowerCase().replaceAll('_', '-').split('-')
  if (language === 'zh') {
    if (parts.includes('hant')) return 'zh-TW'
    if (parts.includes('hans')) return 'zh-CN'
    return parts.some(part => ['tw', 'hk', 'mo'].includes(part)) ? 'zh-TW' : 'zh-CN'
  }
  return ['en', 'ja', 'ru', 'ko'].includes(language) ? language as SiteLanguage : null
}

export function parseSiteLanguage(value: string | null): SiteLanguage | null {
  return siteLanguages.includes(value as SiteLanguage) ? value as SiteLanguage : null
}

export function resolveSiteLanguage(preference: string | null, languages: readonly string[]): SiteLanguage {
  const saved = parseSiteLanguage(preference)
  if (saved) return saved
  for (const language of languages) {
    const supported = matchSiteLanguage(language)
    if (supported) return supported
  }
  return 'en'
}

export function readSiteLanguage(storage: Pick<Storage, 'getItem'>): SiteLanguage | null {
  try { return parseSiteLanguage(storage.getItem(siteLanguageStorageKey)) } catch { return null }
}
