import { QueryClient } from '@tanstack/react-query'
import { ApiError } from '../api'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: true,
      retry: (failureCount, error) => {
        // Don't retry auth/permission/not-found — only transient failures.
        if (error instanceof ApiError && [401, 403, 404, 501].includes(error.status)) return false
        return failureCount < 2
      },
    },
    mutations: { retry: false },
  },
})
