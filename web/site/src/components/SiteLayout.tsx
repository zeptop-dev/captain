import { useEffect, useRef, useState, type ReactNode } from 'react'
import { IconArrowUp, IconArrowUpRight, IconChevronDown, IconMenu2, IconX } from '@tabler/icons-react'
import type { Site } from '../lib/site'

type Link = { label: string; href: string }
type LinkGroup = { label: string; links: Link[] }

function navigation(site?: Site): LinkGroup[] {
  return [
    { label: '特性', links: [
      { label: '网络覆盖', href: '/#coverage' },
      { label: '多端接入', href: '/#platform' },
      ...(site?.features.length ? [{ label: '服务特性', href: '/#features' }] : []),
    ] },
    { label: '数据及支持', links: [
      { label: '连接指南', href: '/portal/connect' },
      ...(site?.links.download ? [{ label: '客户端下载', href: site.links.download }] : []),
      ...(site?.faq.length ? [{ label: '常见问题', href: '/#faq' }] : []),
      ...(site?.links.telegram ? [{ label: '联系支持', href: site.links.telegram }] : []),
    ] },
    { label: '服务及保障', links: [
      { label: '套餐与服务', href: site?.show_plans ? '/#plans' : '/portal/plans' },
      ...(site?.links.tos ? [{ label: '服务条款', href: site.links.tos }] : []),
      { label: '账户安全', href: '/portal/account' },
    ] },
    { label: '关于', links: [
      { label: '首页', href: '/' },
      ...(site?.service_status_url ? [{ label: '服务状态', href: site.service_status_url }] : []),
      ...(site?.probe_url ? [{ label: '服务器监控', href: site.probe_url }] : []),
      ...(site?.links.github ? [{ label: 'GitHub', href: site.links.github }] : []),
    ] },
  ]
}

function Brand({ name }: { name: string }) {
  return <a className="site-brand" href="/" aria-label={`${name} 首页`}>
    <svg viewBox="0 0 28 28" width="25" height="25" fill="none" aria-hidden="true"><path d="m14 2 10.4 6v12L14 26 3.6 20V8Z" stroke="currentColor" strokeWidth="1.3" /><path d="m14 7 6 14-6-4-6 4Z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /><path d="M14 7v10" stroke="currentColor" strokeWidth="1.3" /></svg>
    <span>{name}</span>
  </a>
}

export function SiteLayout({ site, children }: { site?: Site; children: ReactNode }) {
  const name = site?.name || 'Captain'
  const groups = navigation(site)
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
    <a className="site-skip" href="#site-content">跳到主要内容</a>
    <header className="site-header" ref={header}>
      <div className="site-container site-header-inner">
        <Brand name={name} />
        <button type="button" className="site-menu-toggle" aria-label={mobile ? '关闭导航' : '打开导航'} aria-expanded={mobile} aria-controls="site-navigation" onClick={() => setMobile(!mobile)}>{mobile ? <IconX size={21} /> : <IconMenu2 size={21} />}</button>
        <nav id="site-navigation" className={`site-navigation${mobile ? ' is-open' : ''}`} aria-label="主导航">
          {groups.map(group => <div className="site-nav-group" key={group.label}>
            <button type="button" className="site-nav-button" aria-expanded={open === group.label} aria-controls={`site-nav-${group.label}`} onClick={() => setOpen(open === group.label ? null : group.label)}>{group.label}<IconChevronDown size={12} aria-hidden /></button>
            {open === group.label && <div id={`site-nav-${group.label}`} className="site-nav-dropdown">{group.links.map(link => <a key={`${link.label}:${link.href}`} href={link.href} onClick={() => { setOpen(null); setMobile(false) }}>{link.label}<IconArrowUpRight size={13} aria-hidden /></a>)}</div>}
          </div>)}
          <a className="site-account-link" href="/portal/">用户中心</a>
        </nav>
      </div>
    </header>
    <main id="site-content">{children}</main>
    <footer className="site-footer">
      <div className="site-container">
        <a className="site-footer-brand" href="/">{name}<IconArrowUpRight size={32} stroke={1.5} aria-hidden /></a>
        <div className="site-footer-columns">{groups.map(group => <div key={group.label}><h2>{group.label}</h2><ul>{group.links.map(link => <li key={`${link.label}:${link.href}`}><a href={link.href}>{link.label}</a></li>)}</ul></div>)}</div>
      </div>
      <div className="site-footer-bottom"><div className="site-container"><small>© {new Date().getFullYear()} {name}. All rights reserved.</small><a href="#top">返回顶部 <IconArrowUp size={14} aria-hidden /></a></div></div>
    </footer>
  </div>
}
