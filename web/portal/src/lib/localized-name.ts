/** Choose a display name without changing the stable group/category identity. */
export function localizedName(language: string, zh?: string, en?: string, legacy?: string): string {
  const chinese = /^zh(?:[-_]|$)/i.test(language.trim())
  const preferred = chinese ? zh : en
  const alternate = chinese ? en : zh
  return preferred?.trim() || alternate?.trim() || legacy || ''
}
