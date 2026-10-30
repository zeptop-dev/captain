import { Accordion, Alert, Badge, Card, Code, Group, Loader, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'

interface Delivery {
  id: string
  config_revision: string
  state_revision: string
  issued_at: string
  confirmed_at?: string
  last_receipt_at?: string
  observed_at?: string
  fresh_for_ms: number
}
interface Configuration {
  status: 'unknown' | 'unavailable' | 'pending' | 'applied' | 'stale'
  config_revision: string
  state_revision: string
  current?: Delivery
  history: Delivery[]
  freshnessDeadline: number
  wallDeadline: number
}
const short = (value: string) => value.replace(/^cfg1-/, '').slice(0, 12)

export function NodeConfigurationCard({ nodeID }: { nodeID: number }) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => ({ monotonic: performance.now(), wall: Date.now() }))
  useEffect(() => {
    const tick = () => setNow({ monotonic: performance.now(), wall: Date.now() })
    const timer = window.setInterval(tick, 1000)
    window.addEventListener('offline', tick)
    window.addEventListener('online', tick)
    window.addEventListener('focus', tick)
    document.addEventListener('visibilitychange', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('online', tick); window.removeEventListener('focus', tick); document.removeEventListener('visibilitychange', tick) }
  }, [])
  const q = useQuery({ queryKey: ['node-configuration', nodeID], queryFn: async () => {
    // Start the local lease before the request, so network delay only shortens
    // it. Monotonic time avoids extending evidence when the browser clock moves.
    const started = performance.now(), wallStarted = Date.now()
    const value = await api.get<Omit<Configuration, 'freshnessDeadline' | 'wallDeadline'>>(`/api/admin/nodes/${nodeID}/configuration`)
    const remaining = Math.max(0, value.current?.fresh_for_ms ?? 0)
    // Some platforms pause performance.now() during system sleep. The wall
    // deadline expires that case; the monotonic deadline handles clock rollback.
    return { ...value, freshnessDeadline: started + remaining, wallDeadline: wallStarted + remaining }
  }, refetchInterval: 15_000 })
  const d = q.data
  // Never show cached success after a refresh fails: freshness is evidence,
  // not a permanent property of a previously confirmed delivery.
  const disconnected = q.isError || q.fetchStatus === 'paused' || !navigator.onLine
  const status = disconnected ? 'unknown' : d?.status === 'applied' && (now.monotonic >= d.freshnessDeadline || now.wall >= d.wallDeadline) ? 'stale' : d?.status ?? 'unknown'
  return <Card mb="lg" withBorder>
    <Stack gap="sm">
      <Group justify="space-between"><Title order={4}>{t('nodeConfiguration.title')}</Title><Badge color={status === 'applied' ? 'teal' : status === 'pending' ? 'orange' : 'gray'}>{t(`nodeConfiguration.status.${status}`)}</Badge></Group>
      <Text size="sm" c="dimmed">{t('nodeConfiguration.hint')}</Text>
      {q.isLoading && <Loader size="sm" />}
      {q.isError && <Alert color="red">{t('nodeConfiguration.loadError')}</Alert>}
      {d && <>
        <Text size="sm">{t('nodeConfiguration.structure')} <Code title={d.config_revision}>{d.config_revision ? short(d.config_revision) : '—'}</Code> · {t('nodeConfiguration.state')} <Code>{short(d.state_revision)}</Code></Text>
        <Text size="sm" c="dimmed">{t('nodeConfiguration.observed', { at: d.current?.observed_at ? when(d.current.observed_at) : '—' })}</Text>
        {status === 'unknown' && <Text size="sm" c="dimmed">{t('nodeConfiguration.unknownHint')}</Text>}
        {status === 'unavailable' && <Text size="sm" c="dimmed">{t('nodeConfiguration.unavailableHint')}</Text>}
        <Accordion variant="contained"><Accordion.Item value="history"><Accordion.Control>{t('nodeConfiguration.history')}</Accordion.Control><Accordion.Panel>
          <Text size="sm" c="dimmed" mb="sm">{t('nodeConfiguration.historyHint')}</Text>
          {d.history.length ? <Table.ScrollContainer minWidth={580}><Table><Table.Thead><Table.Tr>
            <Table.Th>{t('nodeConfiguration.delivery')}</Table.Th><Table.Th>{t('nodeConfiguration.structure')}</Table.Th><Table.Th>{t('nodeConfiguration.issued')}</Table.Th><Table.Th>{t('nodeConfiguration.confirmed')}</Table.Th>
          </Table.Tr></Table.Thead><Table.Tbody>{d.history.map(item => <Table.Tr key={item.id}>
            <Table.Td><Code title={item.id}>{short(item.id)}</Code></Table.Td><Table.Td><Code title={item.config_revision}>{short(item.config_revision)}</Code></Table.Td><Table.Td>{when(item.issued_at)}</Table.Td><Table.Td>{item.confirmed_at ? when(item.confirmed_at) : '—'}</Table.Td>
          </Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <Text c="dimmed">{t('common.empty')}</Text>}
        </Accordion.Panel></Accordion.Item></Accordion>
      </>}
    </Stack>
  </Card>
}
