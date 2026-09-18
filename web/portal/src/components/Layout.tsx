import { AppShell, Container, Group, Text, UnstyledButton, Menu, ActionIcon } from '@mantine/core'
import { IconActivity, IconLanguage, IconLogout } from '@tabler/icons-react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useSite } from '../lib/theme'
import { api } from '../lib/api'

// The interface languages, and the language the account's mail follows.
const LANGS: [string, string][] = [['zh-CN', '中文'], ['zh-TW', '繁體中文'], ['en', 'English'], ['ja', '日本語'], ['ko', '한국어'], ['ru', 'Русский']]

const items = [{ to: '/', key: 'home' }, { to: '/plans', key: 'plans' }, { to: '/orders', key: 'orders' }, { to: '/servers', key: 'servers' }, { to: '/invite', key: 'invite' }, { to: '/tickets', key: 'tickets' }, { to: '/help', key: 'help' }]

// Top navigation only; the portal is a handful of pages and must work on phones.
export function Layout() {
  const { t, i18n } = useTranslation()
  const { logout } = useAuth()
  const nav = useNavigate()
  const loc = useLocation()
  const site = useSite()
  const brand = site.data?.theme?.portal_title || site.data?.name || 'Captain'
  // Switching the interface language also tells the panel, so the account's
  // mail arrives in the same language. A signed-out visitor just switches.
  const pickLanguage = (code: string) => {
    i18n.changeLanguage(code)
    api.put('/api/portal/me/lang', { lang: code }).catch(() => { /* not signed in, or offline: the interface still switched */ })
  }
  return (
    <AppShell header={{ height: 56 }} padding="md" styles={{ main: { background: 'var(--mantine-color-gray-0)' } }}>
      <AppShell.Header>
        <Container size="sm" h="100%">
          <Group h="100%" justify="space-between">
            <Group gap="lg">
              <Text fw={800} size="lg">{brand}</Text>
              <Group gap="xs" visibleFrom="xs">
                {items.map((it) => {
                  const active = it.to === '/' ? loc.pathname === '/' : loc.pathname.startsWith(it.to)
                  return <UnstyledButton key={it.to} onClick={() => nav(it.to)} px="sm" py={6} style={{ borderRadius: 8, fontWeight: active ? 700 : 500, background: active ? 'var(--mantine-color-cyan-0)' : undefined, color: active ? 'var(--mantine-color-cyan-8)' : undefined }}>{t(`nav.${it.key}`)}</UnstyledButton>
                })}
              </Group>
            </Group>
            <Group gap={4}>
              {site.data?.probe_url && <ActionIcon variant="subtle" color="gray" component="a" href={site.data.probe_url} target="_blank" aria-label={t('nav.status')}><IconActivity size={18} /></ActionIcon>}
              <Menu shadow="md"><Menu.Target><ActionIcon variant="subtle" color="gray"><IconLanguage size={18} /></ActionIcon></Menu.Target>
                <Menu.Dropdown>{LANGS.map(([code, label]) => <Menu.Item key={code} onClick={() => pickLanguage(code)}>{label}</Menu.Item>)}</Menu.Dropdown></Menu>
              <ActionIcon variant="subtle" color="gray" onClick={async () => { await logout(); nav('/login') }} aria-label={t('nav.logout')}><IconLogout size={18} /></ActionIcon>
            </Group>
          </Group>
        </Container>
      </AppShell.Header>
      <AppShell.Main>
        <Container size="sm" py="md"><Outlet /></Container>
        <Group justify="center" gap="xs" hiddenFrom="xs" mt="lg">
          {items.map((it) => <UnstyledButton key={it.to} onClick={() => nav(it.to)} px="sm" py={6} style={{ fontWeight: 600 }}>{t(`nav.${it.key}`)}</UnstyledButton>)}
        </Group>
      </AppShell.Main>
    </AppShell>
  )
}
