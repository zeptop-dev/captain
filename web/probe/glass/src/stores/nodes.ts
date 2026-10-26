// React counterpart of glass/src/stores/nodes.ts. Same merge rules and field
// lists, but immutable: a node object is replaced only when one of its fields
// actually changed, so memoised rows and cards skip unchanged nodes the way
// Vue's per-field reactivity did.
import type { NodeData, TrafficLimitType } from '@shared/types/node'
import type { Client, NodeStatus } from '@shared/utils/rpc'
import { gpuUsageFromStatus } from '@shared/utils/gpuHelper'
import { parseNodeGroups } from '@shared/utils/groupHelper'
import { appStore } from './app'
import { createStore, shallowEqual, useSelector } from './store'

export type { NodeData, TrafficLimitType }

/** WebSocket 连接状态 */
export type WsConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting'

const STATUS_FIELDS = [
  'online', 'time', 'cpu', 'ram', 'swap', 'load', 'load5', 'load15', 'temp', 'disk',
  'net_in', 'net_out', 'net_total_up', 'net_total_down', 'traffic_up', 'traffic_down',
  'process', 'connections', 'connections_udp', 'uptime', 'message',
] as const satisfies readonly (keyof NodeData & keyof NodeStatus)[]

const CLIENT_FIELDS = [
  'name', 'cpu_name', 'virtualization', 'arch', 'cpu_cores', 'cpu_physical_cores', 'os',
  'kernel_version', 'gpu_name', 'ipv4', 'ipv6', 'region', 'location', 'remark', 'public_remark',
  'mem_total', 'swap_total', 'disk_total', 'version', 'weight', 'price', 'billing_cycle',
  'auto_renewal', 'currency', 'expired_at', 'tags', 'hidden', 'traffic_limit',
  'created_at', 'updated_at',
] as const satisfies readonly (keyof NodeData & keyof Client)[]

function areStringArraysEqual(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index])
}

function createNodeFromClient(client: Client): NodeData {
  return {
    uuid: client.uuid,
    name: client.name,
    cpu_name: client.cpu_name,
    virtualization: client.virtualization,
    arch: client.arch,
    cpu_cores: client.cpu_cores,
    cpu_physical_cores: client.cpu_physical_cores,
    os: client.os,
    kernel_version: client.kernel_version,
    gpu_name: client.gpu_name,
    ipv4: client.ipv4,
    ipv6: client.ipv6,
    region: client.region,
    location: client.location,
    remark: client.remark,
    public_remark: client.public_remark,
    mem_total: client.mem_total,
    swap_total: client.swap_total,
    disk_total: client.disk_total,
    version: client.version,
    weight: client.weight,
    price: client.price,
    billing_cycle: client.billing_cycle,
    auto_renewal: client.auto_renewal,
    currency: client.currency,
    expired_at: client.expired_at,
    group: client.group,
    groups: parseNodeGroups(client.group),
    tags: client.tags,
    hidden: client.hidden,
    traffic_limit: client.traffic_limit,
    traffic_limit_type: client.traffic_limit_type as TrafficLimitType,
    created_at: client.created_at,
    updated_at: client.updated_at,
    online: false,
    time: '',
    cpu: 0,
    gpu: 0,
    ram: 0,
    swap: 0,
    load: 0,
    load5: 0,
    load15: 0,
    temp: 0,
    disk: 0,
    net_in: 0,
    net_out: 0,
    net_total_up: 0,
    net_total_down: 0,
    traffic_up: 0,
    traffic_down: 0,
    process: 0,
    connections: 0,
    connections_udp: 0,
    uptime: 0,
  }
}

/** Returns the node with the status applied; the same object when nothing changed. */
function withStatus(node: NodeData, status: NodeStatus): NodeData {
  let next: NodeData | null = null
  const set = <K extends keyof NodeData>(key: K, value: NodeData[K]) => {
    if (node[key] === value)
      return
    next ??= { ...node }
    next[key] = value
  }
  for (const key of STATUS_FIELDS) set(key, status[key] as NodeData[typeof key])
  set('gpu', gpuUsageFromStatus(status))
  set('status_updated_at', status.updated_at)
  return next ?? node
}

/** Returns the node with the client metadata applied; the same object when nothing changed. */
function withClient(node: NodeData, client: Client): NodeData {
  let next: NodeData | null = null
  const set = <K extends keyof NodeData>(key: K, value: NodeData[K]) => {
    if (node[key] === value)
      return
    next ??= { ...node }
    next[key] = value
  }
  for (const key of CLIENT_FIELDS) set(key, client[key] as NodeData[typeof key])
  set('traffic_limit_type', client.traffic_limit_type as TrafficLimitType)
  if (node.group !== client.group) {
    set('group', client.group)
    const nextGroups = parseNodeGroups(client.group)
    if (!areStringArraysEqual(node.groups, nextGroups))
      set('groups', nextGroups)
  }
  return next ?? node
}

function sortByWeight(list: NodeData[]): NodeData[] {
  return [...list].sort((a, b) => a.weight - b.weight)
}

interface NodesRawState {
  nodes: NodeData[]
  wsConnectionState: WsConnectionState
  wsReconnectAttempts: number
}

const raw = createStore<NodesRawState>({
  nodes: [],
  wsConnectionState: 'disconnected',
  wsReconnectAttempts: 0,
})

export interface NodesState extends NodesRawState {
  /** 可见节点（未登录时 hidden 节点不参与公开首页展示） */
  visibleNodes: NodeData[]
  onlineCount: number
  totalCount: number
  groups: string[]
  nodesByUuid: Map<string, NodeData>
  visibleNodesByUuid: Map<string, NodeData>
}

