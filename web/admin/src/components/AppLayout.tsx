import { ActionIcon, AppShell, Avatar, Badge, Box, Burger, Divider, Group, Indicator, Menu, NavLink, ScrollArea, Stack, Text, ThemeIcon, UnstyledButton, useMantineColorScheme } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconLayoutDashboard, IconServer, IconRoute, IconUsers, IconPackage, IconReceipt, IconSettings, IconLogout, IconLanguage, IconWorld, IconTicket, IconMessages, IconGift, IconBook, IconCashBanknote, IconUserShield, IconFileCode, IconCloudDownload, IconGauge, IconCertificate, IconShip, IconSun, IconMoon, IconDotsVertical, IconChevronDown } from '@tabler/icons-react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useQuery } from '@tanstack/react-query'
import { api, type SystemUpdate } from '../lib/api'
import { pageBackground } from '../theme'

const languages = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
  { code: 'ru', label: 'Русский' },
]

// Sidebar groups separated by hairlines, like a hosting console.
const sections = [
  { key: 'workspace', items: [
    { to: '/', key: 'dashboard', icon: IconLayoutDashboard },
    { to: '/nodes', key: 'nodes', icon: IconServer },
    { to: '/entries', key: 'entries', icon: IconRoute },
    { to: '/external', key: 'external', icon: IconCloudDownload },
    { to: '/speedtest', key: 'speedtest', icon: IconGauge },
    { to: '/users', key: 'users', icon: IconUsers },
  ] },
  { key: 'business', items: [
    { to: '/plans', key: 'plans', icon: IconPackage },
    { to: '/orders', key: 'orders', icon: IconReceipt },
    { to: '/coupons', key: 'coupons', icon: IconTicket },
    { to: '/gifts', key: 'gifts', icon: IconGift },
    { to: '/withdrawals', key: 'withdrawals', icon: IconCashBanknote },
    { to: '/tickets', key: 'tickets', icon: IconMessages },
    { to: '/articles', key: 'articles', icon: IconBook },
  ] },
  { key: 'system', items: [
    { to: '/admins', key: 'admins', icon: IconUserShield },
    { to: '/domains', key: 'domains', icon: IconCertificate },
    { to: '/site', key: 'site', icon: IconWorld },
    { to: '/sub-templates', key: 'subTemplates', icon: IconFileCode },
    { to: '/settings', key: 'settings', icon: IconSettings },
  ] },
]

export function Brand({ name, size = 'md' }: { name: string; size?: 'md' | 'lg' }) {
  return (
    <Group gap="xs" wrap="nowrap">
      <ThemeIcon size={size === 'lg' ? 40 : 30} radius="md" variant="filled"><IconShip size={size === 'lg' ? 24 : 18} stroke={2} /></ThemeIcon>
      <Text fw={700} size={size === 'lg' ? 'xl' : 'lg'} style={{ letterSpacing: '-0.01em' }}>{name}</Text>
    </Group>
  )
}

