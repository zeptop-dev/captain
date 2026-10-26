import '@mantine/core/styles.css'
import React, { useEffect, useState } from 'react'
import ReactDOM from 'react-dom/client'
import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { buildTheme } from './theme'
import { applyBrandMetadata } from '../../shared/branding'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { getJSON, type Site } from './lib/site'
import { effectiveSiteScheme, parseSitePreference, readSitePreference, resolveSiteAppearance, siteSchemeStorageKey, type SiteScheme } from './appearance'
import { SiteSchemeContext } from './components/SiteThemeSelector'
import { subscribeSiteLanguage } from './i18n'
import App from './App'
import './site.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 60_000 } } })

// An invite link (?ref=CODE) is remembered in a cookie until the visitor signs up.
const ref = new URLSearchParams(window.location.search).get('ref')
if (ref) fetch('/api/portal/ref', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Code: ref }), credentials: 'same-origin' }).catch(() => {})

// The public site starts light and remembers only the visitor's explicit choice.
// Palette and typography still follow the operator's settings.
function Themed({ children }: { children: ReactNode }) {
  useEffect(subscribeSiteLanguage, [])
  const site = useQuery({ queryKey: ['site'], queryFn: () => getJSON<Site>('/api/site') })
  useEffect(() => {
    if (site.data) return applyBrandMetadata({ name: site.data.name, title: site.data.branding?.title, description: site.data.description, favicon: site.data.branding?.favicon })
  }, [site.data])
  const a = resolveSiteAppearance(site.data?.theme)
  const [preference, setPreference] = useState<SiteScheme | null>(() => {
    try { return readSitePreference(window.localStorage) } catch { return null }
  })
  const selected = effectiveSiteScheme(preference)
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === siteSchemeStorageKey || event.key === null) setPreference(parseSitePreference(event.newValue))
    }
    window.addEventListener('storage', changed)
    return () => window.removeEventListener('storage', changed)
  }, [])
  const select = (value: SiteScheme) => {
    setPreference(value)
    try { window.localStorage.setItem(siteSchemeStorageKey, value) } catch { /* Keep the current-page choice when storage is unavailable. */ }
  }
  // Keep Mantine's initial layout effect away from the portal/admin storage key.
  return <SiteSchemeContext.Provider value={{ selected, select }}><MantineProvider theme={buildTheme(a.primary || 'cyan', a.radius || 'lg', a.font)} cssVariablesResolver={() => ({
    variables: {
      '--site-configured-radius': a.radius ? 'var(--mantine-radius-default)' : 'initial',
    },
    light: {
      '--site-configured-accent': a.primary ? `color-mix(in srgb, var(--mantine-color-${a.primary}-9) 80%, #000)` : 'initial',
      '--site-configured-ink': a.primary ? '#fff' : 'initial',
    },
    dark: {
      '--site-configured-accent': a.primary ? `var(--mantine-color-${a.primary}-3)` : 'initial',
      '--site-configured-ink': a.primary ? '#12170b' : 'initial',
    },
  })} forceColorScheme={selected} colorSchemeManager={{ get: () => selected, set: () => {}, clear: () => {}, subscribe: () => {}, unsubscribe: () => {} }}>{children}</MantineProvider></SiteSchemeContext.Provider>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <Themed><App /></Themed>
    </QueryClientProvider>
  </React.StrictMode>,
)
