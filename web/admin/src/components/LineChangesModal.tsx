import { Accordion, Alert, Badge, Box, Button, Card, Code, Group, Loader, Modal, SimpleGrid, Stack, Text, Title, UnstyledButton } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type LineChange, type LineChangePage } from '../lib/api'
import { when } from '../lib/format'
import { lineChangesURL, lineChangeStatus, lineTargetStatus, observeLineChange, type EvidenceClock, type LineHistorySelection, type ObservedLineChange, type ObservedLineTarget } from '../lib/line-change-evidence'

const clock = (): EvidenceClock => ({ monotonic: performance.now(), wall: Date.now() })
const short = (value: string) => value.replace(/^cfg1-/, '').slice(0, 12)
const color = (status: string) => status === 'applied' ? 'teal' : ['partial', 'pending', 'delivered', 'not_applied'].includes(status) ? 'orange' : 'gray'

export function LineChangesModal({ selection, onClose }: { selection: LineHistorySelection; onClose: () => void }) {
  const { t } = useTranslation()
  const mobile = useMediaQuery('(max-width: 48em)')
  const [pages, setPages] = useState<(number | undefined)[]>([undefined])
  const before = pages[pages.length - 1]
  const [selectedID, setSelectedID] = useState(selection.changeID)
  const [showDetail, setShowDetail] = useState(!!selection.changeID)
  const [now, setNow] = useState(clock)
  useEffect(() => {
    const tick = () => setNow(clock())
    const timer = window.setInterval(tick, 1000)
    window.addEventListener('offline', tick)
    window.addEventListener('online', tick)
    window.addEventListener('focus', tick)
    document.addEventListener('visibilitychange', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('online', tick); window.removeEventListener('focus', tick); document.removeEventListener('visibilitychange', tick) }
  }, [])
  const history = useQuery({ queryKey: ['line-changes', selection.lineID, before], queryFn: async ({ signal }) => {
    const started = clock()
    const response = await api.get<LineChangePage>(lineChangesURL(selection.lineID, before), signal)
    return { ...response, items: response.items.map(item => observeLineChange(item, started)) }
  }, retry: false, refetchInterval: 15_000 })
  const activeID = selectedID ?? history.data?.items[0]?.id
  const detail = useQuery({ queryKey: ['line-change', activeID], queryFn: async ({ signal }) => {
    const started = clock()
    return observeLineChange(await api.get<LineChange>(`/api/admin/line-changes/${encodeURIComponent(activeID!)}`, signal), started)
  }, enabled: !!activeID, retry: false, refetchInterval: 15_000 })
  const historyConnected = !history.isError && history.fetchStatus !== 'paused' && navigator.onLine
  const detailConnected = !detail.isError && detail.fetchStatus !== 'paused' && navigator.onLine
  const turnPage = (next: (number | undefined)[]) => { setPages(next); setSelectedID(undefined) }
  return <Modal opened onClose={onClose} title={t('lineChanges.title')} size="72rem" fullScreen={mobile} closeButtonProps={{ 'aria-label': t('lineChanges.close') }}>
    <Stack>
      {selection.lineID && <Text fw={600} style={{ overflowWrap: 'anywhere' }}>{selection.lineName || `#${selection.lineID}`}</Text>}
      <Alert color="blue">{t('lineChanges.hint')}</Alert>
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
        <Stack gap="sm" style={{ minWidth: 0, display: mobile && showDetail ? 'none' : undefined }}>
          <Group justify="space-between"><Title order={4}>{t('lineChanges.history')}</Title><Button variant="subtle" size="xs" onClick={() => { void history.refetch(); if (activeID) void detail.refetch() }} loading={history.isFetching || detail.isFetching}>{t('common.refresh')}</Button></Group>
          {history.isLoading && <Loader size="sm" />}
          {!historyConnected && <Alert color="red">{t('lineChanges.loadError')}</Alert>}
          {history.data?.items.map(item => <UnstyledButton key={item.id} onClick={() => { setSelectedID(item.id); setShowDetail(true) }} aria-pressed={activeID === item.id} style={{ width: '100%' }}>
            <Card withBorder p="sm" style={{ borderColor: activeID === item.id ? 'var(--mantine-color-indigo-5)' : undefined }}>
              <Stack gap={5}>
                <Group justify="space-between" align="flex-start"><Text fw={600} style={{ overflowWrap: 'anywhere', flex: '1 1 120px' }}>{item.name || `#${item.line_id}`}</Text><ChangeBadge change={item} now={now} connected={historyConnected} /></Group>
                <Text size="sm">{t(`lineChanges.kind.${item.kind}`)} · {t('lineChanges.version', { version: item.version })}</Text>
                <Text size="xs" c="dimmed">{when(item.created_at)}</Text>
              </Stack>
            </Card>
          </UnstyledButton>)}
          {!history.isLoading && !history.isError && history.data?.items.length === 0 && <Text c="dimmed">{t('lineChanges.empty')}</Text>}
          <Group justify="space-between"><Button size="xs" variant="default" disabled={pages.length === 1 || history.isFetching} onClick={() => turnPage(pages.slice(0, -1))}>{t('lineChanges.newer')}</Button><Button size="xs" variant="default" disabled={!history.data?.next_before || history.isFetching || history.isError} onClick={() => turnPage([...pages, history.data!.next_before])}>{t('lineChanges.older')}</Button></Group>
        </Stack>
        <Stack gap="sm" style={{ minWidth: 0, display: mobile && !showDetail ? 'none' : undefined }}>
          <Group justify="space-between"><Title order={4}>{t('lineChanges.application')}</Title>{mobile && <Button variant="subtle" size="xs" onClick={() => setShowDetail(false)}>{t('lineChanges.back')}</Button>}</Group>
          {activeID && detail.isLoading && <Loader size="sm" />}
          {activeID && !detailConnected && <Alert color="red">{t('lineChanges.loadError')}</Alert>}
          {!activeID && <Text c="dimmed">{t('lineChanges.select')}</Text>}
          {detail.data && <>
            <Card withBorder padding="sm"><Stack gap="xs">
              <Group justify="space-between" align="flex-start"><Text fw={600} style={{ overflowWrap: 'anywhere', flex: '1 1 120px' }}>{detail.data.name || `#${detail.data.line_id}`}</Text><ChangeBadge change={detail.data} now={now} connected={detailConnected} /></Group>
              <Text size="sm">{t(`lineChanges.kind.${detail.data.kind}`)} · {t('lineChanges.version', { version: detail.data.version })}</Text>
              <Text size="xs" c="dimmed">{t('lineChanges.created', { at: when(detail.data.created_at) })}</Text>
              <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{t('lineChanges.actor', { actor: detail.data.actor || '—' })}</Text>
              {detail.data.superseded_at && <Text size="xs" c="dimmed">{t('lineChanges.supersededAt', { at: when(detail.data.superseded_at) })}</Text>}
              <Text size="xs" c="dimmed">{t('lineChanges.historyHint')}</Text>
            </Stack></Card>
            {!detail.data.targets.length && <Alert color="gray">{t('lineChanges.noChanges')}</Alert>}
            {detail.data.targets.map(target => <TargetCard key={target.node_id} target={target} now={now} connected={detailConnected} />)}
          </>}
        </Stack>
      </SimpleGrid>
    </Stack>
  </Modal>
}

