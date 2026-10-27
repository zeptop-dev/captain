import { Alert, Anchor, Box, Button, Center, Group, Loader, Paper, Stack, Text, TextInput, Title } from '@mantine/core'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { IconArrowRight, IconBell, IconBrandAbstract, IconBuildingStore, IconCloud, IconSearch, IconServer, IconShieldLock, IconTools, IconWorld } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useRef, useState, type ComponentType } from 'react'
import { Link, Navigate, useLocation, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { resolveSettingsRoute, searchSettings, settingsCategories, settingsCatalog, settingsCategoryPath, settingsEntryPath, type SettingsEntry } from '../lib/settings-catalog'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { PageHeader } from '../components/PageHeader'
import classes from './SettingsPage.module.css'

const editors: Record<string, ComponentType> = {
  site: lazy(() => import('./SitePage').then(m => ({ default: () => <m.default embedded /> }))),
  purchase: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.PurchaseCard }))),
  'service-status': lazy(() => import('../components/settings/ServiceStatusCard')),
  subscription: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.SubscriptionCard }))),
  acme: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.ACMECard }))),
  notice: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.NoticeCard }))),
  invite: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.InviteCard }))),
  surplus: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.SurplusCard }))),
  'login-clients': lazy(() => import('../components/settings/LoginClientsCard')),
  oidc: lazy(() => import('../components/settings/GeneralCards').then(m => ({ default: m.OIDCCard }))),
  registration: lazy(() => import('../components/RegistrationCard').then(m => ({ default: m.RegistrationCard }))),
  trial: lazy(() => import('../components/OpsCards').then(m => ({ default: m.TrialCard }))),
  clients: lazy(() => import('../components/settings/ClientsCard')),
  telegram: lazy(() => import('../components/OpsCards').then(m => ({ default: m.TelegramCard }))),
  mail: lazy(() => import('../components/MailCard').then(m => ({ default: m.MailCard }))),
  webhooks: lazy(() => import('../components/WebhooksCard').then(m => ({ default: m.WebhooksCard }))),
  probe: lazy(() => import('../components/ProbeCard').then(m => ({ default: m.ProbeCard }))),
  'ping-tasks': lazy(() => import('../components/ProbeCard').then(m => ({ default: m.PingTasks }))),
  heartbeat: lazy(() => import('../components/HeartbeatCard').then(m => ({ default: m.HeartbeatCard }))),
  connections: lazy(() => import('../components/ConnLogCard').then(m => ({ default: m.ConnLogCard }))),
  audit: lazy(() => import('../components/AuditRulesCard').then(m => ({ default: m.AuditRulesCard }))),
  limits: lazy(() => import('../components/DynLimitCard').then(m => ({ default: m.DynLimitCard }))),
  access: lazy(() => import('../components/SecurityCard').then(m => ({ default: m.SecurityCard }))),
  'admin-log': lazy(() => import('../components/AdminLogCard').then(m => ({ default: m.AdminLogCard }))),
  backup: lazy(() => import('../components/BackupCard').then(m => ({ default: m.BackupCard }))),
  update: lazy(() => import('../components/UpdateCard').then(m => ({ default: m.UpdateCard }))),
  reset: lazy(() => import('../components/ResetSiteCard').then(m => ({ default: m.ResetSiteCard }))),
}

const categoryIcons = { site: IconWorld, account: IconShieldLock, subscription: IconBrandAbstract, business: IconBuildingStore, proxy: IconServer, monitoring: IconCloud, integrations: IconBell, maintenance: IconTools }

