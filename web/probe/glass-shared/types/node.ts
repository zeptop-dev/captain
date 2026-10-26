// Node data shape shared by the Vue and React builds; the stores on each
// side re-export it so component code keeps one import path per framework.
export type TrafficLimitType = 'up' | 'down' | 'min' | 'max' | 'sum'

export interface NodeData {
  uuid: string
  // Client 信息
  name: string
  cpu_name: string
  virtualization: string
  arch: string
  cpu_cores: number
  cpu_physical_cores?: number
  os: string
  kernel_version: string
  gpu_name?: string
  ipv4?: string
  ipv6?: string
  region: string
  /** Undefined supports old snapshots; null explicitly hides the location. */
  location?: import('../../../shared/geolocation').GeoLocation | null
  remark?: string
  public_remark: string
  mem_total: number
  swap_total: number
  disk_total: number
  version?: string
  weight: number
  price: number
  billing_cycle: number
  auto_renewal: boolean
  currency: string
  expired_at: string
  group: string
  groups: string[]
  tags: string
  hidden: boolean
  traffic_limit: number
  traffic_limit_type: TrafficLimitType
  created_at: string
  updated_at: string
  // Status 信息
  online: boolean
  time: string
  cpu: number
  gpu: number
  ram: number
  swap: number
  load: number
  load5: number
  load15: number
  temp: number
  disk: number
  net_in: number
  net_out: number
  net_total_up: number
  net_total_down: number
  traffic_up?: number
  traffic_down?: number
  process: number
  connections: number
  connections_udp: number
  uptime: number
  message?: string
  status_updated_at?: string
  /** Captain 当前周期流量（数据库周期值，覆盖开机计数） */
  captainTraffic?: number
}
