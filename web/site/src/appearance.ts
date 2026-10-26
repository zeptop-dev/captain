import type { SiteTheme } from './lib/site'

export type SiteScheme = 'light' | 'dark'
const palettes = ['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray']
const radii = ['xs', 'sm', 'md', 'lg', 'xl']

export function resolveSiteAppearance(theme?: SiteTheme) {
  return {
    primary: theme?.primary && palettes.includes(theme.primary) ? theme.primary : undefined,
    radius: theme?.radius && radii.includes(theme.radius) ? theme.radius : undefined,
    font: typeof theme?.font_family === 'string' ? theme.font_family : undefined,
  }
}

// Keep public-site choices independent of the portal/admin and server probe.
export const siteSchemeStorageKey = 'captain-site-color-scheme'

export function parseSitePreference(value: string | null): SiteScheme | null {
  // A legacy automatic choice now uses the light default, independent of OS.
  return value === 'light' || value === 'dark' ? value : null
}

export function readSitePreference(storage: Pick<Storage, 'getItem'>): SiteScheme | null {
  try { return parseSitePreference(storage.getItem(siteSchemeStorageKey)) } catch { return null }
}

export function effectiveSiteScheme(preference: SiteScheme | null): SiteScheme {
  return preference ?? 'light'
}
