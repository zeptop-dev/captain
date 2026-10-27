import type { TFunction } from 'i18next'
import type { Site } from './site'

type Link = { label: string; href: string }
type LinkGroup = { id: string; label: string; links: Link[] }

export function navigation(t: TFunction, site?: Site, hasClients = false): LinkGroup[] {
  const homepage = site?.homepage_enabled !== false
  return [
    ...(homepage ? [{ id: 'features', label: t('nav.features'), links: [
      { label: t('nav.coverage'), href: '/#coverage' },
      { label: t('nav.platform'), href: '/#platform' },
      ...(site?.features.length ? [{ label: t('nav.serviceFeatures'), href: '/#features' }] : []),
    ] }] : []),
    { id: 'support', label: t('nav.support'), links: [
      { label: t('nav.connect'), href: '/portal/connect' },
      ...((site?.links.download || hasClients) ? [{ label: t('nav.download'), href: site?.links.download || '/downloads' }] : []),
      ...(homepage && site?.faq.length ? [{ label: t('nav.faq'), href: '/#faq' }] : []),
      ...(site?.links.telegram ? [{ label: t('nav.contact'), href: site.links.telegram }] : []),
    ] },
    { id: 'services', label: t('nav.services'), links: [
      { label: t('nav.plans'), href: homepage && site?.show_plans ? '/#plans' : '/portal/plans' },
      ...(site?.links.tos ? [{ label: t('nav.terms'), href: site.links.tos }] : []),
      { label: t('nav.security'), href: '/portal/account' },
    ] },
    { id: 'about', label: t('nav.about'), links: [
      ...(homepage ? [{ label: t('nav.home'), href: '/' }] : []),
      ...(site?.service_status_url ? [{ label: t('nav.status'), href: site.service_status_url }] : []),
      ...(site?.probe_url ? [{ label: t('nav.monitoring'), href: site.probe_url }] : []),
      ...(site?.links.github ? [{ label: 'GitHub', href: site.links.github }] : []),
    ] },
  ].filter(group => group.links.length > 0)
}

export const brandDestination = (site?: Pick<Site, 'homepage_enabled'>) => site?.homepage_enabled === false ? '/portal/' : '/'

// The root redirect carries only invitation attribution, never a return URL.
export function portalEntryURL(search: string): string {
  const ref = new URLSearchParams(search).get('ref')?.trim()
  return '/portal/' + (ref ? '?' + new URLSearchParams({ ref }).toString() : '')
}
