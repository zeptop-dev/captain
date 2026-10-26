// Region clusters for the globe and map; shared by the Vue composable and
// the React hook so both builds group nodes identically.
import type { NodeData } from '@shared/types/node'
import { getCountryCodeFromRegion } from '../utils/geoHelper.ts'
import { getRegionDisplayName } from '../utils/regionHelper.ts'
import { geoKey, locateGeo } from '../../../shared/geolocation.ts'

export interface RegionCluster {
  id: string
  code: string
  coord: [number, number]
  label: string
  precision: 'city' | 'country'
  city?: string
  subdivision?: string
  asn?: string
  org?: string
  servers: number
  onlineServers: number
}

export interface ClusterSummary {
  clusters: RegionCluster[]
  totalServers: number
  onlineServers: number
}

function nodeClusterInfo(node: NodeData): Omit<RegionCluster, 'servers' | 'onlineServers'> | null {
  // Null is an explicit decision from the current API (off, hidden or unknown).
  // Only a missing field from an older API may fall back to the country flag.
  const legacyCountry = node.location === undefined ? getCountryCodeFromRegion(node.region) : null
  const location = locateGeo(node.location === undefined && legacyCountry ? { country: legacyCountry, precision: 'country' } : node.location)
  if (!location) return null
  const label = location.precision === 'city' ? [location.city, location.subdivision, getRegionDisplayName(location.country)].filter(Boolean).join(' · ')
    : getRegionDisplayName(location.country) || location.country
  return { id: geoKey(location), code: location.country, coord: [location.lat, location.lng], label, precision: location.precision, city: location.city, subdivision: location.subdivision }
}

export function summarizeClusters(displayNodes: readonly NodeData[]): ClusterSummary {
  const clustersById = new Map<string, RegionCluster>()
  let onlineServers = 0

  for (const node of displayNodes) {
    if (node.online)
      onlineServers += 1

    const info = nodeClusterInfo(node)
    if (!info)
      continue

    let cluster = clustersById.get(info.id)
    if (!cluster) {
      cluster = { ...info, servers: 0, onlineServers: 0 }
      clustersById.set(info.id, cluster)
    }
    if (!cluster.asn && info.asn)
      cluster.asn = info.asn
    if (!cluster.org && info.org)
      cluster.org = info.org
    cluster.servers += 1

    if (node.online)
      cluster.onlineServers += 1
  }

  return {
    clusters: Array.from(clustersById.values()).sort((a, b) => b.servers - a.servers),
    totalServers: displayNodes.length,
    onlineServers,
  }
}

export function clusterKey(cluster: RegionCluster) {
  return `${cluster.id}:${cluster.coord[0]},${cluster.coord[1]}:${cluster.label}:${cluster.precision}:${cluster.asn ?? ''}:${cluster.org ?? ''}:${cluster.servers}:${cluster.onlineServers}`
}
