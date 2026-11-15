import type { Entry, Inbound } from './api'
import type { ServerProxyDetail } from '../components/LinesConnections'

export function inboundDirectory(details: ServerProxyDetail[], entries: Entry[], serverID?: number, protocol?: string | null, usage?: string | null) {
  const linked = new Map<number, Entry[]>()
  for (const entry of entries) linked.set(entry.InboundID, [...(linked.get(entry.InboundID) ?? []), entry])
  return details.flatMap(owner => owner.inbounds.map(ib => ({ ib, owner, entries: linked.get(ib.ID) ?? [] })))
    .filter(row => (!serverID || row.owner.node.id === serverID) && (!protocol || row.ib.Protocol === protocol) && (!usage || inboundUsage(row.ib) === usage))
}

export function managedInboundDestination(inboundID: number, nodeID: number, entries: Entry[]) {
  const managed = entries.find(entry => entry.InboundID === inboundID && entry.ManagedConnection)?.ManagedConnection
  return managed ? `/entries?tab=lines&section=${managed.type}&server=${managed.exit_node_id}` : `/entries?node=${nodeID}`
}

export function inboundUsage(ib: Inbound): 'plan' | 'standalone' | 'managed' {
  return ib.Settings.reverse || ib.Settings.wireguard_slot ? 'managed' : ib.Settings.standalone ? 'standalone' : 'plan'
}
