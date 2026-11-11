import { Alert, Checkbox, Select, Stack, Text, TextInput } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { groupInboundIDs, groupSelection, type NodeAccessDirectory } from '../lib/node-access'

export interface PlanAccessDraft { mode: 'independent' | 'shared' | 'legacy'; group_id: number; name: string; inbound_ids: number[]; revision: string; confirm_shared: boolean }
export function planAccessDraft(directory: NodeAccessDirectory, id: number, group: number | null): PlanAccessDraft {
 const independent=directory.plans.find(p=>p.id===id)?.independent
 return {mode:!id||independent?'independent':group?'shared':'legacy',group_id:group||0,name:'',inbound_ids:groupInboundIDs(directory,group||0),revision:directory.revision,confirm_shared:false}
}
export function PlanAccessFields({value,onChange,directory,disabled=false}:{value:PlanAccessDraft;onChange:(value:PlanAccessDraft)=>void;directory:NodeAccessDirectory;disabled?:boolean}) {
 const {t}=useTranslation()
 const owned=new Set(directory.plans.filter(p=>p.independent).map(p=>p.group_id))
 const groups=directory.groups.filter(g=>!owned.has(g.id))
 const affected=directory.plans.filter(p=>p.group_id===value.group_id).map(p=>p.name)
 const set=(v:Partial<PlanAccessDraft>)=>onChange({...value,...v,confirm_shared:false})
 return <Stack gap="xs">
  <Select label={t('planAccess.mode')} allowDeselect={false} disabled={disabled} value={value.mode} data={[{value:'independent',label:t('planAccess.independent')},{value:'shared',label:t('planAccess.shared')},...(value.mode==='legacy'?[{value:'legacy',label:t('planAccess.legacy')}]:[])]} onChange={v=>{if(v)set({mode:v as PlanAccessDraft['mode'],...(v==='shared'&&owned.has(value.group_id)?{group_id:0}: {})})}} />
  <Text size="xs" c="dimmed">{t('planAccess.hint')}</Text>
  {value.mode==='shared'&&<>
   <Select label={t('planAccess.scheme')} value={value.group_id?String(value.group_id):'new'} disabled={disabled} allowDeselect={false} data={[...groups.map(g=>({value:String(g.id),label:g.name})),{value:'new',label:t('planAccess.newScheme')}]} onChange={v=>{if(v)set({group_id:v==='new'?0:Number(v),inbound_ids:v==='new'?groupSelection(directory,[]):groupInboundIDs(directory,Number(v))})}} />
   {!value.group_id&&<TextInput label={t('planAccess.schemeName')} value={value.name} maxLength={200} disabled={disabled} onChange={e=>set({name:e.currentTarget.value})} />}
   <Alert color="yellow">{t('planAccess.impact')}: {affected.join(' / ')||t('common.none')}</Alert>
  </>}
  {value.mode!=='legacy'&&<>
   <Text size="sm" fw={600}>{t('nodeAccess.availableNodes')}</Text>
   {directory.inbounds.filter(row=>row.editable).map(row=><Checkbox key={row.inbound_id} disabled={disabled||row.policy==='all'} checked={row.policy==='all'||value.inbound_ids.includes(row.inbound_id)} label={`${row.entries.map(e=>e.name).join(' / ')||row.tag} · ${row.node_name}${row.policy==='all'?' · '+t('nodeAccess.alwaysAvailable'):''}`} onChange={e=>set({inbound_ids:groupSelection(directory,e.currentTarget.checked?[...value.inbound_ids,row.inbound_id]:value.inbound_ids.filter(id=>id!==row.inbound_id))})} />)}
   <Checkbox label={t('planAccess.confirm')} checked={value.confirm_shared} disabled={disabled} onChange={e=>onChange({...value,confirm_shared:e.currentTarget.checked})} />
  </>}
 </Stack>
}
