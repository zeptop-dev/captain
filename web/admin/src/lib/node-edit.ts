import type { Node } from './api'

export type NodeEditSection = 'profile' | 'policy'

export interface NodeEditValues {
  Name: string
  PublicAddr: string
  InternalAddr: string
  V6Addr: string
  Domain: string
  DomainShared: boolean
  MonitorURL: string
  DecoyEnabled: boolean
  DecoyUpstream: string
  UserSpeedLimitMbps: number
  MitaQuotas: boolean
  EgressByIngress: boolean
  DStatusSID: string
}

const sectionFields: Record<NodeEditSection, readonly (keyof NodeEditValues)[]> = {
  profile: ['Name', 'PublicAddr', 'InternalAddr', 'V6Addr', 'Domain', 'DomainShared', 'MonitorURL'],
  policy: ['DecoyEnabled', 'DecoyUpstream', 'UserSpeedLimitMbps', 'MitaQuotas', 'EgressByIngress'],
}

// The existing node PATCH replaces scalar fields. Include every saved scalar,
// including legacy DStatusSID, even when the current editor does not expose it.
export function nodeEditValues(node: Node): NodeEditValues {
  return {
    Name: node.name,
    PublicAddr: node.public_addr,
    InternalAddr: node.internal_addr,
    V6Addr: node.v6_addr,
    Domain: node.domain ?? '',
    DomainShared: node.domain_shared ?? false,
    MonitorURL: node.monitor_url,
    DecoyEnabled: node.decoy_enabled ?? false,
    DecoyUpstream: node.decoy_upstream ?? '',
    UserSpeedLimitMbps: node.user_speed_limit_mbps ?? 0,
    MitaQuotas: node.mita_quotas ?? false,
    EgressByIngress: node.egress_by_ingress ?? false,
    DStatusSID: node.dstatus_sid ?? '',
  }
}

// Read the node again before calling this helper. Preserve fields changed on
// the server since the editor opened unless this section explicitly edits them.
// This read/merge/write preserves other sections; it is not a server-side lock.
export function nodeEditPayload(latest: Node, submitted: NodeEditValues, baseline: NodeEditValues, section: NodeEditSection): NodeEditValues {
  const changed = sectionFields[section]
    .filter(field => submitted[field] !== baseline[field])
    .map(field => [field, submitted[field]])
  return { ...nodeEditValues(latest), ...Object.fromEntries(changed) }
}
