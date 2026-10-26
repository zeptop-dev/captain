import { useEffect } from 'react'
import { applyBrandMetadata } from '../../../shared/branding'
import { usePublicBrand } from '../lib/branding'

export function BrandingMetadata() {
  const { data } = usePublicBrand()
  const name = data?.name || 'Captain'
  const { title, favicon } = data?.branding ?? {}
  const description = data?.description
  useEffect(() => applyBrandMetadata({ name, title, favicon, description }), [name, title, favicon, description])
  return null
}
