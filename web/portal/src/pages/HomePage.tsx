import { Alert, Badge, Button, Card, Group, Progress, SimpleGrid, Stack, Text, Title, Menu } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { IconCheck, IconCopy, IconDownload } from '@tabler/icons-react'
import { QRCodeSVG } from 'qrcode.react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { copyText } from '../lib/clipboard'
import { useAuth } from '../lib/auth'
import { bytes, money, when } from '../lib/format'
import { RedeemCard } from '../components/RedeemCard'
import { TelegramCard } from '../components/TelegramCard'

// Deep links understood by the common clients.
function importLinks(url: string) {
  const enc = encodeURIComponent(url)
  return [
    { name: 'Clash / mihomo', href: `clash://install-config?url=${enc}` },
    { name: 'sing-box', href: `sing-box://import-remote-profile?url=${enc}` },
    { name: 'Shadowrocket', href: `shadowrocket://add/sub://${btoa(url)}?remark=Captain` },
    { name: 'Surge', href: `surge:///install-config?url=${enc}` },
  ]
}

export default function HomePage() {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const qc = useQueryClient()
  const notice = useQuery({ queryKey: ['notice'], queryFn: () => api.get<{ enabled: boolean; title?: string; body?: string }>('/api/portal/notice') })
  const providers = useQuery({ queryKey: ['oauth-providers'], queryFn: () => api.get<{ providers: { id: string; name: string }[] }>('/api/oauth/providers') })
  const identities = useQuery({ queryKey: ['identities'], queryFn: () => api.get<{ provider: string; email: string }[]>('/api/oauth/identities') })
  const unlink = useMutation({ mutationFn: (p: string) => api.del(`/api/oauth/identities/${p}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['identities'] }) })
  const { me } = useAuth()
  if (!me) return null
  const sub = me.subscription
  const daysLeft = sub?.expires_at ? Math.max(0, Math.ceil((new Date(sub.expires_at).getTime() - Date.now()) / 86400000)) : null
  return (
    <Stack gap="lg">
      {notice.data?.enabled && <Alert color="blue" title={notice.data.title}><Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>{notice.data.body}</Text></Alert>}
      <Title order={2}>{t('home.hello', { email: me.email })}</Title>

      {!sub && (
        <Card>
          <Title order={4}>{t('home.noPlan')}</Title>
          <Text c="dimmed" mb="md">{t('home.noPlanHint')}</Text>
          <Button component={Link} to="/plans">{t('home.browsePlans')}</Button>
        </Card>
      )}
      {sub && !sub.usable && (
        <Card style={{ borderColor: 'var(--mantine-color-orange-4)' }}>
          <Group justify="space-between"><div><Title order={4}>{t('home.expired')}</Title><Text c="dimmed">{t('home.expiredHint')}</Text></div><Button component={Link} to="/plans" color="orange">{t('home.renew')}</Button></Group>
        </Card>
      )}
      {sub && (
        <SimpleGrid cols={{ base: 1, xs: 3 }}>
          <Card>
            <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.usage')}</Text>
            <Text fz="xl" fw={700} mt={4}>{bytes(sub.used_bytes)}{sub.quota_bytes ? <Text span c="dimmed" fz="sm"> / {bytes(sub.quota_bytes)}</Text> : null}</Text>
            {sub.quota_bytes ? <Progress value={Math.min(100, (sub.used_bytes / sub.quota_bytes) * 100)} mt="sm" /> : <Text size="sm" c="dimmed">{t('home.unlimited')}</Text>}
            {sub.reset_at && <Text size="xs" c="dimmed" mt={6}>{t('home.resetsOn', { date: when(sub.reset_at).split(',')[0] })}</Text>}
          </Card>
          <Card>
            <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.expires')}</Text>
            <Text fz="xl" fw={700} mt={4}>{sub.expires_at ? when(sub.expires_at).split(',')[0] : t('home.never')}</Text>
            {daysLeft !== null && <Badge mt="sm" color={daysLeft > 7 ? 'teal' : 'orange'}>{t('home.daysLeft', { count: daysLeft })}</Badge>}
            <Text size="xs" c="dimmed" mt={6}>{t('home.devices', { count: sub.online_devices })}</Text>
          </Card>
          <Card>
            <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.balance')}</Text>
            <Text fz="xl" fw={700} mt={4}>{money(me.balance_cents)}</Text>
          </Card>
        </SimpleGrid>
      )}

      <Card>
        <Title order={4}>{t('home.subscribe')}</Title>
        <Text c="dimmed" size="sm" mb="md">{t('home.subscribeHint')}</Text>
        <Group align="flex-start" wrap="nowrap" gap="lg">
          <Stack flex={1} gap="sm">
            <Text size="sm" style={{ wordBreak: 'break-all' }} ff="monospace" p="sm" bg="var(--mantine-color-gray-1)">{me.subscription_url}</Text>
            <Group>
              <Button leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />} color={copied ? 'teal' : undefined} onClick={async () => { if (await copyText(me.subscription_url)) { setCopied(true); window.setTimeout(() => setCopied(false), 1500) } }}>{copied ? t('home.copied') : t('home.copy')}</Button>
              <Menu shadow="md" width={200}>
                <Menu.Target><Button variant="light" leftSection={<IconDownload size={16} />}>{t('home.import')}</Button></Menu.Target>
                <Menu.Dropdown>{importLinks(me.subscription_url).map((l) => <Menu.Item key={l.name} component="a" href={l.href}>{l.name}</Menu.Item>)}</Menu.Dropdown>
              </Menu>
            </Group>
          </Stack>
          <Stack align="center" gap={4} visibleFrom="xs">
            <QRCodeSVG value={me.subscription_url} size={112} />
            <Text size="xs" c="dimmed">{t('home.qr')}</Text>
          </Stack>
        </Group>
      </Card>
      <RedeemCard />
      <TelegramCard />
      {(providers.data?.providers ?? []).length > 0 && (
        <Card mt="lg">
          <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.logins')}</Text>
          <Stack gap="xs" mt="sm">
            {providers.data!.providers.map((p) => {
              const linked = identities.data?.find((i) => i.provider === p.id)
              return (
                <Group key={p.id} justify="space-between">
                  <Text size="sm">{p.name}{linked && <Text span c="dimmed"> · {linked.email}</Text>}</Text>
                  {linked
                    ? <Button size="xs" variant="subtle" color="red" onClick={() => unlink.mutate(p.id)}>{t('home.unlink')}</Button>
                    : <Button size="xs" variant="light" component="a" href={`/api/oauth/${p.id}/start?link=1&next=/portal/`}>{t('home.link')}</Button>}
                </Group>
              )
            })}
          </Stack>
        </Card>
      )}
    </Stack>
  )
}
