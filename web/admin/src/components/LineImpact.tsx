import { Alert, Badge, Button, Code, Group, Loader, Modal, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type LineDependencyView, type LinePreview } from '../lib/api'
import { hostPort } from '../lib/ingress'

export function LineDependencies({ value }: { value: LineDependencyView }) {
  const { t } = useTranslation()
  const name = (id: number) => value.dependencies.find(node => node.node_id === id)?.name || `#${id}`
  return <Stack gap="sm">
    <Text size="sm" c="dimmed">{t('lineImpact.topologyHint')}</Text>
    <Group gap="xs">{value.dependencies.map((node, index) => <Badge key={`${node.node_id}-${index}`} variant="light">{node.name} · {t(`lineImpact.roles.${node.role}`)}</Badge>)}</Group>
    <Table.ScrollContainer minWidth={380}><Table><Table.Thead><Table.Tr><Table.Th>{t('lineImpact.from')}</Table.Th><Table.Th>{t('lineImpact.to')}</Table.Th><Table.Th>{t('lineImpact.connection')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{value.edges.map((edge, index) => <Table.Tr key={index}><Table.Td>{name(edge.from)}</Table.Td><Table.Td>{name(edge.to)}</Table.Td><Table.Td>{t(`lineImpact.edges.${edge.kind}`)}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
    <Title order={6}>{t('lineImpact.uses')}</Title>
    {value.uses.length ? value.uses.map(entry => <Text key={entry.entry_id} size="sm">#{entry.entry_id} {entry.name} · {t(`lineImpact.usage.${entry.kind}`)}{!entry.enabled && ` · ${t('common.disabled')}`}{entry.before && entry.after && JSON.stringify(entry.before) !== JSON.stringify(entry.after) && <Text component="span" display="block" size="xs">{t('lineImpact.entryChange')}: {hostPort(entry.before.host, entry.before.port)} → {hostPort(entry.after.host, entry.after.port)}</Text>}</Text>) : <Text size="sm" c="dimmed">{t('lineImpact.noUses')}</Text>}
  </Stack>
}
export function LineImpact({ value }: { value: LinePreview }) {
  const { t } = useTranslation()
  return <Stack>
    <Alert color="blue">{t('lineImpact.scope')}</Alert>
    <Title order={5}>{t('lineImpact.changes', { count: value.changes.length })}</Title>
    {value.changes.length === 0 && <Text size="sm">{t('lineImpact.noChanges')}</Text>}
    {value.changes.map(change => <Stack key={change.node_id} gap={4}>
      <Text fw={600}>{change.name} · #{change.node_id}</Text>
      <Table.ScrollContainer minWidth={540}><Table style={{ tableLayout: 'fixed' }}><Table.Thead><Table.Tr><Table.Th>{t('lineImpact.before')}</Table.Th><Table.Th>{t('lineImpact.after')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody><Table.Tr>{[change.before, change.after].map((forwards, index) => <Table.Td key={index} style={{ verticalAlign: 'top' }}><Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 240 }}>{JSON.stringify(forwards, null, 2)}</Code></Table.Td>)}</Table.Tr></Table.Tbody></Table></Table.ScrollContainer>
    </Stack>)}
    <Title order={6}>{t('lineImpact.endpoints')}</Title>
    <Group>{value.endpoints.map(endpoint => <Code key={endpoint.member_id}>{hostPort(endpoint.host, endpoint.port) || '—'}</Code>)}</Group>
    <Title order={5}>{t('lineImpact.dependencies')}</Title>
    <LineDependencies value={value} />
  </Stack>
}
export function LineDependenciesModal({ id, onClose }: { id: number; onClose: () => void }) {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['line-dependencies', id], queryFn: () => api.get<LineDependencyView>(`/api/admin/lines/${id}/dependencies`), staleTime: 0 })
  return <Modal opened onClose={onClose} title={t('lineImpact.dependencies')} size="lg">
    {query.isPending ? <Loader size="sm" /> : query.isError ? <Alert color="red"><Text>{query.error.message}</Text><Button variant="subtle" onClick={() => query.refetch()}>{t('entryFlow.retry')}</Button></Alert> : <LineDependencies value={query.data} />}
  </Modal>
}
