import type { Line, LineGroup, LinePreview } from './api'

export type ChangeKind = 'inbound' | 'line' | 'entry'
export interface InitialInboundSettings { transport?: { type: string }; tls?: { mode: number; server_name?: string }; cipher?: string }
export interface ChangesetInbound {
  node_id?: number; tag: string; listen: string; port: number; enabled: boolean; sort: number; ingress_id?: number | null
  protocol?: string; core?: string; settings?: InitialInboundSettings; settings_configured?: boolean
}
export interface ChangesetEntry {
  name: string; inbound_id?: number; inbound_ref?: string; line_id?: number; line_ref?: string; line_member_id?: string
  display_host: string; display_port: number; rate: number; sort: number; enabled: boolean; tags: string[]; region: string
}
export interface ChangesetOperation {
  key: string; kind: ChangeKind; id?: number; revision?: string; inbound?: ChangesetInbound; line?: Line; inbound_ref?: string; entry?: ChangesetEntry
}
export interface EditableOperation extends ChangesetOperation { editMode?: boolean; preservedFields?: Record<string, unknown> }
export interface ChangesetModel { name: string; operations: EditableOperation[] }
export interface TopologyDraft extends ChangesetModel {
  id: string; version: number; status: 'draft' | 'committed'; created_at: string; updated_at: string; committed_at?: string
  result?: { key: string; kind: ChangeKind; id: number }[]
}
export interface TopologyCatalog {
  nodes: { id: number; name: string }[]
  ingresses: { id: number; node_id: number; name: string }[]
  inbounds: (ChangesetInbound & { id: number; node_id: number; protocol: string; revision: string; editable: boolean; line_protocol?: string })[]
  lines: { line: Line; revision: string }[]
  entries: (ChangesetEntry & { id: number; revision: string; editable: boolean })[]
}
export interface TopologyPreview {
  revision: string
  changes: { key: string; kind: ChangeKind; id: number; before: Record<string, unknown> | null; after: Record<string, unknown>; forwards?: LinePreview['changes'] }[]
  resolved?: { key: string; kind: ChangeKind; id: number }[]
}
export const changesetKey = () => crypto.randomUUID().replaceAll('-', '').slice(0, 16)
export const newChangesetMember = () => ({ id: changesetKey(), node_id: 0, port: 0, weight: 1, address_mode: 'default' as const })
export const newChangesetGroup = (): LineGroup => ({ strategy: 'failover', members: [newChangesetMember()] })
export function newChangesetOperation(kind: ChangeKind): EditableOperation {
  const base = { key: changesetKey(), kind }
  if (kind === 'inbound') return { ...base, inbound: { node_id: 0, tag: '', listen: '', port: 443, protocol: 'vless', core: '', enabled: true, sort: 0, ingress_id: 0, settings: { transport: { type: 'tcp' } } } }
  if (kind === 'line') return { ...base, line: { id: 0, version: 0, name: '', protocol: 'tcp', backend: 'relay', target_inbound_id: 0, target_address_mode: 'default', groups: [newChangesetGroup()], endpoints: [] } }
  return { ...base, entry: { name: '', inbound_id: 0, line_id: 0, line_member_id: '', display_host: '', display_port: 0, rate: 1, sort: 0, enabled: true, tags: [], region: '' } }
}
export function editableChangesetLine(line: Line) { return !line.path_route && line.backend === 'relay' }
export function operationLabel(op: ChangesetOperation) { return op.inbound?.tag || op.line?.name || op.entry?.name || op.key }
export function selectChangesetResource(op: EditableOperation, id: number, catalog: TopologyCatalog): EditableOperation {
  if (op.id === id && op.revision) return op
  const base = { key: op.key, kind: op.kind, id, editMode: true }
  if (op.kind === 'inbound') {
    const source = catalog.inbounds.find(item => item.id === id && item.editable)
    if (source) return { ...base, revision: source.revision, inbound: { node_id: source.node_id, protocol: source.protocol, tag: source.tag, listen: source.listen, port: source.port, enabled: source.enabled, sort: source.sort, ingress_id: source.ingress_id ?? 0 } }
  }
  if (op.kind === 'line') {
    const source = catalog.lines.find(item => item.line.id === id && editableChangesetLine(item.line))
    if (source) return { ...base, revision: source.revision, line: structuredClone(source.line) }
  }
  if (op.kind === 'entry') {
    const source = catalog.entries.find(item => item.id === id && item.editable)
    if (source) {
      const { name, inbound_id, line_id, line_member_id, display_host, display_port, rate, sort, enabled, tags, region } = source
      return { ...base, revision: source.revision, entry: { name, inbound_id, line_id, line_member_id, display_host, display_port, rate, sort, enabled, tags: tags || [], region: region || '' } }
    }
  }
  return { ...newChangesetOperation(op.kind), key: op.key, id: 0, editMode: true }
}
export function operationInput(op: EditableOperation): ChangesetOperation {
  const base = { key: op.key, kind: op.kind, ...(op.id ? { id: op.id, revision: op.revision } : {}) }
  if (op.kind === 'inbound' && op.inbound) {
    const v = op.inbound
    const inbound: ChangesetInbound = { tag: v.tag.trim(), listen: v.listen.trim(), port: v.port, enabled: v.enabled, sort: v.sort, ...(v.ingress_id !== undefined ? { ingress_id: v.ingress_id } : {}) }
    if (!op.id) {
      inbound.node_id = v.node_id
      inbound.protocol = v.protocol
      inbound.core = v.core
      if (!v.settings_configured && v.settings !== undefined) inbound.settings = structuredClone(v.settings)
    }
    return { ...base, inbound: omitPreserved(inbound, op.preservedFields) }
  }
  if (op.kind === 'line' && op.line) {
    const v = op.line
    return { ...base, ...(op.inbound_ref ? { inbound_ref: op.inbound_ref } : {}), line: {
      id: op.id || 0, version: op.id ? v.version : 0, name: v.name.trim(), protocol: v.protocol, backend: v.backend,
      target_inbound_id: op.inbound_ref ? 0 : v.target_inbound_id, target_address_mode: v.target_address_mode || 'default',
      groups: v.groups.map(group => ({ strategy: group.strategy, members: group.members.map(member => ({ id: member.id, node_id: member.node_id, port: member.port, ingress_id: member.ingress_id ?? null, weight: member.weight, address_mode: member.address_mode || 'default' })) })),
      endpoints: [],
    } }
  }
  if (op.kind === 'entry' && op.entry) {
    const v = op.entry
    return { ...base, entry: omitPreserved({
      name: v.name.trim(), inbound_id: v.inbound_ref ? 0 : v.inbound_id || 0, ...(v.inbound_ref ? { inbound_ref: v.inbound_ref } : {}),
      line_id: v.line_ref ? 0 : v.line_id || 0, ...(v.line_ref ? { line_ref: v.line_ref } : {}), line_member_id: v.line_member_id || '',
      display_host: v.display_host.trim(), display_port: v.display_port, rate: v.rate, sort: v.sort, enabled: v.enabled, tags: [...v.tags], region: v.region.trim(),
    }, op.preservedFields) }
  }
  return base
}
export function changesetInput(model: ChangesetModel, version = 0) { return { name: model.name.trim(), version, operations: model.operations.map(operationInput) } }
export function changesetFingerprint(model: ChangesetModel) { const { version: _, ...input } = changesetInput(model); return JSON.stringify(input) }
function omitPreserved<T extends object>(value: T, preserved?: Record<string, unknown>): T {
  const input = { ...value } as Record<string, unknown>
  for (const [key, initial] of Object.entries(preserved || {})) if (JSON.stringify(input[key]) === JSON.stringify(initial)) delete input[key]
  return input as T
}
const inboundPatchFields = ['tag', 'listen', 'port', 'enabled', 'sort', 'ingress_id']
const entryPatchFields = ['name', 'inbound_id', 'line_id', 'line_member_id', 'display_host', 'display_port', 'rate', 'sort', 'enabled', 'tags', 'region']
export function modelFromDraft(draft: TopologyDraft, catalog?: TopologyCatalog): ChangesetModel {
  return { name: draft.name, operations: structuredClone(draft.operations).map(op => {
    const operation: EditableOperation = { ...op, editMode: !!op.id }
    const kind = op.kind === 'inbound' ? 'inbound' : op.kind === 'entry' ? 'entry' : undefined
    if (!kind || !op[kind]) return operation
    const defaults = newChangesetOperation(kind)[kind]!
    const current = kind === 'inbound' ? catalog?.inbounds.find(item => item.id === op.id) : catalog?.entries.find(item => item.id === op.id)
    const patch = op[kind] as unknown as Record<string, unknown>
    const hydrated = { ...defaults, ...current, ...patch } as Record<string, unknown>
    if (kind === 'inbound' && (op.id || patch.settings_configured)) delete hydrated.settings
    const preservedFields: Record<string, unknown> = {}
    for (const key of kind === 'inbound' ? inboundPatchFields : entryPatchFields) {
      if (patch[key] == null) {
        const live = current as unknown as Record<string, unknown> | undefined
        hydrated[key] = live?.[key] ?? (defaults as unknown as Record<string, unknown>)[key]
        preservedFields[key] = structuredClone(hydrated[key])
      }
    }
    // Metadata may be needed to display existing resources, but never replaces
    // the revision captured by the API author. Untouched omissions stay omitted.
    return { ...operation, [kind]: hydrated, preservedFields } as EditableOperation
  }) }
}
const settingsFingerprint = (op: EditableOperation) => JSON.stringify({ protocol: op.inbound?.protocol, core: op.inbound?.core || '', settings: op.inbound?.settings })
export function keepSavedInboundSettings(model: ChangesetModel, saved: TopologyDraft, key: string): ChangesetModel {
  const stored = saved.operations.find(op => op.key === key)?.inbound
  if (!stored) return model
  return { ...model, operations: model.operations.map(op => op.key !== key || !op.inbound ? op : { ...op, inbound: { ...op.inbound, protocol: stored.protocol, core: stored.core, settings: undefined, settings_configured: true } }) }
}
export function reconcileChangesetSave(current: ChangesetModel, submitted: ChangesetModel, saved: TopologyDraft, catalog?: TopologyCatalog) {
  if (changesetFingerprint(current) === changesetFingerprint(submitted)) return { model: modelFromDraft(saved, catalog), conflicts: [] as string[] }
  let model = current
  const conflicts: string[] = []
  for (const stored of saved.operations) {
    if (stored.id || !stored.inbound?.settings_configured) continue
    const local = current.operations.find(op => op.key === stored.key)
    const sent = submitted.operations.find(op => op.key === stored.key)
    if (!local?.inbound || !sent?.inbound) continue
    if (settingsFingerprint(local) !== settingsFingerprint(sent)) conflicts.push(stored.key)
    else model = keepSavedInboundSettings(model, saved, stored.key)
  }
  return { model, conflicts }
}
export function operationReferences(op: ChangesetOperation) { return [op.inbound_ref, op.entry?.inbound_ref, op.entry?.line_ref].filter((key): key is string => !!key) }
export function operationIsReferenced(operations: ChangesetOperation[], key: string) { return operations.some(op => op.key !== key && operationReferences(op).includes(key)) }
export function inboundLineProtocol(protocol?: string): Line['protocol'] | undefined {
  if (['vless', 'vmess', 'trojan', 'anytls', 'ssh', 'http', 'naive', 'socks'].includes(protocol || '')) return 'tcp'
  if (['shadowsocks', 'snell'].includes(protocol || '')) return 'both'
  if (['hysteria2', 'tuic', 'wireguard'].includes(protocol || '')) return 'udp'
  return undefined
}
export function lineTargetProtocol(op: ChangesetOperation, operations: ChangesetOperation[], catalog: TopologyCatalog) {
  if (op.inbound_ref) return inboundLineProtocol(operations.find(item => item.key === op.inbound_ref)?.inbound?.protocol)
  const inbound = catalog.inbounds.find(item => item.id === op.line?.target_inbound_id)
  if (inbound?.line_protocol !== undefined) return ['tcp', 'udp', 'both'].includes(inbound.line_protocol) ? inbound.line_protocol as Line['protocol'] : undefined
  return inboundLineProtocol(inbound?.protocol)
}
export function changesetValid(model: ChangesetModel) {
  const ops = model.operations, port = (n: number) => Number.isInteger(n) && n > 0 && n <= 65535
  const ref = (key: string | undefined, kind: ChangeKind) => !!key && ops.some(op => op.key === key && op.kind === kind && !op.id && !op.editMode)
  if (!model.name.trim() || ops.length < 1 || ops.length > 32 || new Set(ops.map(op => op.key)).size !== ops.length) return false
  return ops.every(op => {
    if (!op.key || op.editMode && !op.id || op.id && !op.revision) return false
    if (op.kind === 'inbound' && op.inbound) {
      const v = op.inbound
      return !!v.tag.trim() && port(v.port) && (op.id || (v.node_id || 0) > 0 && ['vless', 'vmess', 'trojan', 'shadowsocks'].includes(v.protocol || '')) && (v.settings?.tls?.mode !== 1 || !!v.settings.tls.server_name?.trim())
    }
    if (op.kind === 'line' && op.line) {
      const v = op.line, members = v.groups.flatMap(group => group.members)
      return !!v.name.trim() && editableChangesetLine(v) && (op.inbound_ref ? ref(op.inbound_ref, 'inbound') : v.target_inbound_id > 0) && v.groups.length > 0 && v.groups.length <= 8 && new Set(members.map(member => member.node_id)).size === members.length && v.groups.every((group, groupIndex) => group.members.length > 0 && group.members.length <= 8 && (groupIndex === 0 || v.protocol === 'tcp' || group.members.length < 2 || group.strategy === 'roundrobin') && group.members.every(member => member.node_id > 0 && !!member.id && port(member.port) && Number.isInteger(member.weight) && member.weight >= 1 && member.weight <= 100))
    }
    if (op.kind === 'entry' && op.entry) {
      const v = op.entry
      return !!v.name.trim() && (v.line_ref ? ref(v.line_ref, 'line') : v.line_id ? v.line_id > 0 : v.inbound_ref ? ref(v.inbound_ref, 'inbound') : (v.inbound_id || 0) > 0) && Number.isFinite(v.rate) && v.rate > 0 && Number.isInteger(v.display_port) && v.display_port >= 0 && v.display_port <= 65535
    }
    return false
  })
}
