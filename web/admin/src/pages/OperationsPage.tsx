import { Alert, Badge, Button, Card, Code, Group, Loader, NumberInput, Select, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { observedRequest } from '../lib/observed-request'
import { useObservationClock } from '../lib/use-observation-clock'
import { operationDetailURL, operationPageURL, operationSourceURL, operationSources, operationStatuses, operationStepFresh, operationsURL, type Operation, type OperationFilters, type OperationPage, type OperationSource } from '../lib/operations'
import { PageHeader } from '../components/PageHeader'

export default function OperationsPage() {
  const { id } = useParams(), location = useLocation()
  return id ? <OperationDetail key={id} id={id} /> : <OperationsList key={location.search} />
}
function OperationsList() {
  const { t, i18n } = useTranslation(), [params, setParams] = useSearchParams()
  const filters: OperationFilters = Object.fromEntries(['source', 'status', 'node_id', 'line_id'].map(key => [key, params.get(key) || '']))
  const [draft, setDraft] = useState(filters), [pages, setPages] = useState<string[]>([])
  const q = useQuery({ queryKey: ['operations', filters, pages.at(-1)], queryFn: ({ signal }) => observedRequest(signal, signal => api.get<OperationPage>(operationsURL(filters, pages.at(-1)), signal)), retry: false, refetchInterval: 15000 })
  const page = !q.isError ? q.data?.value : undefined
  const statuses = draft.source ? operationStatuses[draft.source as OperationSource] ?? [] : [...new Set(Object.values(operationStatuses).flat())]
  const date = (value: string) => Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(i18n.language) : '—'
  return <Stack gap="lg">
    <PageHeader title={t('operations.title')} subtitle={t('operations.hint')} />
    <Card withBorder component="form" onSubmit={event => { event.preventDefault(); setPages([]); setParams(Object.fromEntries(Object.entries(draft).filter(([, value]) => value)), { replace: false }) }}><Stack>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
        <Select label={t('operations.sourceLabel')} clearable value={draft.source || null} data={operationSources.map(value => ({ value, label: t(`operations.source.${value}`) }))} onChange={value => setDraft(old => ({ ...old, source: value || '', status: '' }))} />
        <Select label={t('operations.statusLabel')} clearable value={draft.status || null} data={statuses.map(value => ({ value, label: t(`operations.status.${value}`, { defaultValue: value }) }))} onChange={value => setDraft(old => ({ ...old, status: value || '' }))} />
        <NumberInput label={t('operations.nodeID')} value={draft.node_id || ''} min={1} max={Number.MAX_SAFE_INTEGER} allowDecimal={false} onChange={value => setDraft(old => ({ ...old, node_id: String(value) }))} />
        <NumberInput label={t('operations.lineID')} value={draft.line_id || ''} min={1} max={Number.MAX_SAFE_INTEGER} allowDecimal={false} onChange={value => setDraft(old => ({ ...old, line_id: String(value) }))} />
      </SimpleGrid>
      <Group><Button type="submit" size="xs">{t('operations.filter')}</Button><Button size="xs" variant="subtle" onClick={() => { setDraft({}); setPages([]); setParams({}) }}>{t('operations.clear')}</Button><Button size="xs" variant="default" loading={q.isFetching} onClick={() => q.refetch()}>{t('common.refresh')}</Button></Group>
    </Stack></Card>
    {q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="red">{t('operations.loadError')}</Alert>}
    {!q.isPending && page && !page.items.length && <Alert color="gray">{t('operations.empty')}</Alert>}
    {page?.items.map(item => <Card key={item.id} withBorder><Stack gap="sm">
      <Group justify="space-between" align="flex-start"><Stack gap={4} style={{ minWidth: 0, flex: '1 1 180px' }}><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{item.name || t(`operations.source.${item.source}`, { defaultValue: item.source })}</Text><Text size="sm">{t(`operations.source.${item.source}`, { defaultValue: item.source })} · {t(`operations.kind.${item.kind}`, { defaultValue: item.kind })}</Text></Stack><Badge color="gray">{t(`operations.status.${item.status}`, { defaultValue: item.status })}</Badge></Group>
      <Text size="xs" c="dimmed">{t('operations.created')}: {date(item.created_at)}{item.actor ? ` · ${item.actor}` : ''}</Text>
      {item.node_id && <Text size="sm">{t('operations.nodeID')}: {item.node_id}</Text>}
      {item.line_id && <Text size="sm">{t('operations.lineID')}: {item.line_id}</Text>}
      <Group justify="space-between"><Text size="xs" c="dimmed">{t('operations.historical')}</Text><Button component={Link} to={operationPageURL(item.id)} size="xs" variant="light">{t('operations.open')}</Button></Group>
    </Stack></Card>)}
    <Group justify="space-between"><Button variant="default" disabled={!pages.length || q.isFetching} onClick={() => setPages(old => old.slice(0, -1))}>{t('operations.newer')}</Button><Button variant="default" disabled={!page?.next_before || q.isFetching || q.isError} onClick={() => { if (page?.next_before) setPages(old => [...old, page.next_before!]) }}>{t('operations.older')}</Button></Group>
  </Stack>
}
function OperationDetail({ id }: { id: string }) {
  const { t, i18n } = useTranslation(), clock = useObservationClock()
  const q = useQuery({ queryKey: ['operation', id], queryFn: ({ signal }) => observedRequest(signal, signal => api.get<Operation>(operationDetailURL(id), signal)), retry: false, refetchInterval: 15000 })
  const item = !q.isError ? q.data?.value : undefined, origin = item && operationSourceURL(item)
  const available = clock.available && !q.isError && q.fetchStatus !== 'paused'
  const date = (value?: string) => value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(i18n.language) : '—'
  return <Stack gap="lg">
    <PageHeader title={t('operations.detail')} actions={<Button component={Link} to="/operations" variant="default" size="xs">{t('operations.back')}</Button>} />
    <Alert color="blue">{t('operations.hint')}</Alert>
    {q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="red"><Text>{t('operations.detailError')}</Text><Button variant="subtle" onClick={() => q.refetch()}>{t('common.refresh')}</Button></Alert>}
    {item && <>
      <Card withBorder><Stack gap="sm">
        <Group justify="space-between"><Title order={4} style={{ overflowWrap: 'anywhere' }}>{item.name || t(`operations.source.${item.source}`, { defaultValue: item.source })}</Title><Badge color="gray">{t(`operations.status.${item.status}`, { defaultValue: item.status })}</Badge></Group>
        <Text size="sm">{t(`operations.source.${item.source}`, { defaultValue: item.source })} · {t(`operations.kind.${item.kind}`, { defaultValue: item.kind })}</Text>
        {item.actor && <Text size="sm">{t('operations.actor')}: {item.actor}</Text>}
        <SimpleGrid cols={{ base: 1, sm: 3 }}>{(['created_at', 'updated_at', 'completed_at'] as const).map(key => <div key={key}><Text size="xs" c="dimmed">{t(`operations.${key}`)}</Text><Text size="sm">{date(item[key])}</Text></div>)}</SimpleGrid>
        {item.stage && <Text size="sm">{t('operations.stage')}: {t(`operations.phase.${item.stage}`, { defaultValue: item.stage })}</Text>}
        {item.action && <Text size="sm">{t('operations.actionLabel')}: {t(`operations.action.${item.action}`, { defaultValue: item.action })}</Text>}
        {item.outcome && <Text size="sm">{t('operations.outcomeLabel')}: {t(`operations.outcome.${item.outcome}`, { defaultValue: item.outcome })}</Text>}
        {item.status === 'waiting' && <Alert color="gray">{t('operations.waitingHint')}</Alert>}
        {item.target_version && <Text size="sm">{t('operations.targetVersion')}: {item.target_version}</Text>}
        {item.source === 'topology_changeset' && <Alert color="gray">{t('operations.topologyHint')}</Alert>}
        {item.source === 'node_upgrade' && <Alert color="gray">{t('operations.upgradeHint')}</Alert>}
        {item.error_code && <Text size="sm">{t('operations.errorCode')}: <Code style={{ overflowWrap: 'anywhere', whiteSpace: 'normal' }}>{item.error_code}</Code></Text>}
        <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{item.id}</Text>
        <Group><Button size="xs" variant="default" loading={q.isFetching} onClick={() => q.refetch()}>{t('common.refresh')}</Button>{origin && <Button component={Link} size="xs" variant="subtle" to={origin}>{t(['node_job', 'node_upgrade'].includes(item.source) ? 'operations.openNode' : 'operations.openSource')}</Button>}</Group>
      </Stack></Card>
      {!!item.steps?.length && <Stack><Title order={4}>{t('operations.steps')}</Title><Text size="sm" c="dimmed">{t('operations.stepsHint')}</Text>{item.steps.map(step => <Card key={step.id} withBorder><Stack gap="xs">
        <Group justify="space-between"><Text fw={600}>{step.node_name || `#${step.node_id}`}</Text><Badge color="gray">{t(`operations.status.${step.status}`, { defaultValue: step.status })}</Badge></Group>
        <Text size="sm">{t(`operations.phase.${step.phase}`, { defaultValue: step.phase })} · {t('operations.stepOrder', { order: step.order, attempt: step.attempt })}</Text>
        <Text size="sm">{t('operations.confirmed')}: {date(step.confirmed_at)}</Text>
        <Text size="sm">{t('operations.observed')}: {date(step.observed_at)} · {t('operations.received')}: {date(step.last_receipt_at)}</Text>
        <Badge color={q.data && operationStepFresh(step, q.data, clock.now, available) ? 'blue' : 'gray'} w="fit-content">{t(q.data && operationStepFresh(step, q.data, clock.now, available) ? 'operations.stepFresh' : 'operations.stepUnknown')}</Badge>
      </Stack></Card>)}</Stack>}
      {!!item.attempts?.length && <Card withBorder><Stack><Title order={4}>{t('operations.attempts')}</Title>{item.attempts.map((attempt, index) => <Stack key={`${attempt.step_id}-${attempt.attempt}-${index}`} gap={3}>
        <Text size="xs" style={{ overflowWrap: 'anywhere' }}>{attempt.step_id} · {t('operations.attemptNumber', { attempt: attempt.attempt })}</Text><Text size="sm">{t('operations.observed')}: {date(attempt.observed_at)} · {t(attempt.applied ? 'operations.attemptApplied' : 'operations.attemptUnconfirmed')}</Text><Text size="xs" c="dimmed">{t('operations.received')}: {date(attempt.received_at)}</Text>
      </Stack>)}{item.attempts_truncated && <Alert color="gray">{t('operations.attemptsTruncated')}</Alert>}</Stack></Card>}
      {!!item.related?.length && <Card withBorder><Stack><Title order={4}>{t('operations.related')}</Title>{item.related.map(relation => <Group key={`${relation.relation}-${relation.operation_id}`}><Text size="sm">{t(`operations.relation.${relation.relation}`)}</Text>{relation.available ? <Button component={Link} size="xs" variant="subtle" to={operationPageURL(relation.operation_id)}>{t('operations.open')}</Button> : <Text size="sm" c="dimmed">{t('operations.unavailable')}</Text>}</Group>)}</Stack></Card>}
    </>}
  </Stack>
}
