import { Alert, Badge, Button, Card, Code, Group, Modal, Select, Stack, Switch, Table, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { modals } from '@mantine/modals'
import { api } from '../../lib/api'
import { type AgentEndpoint, type EndpointView } from '../../lib/agent-connections'
import { agentHTTPSDockerCommand, agentHTTPSCheckCommand } from '../../lib/agent-https'
import { evidenceFresh, useEvidenceClock } from '../../lib/evidence-clock'
import { toast } from '../../lib/notify'
import { AgentEndpointUsage } from '../AgentEndpointUsage'
import { Copy } from '../Copy'
import { useSettingsDirty } from '../../lib/settings-draft'

const empty: AgentEndpoint = { id: '', name: '', enabled: true, url: '', listen: ':2096', certificate_id: 0, revision: '', default: false, users: 0, pending: 0 }
export default function AgentEndpointsCard() {
 const { t } = useTranslation(); const clock=useEvidenceClock(); const qc = useQueryClient(); const [opened, setOpened] = useState(false)
 const q = useQuery({ queryKey: ['agent-endpoints'], queryFn: async ({ signal }) => { const start = { wall: Date.now(), mono: performance.now() }; const view = await api.get<EndpointView>('/api/admin/settings/agent-endpoints', signal); return { view, start } }, refetchInterval: 5000, retry: false })
 const certs = useQuery({ queryKey: ['certificates'], queryFn: ({ signal }) => api.get<{ certificates: Array<{ id: number; name: string; names: string[] }> }>('/api/admin/certificates', signal), retry: false })
 const [usage,setUsage]=useState<AgentEndpoint|null>(null)
 const form = useForm({ initialValues: empty }); useSettingsDirty(opened && form.isDirty())
 const fresh = !!q.data && !q.isError && evidenceFresh(q.data.start,clock)
 const checkGeneration=useRef(0)
 const [checks,setChecks]=useState<Record<string,{revision:string;status:string;start:{wall:number;mono:number}}>>({})
 const check=useMutation({mutationFn:async(e:AgentEndpoint)=>{const generation=checkGeneration.current;const start={wall:Date.now(),mono:performance.now()};const result=await api.post<{status:string}>(`/api/admin/settings/agent-endpoints/${e.id}/check`,{revision:e.revision});return {id:e.id,revision:e.revision,status:result.status,start,generation}},onSuccess:r=>{if(r.generation===checkGeneration.current)setChecks(old=>({...old,[r.id]:r}))},onError:toast.err})
 const revisions=(q.data?.view.entries??[]).map(e=>e.id+':'+e.revision).join('|')
 // A failed settings read or a changed entry revokes prior diagnostic evidence.
 // oxlint-disable-next-line react/set-state-in-effect
 useEffect(()=>{checkGeneration.current++;setChecks({})},[q.isError,revisions])
 const savedEntry=q.data?.view.entries.find(e=>e.id===form.values.id)
 const refresh = () => { void qc.invalidateQueries({ queryKey: ['agent-endpoints'] }); void qc.invalidateQueries({ queryKey: ['agent-https-choice'] }); void qc.invalidateQueries({ queryKey: ['nodes'] }) }
 const save = useMutation({ mutationFn: (v: AgentEndpoint) => v.id ? api.put(`/api/admin/settings/agent-endpoints/${v.id}`, v) : api.post('/api/admin/settings/agent-endpoints', v), onSuccess: () => { setOpened(false); refresh(); toast.ok(t('common.saved')) }, onError: toast.err })
 const remove = useMutation({ mutationFn: (v: AgentEndpoint) => api.del(`/api/admin/settings/agent-endpoints/${v.id}`, { revision: v.revision }), onSuccess: refresh, onError: toast.err })
 const edit = (value: AgentEndpoint) => { form.setValues(value); form.resetDirty(value); setOpened(true) }
 const close = () => { if (save.isPending) return; if (form.isDirty()) { modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('common.cancel') }, onConfirm: () => setOpened(false) }) } else setOpened(false) }
 return <Card><Stack>
  <Group justify="space-between"><Title order={5}>{t('agentEntries.title')}</Title><Button onClick={() => edit({ ...empty })}>{t('agentEntries.add')}</Button></Group>
  <Text size="sm" c="dimmed">{t('agentEntries.hint')}</Text>
  {q.isError && <Alert color="red">{t('agentEntries.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
  <Table.ScrollContainer minWidth={1060}><Table><Table.Thead><Table.Tr><Table.Th>{t('agentEntries.name')}</Table.Th><Table.Th>{t('agentConnection.url')}</Table.Th><Table.Th>{t('agentEntries.state')}</Table.Th><Table.Th>{t('agentEntries.references')}</Table.Th><Table.Th>{t('agentEntries.actions')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>
   {(q.data?.view.entries ?? []).map(e => <Table.Tr key={e.id}><Table.Td>{e.name} {e.default && <Badge size="xs">{t('agentEntries.default')}</Badge>}</Table.Td><Table.Td miw={220} style={{ overflowWrap: 'anywhere' }}>{e.url}</Table.Td><Table.Td miw={150}><Badge style={{maxWidth:'none'}} color={fresh && q.data?.view.statuses[e.id]?.state === 'listening' ? 'teal' : 'gray'}>{t(`agentEntries.${!fresh ? 'unknown' : q.data?.view.statuses[e.id]?.state || 'disabled'}`)}</Badge></Table.Td><Table.Td>{t('agentEntries.usage', { reported: e.users, configured: e.configured_users ?? 0, pending: e.pending, unknown: e.unknown_users ?? 0 })}</Table.Td><Table.Td><Group gap="xs"><Button variant="subtle" onClick={()=>setUsage(e)}>{t('agentEntries.usageNodes')}</Button><Button variant="subtle" loading={check.isPending && check.variables?.id===e.id} disabled={!fresh||!e.enabled||check.isPending} onClick={()=>{setChecks(old=>{const next={...old};delete next[e.id];return next});check.mutate(e)}}>{t('agentHTTPS.access.check')}</Button>{checks[e.id]?.revision===e.revision&&evidenceFresh(checks[e.id]?.start,clock)&&!q.isError&&<Text size="xs">{t(`agentHTTPS.access.results.${checks[e.id].status}`)}</Text>}<Button variant="subtle" onClick={() => edit(e)}>{t('common.edit')}</Button>{!e.default && e.enabled && <Button variant="subtle" onClick={() => save.mutate({ ...e, default: true })}>{t('agentEntries.makeDefault')}</Button>}<Button color="red" variant="subtle" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text>{t('agentEntries.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, onConfirm: () => remove.mutate(e) })}>{t('common.delete')}</Button></Group></Table.Td></Table.Tr>)}
  </Table.Tbody></Table></Table.ScrollContainer>
  <Text size="xs" c="dimmed">{t('agentEntries.referenceHint')}</Text>
  {usage&&<AgentEndpointUsage entry={usage} close={()=>setUsage(null)} />}
  <Modal opened={opened} onClose={close} closeOnClickOutside={false} title={t('agentEntries.entry')} size="lg"><form onSubmit={form.onSubmit(v => save.mutate(v))}><fieldset disabled={save.isPending} style={{ border: 0, padding: 0 }}><Stack>
   <TextInput required label={t('agentEntries.name')} {...form.getInputProps('name')} />
   <TextInput required label={t('agentConnection.url')} placeholder="https://agent.example.com:2096" {...form.getInputProps('url')} onChange={e => { form.setFieldValue('url', e.currentTarget.value); try { form.setFieldValue('listen', `:${new URL(e.currentTarget.value).port || 443}`) } catch { /* unfinished URL */ } }} />
   <TextInput label={t('agentHTTPS.listen')} {...form.getInputProps('listen')} />
   <Select required label={t('agentHTTPS.certificate')} data={(certs.data?.certificates ?? []).map(c => ({ value: String(c.id), label: `${c.name} · ${c.names.join(', ')}` }))} value={form.values.certificate_id ? String(form.values.certificate_id) : null} onChange={v => form.setFieldValue('certificate_id', Number(v) || 0)} />
   <Switch label={t('agentEntries.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
   <Switch label={t('agentEntries.makeDefault')} disabled={!!form.values.id&&!!q.data?.view.entries.find(e=>e.id===form.values.id)?.default} description={t('agentEntries.defaultHint')} {...form.getInputProps('default', { type: 'checkbox' })} />
   {form.values.id && !form.isDirty() && savedEntry && <><Text size="xs">{t('agentEntries.checkHint')}</Text><Group><Code style={{ overflowWrap: 'anywhere', flex: 1 }}>{agentHTTPSCheckCommand(savedEntry?.url || '')}</Code><Copy value={agentHTTPSCheckCommand(savedEntry?.url || '') || ''} /></Group><Text size="xs">{t('agentEntries.dockerHint')}</Text><Code block>{agentHTTPSDockerCommand(savedEntry?.url || '', true)}</Code></>}
   <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
  </Stack></fieldset></form></Modal>
 </Stack></Card>
}
