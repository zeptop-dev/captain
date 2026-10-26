/** Public appearance fields only. Never pass private settings or HTML here. */
export interface Branding {
  logo_light?: string
  logo_dark?: string
  favicon?: string
  title?: string
}

/** Brand images may be uploaded local assets or ordinary HTTP(S) images. */
export function safeBrandAssetURL(value?: string): string {
  const url = value?.trim() || ''
  if (!url || /[\\\u0000-\u001f\u007f]/.test(url)) return ''
  if (url.startsWith('/') && !url.startsWith('//')) return url
  try {
    const parsed = new URL(url)
    return (parsed.protocol === 'https:' || parsed.protocol === 'http:') && !parsed.username && !parsed.password ? url : ''
  } catch {
    return ''
  }
}

export function brandLogo(branding: Branding | undefined, scheme: 'light' | 'dark'): string {
  const preferred = scheme === 'dark' ? branding?.logo_dark : branding?.logo_light
  const alternate = scheme === 'dark' ? branding?.logo_light : branding?.logo_dark
  return safeBrandAssetURL(preferred) || safeBrandAssetURL(alternate)
}

export interface BrandMetadata {
  name?: string
  title?: string
  description?: string
  favicon?: string
}

/** Own the current SPA's metadata, restoring the previous document on cleanup. */
export function applyBrandMetadata({ name, title, description, favicon }: BrandMetadata): () => void {
  const previousTitle = document.title
  const previousDescription = Array.from(document.head.querySelectorAll<HTMLMetaElement>('meta[name="description"]'))
  const previousIcons = Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]'))
  const replaced = [...previousDescription, ...previousIcons].map(element => ({ element, next: element.nextSibling }))
  for (const { element } of replaced) element.remove()
  document.title = title?.trim() || name?.trim() || 'Captain'
  const added: HTMLElement[] = []
  if (description?.trim()) {
    const meta = document.createElement('meta')
    meta.name = 'description'
    meta.content = description.trim()
    document.head.append(meta)
    added.push(meta)
  }
  const asset = safeBrandAssetURL(favicon)
  if (asset) {
    const link = document.createElement('link')
    link.rel = 'icon'
    link.href = asset
    document.head.append(link)
    added.push(link)
  }
  return () => {
    document.title = previousTitle
    for (const element of added) element.remove()
    // Reverse order preserves adjacent replaced nodes and their original order.
    for (const { element, next } of replaced.reverse()) {
      document.head.insertBefore(element, next?.parentNode === document.head ? next : null)
    }
  }
}
