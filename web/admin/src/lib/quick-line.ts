import type { LineGroup } from './api'
import type { EntryNodeDetail } from './entry-flow'
import { firstFreeIngressPort } from './ingress.ts'
export interface QuickLineDraft { name: string; target_inbound_id: number; groups: LineGroup[] }
export function quickLineGroup(ids: string[], strategy: LineGroup['strategy'], port: number, details: EntryNodeDetail[]): LineGroup {
 return { strategy, members: ids.map(id => {
  const d = details.find(d => d.node.id === Number(id))
  const ingress = d?.ingresses?.find(g => g.require_ingress)
  return { id: '', node_id: Number(id), ingress_id: ingress?.id, port: ingress ? firstFreeIngressPort(ingress, d?.inbounds.map(i => i.Port)) : port, weight: 1, address_mode: 'default' }
 }) }
}
