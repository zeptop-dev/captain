import { Accordion, Alert, Badge, Button, Card, Code, Group, Loader, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { observedRequest } from '../lib/observed-request'
import { useObservationClock } from '../lib/use-observation-clock'
import { renderedConfigStatus, renderedConfigURL, type RenderedConfigView, type RenderedConfigRecord } from '../lib/rendered-config'

export function NodeRenderedConfig({ nodeID }: { nodeID: number }) {
  const { t, i18n } = useTranslation()
  const clock = useObservationClock()
  const query = useQuery({ queryKey: ['rendered-config', nodeID], queryFn: ({ signal }) => observedRequest(signal, signal => api.get<RenderedConfigView>(renderedConfigURL(nodeID), signal)), retry: false, refetchInterval: 15000 })
  const available = clock.available && !query.isError && query.fetchStatus !== 'paused'
  const status = renderedConfigStatus(query.data, clock.now, available), view = query.isError ? undefined : query.data?.value
  const date = (value?: string) => value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(i18n.language) : t('renderedConfig.unknownTime')
  const evidence = (record: RenderedConfigRecord) => <Stack gap="sm">
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
      <Text size="sm">{t('renderedConfig.received')}: {date(record.last_receipt_at)}</Text><Text size="sm">{t('renderedConfig.observedAt')}: {date(record.observed_at)}</Text>
      <Text size="sm">{t('renderedConfig.issued')}: {date(record.issued_at)}</Text><Text size="sm">{t('renderedConfig.confirmed')}: {date(record.confirmed_at)}</Text>
    </SimpleGrid>
    <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{t('renderedConfig.delivery')}: {record.id}</Text>
    {record.metadata_status !== 'valid' && <Alert color="gray">{t(`renderedConfig.metadata.${record.metadata_status}`)}</Alert>}
    {record.metadata_status === 'valid' && record.evidence?.status === 'unavailable' && <Alert color="gray">{t('renderedConfig.partial')}</Alert>}
    {record.metadata_status === 'valid' && (record.evidence?.artifacts ?? []).map((artifact, index) => <Card key={`${artifact.core}-${index}`} withBorder padding="sm"><Stack gap="xs">
      <Group justify="space-between"><Text fw={600}>{artifact.core}</Text><Badge color="gray">{artifact.format.toUpperCase()}</Badge></Group>
      <Text size="sm">{t('renderedConfig.activated')}: {date(artifact.activated_at)}</Text>
      <Text size="sm">{t('renderedConfig.counts', { files: artifact.files, processes: artifact.processes })}</Text>
      <Text size="xs" c="dimmed">{t('renderedConfig.digest')}</Text><Code style={{ whiteSpace: 'normal', overflowWrap: 'anywhere' }}>{artifact.digest}</Code>
      {artifact.runtime_updates && <Alert color="blue">{t('renderedConfig.runtimeUpdates')}</Alert>}
    </Stack></Card>)}
    {record.metadata_status === 'valid' && !record.evidence?.artifacts?.length && <Text size="sm" c="dimmed">{t('renderedConfig.noArtifacts')}</Text>}
  </Stack>
  return <Stack gap="lg">
    <Card withBorder><Stack gap="sm">
      <Group justify="space-between"><Title order={4}>{t('renderedConfig.title')}</Title><Badge color={status === 'observed' ? 'teal' : status === 'pending' ? 'orange' : 'gray'}>{t(`renderedConfig.status.${status}`)}</Badge></Group>
      <Text size="sm">{t('renderedConfig.hint')}</Text>
      <Text size="sm" c="dimmed">{t('renderedConfig.digestHint')}</Text>
      <Group><Button size="xs" variant="default" loading={query.isFetching} onClick={() => query.refetch()}>{t('common.refresh')}</Button><Button component={Link} to={`/operations?node_id=${nodeID}`} size="xs" variant="subtle">{t('operations.title')}</Button></Group>
      {query.isPending && <Loader size="sm" />}
      {query.isError && <Alert color="red">{t('renderedConfig.loadError')}</Alert>}
      {!available && <Text size="sm" c="dimmed">{t('renderedConfig.disconnected')}</Text>}
      {view && <Text size="xs" style={{ overflowWrap: 'anywhere' }}>{t('renderedConfig.expected')}: {view.config_revision || '—'} / {view.state_revision || '—'}</Text>}
    </Stack></Card>
    <Card withBorder><Stack gap="sm"><Title order={5}>{t('renderedConfig.latest')}</Title><Text size="sm" c="dimmed">{t('renderedConfig.latestHint')}</Text>{view?.current ? evidence(view.current) : <Text c="dimmed">{t('renderedConfig.empty')}</Text>}</Stack></Card>
    <Accordion variant="separated"><Accordion.Item value="history"><Accordion.Control>{t('renderedConfig.history')}</Accordion.Control><Accordion.Panel><Stack>
      <Text size="sm" c="dimmed">{t('renderedConfig.historyHint')}</Text>
      {(view?.history ?? []).map(record => <Card key={record.id} withBorder><Stack gap="sm"><Group justify="space-between"><Text size="sm">{date(record.issued_at)}</Text><Badge color="gray">{t('renderedConfig.historical')}</Badge></Group>{evidence(record)}</Stack></Card>)}
      {!view?.history?.length && <Text c="dimmed">{t('renderedConfig.empty')}</Text>}
    </Stack></Accordion.Panel></Accordion.Item></Accordion>
  </Stack>
}
