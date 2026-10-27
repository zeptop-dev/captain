import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import './i18n'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { Notifications } from '@mantine/notifications'
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { ThemedProvider } from './lib/theme'
import { toast } from './lib/notify'
import App from './App'
import { referralCode, rememberReferral } from './lib/referral'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 5_000 } }, queryCache: new QueryCache({ onError: (e, query) => { if (!query.meta?.handlesError) toast.err(e) } }) })

void rememberReferral(referralCode(window.location.search)).catch(() => {})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemedProvider defaultScheme="light">
        <Notifications position="top-center" />
        <BrowserRouter basename="/portal"><App /></BrowserRouter>
      </ThemedProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
