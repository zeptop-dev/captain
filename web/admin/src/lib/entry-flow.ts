import type { Entry, Inbound, Ingress, Line, Node } from './api'
import { ingressPort } from './ingress.ts'

export interface InboundChoice { ib: Inbound; node: Node; ingress?: Ingress }
export interface EntryEndpoint { DisplayHost: string; DisplayPort: number }
export interface EntryNodeDetail { node: Node; inbounds: Inbound[]; ingresses?: Ingress[] }

export function entryEndpoint(choice?: InboundChoice): EntryEndpoint {
  if (!choice) return { DisplayHost: '', DisplayPort: 0 }
  const { ib, node, ingress } = choice
  if (ingress) return { DisplayHost: ingress.entry_domain || ingress.entry_host, DisplayPort: ingressPort(ingress, ib.Port) }
  const tls = ib.Settings?.tls as { mode?: number; server_name?: string } | undefined
  return { DisplayHost: (tls?.mode === 1 && tls.server_name) || node.domain || node.public_addr || '', DisplayPort: ib.Port }
}

// Refresh defaults independently. A manually entered relay address or port is
// retained; a value still equal to the old inbound's default follows the pick.
export function changeEntryInbound(current: EntryEndpoint, previous: InboundChoice | undefined, next: InboundChoice): EntryEndpoint {
  const old = entryEndpoint(previous), fresh = entryEndpoint(next)
  return {
    DisplayHost: !current.DisplayHost || current.DisplayHost === old.DisplayHost ? fresh.DisplayHost : current.DisplayHost,
    DisplayPort: !current.DisplayPort || current.DisplayPort === old.DisplayPort ? fresh.DisplayPort : current.DisplayPort,
  }
}

export function canDialThrough(entries: Entry[], candidateID: number, entryID?: number): boolean {
  const seen = new Set<number>(entryID ? [entryID] : [])
  let id: number | null | undefined = candidateID
  // The new entry itself occupies one level.
  for (let depth = 1; id; depth++) {
    if (depth >= 8 || seen.has(id)) return false
    seen.add(id)
    const item = entries.find(e => e.ID === id)
    if (!item) return false
    id = item.ClientDialerEntryID
  }
  return true
}

export function lineSupportsGroups(backend: Line['backend'], groups: Line['groups']): boolean {
  return backend === 'relay' || groups.every((group, index) => index === 0 || group.members.length <= 1)
}

export function freshLineStatus(at: string | undefined, now: number): boolean {
  const age = now - Date.parse(at ?? '')
  return Number.isFinite(age) && age >= 0 && age <= 180_000
}

export function isSubscriberInbound(ib: Inbound): boolean {
  const reverse = ib.Settings?.reverse as { receiver?: boolean } | undefined
  return !reverse?.receiver
}

export const proxyServer = (node: Node) => node.mode !== 'monitor' && node.proxy_allowed !== false
