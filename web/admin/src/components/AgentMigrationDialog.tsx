import { Alert, Badge, Button, Group, Modal, Select, Stack, Table, Text, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { connectionChoice, endpointChoices, type AgentConnection, type ConnectionChoice } from '../lib/agent-connections'
import { evidenceFresh, useEvidenceClock } from '../lib/evidence-clock'
import { toast } from '../lib/notify'

export function AgentMigrationDialog({ ids, opened, close }: { ids: number[]; opened: boolean; close: () => void }) {
 const { t } = useTranslation(); const clock=useEvidenceClock(); const qc = useQueryClient()
 const [choice, setChoice] = useState<ConnectionChoice>(connectionChoice)
 const [preview, setPreview] = useState<{ signature: string; revision: string; target_url: string; nodes: AgentConnection[] } | null>(null)
 const entries = useQuery({ queryKey: ['agent-https-choice'], queryFn: ({ signal }) => endpointChoices(signal), enabled: opened, refetchInterval: opened ? 5000 : false, retry: false })
 const request = { ...choice, node_ids: ids }; const signature = JSON.stringify(request)
 const check = useMutation({ onMutate:()=>setPreview(null), mutationFn: () => api.post<{ revision: string; target_url: string; nodes: AgentConnection[] }>('/api/admin/agent-connections/preview', request), onSuccess: result => setPreview({ ...result, signature }), onError: toast.err })
 const migrate = useMutation({ mutationFn: () => api.post('/api/admin/agent-connections/migrate', { ...request, revision: preview?.revision }), onSuccess: () => { toast.ok(t('agentEntries.queued')); void qc.invalidateQueries({ queryKey: ['node-connection'] }); void qc.invalidateQueries({ queryKey: ['connection-migrations'] }); void qc.invalidateQueries({ queryKey: ['nodes'] }); close(); setPreview(null) }, onError: error=>{setPreview(null);void qc.invalidateQueries({queryKey:['connection-migrations']});toast.err(error)} })
 const pending = check.isPending || migrate.isPending
 const change = (v: ConnectionChoice) => { setChoice(v); setPreview(null) }
 const ready = preview?.signature === signature
 return <Modal opened={opened} onClose={() => { if (!pending) { setPreview(null); close() } }} title={t('agentEntries.migrate')} size="xl" closeOnClickOutside={false}>
  <Stack><Text size="sm">{t('agentEntries.migrationHint')}</Text>
   <Select label={t('agentEntries.mode')} value={choice.mode} allowDeselect={false} disabled={pending} onChange={v => change({ ...choice, mode: v as ConnectionChoice['mode'] })} data={['default', 'managed', 'custom'].map(value => ({ value, label: t(`agentEntries.${value}`) }))} />
   {choice.mode === 'managed' && <Select label={t('agentEntries.entry')} searchable disabled={pending || entries.isError} value={choice.endpoint_id || null} data={(entries.data?.entries ?? []).map(e => ({ value: e.id, label: `${e.name} · ${e.url}`, disabled: !e.ready || entries.isError || !evidenceFresh(entries.data?.start,clock) }))} onChange={value => change({ ...choice, endpoint_id: value ?? '' })} />}
   {choice.mode === 'custom' && <TextInput label={t('agentConnection.url')} placeholder="https://agent.example.com:2096" disabled={pending} value={choice.url} onChange={e => change({ ...choice, url: e.currentTarget.value })} />}
   {entries.isError && <Alert color="yellow">{t('agentEntries.loadError')} <Button variant="subtle" onClick={() => entries.refetch()}>{t('workspace.retry')}</Button></Alert>}
   <Group><Badge>{t('agentEntries.selected', { count: ids.length })}</Badge><Text size="xs" c="dimmed">{t('agentEntries.concurrency')}</Text></Group>
   {ready && <><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{preview.target_url}</Text><Table.ScrollContainer minWidth={540}><Table><Table.Thead><Table.Tr><Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('agentEntries.actual')}</Table.Th><Table.Th>{t('agentEntries.state')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{preview.nodes.map(n => <Table.Tr key={n.node_id}><Table.Td>{n.name || `#${n.node_id}`}</Table.Td><Table.Td style={{ overflowWrap: 'anywhere' }}>{n.url || '—'}</Table.Td><Table.Td>{t(!n.paired ? 'agentEntries.installOnly' : n.capable ? 'agentEntries.ready' : 'agentEntries.upgradeNeeded')}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer></>}
   <Group justify="flex-end"><Button variant="default" disabled={pending} onClick={close}>{t('common.cancel')}</Button><Button variant="light" loading={check.isPending} disabled={pending || !ids.length || (choice.mode === 'managed' && !choice.endpoint_id) || (choice.mode === 'custom' && !choice.url)} onClick={() => check.mutate()}>{t('agentEntries.preview')}</Button><Button disabled={!ready || pending} loading={migrate.isPending} onClick={() => migrate.mutate()}>{t('agentEntries.start')}</Button></Group>
  </Stack>
 </Modal>
}
