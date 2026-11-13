import { Alert, Badge, Button, Card, Group, Stack, Table, Text } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { type Migration } from '../lib/agent-connections'
import { toast } from '../lib/notify'
export function AgentMigrationProgress() {
 const { t } = useTranslation()
 const q = useQuery({ queryKey: ['connection-migrations'], queryFn: ({ signal }) => api.get<{ migrations: Migration[] }>('/api/admin/agent-connections', signal), refetchInterval: 5000, retry: false })
 const action = useMutation({ mutationFn: ({ id, action }: { id: string; action: 'retry' | 'cancel' }) => api.post(`/api/admin/agent-connections/${id}/${action}`, {}), onSuccess: () => { void q.refetch() }, onError: toast.err })
 if (!q.data?.migrations.length && !q.isError) return null
 return <Card mt="md"><Stack><Text fw={600}>{t('agentEntries.progress')}</Text>{q.isError && <Alert color="yellow">{t('agentEntries.loadError')} <Button onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}<Table.ScrollContainer minWidth={650}><Table><Table.Thead><Table.Tr><Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('agentEntries.target')}</Table.Th><Table.Th>{t('agentEntries.state')}</Table.Th><Table.Th>{t('agentEntries.actions')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{q.data?.migrations.slice(0, 100).map(m => <Table.Tr key={m.id}><Table.Td>{m.name || `#${m.node_id}`}</Table.Td><Table.Td style={{ overflowWrap: 'anywhere' }}>{m.target_url}<Text size="xs" c="dimmed">{m.error}</Text></Table.Td><Table.Td><Badge color={m.status === 'completed' ? 'teal' : m.status === 'failed' ? 'red' : 'blue'}>{t(`agentEntries.${m.status}`)}</Badge></Table.Td><Table.Td><Group>{m.status === 'failed' && <Button variant="subtle" loading={action.isPending} onClick={() => action.mutate({ id: m.id, action: 'retry' })}>{t('workspace.retry')}</Button>}{m.status === 'queued' && <Button variant="subtle" loading={action.isPending} onClick={() => action.mutate({ id: m.id, action: 'cancel' })}>{t('common.cancel')}</Button>}</Group></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer></Stack></Card>
}