function SettingsEditor({ entry }: { entry: SettingsEntry }) {
  const location = useLocation()
  const { t } = useTranslation()
  const section = useRef<HTMLElement>(null)
  const Editor = editors[entry.id]
  const fetching = useIsFetching()
  const queryClient = useQueryClient()
  const positioned = useRef<string | null>(null)
  useEffect(() => {
    if (location.hash !== `#${entry.id}` || fetching || positioned.current === location.key) return
    // Queries above the target may expand after the lazy modules have loaded.
    // Position once per navigation after initial fetches settle, never on polls.
    const timer = setTimeout(() => {
      if (queryClient.isFetching()) return
      positioned.current = location.key
      // Do not steal focus from someone who started editing while data loaded.
      if (document.activeElement?.matches('input, textarea, select, [contenteditable="true"], [contenteditable=""]')) return
      section.current?.scrollIntoView({ block: 'start', behavior: 'instant' })
      section.current?.focus({ preventScroll: true })
    }, 60)
    return () => clearTimeout(timer)
  }, [location.key, location.hash, entry.id, fetching, queryClient])
  return <section id={entry.id} ref={section} tabIndex={-1} className={classes.section} aria-label={t(entry.titleKey)}><Editor /></section>
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const section = useParams()['*'] ?? ''
  const location = useLocation()
  const [searchState, setSearchState] = useState({ scope: location.key, value: '', open: false })
  const search = searchState.scope === location.key ? searchState.value : ''
  const searchOpen = searchState.scope === location.key && searchState.open
  const setSearch = (value: string) => setSearchState(previous => ({ scope: location.key, value, open: previous.scope === location.key && previous.open }))
  const setSearchOpen = (open: boolean) => setSearchState(previous => ({ scope: location.key, value: previous.scope === location.key ? previous.value : '', open }))
  const route = resolveSettingsRoute(section)
  const results = searchSettings(search, key => t(key))
  if (me?.role !== 'admin') return <Alert color="red">{t('workspace.forbidden')}</Alert>
  if (!route) return <Stack><Alert color="yellow">{t('workspace.notFound')}</Alert><Anchor component={Link} to="/settings">{t('nav.settings')}</Anchor></Stack>
  if (route.redirect) return <Navigate replace to={route.redirect} />
  const { category } = route
  const entries = settingsCatalog.filter(entry => entry.category === category)
  return <Box className={classes.root}>
    <PageHeader title={t('nav.settings')} subtitle={t('settingsLayout.hint')} />
    <div className={classes.search} onBlur={e => { if (!e.currentTarget.contains(e.relatedTarget)) setSearchOpen(false) }} onKeyDown={e => { if (e.key === 'Escape') setSearchOpen(false) }}>
      <TextInput aria-label={t('workspace.search')} placeholder={t('settingsLayout.search')} leftSection={<IconSearch size={17} />} value={search} onFocus={() => setSearchOpen(true)} onChange={e => { setSearch(e.currentTarget.value); setSearchOpen(true) }} />
      {searchOpen && search.trim() && <Paper className={classes.results} role="region" aria-label={t('settingsLayout.searchResults')}>
        {results.length ? results.map(entry => <Link className={classes.result} to={settingsEntryPath(entry)} key={entry.id} onClick={() => { setSearchOpen(false); setSearch('') }}><span><Text fw={600} size="sm">{t(entry.titleKey)}</Text><Text size="xs" c="dimmed">{t(`settingsLayout.categories.${entry.category}`)}</Text></span><IconArrowRight size={16} /></Link>) : <Text c="dimmed" p="md" size="sm">{t('workspace.noResults')}</Text>}
      </Paper>}
    </div>
    <nav className={classes.tabs} aria-label={t('settingsLayout.categoriesLabel')}>
      {settingsCategories.map(value => { const Icon = categoryIcons[value]; return <Link key={value} to={settingsCategoryPath(value)} aria-current={category === value ? 'page' : undefined} className={classes.tab}><Icon size={17} /><span>{t(`settingsLayout.categories.${value}`)}</span></Link> })}
    </nav>
    <Group justify="space-between" align="flex-start" mt="lg" mb="md" gap="xs">
      <Box><Title order={3} size="lg">{t(`settingsLayout.categories.${category}`)}</Title><Text size="sm" c="dimmed" mt={4}>{t(`settingsLayout.descriptions.${category}`)}</Text></Box>
      {category === 'account' && <Group gap="sm"><Anchor component={Link} to="/admins" size="sm">{t('nav.admins')}</Anchor><Anchor component={Link} to="/account" size="sm">{t('passkeys.account')}</Anchor></Group>}
      {category === 'site' && <Button component="a" href="/" target="_blank" variant="default" size="xs">{t('site.open')}</Button>}
    </Group>
    <nav className={classes.anchors} aria-label={t('settingsLayout.jumpTo')}>
      {entries.map(entry => <Anchor component={Link} key={entry.id} to={`${settingsCategoryPath(category)}#${entry.id}`} size="xs">{t(entry.titleKey)}</Anchor>)}
    </nav>
    <SettingsDraftBoundary key={category} allowHashNavigation>
      <Stack gap="lg" className={classes.sections}>
        <Suspense fallback={<Center mih={160}><Loader size="sm" /></Center>}>{entries.map(entry => <SettingsEditor key={entry.id} entry={entry} />)}</Suspense>
      </Stack>
    </SettingsDraftBoundary>
  </Box>
}
