import { Alert, Anchor, Badge, Button, Group, SimpleGrid, Skeleton, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { RedeemCard } from '../components/RedeemCard'
import { TelegramCard } from '../components/TelegramCard'
import classes from './SecondaryPages.module.css'

export default function AccountPage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const qc = useQueryClient()
  const providers = useQuery({ queryKey: ['oauth-providers'], queryFn: () => api.get<{ providers: { id: string; name: string }[] }>('/api/oauth/providers') })
  const identities = useQuery({ queryKey: ['identities'], queryFn: () => api.get<{ provider: string; email: string }[]>('/api/oauth/identities'), enabled: !!providers.data?.providers.length })
  const unlink = useMutation({ mutationFn: (p: string) => api.del(`/api/oauth/identities/${p}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['identities'] }), onError: toast.err })
  const removeDevice = useMutation({ mutationFn: (hwid: string) => api.del(`/api/portal/me/hwid-devices/${encodeURIComponent(hwid)}`), onSuccess: () => refresh(), onError: toast.err })
  if (!me) return null
  return <Stack className={classes.page}>
    <header className={classes.header}><div><div className={classes.eyebrow}>05 / {t('accountUI.profile')}</div><Title order={1} className={classes.title}>{t('portalNav.account')}</Title><Text className={classes.description}>{t('accountUI.description')}</Text></div></header>
    <section className={classes.accountRows} aria-label={t('accountUI.profile')}>
      <div className={classes.accountRow}><span className={classes.accountLabel}>{t('auth.email')}</span><div className={classes.accountValue}><strong>{me.email}</strong></div></div>
      <div className={classes.accountRow}><span className={classes.accountLabel}>{t('home.balance')}</span><div className={classes.accountValue}><strong>{money(me.balance_cents)}</strong><div><Anchor component={Link} to="/plans" size="sm">{t('home.browsePlans')} ↗</Anchor></div></div></div>
      <div className={classes.accountRow}><span className={classes.accountLabel}>{t('home.online')}</span><div className={classes.accountValue}><strong>{me.online_devices ?? me.subscription?.online_devices ?? 0}</strong><Text size="xs" c="dimmed" mt={4}>{t('accountUI.onlineHint')}</Text></div></div>
      <div className={classes.accountRow}><span className={classes.accountLabel}>{t('nav.invite')}</span><div className={classes.accountValue}><Anchor component={Link} to="/invite" size="sm">{t('invite.title')} ↗</Anchor><Text size="xs" c="dimmed" mt={4}>{t('accountUI.inviteHint')}</Text></div></div>
    </section>
    {me.hwid_enabled && <section className={classes.accountSection}>
      <Group justify="space-between" align="flex-start" mb="md"><div><Title order={2} className={classes.sectionTitle}>{t('accountUI.registeredDevices')}</Title><Text c="dimmed" size="sm" mt={4}>{me.hwid_limit > 0 ? t('home.devicesHint', { n: me.hwid_devices.length, limit: me.hwid_limit }) : t('home.devicesUnlimited', { n: me.hwid_devices.length })}</Text></div><Badge variant="light">{me.hwid_devices.length}</Badge></Group>
      {me.hwid_devices.length === 0 ? <Text c="dimmed" size="sm">{t('home.noDevices')}</Text> : <Stack gap={0}>{me.hwid_devices.map((d) => <Group className={classes.detailRow} key={d.hwid} justify="space-between" wrap="nowrap">
        <div style={{ minWidth: 0, overflowWrap: 'anywhere' }}><Text size="sm" fw={600}>{[d.platform, d.os_version].filter(Boolean).join(' ') || (d.user_agent ?? '').split(' ')[0] || d.hwid.slice(0, 8)}{d.device_model ? ` · ${d.device_model}` : ''}</Text><Text size="xs" c="dimmed">{t('home.lastSeen')} {when(d.last_seen_at)}</Text></div>
        <Button size="compact-sm" variant="subtle" color="red" style={{ flexShrink: 0 }} loading={removeDevice.isPending && removeDevice.variables === d.hwid} onClick={() => removeDevice.mutate(d.hwid)}>{t('home.removeDevice')}</Button>
      </Group>)}</Stack>}
    </section>}
    <SimpleGrid cols={{ base: 1, md: 2 }} className={classes.accountTools}>
      <Stack gap="lg"><RedeemCard /><TelegramCard /></Stack>
      <Stack gap="lg">
        {providers.isError && <Alert color="red">{t('accountUI.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => providers.refetch()}>{t('auth.retry')}</Button></Alert>}
        {providers.isPending && <Skeleton height={120} radius="lg" />}
        {!!providers.data?.providers.length && <section className={classes.accountSection}>
          <Group gap="xs" mb="md"><Title order={2} className={classes.sectionTitle}>{t('home.logins')}</Title></Group>
          {identities.isPending ? <Skeleton height={60} /> : identities.isError ? <Alert color="red">{t('accountUI.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => identities.refetch()}>{t('auth.retry')}</Button></Alert> : <Stack gap={0}>{providers.data.providers.map((p) => {
            const linked = identities.data?.find((i) => i.provider === p.id)
            return <Group className={classes.detailRow} key={p.id} justify="space-between" wrap="nowrap"><div style={{ minWidth: 0, overflowWrap: 'anywhere' }}><Text fw={600} size="sm">{p.name}</Text>{linked && <Text size="xs" c="dimmed">{linked.email}</Text>}</div>{linked
              ? <Button size="xs" variant="subtle" color="red" loading={unlink.isPending && unlink.variables === p.id} onClick={() => unlink.mutate(p.id)}>{t('home.unlink')}</Button>
              : <Button size="xs" variant="light" component="a" href={`/api/oauth/${p.id}/start?link=1&next=/portal/account`}>{t('home.link')}</Button>}</Group>
          })}</Stack>}
        </section>}
      </Stack>
    </SimpleGrid>
  </Stack>
}
