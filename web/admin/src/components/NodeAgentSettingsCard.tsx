import { Alert, Badge, Button, Card, Group, Modal, Select, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { api, type Node } from '../lib/api'
import { type AgentConnection } from '../lib/agent-connections'
import { nodeEditValues } from '../lib/node-edit'
import { toast } from '../lib/notify'
import { AgentMigrationDialog } from './AgentMigrationDialog'
export function NodeAgentSettingsCard({ node }: { node: Node }) {
 const { t } = useTranslation(); const isAdmin=useAuth().me?.role === 'admin'; const qc = useQueryClient(); const [migrating, setMigrating] = useState(false); const [downloads, setDownloads] = useState(false)
 const [source, setSource] = useState(node.download_source || 'github')
 const q = useQuery({ queryKey: ['node-connection', node.id], queryFn: ({ signal }) => api.get<AgentConnection>(`/api/admin/nodes/${node.id}/agent-connection`, signal), refetchInterval: 5000, retry: false })
 const save = useMutation({ mutationFn: async () => { const latest = await api.get<{ node: Node }>(`/api/admin/nodes/${node.id}`); return api.patch(`/api/admin/nodes/${node.id}`, { ...nodeEditValues(latest.node), DownloadSource: source }) }, onSuccess: () => { setDownloads(false); void qc.invalidateQueries({ queryKey: ['node'] }); toast.ok(t('common.saved')) }, onError: toast.err })
 return <><Card mb="lg"><Stack gap="xs">
  <Group justify="space-between"><Text fw={600}>{t('agentConnection.title')}</Text><Group>{isAdmin && <Button size="xs" variant="light" onClick={() => setMigrating(true)}>{t('agentEntries.migrate')}</Button>}<Button size="xs" variant="subtle" onClick={() => { setSource(node.download_source || 'github'); setDownloads(true) }}>{t('agentConnection.source')}</Button></Group></Group>
  <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('agentEntries.actual')}: {q.data?.url || '—'}</Text>
  <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('agentEntries.target')}: {q.data?.target_url || node.effective_agent_url || '—'}</Text>
  <Group><Badge color={q.isError ? 'gray' : q.data?.status === 'completed' || q.data?.status === 'connected' ? 'teal' : 'orange'}>{t(`agentEntries.${q.isError ? 'unknown' : q.data?.status || 'unknown'}`)}</Badge><Text size="xs" c="dimmed">{t('agentEntries.observedHint')}</Text></Group>
  {q.data?.error && <Alert color="yellow">{q.data.error}</Alert>}
  {q.isError && <Alert color="yellow">{t('agentEntries.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
  <Text size="sm">{t('agentConnection.source')}: {t(node.download_source === 'captain' ? 'agentConnection.captain' : 'agentConnection.github')}</Text><Text size="xs" c="dimmed">{t('agentEntries.downloadHint')}</Text>
 </Stack></Card>
 <AgentMigrationDialog ids={[node.id]} opened={migrating} close={() => setMigrating(false)} />
 <Modal opened={downloads} onClose={() => { if (!save.isPending) setDownloads(false) }} title={t('agentConnection.source')}><Stack><Text size="sm">{t('agentEntries.downloadHint')}</Text><Select value={source} allowDeselect={false} onChange={v => setSource(v === 'captain' ? 'captain' : 'github')} data={[{ value: 'github', label: t('agentConnection.github') }, { value: 'captain', label: t('agentConnection.captain') }]} /><Button loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button></Stack></Modal>
 </>
}
