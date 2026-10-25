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

// The operator chooses the public appearance. A visitor's saved portal/admin
// Mantine preference must not override it, including system-following auto.
export function siteColorSchemeManager(scheme: SiteScheme) {
  return { get: () => scheme, set: () => {}, clear: () => {}, subscribe: () => {}, unsubscribe: () => {} }
}
