import { Alert, Button, Checkbox, Group, Modal, Stack, Table, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { type AgentConnection, type AgentEndpoint } from '../lib/agent-connections'
import { AgentMigrationDialog } from './AgentMigrationDialog'
export function AgentEndpointUsage({ entry, close }: { entry: AgentEndpoint; close: () => void }) {
 const { t } = useTranslation(); const [selected,setSelected]=useState<number[]>([]);const [migrate,setMigrate]=useState(false)
 const q=useQuery({queryKey:['entry-usage',entry.id],queryFn:({signal})=>api.get<{nodes:AgentConnection[];truncated:boolean}>(`/api/admin/agent-connections/entries/${entry.id}`,signal),retry:false,refetchInterval:5000})
 return <><Modal opened onClose={close} title={`${entry.name} · ${t('agentEntries.usageNodes')}`} size="xl"><Stack>
  <Text size="sm" c="dimmed">{t('agentEntries.referenceHint')}</Text>
  {q.isError&&<Alert color="red">{t('agentEntries.loadError')}<Button onClick={()=>q.refetch()}>{t('workspace.retry')}</Button></Alert>}
  {q.data?.truncated&&<Alert>{t('agentEntries.truncated')}</Alert>}
  <Group><Button variant="light" disabled={!q.data||q.isError} onClick={()=>setSelected((q.data?.nodes??[]).map(n=>n.node_id))}>{t('agentEntries.selectAll')}</Button><Button disabled={!selected.length||q.isError} onClick={()=>setMigrate(true)}>{t('agentEntries.migrate')} ({selected.length})</Button></Group>
  <Table.ScrollContainer minWidth={600}><Table><Table.Thead><Table.Tr><Table.Th/><Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('agentEntries.actual')}</Table.Th><Table.Th>{t('agentEntries.state')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{q.data?.nodes.map(n=><Table.Tr key={n.node_id}><Table.Td><Checkbox checked={selected.includes(n.node_id)} aria-label={`${t('agentEntries.select')} ${n.name||n.node_id}`} onChange={e=>setSelected(e.currentTarget.checked?[...selected,n.node_id]:selected.filter(id=>id!==n.node_id))}/></Table.Td><Table.Td>{n.name||`#${n.node_id}`}</Table.Td><Table.Td style={{overflowWrap:'anywhere'}}>{n.url||'—'}</Table.Td><Table.Td>{t(!n.paired?'agentEntries.installOnly':n.capable?'agentEntries.ready':'agentEntries.upgradeNeeded')}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
 </Stack></Modal><AgentMigrationDialog ids={selected} opened={migrate} close={()=>{setMigrate(false);void q.refetch()}}/></>
}