export function AppLayout() {
  const [opened, { toggle, close }] = useDisclosure()
  const { t, i18n } = useTranslation()
  const { me, logout } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const { colorScheme, setColorScheme } = useMantineColorScheme()
  const active = (to: string) => (to === '/' ? loc.pathname === '/' : loc.pathname.startsWith(to))
  // Support sees tickets, users and orders; operators everything but the system section.
  const role = me?.role ?? 'admin'
  const visible = (to: string) => role === 'admin' ? true : role === 'operator' ? !['/site', '/settings', '/admins', '/sub-templates'].includes(to) : ['/', '/tickets', '/users', '/orders'].includes(to)
  const shown = sections.map((s) => ({ ...s, items: s.items.filter((it) => visible(it.to)) })).filter((s) => s.items.length > 0)
  const upd = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), staleTime: 10 * 60_000, refetchInterval: 30 * 60_000, retry: false })
  const lang = languages.find((l) => l.code === i18n.language) ?? languages[0]
  const dark = colorScheme === 'dark'

  return (
    <AppShell navbar={{ width: 248, breakpoint: 'sm', collapsed: { mobile: !opened } }} header={{ height: 56 }} padding="lg" styles={{ main: { background: pageBackground } }}>
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <UnstyledButton onClick={() => { nav('/'); close() }}><Brand name="Captain" /></UnstyledButton>
          </Group>
          <Group gap="xs" wrap="nowrap">
            {me?.version && (
              <Indicator disabled={!upd.data?.captain?.has_update} color="red" size={8} offset={2} processing styles={{ root: { display: 'flex' } }}>
                <Badge variant="light" color="gray" style={{ cursor: 'pointer' }} onClick={() => nav('/settings')} title={upd.data?.captain?.has_update ? t('update.available', { version: upd.data.captain.latest }) : undefined}>{me.version}</Badge>
              </Indicator>
            )}
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar>
        <AppShell.Section grow component={ScrollArea} type="auto" scrollbarSize={6} px="sm" py="sm">
          <Stack gap={0}>
            {shown.map((s, i) => (
              <Box key={s.key}>
                {i > 0 && <Divider my="xs" />}
                {s.items.map((it) => (
                  <NavLink key={it.to} component={UnstyledButton} label={t(`nav.${it.key}`)} leftSection={<it.icon size={18} stroke={1.7} />}
                    variant="light" active={active(it.to)} onClick={(e) => { e.currentTarget.blur(); nav(it.to); close() }}
                    styles={{ root: { borderRadius: 8, marginBottom: 2 }, label: { fontWeight: 500 } }} />
                ))}
              </Box>
            ))}
          </Stack>
        </AppShell.Section>
        <AppShell.Section p="sm" style={{ borderTop: '1px solid var(--mantine-color-default-border)' }}>
          <Group justify="space-between" mb="sm" px={4}>
            <Menu shadow="md" width={160}>
              <Menu.Target>
                <UnstyledButton aria-label="language">
                  <Group gap={6}><IconLanguage size={16} stroke={1.7} /><Text size="sm" fw={500}>{lang.label}</Text><IconChevronDown size={14} opacity={0.6} /></Group>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown>
                {languages.map((l) => <Menu.Item key={l.code} fw={i18n.language === l.code ? 700 : undefined} onClick={() => i18n.changeLanguage(l.code)}>{l.label}</Menu.Item>)}
              </Menu.Dropdown>
            </Menu>
            <ActionIcon variant="subtle" color="gray" aria-label="color scheme" onClick={() => setColorScheme(dark ? 'light' : 'dark')}>{dark ? <IconSun size={18} /> : <IconMoon size={18} />}</ActionIcon>
          </Group>
          <Group gap="sm" wrap="nowrap" p="xs" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 12 }}>
            <Avatar radius="xl" color="brand" variant="light">{(me?.email ?? '?').slice(0, 1).toUpperCase()}</Avatar>
            <Box style={{ flex: 1, minWidth: 0 }}>
              <Text size="sm" fw={600} truncate>{me?.email}</Text>
              <Text size="xs" c="dimmed" truncate>{me ? t(`admins.roles.${me.role}`, { defaultValue: me.role }) : ''}</Text>
            </Box>
            <Menu shadow="md" position="top-end">
              <Menu.Target><ActionIcon variant="subtle" color="gray" aria-label="account menu"><IconDotsVertical size={18} /></ActionIcon></Menu.Target>
              <Menu.Dropdown>
                {role === 'admin' && <Menu.Item leftSection={<IconSettings size={16} />} onClick={() => { nav('/settings'); close() }}>{t('nav.settings')}</Menu.Item>}
                <Menu.Item leftSection={<IconLogout size={16} />} color="red" onClick={async () => { await logout(); nav('/login') }}>{t('app.logout')}</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main><Outlet /></AppShell.Main>
    </AppShell>
  )
}
