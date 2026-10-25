export const languages = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
  { value: 'zh-TW', label: '繁體中文' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'ru', label: 'Русский' },
] as const

export type Language = typeof languages[number]['value']

export function resolveLanguage(requested: string): Language {
  const tag = requested.trim().toLowerCase().replaceAll('_', '-')
  if (/^zh-(?:tw|hk|mo|hant)(?:-|$)/.test(tag)) return 'zh-TW'
  if (tag === 'zh' || tag.startsWith('zh-')) return 'zh-CN'
  return languages.find(({ value }) => value === tag.split('-')[0])?.value ?? 'en'
}

export function readLanguage(readPreference: () => string | null, browserLanguage: string): Language {
  try {
    return resolveLanguage(readPreference() || browserLanguage)
  } catch {
    return resolveLanguage(browserLanguage)
  }
}

export function saveLanguage(value: string, writePreference: (value: Language) => void): boolean {
  const selected = languages.find(language => language.value === value)
  if (!selected) return false
  try {
    writePreference(selected.value)
    return true
  } catch {
    return false
  }
}
