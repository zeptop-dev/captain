import { Accordion, Alert, Badge, Button, Group, Modal, Select, Stack, Table, Text, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { connectionChoice, endpointChoices, type AgentConnection, type ConnectionChoice } from '../lib/agent-connections'
import { evidenceFresh, useEvidenceClock } from '../lib/evidence-clock'
import { toast } from '../lib/notify'
interface Preview { revision: string; target_url: string; nodes: AgentConnection[]; ready?: boolean; trust_proxy?: boolean; expires_at?: number }
export function AgentMigrationDialog({ ids, opened, close }: { ids: number[]; opened: boolean; close: () => void }) {
 const { t } = useTranslation(), clock=useEvidenceClock(), qc=useQueryClient()
 const [choice,setChoice]=useState<ConnectionChoice>(connectionChoice)
 const [batch,setBatch]=useState<{id:string;signature:string}|null>(null)
 const [manual,setManual]=useState<{data:Preview;signature:string}|null>(null)
 const request={...choice,node_ids:ids},signature=JSON.stringify(request)
 const latest=useRef(signature);latest.current=signature
 const mode=choice.verification_mode || 'auto'
 const entries=useQuery({queryKey:['agent-https-choice'],queryFn:({signal})=>endpointChoices(signal),enabled:opened,refetchInterval:opened?5000:false,retry:false})
 const detection=useQuery({queryKey:['agent-connection-detect',batch?.id],enabled:opened&&mode==='auto'&&!!batch&&batch.signature===signature,
  queryFn:async({signal})=>{const start={wall:Date.now(),mono:performance.now()};const data=await api.get<Preview>(`/api/admin/agent-connections/detect/${batch!.id}`,signal);return {...data,start}},refetchInterval:2000,retry:false,gcTime:0,refetchOnWindowFocus:true})
 const check=useMutation({onMutate:()=>{setBatch(null);setManual(null)},mutationFn:async(input:{request:typeof request;signature:string})=>{
  if(input.request.verification_mode==='auto'){const data=await api.post<{batch_id:string}>('/api/admin/agent-connections/detect',input.request);return {batch:data.batch_id,signature:input.signature}}
  const data=await api.post<Preview>('/api/admin/agent-connections/preview',{...input.request,trust_proxy:input.request.verification_mode==='trusted-proxy'});return {data,signature:input.signature}
 },onSuccess:result=>{if(latest.current!==result.signature)return;if(result.batch)setBatch({id:result.batch,signature:result.signature});else if(result.data)setManual({data:result.data,signature:result.signature})},onError:toast.err})
 const autoData=mode==='auto'&&batch?.signature===signature&&!detection.isError&&evidenceFresh(detection.data?.start,clock)?detection.data:undefined
 const data=mode==='auto'?autoData:manual?.signature===signature?manual.data:undefined
 const needsTrust=mode==='trusted-proxy'||!!data?.trust_proxy
 const ready=!!data?.revision&&(mode!=='auto'||data.ready===true)
 const migrate=useMutation({mutationFn:()=>api.post('/api/admin/agent-connections/migrate',{...request,detection_id:mode==='auto'?batch?.id:undefined,trust_proxy:needsTrust,revision:data?.revision}),
  onSuccess:()=>{toast.ok(t('agentEntries.queued'));for(const key of ['node-connection','connection-migrations','nodes'])void qc.invalidateQueries({queryKey:[key]});setBatch(null);setManual(null);close()},onError:error=>{setBatch(null);setManual(null);toast.err(error)}})
 const pending=check.isPending||migrate.isPending
 const change=(v:ConnectionChoice)=>{setChoice(v);setBatch(null);setManual(null)}
 const stop=()=>{if(!pending){setBatch(null);setManual(null);close()}}
 return <Modal opened={opened} onClose={stop} title={t('agentEntries.migrate')} size="xl" closeOnClickOutside={false} closeOnEscape={!pending} withCloseButton={!pending}>
  <Stack><Text size="sm">{t('agentEntries.detectHint')}</Text>
   <Select label={t('agentEntries.mode')} value={choice.mode} allowDeselect={false} disabled={pending} onChange={v=>change({...choice,mode:v as ConnectionChoice['mode']})} data={['default','managed','custom'].map(value=>({value,label:t(`agentEntries.${value}`)}))} />
   {choice.mode==='managed'&&<Select label={t('agentEntries.entry')} searchable disabled={pending||entries.isError} value={choice.endpoint_id||null} data={(entries.data?.entries??[]).map(e=>({value:e.id,label:`${e.name} · ${e.url}`,disabled:!e.ready||entries.isError||!evidenceFresh(entries.data?.start,clock)}))} onChange={v=>change({...choice,endpoint_id:v??''})} />}
   {choice.mode==='custom'&&<TextInput label={t('agentConnection.url')} placeholder="https://agent.example.com:2096" disabled={pending} value={choice.url} onChange={e=>change({...choice,url:e.currentTarget.value})} />}
   <Accordion variant="contained"><Accordion.Item value="advanced"><Accordion.Control>{t('agentEntries.advanced')}</Accordion.Control><Accordion.Panel><Select label={t('agentEntries.verification')} value={mode} allowDeselect={false} disabled={pending} data={['auto','direct','trusted-proxy'].map(value=>({value,label:t(`agentEntries.verificationModes.${value}`)}))} onChange={v=>change({...choice,verification_mode:v as ConnectionChoice['verification_mode']})} /></Accordion.Panel></Accordion.Item></Accordion>
   {entries.isError&&<Alert color="yellow">{t('agentEntries.loadError')}</Alert>}
   {(detection.isError||mode==='auto'&&batch&&!autoData)&&<Alert color="yellow">{t(detection.isError?'agentEntries.loadError':'agentEntries.detectWaiting')}</Alert>}
   <Badge w="fit-content">{t('agentEntries.selected',{count:ids.length})}</Badge>
   {data&&<><Text fw={600} style={{overflowWrap:'anywhere'}}>{data.target_url}</Text><Table.ScrollContainer minWidth={540}><Table><Table.Thead><Table.Tr><Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('agentEntries.actual')}</Table.Th><Table.Th>{t('agentEntries.verification')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{data.nodes.map(n=><Table.Tr key={n.node_id}><Table.Td>{n.name||`#${n.node_id}`}</Table.Td><Table.Td style={{overflowWrap:'anywhere'}}>{n.url||'—'}</Table.Td><Table.Td><Text size="sm">{mode==='auto'?t(`agentEntries.detectionStates.${n.status}`,{defaultValue:t('agentEntries.unknown')}):t(`agentEntries.verificationModes.${mode}`)}</Text>{mode!=='auto'&&n.paired&&!(mode==='trusted-proxy'?n.proxy_capable:n.capable)&&<Text size="xs" c="orange">{t('agentEntries.upgradeNeeded')}</Text>}{n.error&&<Text size="xs" c="orange">{t(`agentEntries.migrationErrors.${n.error}`,{defaultValue:n.error})}</Text>}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer></>}
   {data&&mode==='auto'&&!ready&&<Text size="xs" c="dimmed">{t('agentEntries.detectWaiting')}</Text>}
   {needsTrust&&data&&<Alert color="yellow"><Text size="sm">{t('agentEntries.proxyTrustHint')}</Text><Text size="sm" mt="xs">{t('agentEntries.finalTrustHint')}</Text></Alert>}
   <Group justify="flex-end"><Button variant="default" disabled={pending} onClick={stop}>{t('common.cancel')}</Button><Button variant="light" loading={check.isPending} disabled={pending||!ids.length||choice.mode==='managed'&&!choice.endpoint_id||choice.mode==='custom'&&!choice.url} onClick={()=>check.mutate({request,signature})}>{t(mode==='auto'?'agentEntries.detectPreview':'agentEntries.preview')}</Button><Button disabled={!ready||pending} loading={migrate.isPending} onClick={()=>migrate.mutate()}>{t(needsTrust?'agentEntries.confirmTrustMigrate':'agentEntries.confirmMigrate')}</Button></Group>
  </Stack>
 </Modal>
}
