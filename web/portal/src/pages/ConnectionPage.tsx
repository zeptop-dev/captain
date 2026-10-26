import { Accordion, Alert, Anchor, Button, Card, Center, Group, Modal, Select, Skeleton, Stack, Text, Title, UnstyledButton } from '@mantine/core'
import { IconBook2, IconBrandAndroid, IconBrandApple, IconBrandWindows, IconCheck, IconCopy, IconDeviceDesktop, IconDeviceMobile, IconDownload, IconNetwork, IconQrcode, IconWorld } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'
import { QRCodeSVG } from 'qrcode.react'
import { api, type Server, type WireGuardDevicesResponse } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useSite } from '../lib/theme'
import { copyText } from '../lib/clipboard'
import { detectDevicePlatform, importClients, subscriptionImportURL, type ClientDownload, type DevicePlatform, type ImportClient } from '../lib/connection'
import { toast } from '../lib/notify'
import { WireGuardDevices } from '../components/WireGuardDevices'
import { ClientCatalog, LegacyDownloads } from '../components/ClientCatalog'
import type { PublishedClient } from '../../../shared/client-downloads'
import { ServerList } from './ServersPage'
import classes from './ConnectionPage.module.css'

const platforms = [
  { value: 'windows', icon: IconBrandWindows }, { value: 'macos', icon: IconBrandApple },
  { value: 'ios', icon: IconDeviceMobile }, { value: 'android', icon: IconBrandAndroid },
  { value: 'linux', icon: IconDeviceDesktop }, { value: 'other', icon: IconWorld },
] as const

