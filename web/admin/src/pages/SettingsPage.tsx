import { Alert, Anchor, Box, Button, Card, Center, Group, Loader, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { IconArrowLeft, IconArrowRight, IconSearch } from '@tabler/icons-react'
import { lazy, Suspense, useState, type ComponentType } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { settingsAreas, settingsCatalog, systemAreas, type SettingsArea } from '../lib/settings-catalog'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { PageHeader } from '../components/PageHeader'

const editors: Record<string, ComponentType> = {
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

export default function SettingsPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const section = useParams()['*'] ?? ''
  const [searchState, setSearchState] = useState({ scope: section, value: '' })
  const search = searchState.scope === section ? searchState.value : ''
  const setSearch = (value: string) => setSearchState({ scope: section, value })
  if (me?.role !== 'admin') return <Alert color="red">{t('workspace.forbidden')}</Alert>
  const entry = settingsCatalog.find(e => e.id === section)
  const area = section.startsWith('area/') ? section.slice(5) as SettingsArea : undefined
  if (section && !entry && (!area || !settingsAreas.includes(area))) return <Stack><Alert color="yellow">{t('workspace.notFound')}</Alert><Anchor component={Link} to="/settings">{t('nav.settings')}</Anchor></Stack>
  if (entry) {
    const Editor = editors[entry.id]
    return <Box maw={1100} mx="auto">
      <Group mb="md" justify="space-between"><Button component={Link} to={`/settings/area/${entry.area}`} variant="subtle" leftSection={<IconArrowLeft size={16} />}>{t(`workspace.areas.${entry.area}`)}</Button><Anchor component={Link} to="/settings" size="sm">{t('workspace.allSettings')}</Anchor></Group>
      <PageHeader title={t(entry.titleKey)} subtitle={t(`workspace.areas.${entry.area}`)} />
      <SettingsDraftBoundary key={entry.id}><Suspense fallback={<Center py="xl"><Loader /></Center>}><Editor /></Suspense></SettingsDraftBoundary>
    </Box>
  }
  const term = search.trim().toLocaleLowerCase()
  const areas = area ? [area] : term ? settingsAreas : systemAreas
  const matching = settingsCatalog.filter(e => areas.includes(e.area) && (!term || `${t(e.titleKey)} ${t(e.hintKey)} ${t(`workspace.areas.${e.area}`)} ${e.keywords}`.toLocaleLowerCase().includes(term)))
  return <Box maw={1100} mx="auto">
    <PageHeader title={area ? t(`workspace.areas.${area}`) : t('nav.settings')} subtitle={t('workspace.settingsHint')} />
    <Stack>
      <TextInput aria-label={t('workspace.search')} placeholder={t('workspace.search')} leftSection={<IconSearch size={17} />} value={search} onChange={e => setSearch(e.currentTarget.value)} />
      {(area === 'security' || (!area && !term)) && <Group gap="sm"><Anchor component={Link} to="/admins" size="sm">{t('nav.admins')}</Anchor><Anchor component={Link} to="/account" size="sm">{t('passkeys.account')}</Anchor></Group>}
      {!area && !term && <SimpleGrid cols={{ base: 1, sm: 2 }}>{systemAreas.map(group => <Card component={Link} to={`/settings/area/${group}`} key={group} style={{ textDecoration: 'none', color: 'inherit' }}><Group justify="space-between" wrap="nowrap"><Text fw={600}>{t(`workspace.areas.${group}`)}</Text><IconArrowRight size={17} style={{ flexShrink: 0 }} /></Group><Text size="sm" c="dimmed" mt="xs">{settingsCatalog.filter(e => e.area === group).map(e => t(e.titleKey)).join(' · ')}</Text></Card>)}</SimpleGrid>}
      {(area || term ? areas : []).map(group => {
        const items = matching.filter(e => e.area === group)
        return items.length > 0 && <Stack key={group} gap="sm">{!area && <Title order={4}>{t(`workspace.areas.${group}`)}</Title>}<SimpleGrid cols={{ base: 1, sm: 2 }}>{items.map(item => <Card component={Link} to={`/settings/${item.id}`} key={item.id} style={{ textDecoration: 'none', color: 'inherit' }}>
          <Group justify="space-between" wrap="nowrap"><Text fw={600}>{t(item.titleKey)}</Text><IconArrowRight size={17} style={{ flexShrink: 0 }} /></Group><Text size="sm" c="dimmed" mt="xs" lineClamp={2}>{t(item.hintKey)}</Text>
        </Card>)}</SimpleGrid></Stack>
      })}
      {matching.length === 0 && <Text c="dimmed">{t('workspace.noResults')}</Text>}
      {area === 'business' && !term && <Anchor component={Link} to="/settings/subscription" size="sm">{t('settings.singlePlan')}</Anchor>}
      {!area && !term && <Card><Text fw={600} mb="sm">{t('workspace.businessRules')}</Text><Group>{settingsAreas.filter(a => !systemAreas.includes(a)).map(a => <Button component={Link} to={`/settings/area/${a}`} key={a} size="xs" variant="light">{t(`workspace.areas.${a}`)}</Button>)}</Group></Card>}
      {area && <Anchor component={Link} to="/settings" size="sm">{t('workspace.allSettings')}</Anchor>}
    </Stack>
  </Box>
}
