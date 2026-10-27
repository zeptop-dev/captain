import { useEffect, useRef, useState, type ReactNode } from 'react'
import { IconArrowUp, IconArrowUpRight, IconChevronDown, IconMenu2, IconX } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { getJSON } from '../lib/site'
import type { PublishedClient } from '../../../shared/client-downloads'
import { SiteLanguageSelector } from './SiteLanguageSelector'
import type { Site } from '../lib/site'
import { SiteThemeSelector } from './SiteThemeSelector'
import { SiteBrandMark } from './SiteBrandMark'
import { brandDestination, navigation } from '../lib/site-navigation'

function Brand({ name, site }: { name: string; site?: Site }) {
  const { t } = useTranslation()
  return <a className="site-brand" href={brandDestination(site)} aria-label={t(site?.homepage_enabled === false ? 'nav.brandAccount' : 'nav.brandHome', { name })}>
    <SiteBrandMark branding={site?.branding} />
    <span>{name}</span>
  </a>
}

export function SiteLayout({ site, children }: { site?: Site; children: ReactNode }) {
  const { t } = useTranslation()
  const name = site?.name || 'Captain'
  const catalog = useQuery({ queryKey: ['client-catalog', 'site'], queryFn: () => getJSON<PublishedClient[]>('/api/site/clients') })
  const { primary, footer } = navigation(t, site, !!catalog.data?.length)
  const [open, setOpen] = useState<string | null>(null)
  const [mobile, setMobile] = useState(false)
  const header = useRef<HTMLElement>(null)
  useEffect(() => {
    const outside = (event: PointerEvent) => { if (!header.current?.contains(event.target as Node)) { setOpen(null); setMobile(false) } }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (open) {
        setOpen(null)
        header.current?.querySelector<HTMLButtonElement>(`button[aria-controls="site-nav-${open}"]`)?.focus()
      } else if (mobile) {
        setMobile(false)
        header.current?.querySelector<HTMLButtonElement>('.site-menu-toggle')?.focus()
      }
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [open, mobile])
  const closeNavigation = () => { setOpen(null); setMobile(false) }
  return <div className="site-shell" id="top">
    <a className="site-skip" href="#site-content">{t('nav.skip')}</a>
    <header className="site-header" ref={header}>
      <div className="site-container site-header-inner">
        <Brand name={name} site={site} />
        <div className="site-header-controls">
        <button type="button" className="site-menu-toggle" aria-label={mobile ? t('nav.close') : t('nav.open')} aria-expanded={mobile} aria-controls="site-navigation" onClick={() => setMobile(!mobile)}>{mobile ? <IconX size={21} /> : <IconMenu2 size={21} />}</button>
        <nav id="site-navigation" className={`site-navigation${mobile ? ' is-open' : ''}`} aria-label={t('nav.main')}>
          {primary.map(group => group.links ? <div className="site-nav-group" key={group.id}>
            <button type="button" className="site-nav-button" aria-expanded={open === group.id} aria-controls={`site-nav-${group.id}`} onClick={() => setOpen(open === group.id ? null : group.id)}>{group.label}<IconChevronDown size={12} aria-hidden /></button>
            {open === group.id && <div id={`site-nav-${group.id}`} className="site-nav-dropdown">{group.links.map(link => <a key={`${link.label}:${link.href}`} href={link.href} onClick={closeNavigation}>{link.label}</a>)}</div>}
          </div> : <a className="site-nav-link" key={group.id} href={group.href} onClick={closeNavigation}>{group.label}</a>)}
          <a className="site-account-link site-account-mobile" href="/portal/">{t('nav.account')}</a>
        </nav>
        <SiteThemeSelector />
        <SiteLanguageSelector />
        <a className="site-account-link site-account-desktop" href="/portal/">{t('nav.account')}</a>
        </div>
      </div>
    </header>
    <main id="site-content">{children}</main>
    <footer className="site-footer">
      <div className="site-container">
        <a className="site-footer-brand" href={brandDestination(site)} aria-label={t(site?.homepage_enabled === false ? 'nav.brandAccount' : 'nav.brandHome', { name })}><SiteBrandMark branding={site?.branding} /><span>{name}</span><IconArrowUpRight size={32} stroke={1.5} aria-hidden /></a>
        <div className="site-footer-columns">{footer.map(group => <div key={group.id}><h2>{group.label}</h2><ul>{group.links.map(link => <li key={`${link.label}:${link.href}`}><a href={link.href}>{link.label}</a></li>)}</ul></div>)}</div>
      </div>
      <div className="site-footer-bottom"><div className="site-container"><small>© {new Date().getFullYear()} {name}. {t('nav.rights')}</small><a href="#top">{t('nav.top')} <IconArrowUp size={14} aria-hidden /></a></div></div>
    </footer>
  </div>
}