function ChangeBadge({ change, now, connected }: { change: ObservedLineChange; now: EvidenceClock; connected: boolean }) {
  const { t } = useTranslation()
  const status = lineChangeStatus(change, now, connected)
  return <Badge color={color(status)} style={{ flexShrink: 0 }}>{t(`lineChanges.status.${status}`)}</Badge>
}

function TargetCard({ target, now, connected }: { target: ObservedLineTarget; now: EvidenceClock; connected: boolean }) {
  const { t } = useTranslation()
  const status = lineTargetStatus(target, now, connected)
  return <Card withBorder padding="sm"><Stack gap="xs">
    <Group justify="space-between" align="flex-start"><Text fw={600} style={{ overflowWrap: 'anywhere', flex: '1 1 100px' }}>{target.node_name || `#${target.node_id}`}</Text><Badge color={color(status)}>{t(`lineChanges.targetStatus.${status}`)}</Badge></Group>
    <Text size="sm">{t(`lineChanges.action.${target.action}`)}</Text>
    {status === 'not_applied' && <Text size="xs" c="dimmed">{t('lineChanges.notAppliedHint')}</Text>}
    <Text size="xs" c="dimmed">{t('lineChanges.observed', { at: when(target.observed_at) })}</Text>
    <Text size="xs" c="dimmed">{t('lineChanges.confirmed', { at: when(target.confirmed_at) })}</Text>
    <Accordion variant="contained"><Accordion.Item value="configuration"><Accordion.Control>{t('lineChanges.details')}</Accordion.Control><Accordion.Panel>
      <Stack gap="xs">
        <Text size="xs">{t('nodeConfiguration.structure')} <Code title={target.config_revision}>{short(target.config_revision) || '—'}</Code></Text>
        <Text size="xs">{t('nodeConfiguration.state')} <Code title={target.state_revision}>{short(target.state_revision) || '—'}</Code></Text>
        <Text size="xs">{t('nodeConfiguration.delivery')} <Code title={target.delivery_id}>{short(target.delivery_id) || '—'}</Code></Text>
        <Text size="xs" c="dimmed">{t('lineChanges.issued', { at: when(target.issued_at) })}</Text>
        {(['before', 'after'] as const).map(side => <Box key={side} style={{ minWidth: 0 }}><Text size="xs" fw={600} mb={4}>{t(`lineChanges.${side}`)}</Text><Code block style={{ maxHeight: 240, overflow: 'auto', fontSize: 11 }}>{JSON.stringify(target[side], null, 2)}</Code></Box>)}
      </Stack>
    </Accordion.Panel></Accordion.Item></Accordion>
  </Stack></Card>
}
