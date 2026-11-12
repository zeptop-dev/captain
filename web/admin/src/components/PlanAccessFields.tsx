import { Alert, Button, Checkbox, Group, Select, Stack, Text, TextInput } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { groupSelection, type NodeAccessDirectory } from '../lib/node-access'

import { staticInboundIDs, type PlanAccessDraft } from '../lib/plan-access'
export { planAccessDraft, type PlanAccessDraft } from '../lib/plan-access'

export function PlanAccessFields({value,onChange,directory,disabled=false}:{value:PlanAccessDraft;onChange:(value:PlanAccessDraft)=>void;directory:NodeAccessDirectory;disabled?:boolean}) {
 const {t}=useTranslation()
 const owned=new Set(directory.plans.filter(p=>p.independent).map(p=>p.group_id))
 const groups=directory.groups.filter(g=>!owned.has(g.id))
 const affected=directory.plans.filter(p=>p.group_id===value.group_id).map(p=>p.name)
 const set=(v:Partial<PlanAccessDraft>)=>onChange({...value,...v,confirm_shared:false})
 const followed=(row:NodeAccessDirectory['inbounds'][number])=>row.entries.some(e=>e.category_id&&value.category_ids.includes(e.category_id))
 const categoryRows=(id:number)=>directory.inbounds.filter(row=>row.editable&&row.entries.some(e=>e.category_id===id)).map(row=>row.inbound_id)
 return <Stack gap="xs">
  <Select label={t('planAccess.mode')} allowDeselect={false} disabled={disabled} value={value.mode} data={[{value:'independent',label:t('planAccess.independent')},{value:'shared',label:t('planAccess.shared')},...(value.mode==='legacy'?[{value:'legacy',label:t('planAccess.legacy')}]:[])]} onChange={v=>{if(v)set({mode:v as PlanAccessDraft['mode'],...(v==='shared'&&owned.has(value.group_id)?{group_id:0}: {})})}} />
  <Text size="xs" c="dimmed">{t('planSelection.scope')}</Text>
  {value.mode==='shared'&&<>
   <Select label={t('planAccess.scheme')} value={value.group_id?String(value.group_id):'new'} disabled={disabled} allowDeselect={false} data={[...groups.map(g=>({value:String(g.id),label:g.name})),{value:'new',label:t('planAccess.newScheme')}]} onChange={v=>{if(v)set({group_id:v==='new'?0:Number(v),inbound_ids:v==='new'?groupSelection(directory,[]):staticInboundIDs(directory,Number(v)),category_ids:v==='new'?[]:[...(directory.groups.find(g=>g.id===Number(v))?.category_ids??[])]})}} />
   {!value.group_id&&<TextInput label={t('planAccess.schemeName')} value={value.name} maxLength={200} disabled={disabled} onChange={e=>set({name:e.currentTarget.value})} />}
   <Alert color="yellow">{t('planAccess.impact')}: {affected.join(' / ')||t('common.none')}</Alert>
  </>}
  {value.mode!=='legacy'&&<>
   <Text size="sm" fw={600}>{t('planSelection.categories')}</Text>
   <Text size="xs" c="dimmed">{t('planSelection.snapshot')}</Text>
   {(directory.categories??[]).map(category=><Stack key={category.id} gap={4}>
    <Group><Text size="sm">{category.name}</Text><Button size="compact-xs" variant="light" disabled={disabled} onClick={()=>set({inbound_ids:groupSelection(directory,[...new Set([...value.inbound_ids,...categoryRows(category.id)])])})}>{t('planSelection.selectCurrent')}</Button></Group>
    <Checkbox label={t('planSelection.follow')} checked={value.category_ids.includes(category.id)} disabled={disabled} onChange={e=>set({category_ids:e.currentTarget.checked?[...value.category_ids,category.id]:value.category_ids.filter(id=>id!==category.id)})}/>
   </Stack>)}
   {!!value.category_ids.length&&<Alert color="yellow">{t('planSelection.followWarning')}</Alert>}
   <Text size="sm" fw={600}>{t('nodeAccess.availableNodes')}</Text>
   {directory.inbounds.filter(row=>row.editable).map(row=><Checkbox key={row.inbound_id} disabled={disabled||row.policy==='all'||followed(row)} checked={row.policy==='all'||followed(row)||value.inbound_ids.includes(row.inbound_id)} label={`${row.entries.map(e=>e.name).join(' / ')||row.tag} · ${row.node_name}${followed(row)?' · '+t('planSelection.fromCategory'):''}`} onChange={e=>set({inbound_ids:groupSelection(directory,e.currentTarget.checked?[...value.inbound_ids,row.inbound_id]:value.inbound_ids.filter(id=>id!==row.inbound_id))})} />)}
   <Text size="xs" c="dimmed">{t('nodeAccess.sharedHint')}</Text>
   <Checkbox label={t('planAccess.confirm')} checked={value.confirm_shared} disabled={disabled} onChange={e=>onChange({...value,confirm_shared:e.currentTarget.checked})} />
  </>}
 </Stack>
}
