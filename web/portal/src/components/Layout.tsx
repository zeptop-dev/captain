import { ActionIcon, Avatar, Container, Group, Menu, Text, UnstyledButton } from '@mantine/core'
import { IconActivity, IconArrowUpRight, IconCreditCard, IconHelpCircle, IconHome, IconLanguage, IconLogout, IconPlugConnected, IconSailboat, IconUser } from '@tabler/icons-react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useSite } from '../lib/theme'
import { api } from '../lib/api'
import styles from './Layout.module.css'

const LANGS: [string, string][] = [['zh-CN', '中文'], ['zh-TW', '繁體中文'], ['en', 'English'], ['ja', '日本語'], ['ko', '한국어'], ['ru', 'Русский']]
const sections = [
  { to: '/', key: 'overview', icon: IconHome, paths: ['/'] },
  { to: '/connect', key: 'connect', icon: IconPlugConnected, paths: ['/connect', '/servers'] },
  { to: '/plans', key: 'billing', icon: IconCreditCard, paths: ['/plans', '/orders'] },
  { to: '/help', key: 'help', icon: IconHelpCircle, paths: ['/help', '/tickets', '/status'] },
  { to: '/account', key: 'account', icon: IconUser, paths: ['/account', '/invite'] },
]

export function Layout() {
  const { t, i18n } = useTranslation()
  const { me, logout } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const site = useSite()
  const brand = site.data?.theme?.portal_title || site.data?.name || 'Captain'
  const active = sections.find((section) => section.paths.includes(loc.pathname))
  const secondary = active?.key === 'billing'
    ? [{ to: '/plans', label: t('portalNav.plans') }, { to: '/orders', label: t('nav.orders') }]
    : active?.key === 'help'
      ? [{ to: '/help', label: t('help.title') }, { to: '/tickets', label: t('nav.tickets') }, { to: '/status', label: t('serviceStatus.title') }]
      : active?.key === 'account'
        ? [{ to: '/account', label: t('portalNav.account') }, { to: '/invite', label: t('nav.invite') }]
        : []
  const pickLanguage = (code: string) => {
    i18n.changeLanguage(code)
    api.put('/api/portal/me/lang', { lang: code }).catch(() => { /* Keep the local language if saving the preference fails. */ })
  }
  useEffect(() => {
    // Preserve links to the device section's former location.
    if (loc.pathname === '/' && loc.hash === '#wireguard-devices') {
      nav('/connect#wireguard-devices', { replace: true })
      return
    }
    if (!loc.hash) window.scrollTo({ top: 0, behavior: 'instant' })
  }, [loc.pathname, loc.hash, nav])
  const links = sections.map(({ to, key, icon: Icon }) => (
    <Link key={key} to={to} className={styles.navLink} data-active={active?.key === key || undefined} aria-current={active?.key === key ? 'page' : undefined}>
      <Icon size={19} stroke={1.8} aria-hidden /><span>{t(`portalNav.${key}`)}</span>
    </Link>
  ))
  return (
    <div className={styles.shell}>
      <a className={styles.skipLink} href="#portal-main">{t('portalNav.skip')}</a>
      <header className={styles.header}>
        <Container size={1200} className={styles.headerInner}>
          <Link to="/" className={styles.brand} aria-label={brand}>
            <span className={styles.brandMark}><IconSailboat size={25} stroke={1.7} aria-hidden /></span>
            <Text fw={750} fz="lg" className={styles.brandName}>{brand}</Text>
          </Link>
          <nav className={styles.desktopNav} aria-label={t('portalNav.navigation')}>{links}</nav>
          <Group gap={6} wrap="nowrap">
            {site.data?.probe_url && <ActionIcon className={styles.statusLink} variant="subtle" color="gray" component="a" href={site.data.probe_url} target="_blank" rel="noreferrer" aria-label={t('serviceStatus.serverMonitoring')}><IconActivity size={20} /></ActionIcon>}
            <Menu shadow="md" position="bottom-end">
              <Menu.Target><ActionIcon variant="subtle" color="gray" size="lg" aria-label={t('portalNav.language')}><IconLanguage size={20} /></ActionIcon></Menu.Target>
              <Menu.Dropdown>{LANGS.map(([code, label]) => <Menu.Item key={code} onClick={() => pickLanguage(code)}>{label}</Menu.Item>)}</Menu.Dropdown>
            </Menu>
            <Menu shadow="md" position="bottom-end" width={240}>
              <Menu.Target><UnstyledButton aria-label={t('portalNav.account')}><Avatar radius="xl" size={34}>{me?.email.slice(0, 1).toUpperCase()}</Avatar></UnstyledButton></Menu.Target>
              <Menu.Dropdown>
                <Menu.Label style={{ overflowWrap: 'anywhere' }}>{me?.email}</Menu.Label>
                <Menu.Item component={Link} to="/account" leftSection={<IconUser size={16} />}>{t('portalNav.account')}</Menu.Item>
                {site.data?.probe_url && <Menu.Item component="a" href={site.data.probe_url} target="_blank" rel="noreferrer" leftSection={<IconActivity size={16} />} rightSection={<IconArrowUpRight size={14} />}>{t('serviceStatus.serverMonitoring')}</Menu.Item>}
                <Menu.Item component={Link} to="/status" leftSection={<IconActivity size={16} />}>{t('serviceStatus.title')}</Menu.Item>
                <Menu.Divider />
                <Menu.Item color="red" leftSection={<IconLogout size={16} />} onClick={async () => { await logout(); nav('/login') }}>{t('nav.logout')}</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Container>
      </header>
      <main id="portal-main" className={styles.main}>
        <Container size={1200}>
          {secondary.length > 0 && <nav className={styles.secondaryNav} aria-label={t('portalNav.sectionNavigation')}>
            {secondary.map((item) => <Link key={item.to} to={item.to} data-active={loc.pathname === item.to || undefined} aria-current={loc.pathname === item.to ? 'page' : undefined}>{item.label}</Link>)}
          </nav>}
          <Outlet />
          <footer className={styles.footer}><span>{brand}</span><Link to="/help">{t('portalNav.help')}</Link></footer>
        </Container>
      </main>
      <nav className={styles.mobileNav} aria-label={t('portalNav.navigation')}>{links}</nav>
    </div>
  )
}
