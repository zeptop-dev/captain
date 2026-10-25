import type { SiteTheme } from './lib/site'

export type SiteScheme = 'light' | 'dark' | 'auto'
const palettes = ['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray']
const radii = ['xs', 'sm', 'md', 'lg', 'xl']

export function resolveSiteAppearance(theme?: SiteTheme) {
  const scheme: SiteScheme = theme?.site_scheme === 'light' || theme?.site_scheme === 'auto' ? theme.site_scheme : 'dark'
  return {
    scheme,
    primary: theme?.primary && palettes.includes(theme.primary) ? theme.primary : undefined,
    radius: theme?.radius && radii.includes(theme.radius) ? theme.radius : undefined,
    font: typeof theme?.font_family === 'string' ? theme.font_family : undefined,
  }
}

// Keep public-site choices independent of the portal/admin and server probe.
export const siteSchemeStorageKey = 'captain-site-color-scheme'

export function parseSitePreference(value: string | null): SiteScheme | null {
  return value === 'light' || value === 'dark' || value === 'auto' ? value : null
}

export function readSitePreference(storage: Pick<Storage, 'getItem'>): SiteScheme | null {
  try { return parseSitePreference(storage.getItem(siteSchemeStorageKey)) } catch { return null }
}

export function effectiveSiteScheme(preference: SiteScheme | null, configured: SiteScheme, systemDark: boolean) {
  const selected = preference ?? configured
  return { selected, computed: selected === 'auto' ? (systemDark ? 'dark' : 'light') : selected } as const
}
