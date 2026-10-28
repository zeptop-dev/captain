import { Alert, Badge, Button, Card, Group, Loader, Modal, Stack, Table, Text } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { addressType, dnsFamilyLabel, dnsStatusColor, syncHasChanges, syncResultStatus, type DNSHealth, type DNSSyncPlan, type DNSSyncResult } from '../lib/dns-health'

export function DNSSyncModal({ nodeID, close }: { nodeID: number; close: () => void }) {
  const { t } = useTranslation()
  const mobile = useMediaQuery('(max-width: 47.99em)')
  const qc = useQueryClient()
  const [attempted, setAttempted] = useState(false)
  const plan = useQuery({
    queryKey: ['dns-sync', nodeID], queryFn: ({ signal }) => api.get<DNSSyncPlan>(`/api/admin/nodes/${nodeID}/dns-sync`, signal),
    retry: false, gcTime: 0, staleTime: 0, refetchOnMount: 'always', refetchOnWindowFocus: false,
  })
  const apply = useMutation({
    retry: false,
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: ['dns-health'] })
      // A provider request may succeed even when its response is lost. Retire
      // old green evidence before writing and do not let a late GET restore it.
      qc.setQueriesData<DNSHealth[]>({ queryKey: ['dns-health'] }, previous => previous?.map(health => health.node_id === nodeID ? { ...health, stale: true } : health))
    },
    mutationFn: (revision: string) => api.post<{ results: DNSSyncResult[] }>(`/api/admin/nodes/${nodeID}/dns-sync`, { revision }),
    onSettled: () => {
      for (const key of ['dns-health', 'dns-history', 'selfcheck']) qc.invalidateQueries({ queryKey: [key] })
    },
  })
  const reload = async () => {
    apply.reset()
    setAttempted(true)
    const result = await plan.refetch()
    if (!result.isError) setAttempted(false)
  }
  const reason = (code?: string) => t(`dnsHealth.sync.reasons.${code ?? 'unknown'}`, { defaultValue: t('dnsHealth.sync.reasons.unknown') })
  const data = !plan.isError ? plan.data : undefined
  const canApply = !plan.isFetching && !attempted && !!data && syncHasChanges(data)
  return <Modal opened onClose={() => { if (!apply.isPending) close() }} withCloseButton={!apply.isPending} closeOnClickOutside={false} closeOnEscape={!apply.isPending} size="xl" title={t('dnsHealth.sync.title')}>
    <Stack>
      <Text size="sm" c="dimmed">{t('dnsHealth.sync.hint')}</Text>
      {plan.isFetching && <Group gap="xs"><Loader size="sm" /><Text size="sm">{t('dnsHealth.sync.loading')}</Text></Group>}
      {plan.isError && <Alert color="red">{t('dnsHealth.sync.loadError')}</Alert>}
      {data && <>
        <Text fw={600} style={{ overflowWrap: 'anywhere' }}>{data.host || t('dnsHealth.sync.reasons.no_domain')}</Text>
        {data.reason && <Alert color="yellow">{reason(data.reason)}</Alert>}
        {data.records.length > 0 && (mobile ? <Stack gap="xs">{data.records.map(record => <Card key={record.type} withBorder p="sm"><Stack gap="xs">
          <Group justify="space-between" gap="xs"><Text size="sm" fw={600}>{dnsFamilyLabel(record.type)}</Text><Badge variant="light">{t(`dnsHealth.sync.actions.${record.action}`, { defaultValue: t('dnsHealth.sync.actions.skipped') })}</Badge></Group>
          <div><Text size="xs" c="dimmed">{t('dnsHealth.sync.before')}</Text><Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>{record.before?.content || '—'}</Text>{record.before && <Text size="xs" c="dimmed">TTL: {record.before.ttl === 1 ? t('dnsHealth.auto') : record.before.ttl} · {t('dnsHealth.proxy')}: {t(record.before.proxied ? 'dnsHealth.on' : 'dnsHealth.off')}</Text>}</div>
          <div><Text size="xs" c="dimmed">{t('dnsHealth.sync.expected')}</Text><Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>{record.expected || '—'}</Text></div>
          {record.reason && <Text size="xs" c="orange">{reason(record.reason)}</Text>}
        </Stack></Card>)}</Stack> : <Table.ScrollContainer minWidth={620}><Table verticalSpacing="sm">
          <Table.Thead><Table.Tr>{['record', 'before', 'expected', 'action'].map(key => <Table.Th key={key}>{t(`dnsHealth.sync.${key}`)}</Table.Th>)}</Table.Tr></Table.Thead>
          <Table.Tbody>{data.records.map(record => <Table.Tr key={record.type}>
            <Table.Td><Text size="sm" style={{ whiteSpace: 'nowrap' }}>{dnsFamilyLabel(record.type)}</Text></Table.Td>
            <Table.Td style={{ maxWidth: 240 }}><Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>{record.before?.content || '—'}</Text>{record.before && <Text size="xs" c="dimmed">TTL: {record.before.ttl === 1 ? t('dnsHealth.auto') : record.before.ttl} · {t('dnsHealth.proxy')}: {t(record.before.proxied ? 'dnsHealth.on' : 'dnsHealth.off')}</Text>}</Table.Td>
            <Table.Td style={{ maxWidth: 240 }}><Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>{record.expected || '—'}</Text></Table.Td>
            <Table.Td><Text size="sm">{t(`dnsHealth.sync.actions.${record.action}`, { defaultValue: t('dnsHealth.sync.actions.skipped') })}</Text>{record.reason && <Text size="xs" c="orange">{reason(record.reason)}</Text>}</Table.Td>
          </Table.Tr>)}</Table.Tbody>
        </Table></Table.ScrollContainer>)}
        {!data.reason && !syncHasChanges(data) && <Text size="sm" c="dimmed">{t('dnsHealth.sync.nothing')}</Text>}
      </>}
      {apply.isError && <Alert color={apply.error instanceof ApiError && apply.error.status === 409 ? 'yellow' : 'red'}>{t(apply.error instanceof ApiError && apply.error.status === 409 ? 'dnsHealth.sync.changed' : 'dnsHealth.sync.requestUnknown')}</Alert>}
      {apply.data && <Stack gap="xs">
        <Text fw={600}>{t('dnsHealth.sync.result')}</Text>
        {apply.data.results.map((result, index) => {
          const status = syncResultStatus(result), family = addressType(result.ip)
          return <Alert key={`${result.name}-${result.ip}-${index}`} color={status === 'unknown' || status === 'skipped' ? 'yellow' : dnsStatusColor(status)}>
            <Group gap="xs"><Text size="sm" fw={600}>{family ? dnsFamilyLabel(family) : result.name}</Text><Badge color={status === 'unknown' ? 'orange' : dnsStatusColor(status)}>{t(`dnsHealth.sync.outcomes.${status}`)}</Badge></Group>
            <Text size="sm" ff="monospace" style={{ overflowWrap: 'anywhere' }}>{result.ip || '—'}</Text>
            {result.reason && <Text size="xs">{reason(result.reason)}</Text>}
            {status === 'unknown' && <Text size="xs">{t('dnsHealth.sync.requestUnknown')}</Text>}
          </Alert>
        })}
        <Text size="sm" c="dimmed">{t('dnsHealth.sync.resultHint')}</Text>
      </Stack>}
      <Group justify="space-between">
        <Button variant="subtle" disabled={plan.isFetching || apply.isPending} onClick={() => void reload()}>{t('dnsHealth.sync.reload')}</Button>
        <Group><Button variant="default" disabled={apply.isPending} onClick={close}>{t('dnsHealth.sync.close')}</Button><Button disabled={!canApply} loading={apply.isPending} onClick={() => { if (data && canApply) { setAttempted(true); apply.mutate(data.revision) } }}>{t('dnsHealth.sync.apply')}</Button></Group>
      </Group>
    </Stack>
  </Modal>
}
