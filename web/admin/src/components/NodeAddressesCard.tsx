import { Alert, Badge, Card, Group, SimpleGrid, Stack, Switch, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { addressObservationFresh, addressFallbackFresh, addressFallbackError, fallbackFollowState, type AddressFallback, type AddressFallbackFollow } from '../lib/node-addresses'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

type Family = { address: string; last_success: number; confirmed_at: number }
type AddressPolicy = Partial<Pick<Addresses, 'auto_ipv4' | 'auto_ipv6' | 'fallback_auto_ipv4' | 'fallback_auto_ipv6'>>
type Addresses = { auto_ipv4: boolean; auto_ipv6: boolean; fallback_auto_ipv4?: boolean; fallback_auto_ipv6?: boolean; entry_ipv4: string; entry_ipv6: string; observed: { received_at: number; ipv4: Family; ipv6: Family; fallback_ipv4?: AddressFallback; fallback_ipv6?: AddressFallback; fallback_follow_ipv4?: AddressFallbackFollow; fallback_follow_ipv6?: AddressFallbackFollow }; dns_pending: boolean }
export function NodeAddressesCard({ nodeID, canWrite }: { nodeID: number; canWrite: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const queryKey = ['node-addresses', nodeID]
  const q = useQuery({ queryKey, queryFn: ({ signal }) => api.get<Addresses>(`/api/admin/nodes/${nodeID}/addresses`, signal), refetchInterval: 30_000, retry: false })
  const save = useMutation({
    mutationFn: ({ id, policy }: { id: number; policy: AddressPolicy }) => api.put<Addresses>(`/api/admin/settings/nodes/${id}/addresses`, policy),
    onMutate: async ({ id }) => { await qc.cancelQueries({ queryKey: ['node-addresses', id] }) },
    onSuccess: (value, { id }) => { qc.setQueryData(['node-addresses', id], value); qc.invalidateQueries({ queryKey: ['node-addresses', id] }); qc.invalidateQueries({ queryKey: ['node'] }); toast.ok(t('common.saved')) },
    onError: (error, { id }) => { qc.invalidateQueries({ queryKey: ['node-addresses', id] }); toast.err(error) },
  })
  const [clock, setClock] = useState(() => ({ now: Date.now() / 1000, monotonic: performance.now(), online: navigator.onLine }))
  useEffect(() => {
    const tick = () => setClock(previous => {
      const monotonic = performance.now()
      return { now: Math.max(previous.now + Math.max(0, monotonic - previous.monotonic) / 1000, Date.now() / 1000), monotonic, online: navigator.onLine }
    })
    const timer = window.setInterval(tick, 5000)
    window.addEventListener('online', tick); window.addEventListener('offline', tick); document.addEventListener('visibilitychange', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('online', tick); window.removeEventListener('offline', tick); document.removeEventListener('visibilitychange', tick) }
  }, [])
  const fresh = !save.isPending && !q.isError && clock.online && addressObservationFresh(q.data?.observed.received_at, clock.now)
  return <Card mb="lg"><Stack gap="sm">
    <Title order={5}>{t('nodeAddresses.title')}</Title>
    <Text size="sm" c="dimmed">{t('nodeAddresses.hint')}</Text>
    {q.isError && <Alert color="red">{t('dnsHealth.loadError')}</Alert>}
    {!fresh && <Text size="sm" c="dimmed">{t('nodeAddresses.waiting')}</Text>}
    {q.data && <>
      <SimpleGrid cols={{ base: 1, sm: 2 }}>{(['ipv4', 'ipv6'] as const).map(family => {
        const value = q.data!.observed[family]
        const fallback = q.data!.observed[family === 'ipv4' ? 'fallback_ipv4' : 'fallback_ipv6']
        const fallbackFresh = !save.isPending && !q.isError && clock.online && addressFallbackFresh(fallback, clock.now)
        const following = family === 'ipv4' ? q.data!.auto_ipv4 : q.data!.auto_ipv6
        const fallbackAllowed = !!(family === 'ipv4' ? q.data!.fallback_auto_ipv4 : q.data!.fallback_auto_ipv6)
        const candidate = q.data!.observed[family === 'ipv4' ? 'fallback_follow_ipv4' : 'fallback_follow_ipv6']
        const current = fresh && value.last_success === q.data!.observed.received_at
        const entry = family === 'ipv4' ? q.data!.entry_ipv4 : q.data!.entry_ipv6
        const fallbackState = fallbackFollowState({ enabled: following && fallbackAllowed, fresh: fallbackFresh, primary: current, fallback, candidate, entry })
        const fallbackLegacy = fallbackFresh && (fallback?.check_status === undefined || (fallback.check_status === 'no_conflict' && !/^[a-f0-9]{32}$/.test(fallback.run_id ?? '')))
        const confirming = following && current && !value.confirmed_at
        const applying = following && current && !!value.confirmed_at && entry !== value.address
        return <Card key={family} component="section" aria-label={family === 'ipv4' ? 'IPv4' : 'IPv6'} withBorder p="sm"><Stack gap="xs">
          <Group justify="space-between"><Text fw={600}>{family === 'ipv4' ? 'IPv4' : 'IPv6'}</Text><Badge color={current ? 'teal' : fallbackState === 'following' ? 'blue' : 'gray'}>{t(current ? 'nodeAddresses.detected' : fallbackState === 'following' ? 'nodeAddresses.fallbackFollowing' : 'nodeAddresses.unknown')}</Badge></Group>
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
            {fallbackLegacy && <Text size="xs" c="orange">{t('nodeAddresses.fallbackLegacy')}</Text>}
            {fallbackState === 'disabled'
              ? <Text size="xs" c="dimmed">{t('nodeAddresses.fallbackHint')}</Text>
              : <Text size="xs" c={fallbackState === 'following' ? 'teal' : ['conflict', 'confirming', 'applying'].includes(fallbackState) ? 'orange' : 'dimmed'}>{t(`nodeAddresses.fallbackStatus.${fallbackState}`)}</Text>}
          </Stack></Card>}
          {canWrite && <Stack gap="xs">
            <Switch label={t('nodeAddresses.follow')} checked={following} disabled={save.isPending || (!following && !fresh)} onChange={e => save.mutate({ id: nodeID, policy: family === 'ipv4' ? { auto_ipv4: e.currentTarget.checked } : { auto_ipv6: e.currentTarget.checked } })} />
            <Switch label={t('nodeAddresses.fallbackFollow')} description={t('nodeAddresses.fallbackFollowHint')} checked={fallbackAllowed} disabled={save.isPending || !following} onChange={e => save.mutate({ id: nodeID, policy: family === 'ipv4' ? { fallback_auto_ipv4: e.currentTarget.checked } : { fallback_auto_ipv6: e.currentTarget.checked } })} />
          </Stack>}
        </Stack></Card>
      })}</SimpleGrid>
      <Text size="xs" c="dimmed">{t('nodeAddresses.behavior')}</Text>
      {q.data.dns_pending && <Text size="sm" c="orange">{t('nodeAddresses.dnsPending')}</Text>}
    </>}
  </Stack></Card>
}
