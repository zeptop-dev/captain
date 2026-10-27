import type { TFunction } from 'i18next'
import type { Site } from './site'

type Link = { label: string; href: string }
type LinkGroup = { id: string; label: string; links: Link[] }
type NavigationItem = (Link & { id: string; links?: never }) | (LinkGroup & { href?: never })

export function navigation(t: TFunction, site?: Site, hasClients = false): { primary: NavigationItem[]; footer: LinkGroup[] } {
  const homepage = site?.homepage_enabled !== false
  const plans = { label: t('nav.plans'), href: homepage && site?.show_plans ? '/#plans' : '/portal/plans' }
  const downloads = site?.links.download || hasClients
    ? [{ label: t('nav.download'), href: site?.links.download || '/downloads' }]
    : []
  const support = [
    { label: t('nav.connect'), href: '/portal/connect' },
    ...(homepage && site?.faq.length ? [{ label: t('nav.faq'), href: '/#faq' }] : []),
    ...(site?.links.telegram ? [{ label: t('nav.contact'), href: site.links.telegram }] : []),
  ]
  const status = site?.service_status_url ? [{ label: t('nav.status'), href: site.service_status_url }] : []
  return {
    primary: [
      { id: 'plans', ...plans },
      ...downloads.map(link => ({ id: 'downloads', ...link })),
      { id: 'support', label: t('nav.support'), links: support },
      ...status.map(link => ({ id: 'status', ...link })),
    ],
    // The footer is a complete index; the header focuses on common actions.
    footer: [
      { id: 'services', label: t('nav.services'), links: [
        plans,
        ...(homepage ? [
          { label: t('nav.coverage'), href: '/#coverage' },
          { label: t('nav.platform'), href: '/#platform' },
          ...(site?.features.length ? [{ label: t('nav.serviceFeatures'), href: '/#features' }] : []),
        ] : []),
      ] },
      { id: 'support', label: t('nav.support'), links: [
        ...downloads, ...support, ...status,
      ] },
      { id: 'links', label: t('nav.termsLinks'), links: [
        ...(site?.links.tos ? [{ label: t('nav.terms'), href: site.links.tos }] : []),
        ...(site?.links.github ? [{ label: 'GitHub', href: site.links.github }] : []),
      ] },
    ].filter(group => group.links.length > 0),
  }
}

export const brandDestination = (site?: Pick<Site, 'homepage_enabled'>) => site?.homepage_enabled === false ? '/portal/' : '/'

// The root redirect carries only invitation attribution, never a return URL.
export function portalEntryURL(search: string): string {
  const ref = new URLSearchParams(search).get('ref')?.trim()
  return '/portal/' + (ref ? '?' + new URLSearchParams({ ref }).toString() : '')
}
