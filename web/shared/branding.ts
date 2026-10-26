/** Public appearance fields only. Never pass private settings or HTML here. */
export interface Branding {
  logo_light?: string
  logo_dark?: string
  favicon?: string
  title?: string
}

/** Brand images may be uploaded local assets or ordinary HTTP(S) images. */
export function safeBrandAssetURL(value?: string): string {
  const url = value || ''
  if (!url || url.trim() !== url || url.length > 2048 || /[\\\u0000-\u001f\u007f]/.test(url)) return ''
  try {
    if (url.startsWith('/') && !url.startsWith('//')) {
      const path = decodeURIComponent(url.split(/[?#]/, 1)[0])
      if (path.startsWith('//') || /[\\\u0000-\u001f\u007f]/.test(path)) return ''
      // URL.parse on the server rejects malformed escapes in the fragment too.
      if (url.includes('#')) decodeURIComponent(url.slice(url.indexOf('#') + 1))
      return url
    }
    if (!/^https?:\/\//i.test(url) || url.split('/')[2]?.includes('@')) return ''
    const parsed = new URL(url)
    decodeURIComponent(parsed.pathname)
    decodeURIComponent(parsed.hash)
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

// A compiled local asset also clears a browser's cached icon when the custom
// favicon is removed; deleting the link alone may leave that old icon visible.
export const defaultBrandFavicon = new URL('./captain-mark.svg', import.meta.url).href

/** Own the current SPA's metadata, restoring the previous document on cleanup. */
export function applyBrandMetadata({ name, title, description, favicon }: BrandMetadata): () => void {
  const previousTitle = document.title
  const replaced = Array.from(document.head.querySelectorAll<HTMLMetaElement | HTMLLinkElement>('meta[name="description"],link[rel~="icon"]')).map(element => ({ element, next: element.nextSibling }))
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
  const link = document.createElement('link')
  link.rel = 'icon'
  link.href = safeBrandAssetURL(favicon) || defaultBrandFavicon
  link.onerror = () => { if (link.href !== defaultBrandFavicon) link.href = defaultBrandFavicon }
  document.head.append(link)
  added.push(link)
  return () => {
    document.title = previousTitle
    for (const element of added) element.remove()
    // Reverse order preserves adjacent replaced nodes and their original order.
    for (const { element, next } of replaced.reverse()) {
      document.head.insertBefore(element, next?.parentNode === document.head ? next : null)
    }
  }
}
