import { ActionIcon, AppShell, Avatar, Badge, Box, Burger, Group, Indicator, Menu, NavLink, ScrollArea, Text, ThemeIcon, UnstyledButton, useMantineColorScheme, useComputedColorScheme } from '@mantine/core'
import { useState } from 'react'
import { brandLogo } from '../../../shared/branding'
import { usePublicBrand } from '../lib/branding'
import { useDisclosure } from '@mantine/hooks'
import { IconLayoutDashboard, IconServer, IconUsers, IconSettings, IconLogout, IconLanguage, IconGauge, IconShip, IconSun, IconMoon, IconDotsVertical, IconChevronDown, IconLink, IconReceipt, IconMessage, IconTemplate } from '@tabler/icons-react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useQuery } from '@tanstack/react-query'
import { api, type SystemUpdate } from '../lib/api'
import { pageBackground } from '../theme'
import { visibleNavigation, navigationGroup } from '../lib/navigation'
import { GroupedNavigation } from './GroupedNavigation'
import { WorkspaceNavigation } from './WorkspaceNavigation'

const languages = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
  { code: 'ru', label: 'Русский' },
]

const icons = {
  overview: IconLayoutDashboard, users: IconUsers, nodes: IconServer,
  entries: IconLink, subscription: IconTemplate, monitoring: IconGauge,
  business: IconReceipt, support: IconMessage,
}

export function Brand({ size = 'md' }: { size?: 'md' | 'lg' }) {
  const { data } = usePublicBrand()
  const name = data?.name || 'Captain'
  const scheme = useComputedColorScheme('light')
  const src = brandLogo(data?.branding, scheme)
  const [failed, setFailed] = useState('')
  return (
    <Group gap="xs" wrap="nowrap">
      {src && failed !== src ? <img src={src} alt="" onError={() => setFailed(src)} style={{ maxWidth: size === 'lg' ? 80 : 60, height: size === 'lg' ? 40 : 30, objectFit: 'contain', flexShrink: 0 }} /> : <ThemeIcon size={size === 'lg' ? 40 : 30} radius="md" variant="filled"><IconShip size={size === 'lg' ? 24 : 18} stroke={2} /></ThemeIcon>}
      <Text fw={700} size={size === 'lg' ? 'xl' : 'lg'} style={{ letterSpacing: '-0.01em', overflowWrap: 'anywhere' }}>{name}</Text>
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
  const role = me?.role ?? ''
  const shown = visibleNavigation(role)
  const group = navigationGroup(loc.pathname)
  const upd = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), staleTime: 10 * 60_000, refetchInterval: 30 * 60_000, retry: false, enabled: role === 'admin' })
  const lang = languages.find((l) => l.code === i18n.language) ?? languages[0]
  const dark = colorScheme === 'dark'

  return (
    <AppShell navbar={{ width: 248, breakpoint: 'sm', collapsed: { mobile: !opened } }} header={{ height: 56 }} padding="lg" styles={{ main: { background: pageBackground } }}>
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger aria-label={t('workspace.navigation')} opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <UnstyledButton onClick={() => { nav('/'); close() }}><Brand /></UnstyledButton>
          </Group>
          <Group gap="xs" wrap="nowrap">
            {me?.version && (
              <Indicator disabled={!upd.data?.captain?.has_update} color="red" size={8} offset={2} processing styles={{ root: { display: 'flex' } }}>
                <Badge variant="light" color="gray" style={{ cursor: role === 'admin' ? 'pointer' : undefined }} onClick={role === 'admin' ? () => nav('/settings/category/maintenance#update') : undefined} title={upd.data?.captain?.has_update ? t('update.available', { version: upd.data.captain.latest }) : undefined}>{me.version}</Badge>
              </Indicator>
            )}
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar>
        <AppShell.Section grow component={ScrollArea} type="auto" scrollbarSize={6} px="sm" py="sm">
          <GroupedNavigation key={role} groups={shown} icons={icons} activeGroup={group} onNavigate={close} />
        </AppShell.Section>
        <AppShell.Section p="sm" style={{ borderTop: '1px solid var(--mantine-color-default-border)' }}>
          {role === 'admin' && <NavLink component={Link} to="/settings/category/site" label={t('workspace.systemSettings')} leftSection={<IconSettings size={18} />} active={group === 'system'} aria-current={group === 'system' ? 'page' : undefined} onClick={close} mb="sm" styles={{ root: { borderRadius: 8 } }} />}
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
                <Menu.Item leftSection={<IconSettings size={16} />} onClick={() => { nav('/account'); close() }}>{t('passkeys.account')}</Menu.Item>
                {role === 'admin' && <Menu.Item leftSection={<IconSettings size={16} />} onClick={() => { nav('/settings/category/site'); close() }}>{t('nav.settings')}</Menu.Item>}
                <Menu.Item leftSection={<IconLogout size={16} />} color="red" onClick={async () => { await logout(); nav('/login') }}>{t('app.logout')}</Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main><WorkspaceNavigation /><Outlet /></AppShell.Main>
    </AppShell>
  )
}
