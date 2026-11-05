import { Alert, Badge, Card, Group, SimpleGrid, Stack, Switch, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { addressObservationFresh, addressFallbackError, type AddressFallback } from '../lib/node-addresses'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

type Family = { address: string; last_success: number; confirmed_at: number }
type Addresses = { auto_ipv4: boolean; auto_ipv6: boolean; entry_ipv4: string; entry_ipv6: string; observed: { received_at: number; ipv4: Family; ipv6: Family; fallback_ipv4?: AddressFallback; fallback_ipv6?: AddressFallback }; dns_pending: boolean }
export function NodeAddressesCard({ nodeID, canWrite }: { nodeID: number; canWrite: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['node-addresses', nodeID], queryFn: () => api.get<Addresses>(`/api/admin/nodes/${nodeID}/addresses`), refetchInterval: 30_000 })
  const save = useMutation({ mutationFn: (value: Partial<Pick<Addresses, 'auto_ipv4' | 'auto_ipv6'>>) => api.put(`/api/admin/settings/nodes/${nodeID}/addresses`, value), onSuccess: () => { qc.invalidateQueries({ queryKey: ['node-addresses', nodeID] }); qc.invalidateQueries({ queryKey: ['node'] }); toast.ok(t('common.saved')) }, onError: toast.err })
  const [clock, setClock] = useState(() => ({ now: Date.now() / 1000, online: navigator.onLine }))
  useEffect(() => {
    const tick = () => setClock(previous => ({ now: Math.max(previous.now, Date.now() / 1000), online: navigator.onLine }))
    const timer = window.setInterval(tick, 5000)
    window.addEventListener('online', tick); window.addEventListener('offline', tick); document.addEventListener('visibilitychange', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('online', tick); window.removeEventListener('offline', tick); document.removeEventListener('visibilitychange', tick) }
  }, [])
  const fresh = !q.isError && clock.online && addressObservationFresh(q.data?.observed.received_at, clock.now)
  return <Card mb="lg"><Stack gap="sm">
    <Title order={5}>{t('nodeAddresses.title')}</Title>
    <Text size="sm" c="dimmed">{t('nodeAddresses.hint')}</Text>
    {q.isError && <Alert color="red">{t('dnsHealth.loadError')}</Alert>}
    {!fresh && <Text size="sm" c="dimmed">{t('nodeAddresses.waiting')}</Text>}
    {q.data && <>
      <SimpleGrid cols={{ base: 1, sm: 2 }}>{(['ipv4', 'ipv6'] as const).map(family => {
        const value = q.data!.observed[family]
        const fallback = q.data!.observed[family === 'ipv4' ? 'fallback_ipv4' : 'fallback_ipv6']
        const fallbackFresh = !!fallback?.address && !fallback.error && !q.isError && clock.online && addressObservationFresh(fallback?.received_at, clock.now)
        const following = family === 'ipv4' ? q.data!.auto_ipv4 : q.data!.auto_ipv6
        const current = fresh && value.last_success === q.data!.observed.received_at
        const entry = family === 'ipv4' ? q.data!.entry_ipv4 : q.data!.entry_ipv6
        const confirming = following && current && !value.confirmed_at
        const applying = following && current && !!value.confirmed_at && entry !== value.address
        return <Card key={family} withBorder p="sm"><Stack gap="xs">
          <Group justify="space-between"><Text fw={600}>{family === 'ipv4' ? 'IPv4' : 'IPv6'}</Text><Badge color={current ? 'teal' : 'gray'}>{t(current ? 'nodeAddresses.detected' : 'nodeAddresses.unknown')}</Badge></Group>
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('nodeAddresses.machine')}: {value.address || '—'}</Text>
          <Text size="xs" c="dimmed">{t('nodeAddresses.lastSuccess')}: {value.last_success ? new Date(value.last_success * 1000).toLocaleString() : '—'}</Text>
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('nodeAddresses.entry')}: {(family === 'ipv4' ? q.data!.entry_ipv4 : q.data!.entry_ipv6) || '—'}</Text>
          {confirming && <Text size="xs" c="orange">{t('nodeAddresses.confirming')}</Text>}
          {applying && <Text size="xs" c="orange">{t('nodeAddresses.applying')}</Text>}
          {fallback && <Card withBorder p="xs"><Stack gap={4}>
            <Group justify="space-between"><Text size="sm" fw={500}>{t('nodeAddresses.fallbackTitle')}</Text><Badge color="gray">{t(fallbackFresh ? 'nodeAddresses.fallbackSingle' : 'nodeAddresses.fallbackStale')}</Badge></Group>
            <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{fallback.address || '—'}</Text>
            <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{t('nodeAddresses.fallbackSource')}: {t('nodeAddresses.fallbackCloudflare')} · {fallback.origin}</Text>
            <Text size="xs" c="dimmed">{t('nodeAddresses.fallbackSampled')}: {fallback.sampled_at ? new Date(fallback.sampled_at * 1000).toLocaleString() : '—'}</Text>
            {fallback.error && <Text size="xs" c="orange">{t(`nodeAddresses.fallbackErrors.${addressFallbackError(fallback.error)}`)}</Text>}
            <Text size="xs" c="dimmed">{t('nodeAddresses.fallbackHint')}</Text>
          </Stack></Card>}
          {canWrite && <Switch label={t('nodeAddresses.follow')} checked={following} disabled={save.isPending || (!following && !fresh)} onChange={e => save.mutate(family === 'ipv4' ? { auto_ipv4: e.currentTarget.checked } : { auto_ipv6: e.currentTarget.checked })} />}
        </Stack></Card>
      })}</SimpleGrid>
      <Text size="xs" c="dimmed">{t('nodeAddresses.behavior')}</Text>
      {q.data.dns_pending && <Text size="sm" c="orange">{t('nodeAddresses.dnsPending')}</Text>}
    </>}
  </Stack></Card>
}
