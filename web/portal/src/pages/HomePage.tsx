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
import { toast } from '../lib/notify'
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
  const { me, refresh } = useAuth()
  const cancelQueued = useMutation({ mutationFn: (id: number) => api.del(`/api/portal/subscriptions/${id}`), onSuccess: () => refresh(), onError: toast.err })
  const removeDevice = useMutation({ mutationFn: (hwid: string) => api.del(`/api/portal/me/hwid-devices/${encodeURIComponent(hwid)}`), onSuccess: () => refresh(), onError: toast.err })
  if (!me) return null
  const subs = me.subscriptions ?? []
  const sub = me.subscription
  const anyUsable = subs.some((s) => s.usable)
  const daysOf = (iso: string | null) => (iso ? Math.max(0, Math.ceil((new Date(iso).getTime() - Date.now()) / 86400000)) : null)
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
      {sub && !anyUsable && (
        <Card style={{ borderColor: 'var(--mantine-color-orange-4)' }}>
          <Group justify="space-between"><div><Title order={4}>{t('home.expired')}</Title><Text c="dimmed">{t('home.expiredHint')}</Text></div><Button component={Link} to="/plans" color="orange">{t('home.renew')}</Button></Group>
        </Card>
      )}
      {subs.length > 0 && (
        <SimpleGrid cols={{ base: 1, xs: 2 }}>
          {subs.map((s) => {
            const days = daysOf(s.expires_at)
            const queued = s.status === 'queued'
            return (
              <Card key={s.id} style={queued ? { borderStyle: 'dashed' } : undefined}>
                <Group justify="space-between" align="flex-start" mb="xs">
                  <Text fw={700}>{s.plan_name}</Text>
                  <Badge variant="light" color={queued ? 'gray' : s.usable ? 'teal' : 'orange'}>{queued ? t('home.statusQueued') : s.usable ? t('home.statusActive') : t('home.statusLapsed')}</Badge>
                </Group>
                {queued ? (
                  <>
                    <Text size="sm" c="dimmed">{s.period_days ? t('home.queuedHint', { days: s.period_days }) : t('home.queuedHintForever')}</Text>
                    <Text size="sm" c="dimmed">{s.quota_bytes ? t('home.queuedQuota', { quota: bytes(s.quota_bytes) }) : t('home.unlimited')}</Text>
                    <Group justify="flex-end" mt="sm"><Button size="xs" variant="subtle" color="gray" loading={cancelQueued.isPending} onClick={() => cancelQueued.mutate(s.id)}>{t('home.cancelQueued')}</Button></Group>
                  </>
                ) : (
                  <>
                    <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.usage')}</Text>
                    <Text fz="lg" fw={700}>{bytes(s.used_bytes)}{s.quota_bytes ? <Text span c="dimmed" fz="sm"> / {bytes(s.quota_bytes)}</Text> : null}</Text>
                    {s.quota_bytes ? <Progress value={Math.min(100, (s.used_bytes / s.quota_bytes) * 100)} mt={4} /> : <Text size="sm" c="dimmed">{t('home.unlimited')}</Text>}
                    {s.reset_at && <Text size="xs" c="dimmed" mt={4}>{t('home.resetsOn', { date: when(s.reset_at).split(',')[0] })}</Text>}
                    <Group gap="xs" mt="sm" align="center">
                      <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.expires')}</Text>
                      <Text size="sm" fw={600}>{s.expires_at ? when(s.expires_at).split(',')[0] : t('home.never')}</Text>
                      {days !== null && <Badge size="sm" color={days > 7 ? 'teal' : 'orange'}>{t('home.daysLeft', { count: days })}</Badge>}
                    </Group>
                  </>
                )}
              </Card>
            )
          })}
        </SimpleGrid>
      )}
      {me.hwid_enabled && (
        <Card>
          <Group justify="space-between" align="flex-start">
            <div><Title order={4}>{t('home.devices')}</Title><Text c="dimmed" size="sm">{me.hwid_limit > 0 ? t('home.devicesHint', { n: me.hwid_devices.length, limit: me.hwid_limit }) : t('home.devicesUnlimited', { n: me.hwid_devices.length })}</Text></div>
          </Group>
          {me.hwid_devices.length === 0 ? <Text size="sm" c="dimmed" mt="sm">{t('home.noDevices')}</Text> : (
            <Stack gap={6} mt="sm">
              {me.hwid_devices.map((d) => (
                <Group key={d.hwid} justify="space-between" wrap="nowrap">
                  <div>
                    <Text size="sm">{[d.platform, d.os_version].filter(Boolean).join(' ') || (d.user_agent ?? '').split(' ')[0] || d.hwid.slice(0, 8)}{d.device_model ? ' · ' + d.device_model : ''}</Text>
                    <Text size="xs" c="dimmed">{t('home.lastSeen')} {when(d.last_seen_at)}</Text>
                  </div>
                  <Button size="compact-xs" variant="subtle" color="red" loading={removeDevice.isPending} onClick={() => removeDevice.mutate(d.hwid)}>{t('home.removeDevice')}</Button>
                </Group>
              ))}
            </Stack>
          )}
        </Card>
      )}
      <SimpleGrid cols={{ base: 2 }}>
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.balance')}</Text>
          <Text fz="xl" fw={700} mt={4}>{money(me.balance_cents)}</Text>
        </Card>
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('home.online')}</Text>
          <Text fz="xl" fw={700} mt={4}>{me.online_devices ?? sub?.online_devices ?? 0}</Text>
        </Card>
      </SimpleGrid>

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