let lastRaw: NodesRawState | null = null
let lastPrivate: boolean | null = null
let lastState: NodesState | null = null

function derive(state: NodesRawState): NodesState {
  const privateAllowed = appStore.getState().privateFeaturesAllowed
  if (lastState && lastRaw === state && lastPrivate === privateAllowed)
    return lastState
  const visibleNodes = privateAllowed ? state.nodes : state.nodes.filter(n => !n.hidden)
  const groupSet = new Set<string>()
  for (const n of visibleNodes) {
    for (const group of n.groups) groupSet.add(group)
  }
  const next: NodesState = {
    ...state,
    visibleNodes,
    onlineCount: visibleNodes.filter(n => n.online).length,
    totalCount: visibleNodes.length,
    groups: Array.from(groupSet),
    nodesByUuid: new Map(state.nodes.map(n => [n.uuid, n])),
    visibleNodesByUuid: new Map(visibleNodes.map(n => [n.uuid, n])),
  }
  // Keep identities of derived collections that did not change.
  if (lastState) {
    if (shallowArrayEqual(lastState.visibleNodes, next.visibleNodes))
      next.visibleNodes = lastState.visibleNodes
    if (areStringArraysEqual(lastState.groups, next.groups))
      next.groups = lastState.groups
    if (lastState.nodes === next.nodes)
      next.nodesByUuid = lastState.nodesByUuid
    if (lastState.visibleNodes === next.visibleNodes)
      next.visibleNodesByUuid = lastState.visibleNodesByUuid
  }
  lastRaw = state
  lastPrivate = privateAllowed
  lastState = next
  return next
}

function shallowArrayEqual<T>(a: readonly T[], b: readonly T[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

function subscribe(listener: () => void): () => void {
  const offNodes = raw.subscribe(listener)
  // visibleNodes depends on the login state held by the app store.
  const offApp = appStore.subscribe(listener)
  return () => {
    offNodes()
    offApp()
  }
}

function setNodes(nodes: NodeData[]): void {
  if (!shallowArrayEqual(raw.getState().nodes, nodes))
    raw.setState({ nodes })
}

export const nodesStore = {
  getState: (): NodesState => derive(raw.getState()),
  subscribe,

  /** 初始化节点数据（首次加载） */
  initNodes(clients: Record<string, Client>, statuses: Record<string, NodeStatus>): void {
    // Existing nodes keep their order and new ones are appended before the
    // stable weight sort, as in the Vue store, so equal weights do not swap.
    const seen = new Set<string>()
    const next: NodeData[] = []
    for (const node of raw.getState().nodes) {
      const client = clients[node.uuid]
      if (!client)
        continue
      seen.add(node.uuid)
      const status = statuses[node.uuid]
      const updated = withClient(node, client)
      next.push(status ? withStatus(updated, status) : updated)
    }
    for (const [uuid, client] of Object.entries(clients)) {
      if (!client || seen.has(uuid))
        continue
      const status = statuses[uuid]
      const node = createNodeFromClient(client)
      next.push(status ? withStatus(node, status) : node)
    }
    setNodes(sortByWeight(next))
  },

  /** 按 weight 升序排序节点（weight 越小越靠前） */
  sortNodesByWeight(): void {
    setNodes(sortByWeight(raw.getState().nodes))
  },

  /** 更新节点状态（实时更新） */
  updateNodeStatuses(statuses: Record<string, NodeStatus>): void {
    const list = raw.getState().nodes
    const next = list.map((node) => {
      const status = statuses[node.uuid]
      return status ? withStatus(node, status) : node
    })
    setNodes(next)
  },

  /** 更新节点基本信息；仅在结构或 weight 变化时重排序 */
  updateNodeClients(clients: Record<string, Client>): void {
    const list = raw.getState().nodes
    let needSort = false
    const seen = new Set<string>()
    const next: NodeData[] = []
    for (const node of list) {
      const client = clients[node.uuid]
      if (!client) {
        needSort = true
        continue
      }
      seen.add(node.uuid)
      const updated = withClient(node, client)
      if (updated.weight !== node.weight)
        needSort = true
      next.push(updated)
    }
    for (const [uuid, client] of Object.entries(clients)) {
      if (!client || seen.has(uuid))
        continue
      next.push(createNodeFromClient(client))
      needSort = true
    }
    setNodes(needSort ? sortByWeight(next) : next)
  },

  /**
   * Applies Captain-only fields (period traffic and exact group labels) that
   * the Komari-shaped Client does not carry.
   */
  patchNodes(patch: (node: NodeData) => Partial<NodeData> | null): void {
    const list = raw.getState().nodes
    const next = list.map((node) => {
      const fields = patch(node)
      if (!fields)
        return node
      const merged = { ...node, ...fields }
      return shallowEqual(node, merged) ? node : merged
    })
    setNodes(next)
  },

  updateWsState(state: WsConnectionState, attempts?: number): void {
    raw.setState(attempts === undefined
      ? { wsConnectionState: state }
      : { wsConnectionState: state, wsReconnectAttempts: attempts })
  },

  /** 清空所有节点数据 */
  clearNodes(): void {
    raw.setState({ nodes: [] })
  },
}

const readNodes = () => nodesStore.getState()

/** Selects one value (or a shallow-equal object of values) from the nodes store. */
export function useNodes<S>(select: (state: NodesState) => S): S {
  return useSelector(subscribe, readNodes, select, shallowEqual)
}
