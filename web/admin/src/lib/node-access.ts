export type AccessPolicy = 'all' | 'groups' | 'none'
export interface AccessGroup { id: number; name: string }
export interface AccessPlan { id: number; name: string; group_id: number | null }
export interface InboundAccess {
  inbound_id: number; node_id: number; node_name: string; tag: string; protocol: string; port: number
  policy: AccessPolicy; group_ids: number[]; legacy: boolean; editable: boolean; revision: string
  entries: { id: number; name: string }[]; plan_ids: number[]
}
export interface NodeAccessDirectory { revision: string; inbounds: InboundAccess[]; groups: AccessGroup[]; plans: AccessPlan[] }
export interface AccessDraft { source: InboundAccess; policy: AccessPolicy; group_ids: number[]; confirm_shared: boolean }
export interface AccessPayload { inbound_id: number; revision: string; policy: AccessPolicy; group_ids: number[]; confirm_shared: boolean }
const sorted = (ids: number[]) => [...new Set(ids)].sort((a, b) => a - b)
export const sameIDs = (a: number[], b: number[]) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b))
export const accessDraft = (source: InboundAccess): AccessDraft => ({ source, policy: source.policy, group_ids: [...source.group_ids], confirm_shared: false })
export const accessChanged = (draft: AccessDraft) => draft.policy !== draft.source.policy || !sameIDs(draft.policy === 'groups' ? draft.group_ids : [], draft.source.policy === 'groups' ? draft.source.group_ids : [])
export const sharedEntries = (row: InboundAccess, entryID?: number) => row.entries.filter(e => e.id !== entryID)
export const validAccess = (draft: AccessDraft, directory: NodeAccessDirectory) => draft.source.editable && draft.source.revision === directory.revision && (draft.policy !== 'groups' || draft.group_ids.length > 0 && draft.group_ids.every(id => directory.groups.some(g => g.id === id)))
// Always bind a draft to the effective target, including a line's final inbound.
export function changedAccessPayload(draft: AccessDraft | null, inboundID: number): AccessPayload | undefined {
  if (!draft || draft.source.inbound_id !== inboundID || !accessChanged(draft)) return undefined
  return { inbound_id: inboundID, revision: draft.source.revision, policy: draft.policy, group_ids: draft.policy === 'groups' ? sorted(draft.group_ids) : [], confirm_shared: draft.confirm_shared }
}
export const availableToGroup = (row: InboundAccess, groupID: number) => row.editable && (row.policy === 'all' || row.policy === 'groups' && row.group_ids.includes(groupID))
export const groupInboundIDs = (directory: NodeAccessDirectory, groupID: number) => directory.inbounds.filter(row => availableToGroup(row, groupID)).map(row => row.inbound_id)
export function groupSelection(directory: NodeAccessDirectory, selected: number[]) {
  return directory.inbounds.filter(row => row.editable && (row.policy === 'all' || selected.includes(row.inbound_id))).map(row => row.inbound_id)
}
export function accessMatches(row: InboundAccess | undefined, filter: string | null): boolean {
  if (!filter) return true
  if (!row) return false
  if (filter.startsWith('group:')) return availableToGroup(row, Number(filter.slice(6)))
  return row.policy === filter
}
export function preserveExistingGroup<T extends { id: string; group_id?: number | null }>(value: T): Omit<T, 'group_id'> | T {
  if (!value.id) return value
  const { group_id: _legacy, ...rest } = value
  return rest
}
