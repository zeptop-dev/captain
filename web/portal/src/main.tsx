import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import './i18n'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { MantineProvider } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { theme } from './theme'
import { toast } from './lib/notify'
import App from './App'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 5_000 } }, queryCache: new QueryCache({ onError: (e) => toast.err(e) }) })

const ref = new URLSearchParams(window.location.search).get('ref')
if (ref) fetch('/api/portal/ref', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Code: ref }), credentials: 'same-origin' }).catch(() => {})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <MantineProvider theme={theme} defaultColorScheme="light">
        <Notifications position="top-center" />
        <BrowserRouter basename="/portal"><App /></BrowserRouter>
      </MantineProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
