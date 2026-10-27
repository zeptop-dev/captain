import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { ResourceHost, ResourceOptions } from './resources'
import type { GeoLocation, LocationSettings } from './location'
import type { NodeCost } from './node-infrastructure'
import type { NodeAutoRenewal } from './node-renewal'
import type { ProbeStorage, ProbeStorageCandidate } from './probe-storage'
import type { RegionDetection } from '../components/NodeRegionSelect'
import type { NodeBandwidth } from '../components/NodeBandwidthFields'

export interface NodeProbe {
  infrastructure_revision?: string
  infrastructure_linked?: boolean
  infrastructure_supplier_linked?: boolean
  storage_candidates?: ProbeStorageCandidate[]
  region_countries: string[]
  probe: { storage?: ProbeStorage; region_mode: string; region_detection: RegionDetection; resources?: ResourceOptions; node_id: number; hidden: boolean; info: { location_settings?: LocationSettings; location?: GeoLocation; bandwidth?: NodeBandwidth; billing?: NodeCost; auto_renewal?: NodeAutoRenewal; traffic_reset?: { mode: string }; region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }; limit_bytes: number; reset_day: number; mode: string; period_start: string; used_up: number; used_down: number; prev_used: number }
  billed: number
  live?: { at: string; host: ResourceHost }
}

export function useNodeProbe(nodeID: number) {
  return useQuery({ queryKey: ['node-probe', nodeID], queryFn: ({ signal }) => api.get<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`, signal), refetchInterval: 10_000 })
}
