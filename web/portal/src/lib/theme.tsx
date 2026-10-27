import { createTheme, MantineProvider, type ButtonProps, type MantineTheme } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useMediaQuery } from '@mantine/hooks'
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { applyBrandMetadata, type Branding } from '../../../shared/branding'
import { effectivePortalScheme, parsePortalPreference, portalSchemeStorageKey, readPortalPreference, type PortalScheme } from './appearance'
import './theme.css'

export interface SiteTheme { primary?: string; radius?: string; scheme?: string; site_scheme?: string; font_family?: string; portal_title?: string }
export interface SiteInfo { homepage_enabled?: boolean; name: string; description?: string; branding?: Branding; theme?: SiteTheme; probe_url?: string }

const colors = new Set(['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray'])
const radii = new Set(['xs', 'sm', 'md', 'lg', 'xl'])

// The portal shares the homepage's visual language while retaining operator
// palette, radius and font overrides. Destructive/status colors stay semantic.
export function buildTheme(t?: SiteTheme) {
  const primary = t?.primary && colors.has(t.primary) ? t.primary : 'olive'
  const radius = t?.radius && radii.has(t.radius) ? t.radius : '3px'
  return createTheme({
    colors: { olive: ['#f4f9e9', '#e8f2cf', '#dfefb1', '#d5f879', '#a4c55e', '#759a35', '#4c6d16', '#425e13', '#344b0f', '#27380b'] },
    primaryColor: primary,
    primaryShade: { light: primary === 'olive' ? 6 : 9, dark: 3 },
    autoContrast: true,
    defaultRadius: radius,
    fontFamily: t?.font_family || '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
    headings: { fontWeight: '500' },
    components: {
      Card: { defaultProps: { withBorder: true, padding: 'xl', radius, shadow: 'none' } },
      Button: {
        defaultProps: { radius },
        styles: { root: { fontWeight: 500 } },
        vars: (_: MantineTheme, props: ButtonProps) => !props.color && (!props.variant || props.variant === 'filled') ? { root: {
          '--button-bg': 'var(--portal-accent)',
          '--button-hover': 'color-mix(in srgb, var(--portal-accent) 88%, var(--portal-text))',
          '--button-color': 'var(--portal-accent-ink)',
        } } : { root: {} },
      },
      Input: { styles: { input: { backgroundColor: 'var(--portal-panel)' } } },
      Modal: { defaultProps: { centered: true }, styles: { content: { backgroundColor: 'var(--portal-panel)' }, header: { backgroundColor: 'var(--portal-panel)' } } },
    },
  })
}

const AppearanceContext = createContext<{ scheme: PortalScheme; toggle: () => void }>({ scheme: 'light', toggle: () => {} })
export const usePortalAppearance = () => useContext(AppearanceContext)

export function useSite() {
  return useQuery({ queryKey: ['site-info'], queryFn: async () => (await fetch('/api/site')).json() as Promise<SiteInfo>, staleTime: 60_000 })
}

// Wraps MantineProvider with the fetched theme. Renders with defaults until
// the settings arrive so the first paint is not delayed.
export function ThemedProvider({ children, defaultScheme }: { children: ReactNode; defaultScheme: 'light' | 'dark' }) {
  const site = useSite()
  useEffect(() => {
    if (site.data) return applyBrandMetadata({ name: site.data.name, title: site.data.branding?.title, description: site.data.description, favicon: site.data.branding?.favicon })
  }, [site.data])
  const t = site.data?.theme
  const [preference, setPreference] = useState<PortalScheme | null>(() => {
    try { return readPortalPreference(window.localStorage) } catch { return null }
  })
  const systemDark = useMediaQuery('(prefers-color-scheme: dark)')
  const scheme = effectivePortalScheme(preference, t?.scheme, systemDark, defaultScheme)
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === portalSchemeStorageKey || event.key === null) setPreference(parsePortalPreference(event.newValue))
    }
    window.addEventListener('storage', changed)
    return () => window.removeEventListener('storage', changed)
  }, [])
  const toggle = () => {
    const next = scheme === 'dark' ? 'light' : 'dark'
    setPreference(next)
    try { window.localStorage.setItem(portalSchemeStorageKey, next) } catch { /* Keep the current-page choice without browser storage. */ }
  }
  const primary = t?.primary && colors.has(t.primary) ? t.primary : 'olive'
  return <AppearanceContext.Provider value={{ scheme, toggle }}><MantineProvider theme={buildTheme(t)} forceColorScheme={scheme}
    colorSchemeManager={{ get: () => scheme, set: () => {}, clear: () => {}, subscribe: () => {}, unsubscribe: () => {} }}
    cssVariablesResolver={() => ({ variables: {}, light: {
      '--portal-accent': primary === 'olive' ? '#4c6d16' : `color-mix(in srgb, var(--mantine-color-${primary}-9) 80%, #000)`,
      '--portal-accent-ink': '#fff',
    }, dark: { '--portal-accent': `var(--mantine-color-${primary}-3)`, '--portal-accent-ink': '#12170b' } })}>
    {children}
  </MantineProvider></AppearanceContext.Provider>
}
