import { Alert, Badge, Button, Group, Loader, Modal, Stack, Table, Text } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { type Migration } from '../lib/agent-connections'
import { toast } from '../lib/notify'

export function AgentMigrationHistoryButton({ nodeID, onOpenedChange }: { nodeID?: number; onOpenedChange?: (opened: boolean) => void }) {
  const { t } = useTranslation()
  const [opened, setOpened] = useState(false)
  return <>
    <Button size="xs" variant="subtle" onClick={() => { setOpened(true); onOpenedChange?.(true) }}>{t('agentEntries.history')}</Button>
    <Modal opened={opened} onClose={() => { setOpened(false); onOpenedChange?.(false) }} title={t('agentEntries.history')} size="xl">
      {opened && <AgentMigrationProgress nodeID={nodeID} />}
    </Modal>
  </>
}

function AgentMigrationProgress({ nodeID }: { nodeID?: number }) {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['connection-migrations'], queryFn: ({ signal }) => api.get<{ migrations: Migration[] }>('/api/admin/agent-connections', signal), refetchInterval: 5000, retry: false })
  const action = useMutation({ mutationFn: ({ id, action }: { id: string; action: 'retry' | 'cancel' }) => api.post(`/api/admin/agent-connections/${id}/${action}`, {}), onSuccess: () => { void q.refetch() }, onError: toast.err })
  const migrations = (q.data?.migrations ?? []).filter(m => nodeID === undefined || m.node_id === nodeID)
  return <Stack>
    <Text size="sm" c="dimmed">{t('agentEntries.historyHint')}</Text>
    {q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="yellow">{t('agentEntries.loadError')} <Button onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
    {q.data && migrations.length === 0 && <Text size="sm" c="dimmed">{t('agentEntries.noHistory')}</Text>}
    {migrations.length > 0 && <Table.ScrollContainer minWidth={650}><Table>
      <Table.Thead><Table.Tr><Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('agentEntries.target')}</Table.Th><Table.Th>{t('agentEntries.state')}</Table.Th><Table.Th>{t('agentEntries.actions')}</Table.Th></Table.Tr></Table.Thead>
      <Table.Tbody>{migrations.map(m => <Table.Tr key={m.id}>
        <Table.Td>{m.name || `#${m.node_id}`}</Table.Td>
        <Table.Td style={{ overflowWrap: 'anywhere' }}>{m.target_url}{m.status !== 'completed' && m.error && <Text size="xs" c="dimmed">{t(`agentEntries.migrationErrors.${m.error}`, { defaultValue: m.error })}</Text>}</Table.Td>
        <Table.Td><Badge color={m.status === 'completed' ? 'teal' : m.status === 'failed' ? 'red' : 'blue'}>{t(`agentEntries.${m.status}`, { defaultValue: t('agentEntries.unknown') })}</Badge></Table.Td>
        <Table.Td><Group>{m.status === 'failed' && <Button variant="subtle" loading={action.isPending} onClick={() => action.mutate({ id: m.id, action: 'retry' })}>{t('workspace.retry')}</Button>}{m.status === 'queued' && <Button variant="subtle" loading={action.isPending} onClick={() => action.mutate({ id: m.id, action: 'cancel' })}>{t('common.cancel')}</Button>}</Group></Table.Td>
      </Table.Tr>)}</Table.Tbody>
    </Table></Table.ScrollContainer>}
  </Stack>
}
