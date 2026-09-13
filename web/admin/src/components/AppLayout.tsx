import { AppShell, Badge, Box, Burger, Group, NavLink, Stack, Text, UnstyledButton, Menu, ActionIcon } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconLayoutDashboard, IconServer, IconRoute, IconUsers, IconPackage, IconReceipt, IconSettings, IconLogout, IconLanguage, IconWorld, IconTicket, IconMessages, IconGift, IconBook } from '@tabler/icons-react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useQuery } from '@tanstack/react-query'
import { api, type SystemUpdate } from '../lib/api'
import { Indicator } from '@mantine/core'

const sections = [
  { key: 'workspace', items: [
    { to: '/', key: 'dashboard', icon: IconLayoutDashboard },
    { to: '/nodes', key: 'nodes', icon: IconServer },
    { to: '/entries', key: 'entries', icon: IconRoute },
    { to: '/users', key: 'users', icon: IconUsers },
  ] },
  { key: 'business', items: [
    { to: '/plans', key: 'plans', icon: IconPackage },
    { to: '/orders', key: 'orders', icon: IconReceipt },
    { to: '/coupons', key: 'coupons', icon: IconTicket },
    { to: '/gifts', key: 'gifts', icon: IconGift },
    { to: '/tickets', key: 'tickets', icon: IconMessages },
    { to: '/articles', key: 'articles', icon: IconBook },
  ] },
  { key: 'system', items: [
    { to: '/site', key: 'site', icon: IconWorld },
    { to: '/settings', key: 'settings', icon: IconSettings },
  ] },
]

export function AppLayout() {
  const [opened, { toggle, close }] = useDisclosure()
  const { t, i18n } = useTranslation()
  const { me, logout } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const active = (to: string) => (to === '/' ? loc.pathname === '/' : loc.pathname.startsWith(to))
  const upd = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), staleTime: 10 * 60_000, refetchInterval: 30 * 60_000, retry: false })

  return (
    <AppShell navbar={{ width: 240, breakpoint: 'sm', collapsed: { mobile: !opened } }} header={{ height: 52 }} padding="lg">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="sm">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <Text fw={700} size="lg" style={{ letterSpacing: '0.02em' }}>Captain</Text>
            {me?.version && <Indicator disabled={!upd.data?.captain?.has_update} color="red" size={8} offset={2} processing><Badge size="xs" variant="outline" color="gray" style={{ cursor: 'pointer' }} onClick={() => nav('/settings')} title={upd.data?.captain?.has_update ? t('update.available', { version: upd.data.captain.latest }) : undefined}>{me.version}</Badge></Indicator>}
          </Group>
          <Group gap="xs">
            <Menu shadow="md">
              <Menu.Target><ActionIcon variant="subtle" color="gray" aria-label="language"><IconLanguage size={18} /></ActionIcon></Menu.Target>
              <Menu.Dropdown>
                <Menu.Item onClick={() => i18n.changeLanguage('zh-CN')}>中文</Menu.Item>
                <Menu.Item onClick={() => i18n.changeLanguage('en')}>English</Menu.Item>
              </Menu.Dropdown>
            </Menu>
            <Text size="sm" c="dimmed">{me?.email}</Text>
            <ActionIcon variant="subtle" color="gray" aria-label="logout" onClick={async () => { await logout(); nav('/login') }}><IconLogout size={18} /></ActionIcon>
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="sm">
        <Stack gap="lg">
          {sections.map((s) => (
            <Box key={s.key}>
              <Text size="xs" tt="uppercase" c="dimmed" fw={600} px="sm" mb={4} style={{ letterSpacing: '0.08em' }}>{t(`nav.${s.key}`)}</Text>
              {s.items.map((it) => (
                <NavLink key={it.to} component={UnstyledButton} label={t(`nav.${it.key}`)} leftSection={<it.icon size={18} stroke={1.6} />}
                  active={active(it.to)} onClick={() => { nav(it.to); close() }} style={{ borderRadius: 8 }} />
              ))}
            </Box>
          ))}
        </Stack>
      </AppShell.Navbar>
      <AppShell.Main><Outlet /></AppShell.Main>
    </AppShell>
  )
}
