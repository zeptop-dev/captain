import type { ForwardAddressMode, Ingress, Inbound, Node } from './api'
export function ingressPort(g: Ingress, local: number): number {
  if (g.port_mappings?.length) {
    const m = g.port_mappings.find(m => local >= m.local_from && local <= m.local_to)
    return m ? m.public_from + local - m.local_from : 0
  }
  if (g.port_from && (local < g.port_from || local > g.port_to)) return 0
  const p = local + g.port_offset
  return p > 0 && p <= 65535 ? p : 0
}
export function firstFreeIngressPort(g: Ingress, used: number[] = []): number {
  const ranges = g.port_mappings?.length ? g.port_mappings.map(m => [m.local_from, m.local_to]) : [[g.port_from || 1024, g.port_to || 65535]]
  for (const [from, to] of ranges) for (let p = from; p <= to; p++) if (!used.includes(p) && !g.reserved_ports?.includes(p) && ingressPort(g, p)) return p
  return 0
}
export const ingressPortLabel = (g: Ingress): string => g.port_mappings?.length ? g.port_mappings.map(m => `${m.local_from}–${m.local_to} → ${m.public_from}–${m.public_from + m.local_to - m.local_from}`).join(', ') : g.port_from ? `${g.port_from}–${g.port_to}${g.port_offset ? ` (${g.port_offset > 0 ? '+' : ''}${g.port_offset})` : ''}` : ''
export const hostPort = (host: string, port: number) => `${host.includes(':') ? `[${host}]` : host}:${port}`

// Internal selection is explicit, uses the actual listener port, and never
// falls back to a public or provider address when inventory is incomplete.
export function forwardingAddress(node: Pick<Node, 'domain' | 'public_addr'> & Partial<Pick<Node, 'internal_addr' | 'v6_addr'>>, port: number, ingress?: Ingress, mode: ForwardAddressMode = 'default'): string {
  if (mode === 'internal') return node.internal_addr?.trim() && port > 0 ? hostPort(node.internal_addr.trim(), port) : ''
  const host = ingress ? ingress.line_ip || ingress.entry_domain || ingress.entry_host : node.domain || node.public_addr || node.v6_addr || ''
  const targetPort = ingress && !ingress.line_ip ? ingressPort(ingress, port) : port
  return host && targetPort > 0 ? hostPort(host, targetPort) : ''
}

// Additional legacy targets still use the default mapping. Internal candidates
// are represented by managed line members rather than unowned address strings.
export function forwardTargetAddress(pick: { ib: Pick<Inbound, 'Port'>; node: Pick<Node, 'domain' | 'public_addr'> & Partial<Pick<Node, 'internal_addr' | 'v6_addr'>>; ingress?: Ingress }, mode: ForwardAddressMode = 'default') {
  return forwardingAddress(pick.node, pick.ib.Port, pick.ingress, mode)
}
export function forwardTargetProtocol(protocol: string): 'udp' | 'both' {
  return ['hysteria2', 'tuic'].includes(protocol) ? 'udp' : 'both'
}
