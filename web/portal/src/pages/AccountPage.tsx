import { Alert, Anchor, Avatar, Badge, Button, Card, Group, SimpleGrid, Skeleton, Stack, Text, Title } from '@mantine/core'
import { IconArrowUpRight, IconDevices, IconGift, IconUser, IconWallet } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { RedeemCard } from '../components/RedeemCard'
import { TelegramCard } from '../components/TelegramCard'

export default function AccountPage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const qc = useQueryClient()
  const providers = useQuery({ queryKey: ['oauth-providers'], queryFn: () => api.get<{ providers: { id: string; name: string }[] }>('/api/oauth/providers') })
  const identities = useQuery({ queryKey: ['identities'], queryFn: () => api.get<{ provider: string; email: string }[]>('/api/oauth/identities'), enabled: !!providers.data?.providers.length })
  const unlink = useMutation({ mutationFn: (p: string) => api.del(`/api/oauth/identities/${p}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['identities'] }), onError: toast.err })
  const removeDevice = useMutation({ mutationFn: (hwid: string) => api.del(`/api/portal/me/hwid-devices/${encodeURIComponent(hwid)}`), onSuccess: () => refresh(), onError: toast.err })
  if (!me) return null
  return <Stack gap="xl">
    <div><Title order={1} fz={28}>{t('portalNav.account')}</Title><Text c="dimmed" mt={6}>{t('accountUI.description')}</Text></div>
    <Card>
      <Group gap="md" wrap="nowrap"><Avatar size={52} radius="xl">{me.email.slice(0, 1).toUpperCase()}</Avatar><div style={{ minWidth: 0 }}><Text fw={700} fz="lg" style={{ overflowWrap: 'anywhere' }}>{me.email}</Text><Text size="sm" c="dimmed">{t('accountUI.profile')}</Text></div></Group>
    </Card>
    <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="lg">
      <Card><Group justify="space-between"><Text c="dimmed" size="sm">{t('home.balance')}</Text><IconWallet size={20} /></Group><Text fz={30} fw={750} mt="md">{money(me.balance_cents)}</Text><Anchor component={Link} to="/plans" size="sm" mt="sm">{t('home.browsePlans')} →</Anchor></Card>
      <Card><Group justify="space-between"><Text c="dimmed" size="sm">{t('home.online')}</Text><IconDevices size={20} /></Group><Text fz={30} fw={750} mt="md">{me.online_devices ?? me.subscription?.online_devices ?? 0}</Text><Text size="xs" c="dimmed" mt="sm">{t('accountUI.onlineHint')}</Text></Card>
      <Card component={Link} to="/invite" style={{ textDecoration: 'none', color: 'inherit' }}><Group justify="space-between"><Text c="dimmed" size="sm">{t('nav.invite')}</Text><IconGift size={20} /></Group><Text fw={700} fz="lg" mt="md">{t('invite.title')}</Text><Group gap={4} mt="sm"><Text size="sm" c="dimmed">{t('accountUI.inviteHint')}</Text><IconArrowUpRight size={16} /></Group></Card>
    </SimpleGrid>
    {me.hwid_enabled && <Card>
      <Group justify="space-between" align="flex-start" mb="md"><div><Title order={3} fz="lg">{t('accountUI.registeredDevices')}</Title><Text c="dimmed" size="sm" mt={4}>{me.hwid_limit > 0 ? t('home.devicesHint', { n: me.hwid_devices.length, limit: me.hwid_limit }) : t('home.devicesUnlimited', { n: me.hwid_devices.length })}</Text></div><Badge variant="light">{me.hwid_devices.length}</Badge></Group>
      {me.hwid_devices.length === 0 ? <Text c="dimmed" size="sm">{t('home.noDevices')}</Text> : <Stack gap="md">{me.hwid_devices.map((d) => <Group key={d.hwid} justify="space-between" wrap="nowrap">
        <div style={{ minWidth: 0, overflowWrap: 'anywhere' }}><Text size="sm" fw={600}>{[d.platform, d.os_version].filter(Boolean).join(' ') || (d.user_agent ?? '').split(' ')[0] || d.hwid.slice(0, 8)}{d.device_model ? ` · ${d.device_model}` : ''}</Text><Text size="xs" c="dimmed">{t('home.lastSeen')} {when(d.last_seen_at)}</Text></div>
        <Button size="compact-sm" variant="subtle" color="red" style={{ flexShrink: 0 }} loading={removeDevice.isPending && removeDevice.variables === d.hwid} onClick={() => removeDevice.mutate(d.hwid)}>{t('home.removeDevice')}</Button>
      </Group>)}</Stack>}
    </Card>}
    <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg" style={{ alignItems: 'start' }}>
      <Stack gap="lg"><RedeemCard /><TelegramCard /></Stack>
      <Stack gap="lg">
        {providers.isError && <Alert color="red">{t('accountUI.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => providers.refetch()}>{t('auth.retry')}</Button></Alert>}
        {providers.isPending && <Skeleton height={120} radius="lg" />}
        {!!providers.data?.providers.length && <Card>
          <Group gap="xs" mb="md"><IconUser size={20} /><Title order={3} fz="lg">{t('home.logins')}</Title></Group>
          {identities.isPending ? <Skeleton height={60} /> : identities.isError ? <Alert color="red">{t('accountUI.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => identities.refetch()}>{t('auth.retry')}</Button></Alert> : <Stack gap="md">{providers.data.providers.map((p) => {
            const linked = identities.data?.find((i) => i.provider === p.id)
            return <Group key={p.id} justify="space-between" wrap="nowrap"><div style={{ minWidth: 0, overflowWrap: 'anywhere' }}><Text fw={600} size="sm">{p.name}</Text>{linked && <Text size="xs" c="dimmed">{linked.email}</Text>}</div>{linked
              ? <Button size="xs" variant="subtle" color="red" loading={unlink.isPending && unlink.variables === p.id} onClick={() => unlink.mutate(p.id)}>{t('home.unlink')}</Button>
              : <Button size="xs" variant="light" component="a" href={`/api/oauth/${p.id}/start?link=1&next=/portal/account`}>{t('home.link')}</Button>}</Group>
          })}</Stack>}
        </Card>}
      </Stack>
    </SimpleGrid>
  </Stack>
}
