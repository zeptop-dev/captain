import { safeBrandAssetURL } from '../../../shared/branding'
import { useQuery } from '@tanstack/react-query'
import { api, type SiteSettings } from './api'

export const publicBrandQueryKey = ['public-site-brand']

export function usePublicBrand() {
  return useQuery({ queryKey: publicBrandQueryKey, queryFn: () => api.get<SiteSettings>('/api/site'), staleTime: 60_000 })
}

export function validBrandAssetInput(value: string | undefined): boolean {
  if (!value) return true
  const url = safeBrandAssetURL(value)
  return !!url && (!url.startsWith('/brand-assets/') || /^\/brand-assets\/[a-f0-9]{64}\.png$/.test(url))
}
