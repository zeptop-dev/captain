import type { NodeAccessDirectory } from './node-access'

export interface PlanAccessDraft { mode: 'independent' | 'shared' | 'legacy'; group_id: number; name: string; inbound_ids: number[]; category_ids: number[]; revision: string; confirm_shared: boolean }
export function staticInboundIDs(directory:NodeAccessDirectory, group:number) {
 return directory.inbounds.filter(row=>row.editable&&(row.policy==='all'||(row.static_group_ids??row.group_ids).includes(group))).map(row=>row.inbound_id)
}
export function planAccessDraft(directory: NodeAccessDirectory, id: number, group: number | null): PlanAccessDraft {
 const independent=directory.plans.find(p=>p.id===id)?.independent
 return {mode:!id||independent?'independent':group?'shared':'legacy',group_id:group||0,name:'',inbound_ids:staticInboundIDs(directory,group||0),category_ids:[...(directory.groups.find(g=>g.id===group)?.category_ids??[])],revision:directory.revision,confirm_shared:false}
}
