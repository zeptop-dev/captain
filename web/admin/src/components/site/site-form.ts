import type { Entry, SiteSettings } from '../../lib/api'

export type SiteValues = {
  homepage_enabled: boolean
  map_revision: number
  map_source: 'manual' | 'entries'
  status_map_source: 'manual' | 'entries'
  map_entry_ids: number[]
  logo_light: string
  logo_dark: string
  favicon: string
  brand_title: string
  name: string
  tagline: string
  description: string
  show_plans: boolean
  telegram: string
  tos: string
  download: string
  github: string
  hub: string
  locations: string
  features: string
  faq: string
  primary: string
  radius: string
  scheme: string
  site_scheme: string | undefined
  font_family: string
  portal_title: string
  inject_head: string
  inject_body: string
}

export function siteValues(s: SiteSettings): SiteValues {
  return {
    homepage_enabled: s.homepage_enabled !== false,
    map_revision: s.map_revision ?? 0,
    map_source: s.map_source ?? 'manual',
    status_map_source: s.status_map_source ?? 'manual',
    map_entry_ids: [...(s.map_entry_ids ?? [])],
    logo_light: s.branding?.logo_light ?? '',
    logo_dark: s.branding?.logo_dark ?? '',
    favicon: s.branding?.favicon ?? '',
    brand_title: s.branding?.title ?? '',
    name: s.name,
    tagline: s.tagline,
    description: s.description,
    show_plans: s.show_plans,
    telegram: s.links.telegram ?? '',
    tos: s.links.tos ?? '',
    download: s.links.download ?? '',
    github: s.links.github ?? '',
    hub: s.hub ? JSON.stringify(s.hub) : '',
    locations: JSON.stringify(s.locations, null, 2),
    features: JSON.stringify(s.features, null, 2),
    faq: JSON.stringify(s.faq, null, 2),
    primary: s.theme?.primary || 'cyan',
    radius: s.theme?.radius || 'lg',
    scheme: s.theme?.scheme || 'light',
    // The public site uses its own browser preference, but probes still inherit this setting.
    site_scheme: s.theme?.site_scheme,
    font_family: s.theme?.font_family ?? '',
    portal_title: s.theme?.portal_title ?? '',
    inject_head: s.inject_head ?? '',
    inject_body: s.inject_body ?? '',
  }
}

export function sitePayload(v: SiteValues): SiteSettings {
  return {
    homepage_enabled: v.homepage_enabled,
    map_revision: v.map_revision,
    map_source: v.map_source,
    status_map_source: v.status_map_source,
    map_entry_ids: v.map_entry_ids,
    branding: { logo_light: v.logo_light, logo_dark: v.logo_dark, favicon: v.favicon, title: v.brand_title },
    name: v.name,
    tagline: v.tagline,
    description: v.description,
    show_plans: v.show_plans,
    links: { telegram: v.telegram, tos: v.tos, download: v.download, github: v.github },
    hub: v.hub.trim() ? JSON.parse(v.hub) : null,
    locations: JSON.parse(v.locations),
    features: JSON.parse(v.features),
    faq: JSON.parse(v.faq),
    theme: {
      primary: v.primary,
      radius: v.radius,
      scheme: v.scheme,
      site_scheme: v.site_scheme,
      font_family: v.font_family,
      portal_title: v.portal_title,
    },
    inject_head: v.inject_head,
    inject_body: v.inject_body,
  }
}

export function missingMapEntryIDs(ids: number[], entries: Pick<Entry, 'ID'>[] | undefined): number[] {
  return ids.filter(id => !entries?.some(entry => entry.ID === id))
}
