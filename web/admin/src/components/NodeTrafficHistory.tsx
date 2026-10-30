import { Accordion, Alert, Badge, Button, Card, Code, Group, Loader, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes } from '../lib/format'
import { evidenceIsFresh, evidenceLease } from '../lib/line-change-evidence'
import { acceptedBatchBytes, historyQuality, sourceInterval, trafficHistoryURL, validHistoryTime, type TrafficHistoryPage } from '../lib/traffic-history'

const clock = () => ({ monotonic: performance.now(), wall: Date.now() })
export function NodeTrafficHistory({ nodeID }: { nodeID: number }) {
  const { t, i18n } = useTranslation()
  const [cursors, setCursors] = useState<number[]>([])
  const [, setNow] = useState(clock)
  const before = cursors.at(-1)
  useEffect(() => {
    const tick = () => setNow(clock()), timer = window.setInterval(tick, 1000)
    window.addEventListener('offline', tick); window.addEventListener('online', tick); window.addEventListener('focus', tick)
    document.addEventListener('visibilitychange', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('online', tick); window.removeEventListener('focus', tick); document.removeEventListener('visibilitychange', tick) }
  }, [])
  const q = useQuery({ queryKey: ['node-traffic-history', nodeID, before], queryFn: async ({ signal }) => {
    const start = clock(), page = await api.get<TrafficHistoryPage>(trafficHistoryURL(nodeID, before), signal)
    if (!Array.isArray(page.items)) throw new Error('invalid history response')
    return { page, lease: evidenceLease(start, page.delivery?.fresh_for_ms ?? 0) }
  }, retry: false, staleTime: 0 })
  const page = q.data?.page, delivery = page?.delivery
  const fresh = !!q.data && !q.isError && q.fetchStatus !== 'paused' && evidenceIsFresh(q.data.lease, clock(), navigator.onLine)
  const date = (value?: string) => validHistoryTime(value) ? new Date(value!).toLocaleString(i18n.language) : t('observability.unknown')
  const reason = (value: string) => t(`trafficHistory.reasons.${value}`, { defaultValue: t('trafficHistory.reasons.other') })
  return <Stack gap="lg">
    <Card withBorder><Stack gap="sm">
      <Group justify="space-between"><Title order={4}>{t('trafficHistory.title')}</Title><Button size="xs" variant="default" loading={q.isFetching} onClick={() => cursors.length ? setCursors([]) : q.refetch()}>{t('trafficHistory.latest')}</Button></Group>
      <Text size="sm" c="dimmed">{t('trafficHistory.scope')}</Text>
      <Alert color="blue">{t('trafficHistory.accounting')}</Alert>
      {page && <Text size="xs" c="dimmed">{t('trafficHistory.retention', { days: page.retention.days, rows: page.retention.max_rows.toLocaleString(i18n.language), global: page.retention.global_max_rows.toLocaleString(i18n.language) })}</Text>}
    </Stack></Card>
    {q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="red"><Stack gap="xs"><Text size="sm">{t('trafficHistory.loadError')}</Text><Button size="xs" variant="light" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Stack></Alert>}
    {page && <Card withBorder><Stack gap="sm">
      <Group justify="space-between"><Title order={5}>{t('trafficHistory.delivery')}</Title><Badge color={fresh ? 'blue' : 'gray'}>{t(fresh ? 'observability.fresh' : 'observability.unknown')}</Badge></Group>
      <Text size="xs" c="dimmed">{t('trafficHistory.deliveryScope')}</Text>
      {delivery ? <>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>{(['pending_bytes', 'buffered_bytes', 'journal_bytes'] as const).map(key => <div key={key}><Text size="xs" c="dimmed">{t(`trafficHistory.${key}`)}</Text><Text fw={600}>{fresh ? bytes(delivery[key]) : '—'}</Text></div>)}</SimpleGrid>
        <Text size="xs">{t('trafficHistory.observed')}: {date(delivery.sampled_at)} · {t('trafficHistory.received')}: {date(delivery.received_at)}</Text>
        <Text size="xs">{t('trafficHistory.oldest')}: {date(delivery.oldest_at)}</Text>
        {!fresh && <Text size="sm" c="dimmed">{t('trafficHistory.deliveryStale')}</Text>}
      </> : <Text c="dimmed" size="sm">{t('trafficHistory.deliveryUnavailable')}</Text>}
    </Stack></Card>}
    {page && !page.items.length && <Alert color="gray">{t('trafficHistory.empty')}</Alert>}
    {!!page?.items.length && <Accordion variant="separated">{page.items.map(item => {
      const totals = acceptedBatchBytes(item), quality = historyQuality(item), sample = item.metadata_status === 'valid' ? item.sample : undefined
      return <Accordion.Item key={item.id} value={String(item.id)}><Accordion.Control>
        <Stack gap={4}><Group gap="xs"><Text size="sm" fw={600}>{t('trafficHistory.received')}: {date(item.received_at)}</Text><Badge color={quality === 'observed' ? 'blue' : 'gray'}>{t(`trafficHistory.quality.${quality}`)}</Badge></Group><Text size="xs" c="dimmed">{t('trafficHistory.totals', { up: totals ? bytes(totals.up) : t('observability.unknown'), down: totals ? bytes(totals.down) : t('observability.unknown') })}</Text></Stack>
      </Accordion.Control><Accordion.Panel><Stack gap="sm">
        <Text size="sm">{t('trafficHistory.received')}: {date(item.received_at)}</Text>
        <Text size="sm">{t('trafficHistory.accountingDay')}: {item.accounting_day}</Text>
        {item.discarded_samples > 0 && <Alert color="yellow">{t('trafficHistory.discarded', { count: item.discarded_samples })}</Alert>}
        <Text size="sm">{t('trafficHistory.delay')}: {t('trafficHistory.delayUnknown')}</Text>
        {sample ? <>
          <Text size="sm">{t('trafficHistory.collection')}: {date(sample.collection_started_at)} → {date(sample.collection_ended_at)}</Text>
          <Text size="xs" c="dimmed">{t('trafficHistory.collectionHint', { count: sample.segments })}</Text>
          {!!sample.reasons?.length && <Text size="sm">{sample.reasons.map(reason).join(' · ')}</Text>}
          <Title order={6}>{t('trafficHistory.sources')}</Title>
          {(sample.sources ?? []).map((source, index) => {
            const interval = sourceInterval(source)
            return <Card key={index} withBorder padding="sm"><Stack gap={4}>
              <Group gap="xs"><Code style={{ overflowWrap: 'anywhere', whiteSpace: 'normal' }}>{source.core}</Code><Text size="sm">{t(`trafficHistory.counter.${source.counter}`)}</Text><Badge color="gray">{t(`trafficHistory.quality.${source.quality}`, { defaultValue: t('observability.unknown') })}</Badge></Group>
              <Text size="sm">{t('trafficHistory.interval')}: {interval ? `${date(interval[0])} → ${date(interval[1])}` : t('trafficHistory.intervalUnknown')}</Text>
              {!interval && <Text size="xs">{t('trafficHistory.readAt')}: {date(source.ended_at)}</Text>}
              <Text size="xs" c="dimmed">{t('trafficHistory.segments', { count: source.segments })}</Text>
              {!!source.reasons?.length && <Text size="xs">{source.reasons.map(reason).join(' · ')}</Text>}
            </Stack></Card>
          })}
        </> : <Alert color="gray">{t(`trafficHistory.${item.metadata_status === 'legacy' ? 'legacy' : 'invalid'}`)}</Alert>}
        <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{t('trafficHistory.batch')}: {item.epoch} / {String(item.sequence)}</Text>
      </Stack></Accordion.Panel></Accordion.Item>
    })}</Accordion>}
    {page && <Group justify="space-between"><Button variant="default" disabled={!cursors.length || q.isFetching} onClick={() => setCursors(old => old.slice(0, -1))}>{t('trafficHistory.newer')}</Button><Button variant="default" disabled={!page.next_before || q.isFetching} onClick={() => { if (page.next_before) setCursors(old => [...old, page.next_before!]) }}>{t('trafficHistory.older')}</Button></Group>}
  </Stack>
}
