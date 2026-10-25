import '@mantine/core/styles.css'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { buildTheme } from './theme'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { getJSON, type Site } from './lib/site'
import { resolveSiteAppearance, siteColorSchemeManager } from './appearance'
import App from './App'
import './site.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 60_000 } } })

// An invite link (?ref=CODE) is remembered in a cookie until the visitor signs up.
const ref = new URLSearchParams(window.location.search).get('ref')
if (ref) fetch('/api/portal/ref', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Code: ref }), credentials: 'same-origin' }).catch(() => {})

// The landing page follows the operator's theme (colour, radius, scheme).
function Themed({ children }: { children: ReactNode }) {
  const site = useQuery({ queryKey: ['site'], queryFn: () => getJSON<Site>('/api/site') })
  const a = resolveSiteAppearance(site.data?.theme)
  return <MantineProvider key={a.scheme} theme={buildTheme(a.primary || 'cyan', a.radius || 'lg', a.font)} cssVariablesResolver={() => ({
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
  })} forceColorScheme={a.scheme === 'auto' ? undefined : a.scheme} defaultColorScheme={a.scheme} colorSchemeManager={siteColorSchemeManager(a.scheme)}>{children}</MantineProvider>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <Themed><App /></Themed>
    </QueryClientProvider>
  </React.StrictMode>,
)
