import { useEffect, useRef, useState, type ReactNode } from 'react'
import { IconArrowUp, IconArrowUpRight, IconChevronDown, IconMenu2, IconX } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { useQuery } from '@tanstack/react-query'
import { getJSON } from '../lib/site'
import type { PublishedClient } from '../../../shared/client-downloads'
import { SiteLanguageSelector } from './SiteLanguageSelector'
import type { Site } from '../lib/site'
import { SiteThemeSelector } from './SiteThemeSelector'
import { SiteBrandMark } from './SiteBrandMark'

type Link = { label: string; href: string }
type LinkGroup = { id: string; label: string; links: Link[] }

function navigation(t: TFunction, site?: Site, hasClients = false): LinkGroup[] {
  return [
    { id: 'features', label: t('nav.features'), links: [
      { label: t('nav.coverage'), href: '/#coverage' },
      { label: t('nav.platform'), href: '/#platform' },
      ...(site?.features.length ? [{ label: t('nav.serviceFeatures'), href: '/#features' }] : []),
    ] },
    { id: 'support', label: t('nav.support'), links: [
      { label: t('nav.connect'), href: '/portal/connect' },
      ...((site?.links.download || hasClients) ? [{ label: t('nav.download'), href: site?.links.download || '/downloads' }] : []),
      ...(site?.faq.length ? [{ label: t('nav.faq'), href: '/#faq' }] : []),
      ...(site?.links.telegram ? [{ label: t('nav.contact'), href: site.links.telegram }] : []),
    ] },
    { id: 'services', label: t('nav.services'), links: [
      { label: t('nav.plans'), href: site?.show_plans ? '/#plans' : '/portal/plans' },
      ...(site?.links.tos ? [{ label: t('nav.terms'), href: site.links.tos }] : []),
      { label: t('nav.security'), href: '/portal/account' },
    ] },
    { id: 'about', label: t('nav.about'), links: [
      { label: t('nav.home'), href: '/' },
      ...(site?.service_status_url ? [{ label: t('nav.status'), href: site.service_status_url }] : []),
      ...(site?.probe_url ? [{ label: t('nav.monitoring'), href: site.probe_url }] : []),
      ...(site?.links.github ? [{ label: 'GitHub', href: site.links.github }] : []),
    ] },
  ]
}

function Brand({ name, site }: { name: string; site?: Site }) {
  const { t } = useTranslation()
  return <a className="site-brand" href="/" aria-label={t('nav.brandHome', { name })}>
    <SiteBrandMark branding={site?.branding} />
    <span>{name}</span>
  </a>
}

export function SiteLayout({ site, children }: { site?: Site; children: ReactNode }) {
  const { t } = useTranslation()
  const name = site?.name || 'Captain'
  const catalog = useQuery({ queryKey: ['client-catalog', 'site'], queryFn: () => getJSON<PublishedClient[]>('/api/site/clients') })
  const groups = navigation(t, site, !!catalog.data?.length)
  const [open, setOpen] = useState<string | null>(null)
  const [mobile, setMobile] = useState(false)
  const header = useRef<HTMLElement>(null)
  useEffect(() => {
    const outside = (event: PointerEvent) => { if (!header.current?.contains(event.target as Node)) { setOpen(null); setMobile(false) } }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { setOpen(null); setMobile(false) } }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [])
  return <div className="site-shell" id="top">
    <a className="site-skip" href="#site-content">{t('nav.skip')}</a>
    <header className="site-header" ref={header}>
      <div className="site-container site-header-inner">
        <Brand name={name} site={site} />
        <div className="site-header-controls">
        <button type="button" className="site-menu-toggle" aria-label={mobile ? t('nav.close') : t('nav.open')} aria-expanded={mobile} aria-controls="site-navigation" onClick={() => setMobile(!mobile)}>{mobile ? <IconX size={21} /> : <IconMenu2 size={21} />}</button>
        <nav id="site-navigation" className={`site-navigation${mobile ? ' is-open' : ''}`} aria-label={t('nav.main')}>
          {groups.map(group => <div className="site-nav-group" key={group.id}>
            <button type="button" className="site-nav-button" aria-expanded={open === group.id} aria-controls={`site-nav-${group.id}`} onClick={() => setOpen(open === group.id ? null : group.id)}>{group.label}<IconChevronDown size={12} aria-hidden /></button>
            {open === group.id && <div id={`site-nav-${group.id}`} className="site-nav-dropdown">{group.links.map(link => <a key={`${link.label}:${link.href}`} href={link.href} onClick={() => { setOpen(null); setMobile(false) }}>{link.label}<IconArrowUpRight size={13} aria-hidden /></a>)}</div>}
          </div>)}
          <a className="site-account-link" href="/portal/">{t('nav.account')}</a>
        </nav>
        <SiteThemeSelector />
        <SiteLanguageSelector />
        </div>
      </div>
    </header>
    <main id="site-content">{children}</main>
    <footer className="site-footer">
      <div className="site-container">
        <a className="site-footer-brand" href="/"><SiteBrandMark branding={site?.branding} /><span>{name}</span><IconArrowUpRight size={32} stroke={1.5} aria-hidden /></a>
        <div className="site-footer-columns">{groups.map(group => <div key={group.id}><h2>{group.label}</h2><ul>{group.links.map(link => <li key={`${link.label}:${link.href}`}><a href={link.href}>{link.label}</a></li>)}</ul></div>)}</div>
      </div>
      <div className="site-footer-bottom"><div className="site-container"><small>© {new Date().getFullYear()} {name}. {t('nav.rights')}</small><a href="#top">{t('nav.top')} <IconArrowUp size={14} aria-hidden /></a></div></div>
    </footer>
  </div>
}
