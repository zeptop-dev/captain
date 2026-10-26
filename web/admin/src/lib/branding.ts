import { useQuery } from '@tanstack/react-query'
import { api, type SiteSettings } from './api'

export const publicBrandQueryKey = ['public-site-brand']

export function usePublicBrand() {
  return useQuery({ queryKey: publicBrandQueryKey, queryFn: () => api.get<SiteSettings>('/api/site'), staleTime: 60_000 })
}

