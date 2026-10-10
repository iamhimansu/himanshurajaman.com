import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import App from './App'
import { ApiError, WarmingError } from './lib/api'

import './styles/global.css'
import './styles/code.css'
import './styles/print.css'

// Retry policy:
// - Warming (503) is expected on cold start; retry persistently with a short
//   fixed delay so the UI recovers automatically once ingest finishes.
// - 404 is a real "not found"; never retry (the Post page needs it fast).
// - Other errors get a few backed-off retries.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: (failureCount, error) => {
        if (error instanceof WarmingError) return failureCount < 60
        if (error instanceof ApiError && error.status === 404) return false
        return failureCount < 3
      },
      retryDelay: (failureCount, error) => {
        if (error instanceof WarmingError) return 2000
        return Math.min(1000 * 2 ** failureCount, 10000)
      },
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
