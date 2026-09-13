import { createTheme, MantineProvider } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { type ReactNode } from 'react'

export interface SiteTheme { primary?: string; radius?: string; scheme?: string; site_scheme?: string; font_family?: string; portal_title?: string }
export interface SiteInfo { name: string; theme?: SiteTheme }

const colors = new Set(['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray'])
const radii = new Set(['xs', 'sm', 'md', 'lg', 'xl'])

// Builds the Mantine theme from the operator's settings; defaults keep the
// original look when nothing is configured.
export function buildTheme(t?: SiteTheme) {
  const primary = t?.primary && colors.has(t.primary) ? t.primary : 'cyan'
  const radius = t?.radius && radii.has(t.radius) ? t.radius : 'lg'
  return createTheme({
    primaryColor: primary,
    primaryShade: 6,
    defaultRadius: radius,
    fontFamily: t?.font_family || 'Inter, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
    headings: { fontWeight: '700' },
    components: {
      Card: { defaultProps: { withBorder: true, padding: 'xl', radius } },
      Button: { defaultProps: { radius: radius === 'xl' ? 'lg' : 'md' } },
    },
  })
}

export function useSite() {
  return useQuery({ queryKey: ['site-info'], queryFn: async () => (await fetch('/api/site')).json() as Promise<SiteInfo>, staleTime: 60_000 })
}

// Wraps MantineProvider with the fetched theme. Renders with defaults until
// the settings arrive so the first paint is not delayed.
export function ThemedProvider({ children, defaultScheme }: { children: ReactNode; defaultScheme: 'light' | 'dark' }) {
  const site = useSite()
  const t = site.data?.theme
  const scheme = (t?.scheme === 'dark' || t?.scheme === 'light' || t?.scheme === 'auto') ? t.scheme : defaultScheme
  return <MantineProvider theme={buildTheme(t)} forceColorScheme={scheme === 'auto' ? undefined : (scheme as 'light' | 'dark')} defaultColorScheme={scheme === 'auto' ? 'auto' : scheme}>{children}</MantineProvider>
}