export default function ConnectionPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const site = useSite()
  const location = useLocation()
  const [platform, setPlatform] = useState<DevicePlatform>(() => detectDevicePlatform(navigator.userAgent, navigator.maxTouchPoints))
  const [client, setClient] = useState<ImportClient>(() => platform === 'ios' ? 'shadowrocket' : platform === 'other' ? 'manual' : 'clash')
  const [qrOpened, setQrOpened] = useState(false)
  const downloads = useQuery({ queryKey: ['clients'], queryFn: () => api.get<ClientDownload[]>('/api/portal/clients'), meta: { handlesError: true } })
  const catalog = useQuery({ queryKey: ['client-catalog', 'portal'], queryFn: () => api.get<PublishedClient[]>('/api/portal/client-catalog'), meta: { handlesError: true } })
  // Observe the children’s initial requests without starting another fetch.
  // Their final heights must be present before positioning a deep link.
  const nodes = useQuery({ queryKey: ['servers'], queryFn: () => api.get<Server[]>('/api/portal/servers'), enabled: false })
  const wireguard = useQuery({ queryKey: ['wireguard-devices', '/api/portal/me/wireguard-devices'], queryFn: () => api.get<WireGuardDevicesResponse>('/api/portal/me/wireguard-devices'), enabled: false })
  const positionedNavigation = useRef<string | null>(null)
  const selectedDownloads = (downloads.data ?? []).filter(download => download.platform === platform)
  const selectedCatalog = (catalog.data ?? []).filter(item => item.platform === platform)
  const clients = importClients.filter(choice => choice.platforms.includes(platform))
  const selectedClient = importClients.find(choice => choice.value === client)
  const subscriptionURL = me?.subscription_url ?? ''
  const importURL = subscriptionImportURL(subscriptionURL, client, site.data?.name)
  const changePlatform = (value: DevicePlatform) => {
    setPlatform(value)
    setClient(value === 'ios' ? 'shadowrocket' : value === 'other' ? 'manual' : 'clash')
  }
  const copy = async () => { if (await copyText(subscriptionURL)) toast.ok(t('home.copied')) }
  useEffect(() => {
    const target = location.hash === '#nodes' ? 'nodes' : (location.hash === '#wireguard' || location.hash === '#wireguard-devices') ? 'wireguard' : null
    if (!target || downloads.isPending || catalog.isPending || nodes.isPending || (target === 'wireguard' && wireguard.isPending)) return
    const navigation = `${location.key}:${location.hash}`
    if (positionedNavigation.current === navigation) return
    const frame = window.requestAnimationFrame(() => {
      document.getElementById(target)?.scrollIntoView({ block: 'start' })
      positionedNavigation.current = navigation
    })
    return () => window.cancelAnimationFrame(frame)
  }, [location.hash, location.key, downloads.isPending, catalog.isPending, nodes.isPending, wireguard.isPending])

  return <Stack gap="xl">
    <Group justify="space-between" align="flex-start" className={classes.welcome}>
      <div>
        <Text className={classes.eyebrow}>{t('portalNav.connect')}</Text>
        <Title order={1} className={classes.heading}>{t('connection.title')}</Title>
        <Text size="sm" c="dimmed" mt={10}>{t('connection.subtitle')}</Text>
      </div>
      <Group gap="xs"><Button variant="subtle" component={Link} to="/status">{t('serviceStatus.title')}</Button><Button variant="default" component={Link} to="/help" leftSection={<IconBook2 size={17} />}>{t('connection.guide')}</Button></Group>
    </Group>

    {me && !(me.subscriptions ?? []).some(subscription => subscription.usable) && <Alert color="orange"><Group justify="space-between" gap="sm"><Text size="sm" style={{ flex: '1 1 240px' }}>{t('connection.planNeeded')}</Text><Button component={Link} to="/plans" variant="light" size="xs" color="orange">{t('home.browsePlans')}</Button></Group></Alert>}
    <Card className={classes.setup}>
      <div className={classes.setupColumns}>
        <div className={classes.deviceColumn}>
          <div>
            <Group gap="xs" mb="md"><Text className={classes.stepLabel}>01</Text><Title order={3} fz="lg">{t('connection.chooseDevice')}</Title></Group>
            <div className={classes.platforms} role="group" aria-label={t('connection.chooseDevice')}>
              {platforms.map(({ value, icon: Icon }) => <UnstyledButton key={value} className={classes.platform} aria-pressed={platform === value} onClick={() => changePlatform(value)}>
                <Icon size={23} stroke={1.6} aria-hidden />
                <Text size="sm" fw={600}>{t(`help.platform.${value}`)}</Text>
              </UnstyledButton>)}
            </div>
          </div>
          <Stack className={classes.installPanel} gap="sm" style={{ minWidth: 0 }}>
            <Group gap="xs"><Text className={classes.stepLabel}>02</Text><Title order={3} fz="lg">{t('connection.install')}</Title></Group>
            <Text size="sm" c="dimmed">{t('connection.installHint')}</Text>
            {(downloads.isPending || catalog.isPending) && <Stack gap="sm"><Skeleton height={56} /><Skeleton height={56} /></Stack>}
            {(downloads.isError || catalog.isError) && <Alert color="red" title={t('connection.downloadError')}><Button variant="subtle" size="compact-sm" onClick={() => { void downloads.refetch(); void catalog.refetch() }}>{t('connection.retry')}</Button></Alert>}
            {downloads.isSuccess && catalog.isSuccess && !selectedDownloads.length && !selectedCatalog.length && <Stack gap="xs" py="md">
              <Text size="sm">{t('connection.noDownloads')}</Text>
              <Anchor component={Link} to="/help?tab=downloads" size="sm">{t('connection.allDownloads')}</Anchor>
            </Stack>}
            {selectedCatalog.length > 0 && <ClientCatalog clients={selectedCatalog} platform={platform} compact />}
            <LegacyDownloads items={selectedDownloads} />
            <Text size="xs" c="dimmed">{t('connection.architectureHint')}</Text>
          </Stack>
        </div>
        <Stack className={classes.importPanel} gap="sm">
          <Group gap="xs"><Text className={classes.stepLabel}>03</Text><Title order={3} fz="lg">{t('connection.importTitle')}</Title></Group>
          <Text size="sm" c="dimmed">{t('connection.importHint')}</Text>
          <Select label={t('connection.installedClient')} value={client} onChange={value => value && setClient(value as ImportClient)} data={[...clients.map(choice => ({ value: choice.value, label: choice.label })), { value: 'manual', label: t('connection.otherClient') }]} allowDeselect={false} />
          {importURL ? <Button mt={4} size="md" component="a" href={importURL} leftSection={<IconDownload size={18} />}>{t('connection.importInto', { name: selectedClient?.label })}</Button> : <Button mt={4} size="md" disabled={!subscriptionURL} onClick={copy} leftSection={<IconCopy size={18} />}>{t('home.copy')}</Button>}
          <Group grow gap="sm">{importURL && <Button variant="default" disabled={!subscriptionURL} onClick={copy} leftSection={<IconCopy size={16} />}>{t('home.copy')}</Button>}<Button variant="default" disabled={!subscriptionURL} onClick={() => setQrOpened(true)} leftSection={<IconQrcode size={16} />}>{t('connection.qr')}</Button></Group>
          <Text size="xs" c="dimmed">{t('connection.importNote')}</Text>
        </Stack>
      </div>
      <Group className={classes.finish} gap="sm" align="flex-start" wrap="nowrap">
        <IconCheck size={20} style={{ flexShrink: 0, marginTop: 2 }} aria-hidden />
        <div><Text size="sm" fw={600}>{t('connection.finishTitle')}</Text><Text size="sm" mt={3}>{t('connection.finishHint')}</Text></div>
      </Group>
      <Accordion variant="default" className={classes.manual}>
        <Accordion.Item value="manual"><Accordion.Control icon={<IconCopy size={17} />}>{t('connection.manual')}</Accordion.Control><Accordion.Panel>
          <Text size="sm" c="dimmed" mb="sm">{t('home.subscribeHint')}</Text>
          <div className={classes.subscription}>{subscriptionURL}</div>
          <Button mt="sm" variant="light" disabled={!subscriptionURL} onClick={copy}>{t('home.copy')}</Button>
        </Accordion.Panel></Accordion.Item>
      </Accordion>
    </Card>

    <section id="nodes" className={classes.nodes}>
      <Group gap="xs" mb="md"><IconNetwork size={20} aria-hidden /><Title order={2} fz="xl">{t('servers.title')}</Title></Group>
      <ServerList />
    </section>
    <div id="wireguard" className={classes.nodes}><WireGuardDevices endpoint="/api/portal/me/wireguard-devices" /></div>

    <Modal opened={qrOpened} onClose={() => setQrOpened(false)} title={t('connection.qr')} centered size="sm">
      <Stack align="center" gap="md"><Center className={classes.qr}><QRCodeSVG value={subscriptionURL} size={200} marginSize={1} /></Center><Text size="sm" c="dimmed" ta="center">{t('connection.qrHint')}</Text><Button fullWidth onClick={copy}>{t('home.copy')}</Button></Stack>
    </Modal>
  </Stack>
}
