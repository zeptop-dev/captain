import type { Line } from './api'

export type LineInput = Pick<Line, 'name' | 'backend' | 'protocol' | 'target_inbound_id' | 'target_address_mode' | 'groups'> & { version?: number }
// Exclude form-only keys, transient status and previous preview tokens. The same
// exact payload identifies a review and its eventual save.
export function lineImpactInput(draft: LineInput, protocol: Line['protocol'], version?: number): LineInput {
  return {
    name: draft.name.trim(), backend: draft.backend, protocol,
    target_inbound_id: draft.target_inbound_id, target_address_mode: draft.target_address_mode || 'default',
    groups: draft.groups.map((group, index) => ({ strategy: group.strategy, members: group.members.map(member => ({
      id: member.id, node_id: member.node_id, port: member.port, ingress_id: member.ingress_id || null,
      weight: member.weight, address_mode: index > 0 ? member.address_mode || 'default' : 'default',
    })) })),
    ...(version !== undefined ? { version } : {}),
  }
}
export function currentLineReview(source: string | undefined, input: LineInput): boolean {
  return source !== undefined && source === JSON.stringify(input)
}
