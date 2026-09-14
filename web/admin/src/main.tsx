import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import '@mantine/charts/styles.css'
import '@mantine/dates/styles.css'
import './i18n'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { MantineProvider } from '@mantine/core'
import { ModalsProvider } from '@mantine/modals'
import { Notifications } from '@mantine/notifications'
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { toast } from './lib/notify'
import { BrowserRouter } from 'react-router-dom'
import { theme } from './theme'
import App from './App'

// Any failed query surfaces as a toast so an API error never looks like an empty list.
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5_000 } },
  queryCache: new QueryCache({ onError: (e) => toast.err(e) }),
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <MantineProvider theme={theme} defaultColorScheme="light">
        <ModalsProvider>
          <Notifications position="top-right" />
          <BrowserRouter basename="/admin">
            <App />
          </BrowserRouter>
        </ModalsProvider>
      </MantineProvider>
    </QueryClientProvider>
  </React.StrictMode>,
)
