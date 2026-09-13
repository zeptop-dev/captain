import '@mantine/core/styles.css'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { buildTheme } from './theme'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import App from './App'
import './site.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 60_000 } } })

// An invite link (?ref=CODE) is remembered in a cookie until the visitor signs up.
const ref = new URLSearchParams(window.location.search).get('ref')
if (ref) fetch('/api/portal/ref', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Code: ref }), credentials: 'same-origin' }).catch(() => {})

// The landing page follows the operator's theme (colour, radius, scheme).
function Themed({ children }: { children: ReactNode }) {
  const site = useQuery({ queryKey: ['site'], queryFn: async () => (await fetch('/api/site')).json() as Promise<{ theme?: { primary?: string; radius?: string; site_scheme?: string; font_family?: string } }> })
  const t = site.data?.theme
  const scheme = t?.site_scheme === 'light' || t?.site_scheme === 'auto' ? t.site_scheme : 'dark'
  return <MantineProvider theme={buildTheme(t?.primary || 'cyan', t?.radius || 'lg', t?.font_family)} forceColorScheme={scheme === 'auto' ? undefined : scheme} defaultColorScheme={scheme}>{children}</MantineProvider>
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <Themed><App /></Themed>
    </QueryClientProvider>
  </React.StrictMode>,
)
